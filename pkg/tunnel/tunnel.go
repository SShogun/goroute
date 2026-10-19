package tunnel

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

type TunnelStatus struct {
	Name       string `json:"name"`
	Supported  bool   `json:"supported"`
	Installed  bool   `json:"installed"`
	BinaryPath string `json:"binary_path,omitempty"`
	Running    bool   `json:"running"`
	Pid        int    `json:"pid,omitempty"`
	PublicURL  string `json:"public_url,omitempty"`
	ApiURL     string `json:"api_url,omitempty"`
	TargetURL  string `json:"target_url,omitempty"`
	Phase      string `json:"phase"` // "stopped", "starting", "running", "error"
	LastError  string `json:"last_error,omitempty"`
}

type Manager struct {
	mu         sync.Mutex
	targetPort int
	cfCmd      *exec.Cmd
	cfStatus   TunnelStatus
	tsStatus   TunnelStatus
}

var tryCfRegex = regexp.MustCompile(`https://[a-zA-Z0-9-]+\.trycloudflare\.com`)

func NewManager(targetPort int) *Manager {
	m := &Manager{
		targetPort: targetPort,
		cfStatus: TunnelStatus{
			Name:      "cloudflared",
			Supported: true,
			Phase:     "stopped",
		},
		tsStatus: TunnelStatus{
			Name:      "tailscale",
			Supported: true,
			Phase:     "stopped",
		},
	}
	m.probeBinaries()
	return m
}

func (m *Manager) findCloudflared() string {
	home, _ := os.UserHomeDir()
	candidates := []string{
		filepath.Join(home, ".goroute", "bin", "cloudflared"),
		"/usr/local/bin/cloudflared",
		"/usr/bin/cloudflared",
	}
	for _, c := range candidates {
		if fi, err := os.Stat(c); err == nil && !fi.IsDir() && fi.Mode()&0111 != 0 {
			// verify execution
			cmd := exec.Command(c, "--version")
			if err := cmd.Run(); err == nil {
				return c
			}
		}
	}
	// Try PATH
	if p, err := exec.LookPath("cloudflared"); err == nil {
		cmd := exec.Command(p, "--version")
		if err := cmd.Run(); err == nil {
			return p
		}
	}
	return ""
}

func (m *Manager) probeBinaries() {
	m.mu.Lock()
	defer m.mu.Unlock()

	cfBin := m.findCloudflared()
	if cfBin != "" {
		m.cfStatus.Installed = true
		m.cfStatus.BinaryPath = cfBin
	} else {
		m.cfStatus.Installed = false
		m.cfStatus.BinaryPath = ""
	}

	if p, err := exec.LookPath("tailscale"); err == nil {
		m.tsStatus.Installed = true
		m.tsStatus.BinaryPath = p
	} else {
		m.tsStatus.Installed = false
		m.tsStatus.BinaryPath = ""
	}
}

func (m *Manager) GetCloudflaredStatus() TunnelStatus {
	m.probeBinaries()
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cfStatus
}

func (m *Manager) StartCloudflared(ctx context.Context) (TunnelStatus, error) {
	m.probeBinaries()
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.cfStatus.Running && m.cfCmd != nil && m.cfCmd.Process != nil {
		return m.cfStatus, nil
	}

	bin := m.cfStatus.BinaryPath
	if bin == "" {
		m.cfStatus.Phase = "not_installed"
		m.cfStatus.LastError = "cloudflared binary not found or not executable"
		return m.cfStatus, fmt.Errorf("%s", m.cfStatus.LastError)
	}

	target := fmt.Sprintf("http://127.0.0.1:%d", m.targetPort)
	cmd := exec.Command(bin, "tunnel", "--url", target, "--no-autoupdate")
	stderr, err := cmd.StderrPipe()
	if err != nil {
		m.cfStatus.Phase = "error"
		m.cfStatus.LastError = err.Error()
		return m.cfStatus, err
	}

	if err := cmd.Start(); err != nil {
		m.cfStatus.Phase = "error"
		m.cfStatus.LastError = err.Error()
		return m.cfStatus, err
	}

	m.cfCmd = cmd
	m.cfStatus.Running = true
	m.cfStatus.Pid = cmd.Process.Pid
	m.cfStatus.TargetURL = target
	m.cfStatus.Phase = "starting"
	m.cfStatus.LastError = ""

	urlChan := make(chan string, 1)

	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			line := scanner.Text()
			if m := tryCfRegex.FindString(line); m != "" {
				select {
				case urlChan <- m:
				default:
				}
			}
		}
		_ = cmd.Wait()

		m.mu.Lock()
		m.cfStatus.Running = false
		m.cfStatus.Phase = "stopped"
		m.cfStatus.PublicURL = ""
		m.cfCmd = nil
		m.mu.Unlock()
	}()

	select {
	case pubURL := <-urlChan:
		m.cfStatus.PublicURL = pubURL
		m.cfStatus.ApiURL = pubURL + "/v1"
		m.cfStatus.Phase = "running"
		return m.cfStatus, nil
	case <-time.After(20 * time.Second):
		return m.cfStatus, nil
	}
}

