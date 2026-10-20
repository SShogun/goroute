package acp

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"time"
)

type AcpSession struct {
	ID           string    `json:"id"`
	AgentID      string    `json:"agent_id"`
	Binary       string    `json:"binary"`
	Alive        bool      `json:"alive"`
	CreatedAt    time.Time `json:"created_at"`
	StdoutBuffer string    `json:"stdout_buffer"`
	StderrBuffer string    `json:"stderr_buffer"`

	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
	stderr io.ReadCloser
	mu     sync.Mutex
}

const maxBufferChars = 1048576 // 1MB capped

type Manager struct {
	mu       sync.RWMutex
	sessions map[string]*AcpSession
}

var GlobalManager = NewManager()

func NewManager() *Manager {
	return &Manager{
		sessions: make(map[string]*AcpSession),
	}
}

func (m *Manager) Spawn(agentID, binary string, args []string, env map[string]string) (*AcpSession, error) {
	sessionID := fmt.Sprintf("acp-%s-%d", agentID, time.Now().UnixNano())
	cmd := exec.Command(binary, args...)

	for k, v := range env {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("stdin pipe error: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe error: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("stderr pipe error: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("spawn failed: %w", err)
	}

	session := &AcpSession{
		ID:        sessionID,
		AgentID:   agentID,
		Binary:    binary,
		Alive:     true,
		CreatedAt: time.Now(),
		cmd:       cmd,
		stdin:     stdin,
		stdout:    stdout,
		stderr:    stderr,
	}

	go session.readStdoutLoop()
	go session.readStderrLoop()
	go session.waitLoop()

	m.mu.Lock()
	m.sessions[sessionID] = session
	m.mu.Unlock()

	return session, nil
}

func (s *AcpSession) readStdoutLoop() {
	scanner := bufio.NewScanner(s.stdout)
	for scanner.Scan() {
		text := scanner.Text() + "\n"
		s.mu.Lock()
		s.StdoutBuffer += text
		if len(s.StdoutBuffer) > maxBufferChars {
			s.StdoutBuffer = s.StdoutBuffer[len(s.StdoutBuffer)-maxBufferChars:]
		}
		s.mu.Unlock()
	}
}

func (s *AcpSession) readStderrLoop() {
	scanner := bufio.NewScanner(s.stderr)
	for scanner.Scan() {
		text := scanner.Text() + "\n"
		s.mu.Lock()
		s.StderrBuffer += text
		if len(s.StderrBuffer) > maxBufferChars {
			s.StderrBuffer = s.StderrBuffer[len(s.StderrBuffer)-maxBufferChars:]
		}
		s.mu.Unlock()
	}
}

func (s *AcpSession) waitLoop() {
	_ = s.cmd.Wait()
	s.mu.Lock()
	s.Alive = false
	s.mu.Unlock()
}

func (s *AcpSession) SendPrompt(ctx context.Context, prompt string) (string, error) {
	s.mu.Lock()
	if !s.Alive {
		s.mu.Unlock()
		return "", fmt.Errorf("session %s is dead", s.ID)
	}
	// Clear stdout buffer for turn
	s.StdoutBuffer = ""
	s.mu.Unlock()

	if _, err := io.WriteString(s.stdin, prompt+"\n"); err != nil {
		return "", fmt.Errorf("write prompt failed: %w", err)
	}

	// Poll until response or timeout
	deadline := time.After(30 * time.Second)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-deadline:
			s.mu.Lock()
			buf := s.StdoutBuffer
			s.mu.Unlock()
			return buf, nil
		case <-ticker.C:
			s.mu.Lock()
			alive := s.Alive
			buf := s.StdoutBuffer
			s.mu.Unlock()

			if !alive {
				return buf, nil
			}
			if len(buf) > 0 && (buf[len(buf)-1] == '\n' || buf[len(buf)-1] == '>') {
				return buf, nil
			}
		}
	}
}

func (s *AcpSession) Kill() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.Alive || s.cmd.Process == nil {
		return nil
	}
	s.Alive = false
	return s.cmd.Process.Kill()
}

func (m *Manager) ListSessions() []map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var list []map[string]interface{}
	for _, s := range m.sessions {
		s.mu.Lock()
		alive := s.Alive
		ageSec := int64(time.Since(s.CreatedAt).Seconds())
		bufLen := len(s.StdoutBuffer)
		s.mu.Unlock()

		list = append(list, map[string]interface{}{
			"id":         s.ID,
			"agent_id":   s.AgentID,
			"binary":     s.Binary,
			"alive":      alive,
			"age_sec":    ageSec,
			"buffer_len": bufLen,
		})
	}
	return list
}

func (m *Manager) KillSession(id string) error {
	m.mu.RLock()
	s, ok := m.sessions[id]
	m.mu.RUnlock()
	if !ok {
		return fmt.Errorf("session %s not found", id)
	}
	err := s.Kill()
	m.mu.Lock()
	delete(m.sessions, id)
	m.mu.Unlock()
	return err
}