func (m *Manager) StopCloudflared() (TunnelStatus, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.cfCmd != nil && m.cfCmd.Process != nil {
		_ = m.cfCmd.Process.Kill()
		m.cfCmd = nil
	}

	m.cfStatus.Running = false
	m.cfStatus.Phase = "stopped"
	m.cfStatus.PublicURL = ""
	m.cfStatus.ApiURL = ""
	m.cfStatus.Pid = 0
	return m.cfStatus, nil
}

func (m *Manager) GetTailscaleStatus() TunnelStatus {
	m.probeBinaries()
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.tsStatus.Installed {
		return m.tsStatus
	}

	// Check if tailscale serve status has active tunnels
	out, err := exec.Command("tailscale", "serve", "status").CombinedOutput()
	if err == nil && len(out) > 0 && !strings.Contains(string(out), "No serve config") {
		m.tsStatus.Running = true
		m.tsStatus.Phase = "running"
	} else {
		m.tsStatus.Running = false
		m.tsStatus.Phase = "stopped"
	}
	return m.tsStatus
}

func (m *Manager) StartTailscale(ctx context.Context) (TunnelStatus, error) {
	m.probeBinaries()
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.tsStatus.Installed {
		return m.tsStatus, fmt.Errorf("tailscale binary not found")
	}

	target := fmt.Sprintf("http://127.0.0.1:%d", m.targetPort)
	out, err := exec.Command("tailscale", "serve", "--bg", fmt.Sprintf("%d", m.targetPort)).CombinedOutput()
	if err != nil {
		m.tsStatus.Phase = "error"
		m.tsStatus.LastError = string(out)
		return m.tsStatus, fmt.Errorf("tailscale serve failed: %s", string(out))
	}

	m.tsStatus.Running = true
	m.tsStatus.Phase = "running"
	m.tsStatus.TargetURL = target
	return m.tsStatus, nil
}

func (m *Manager) StopTailscale() (TunnelStatus, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	_ = exec.Command("tailscale", "serve", "reset").Run()
	m.tsStatus.Running = false
	m.tsStatus.Phase = "stopped"
	return m.tsStatus, nil
}

// HttpHandler exposes standard OmniRoute endpoints:
// GET /api/tunnels/cloudflared
// POST /api/tunnels/cloudflared
// GET /api/tunnels/tailscale
// POST /api/tunnels/tailscale/enable
// POST /api/tunnels/tailscale/disable
func (m *Manager) RegisterRoutes(mux interface {
	Get(pattern string, handler http.HandlerFunc)
	Post(pattern string, handler http.HandlerFunc)
}) {
	mux.Get("/api/tunnels/cloudflared", func(w http.ResponseWriter, r *http.Request) {
		status := m.GetCloudflaredStatus()
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"supported":%t,"installed":%t,"running":%t,"phase":%q,"publicUrl":%q,"apiUrl":%q,"lastError":%q}`,
			status.Supported, status.Installed, status.Running, status.Phase, status.PublicURL, status.ApiURL, status.LastError)
	})

	mux.Post("/api/tunnels/cloudflared", func(w http.ResponseWriter, r *http.Request) {
		// Read action
		var req struct {
			Action string `json:"action"`
		}
		_ = jsonDecode(r, &req)

		var st TunnelStatus
		var err error
		if req.Action == "disable" || req.Action == "stop" {
			st, err = m.StopCloudflared()
		} else {
			st, err = m.StartCloudflared(r.Context())
		}

		w.Header().Set("Content-Type", "application/json")
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprintf(w, `{"success":false,"error":%q}`, err.Error())
			return
		}
		fmt.Fprintf(w, `{"success":true,"action":%q,"status":{"running":%t,"phase":%q,"publicUrl":%q}}`,
			req.Action, st.Running, st.Phase, st.PublicURL)
	})

	mux.Get("/api/tunnels/tailscale", func(w http.ResponseWriter, r *http.Request) {
		status := m.GetTailscaleStatus()
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"supported":%t,"installed":%t,"running":%t,"phase":%q,"apiUrl":%q,"lastError":%q}`,
			status.Supported, status.Installed, status.Running, status.Phase, status.ApiURL, status.LastError)
	})

	mux.Post("/api/tunnels/tailscale/enable", func(w http.ResponseWriter, r *http.Request) {
		st, err := m.StartTailscale(r.Context())
		w.Header().Set("Content-Type", "application/json")
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprintf(w, `{"success":false,"error":%q}`, err.Error())
			return
		}
		fmt.Fprintf(w, `{"success":true,"running":%t,"phase":%q}`, st.Running, st.Phase)
	})

	mux.Post("/api/tunnels/tailscale/disable", func(w http.ResponseWriter, r *http.Request) {
		st, _ := m.StopTailscale()
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"success":true,"running":%t,"phase":%q}`, st.Running, st.Phase)
	})
}

func jsonDecode(r *http.Request, v interface{}) error {
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(v)
}
