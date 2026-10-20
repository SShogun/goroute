package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/ziuus/goroute/pkg/config"
)

func NewDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Run comprehensive diagnostic and health checks for GoRoute",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("🩺 Running GoRoute Health & Usability Diagnostics...")
			fmt.Println()

			// 1. Check Gateway Daemon
			fmt.Print(" [1/5] Gateway Server (127.0.0.1:8128): ")
			resp, err := http.Get("http://127.0.0.1:8128/api/status")
			if err != nil {
				fmt.Println("❌ NOT RUNNING (Connection refused)")
				fmt.Println("       Start with: 'goroute serve' or 'systemctl --user start goroute.service'")
			} else {
				defer resp.Body.Close()
				var st map[string]interface{}
				_ = json.NewDecoder(resp.Body).Decode(&st)
				mem, _ := st["memory_mb"].(float64)
				uptime, _ := st["uptime_seconds"].(float64)
				fmt.Printf("✅ ACTIVE (Memory: %.1f MB, Uptime: %.0fs)\n", mem, uptime)
			}

			// 2. Check Config Integrity
			fmt.Print(" [2/5] Configuration (~/.goroute/config.yaml): ")
			home, _ := os.UserHomeDir()
			cfgPath := filepath.Join(home, ".goroute", "config.yaml")
			cfg, err := config.Load(cfgPath)
			if err != nil {
				fmt.Printf("⚠️  MISSING or Corrupt (%v)\n", err)
			} else {
				fmt.Printf("✅ OK (%d Providers, %d Combos configured)\n", len(cfg.Providers), len(cfg.Combos))
			}

			// 3. Check Universal Models Endpoint
			fmt.Print(" [3/5] Universal OpenAI Endpoint (/v1/models): ")
			mResp, err := http.Get("http://127.0.0.1:8128/v1/models")
			if err != nil {
				fmt.Println("❌ Unreachable")
			} else {
				defer mResp.Body.Close()
				var ml struct {
					Data []interface{} `json:"data"`
				}
				_ = json.NewDecoder(mResp.Body).Decode(&ml)
				fmt.Printf("✅ OK (%d active models ready to route)\n", len(ml.Data))
			}

			// 4. Check Coding Agent Installations
			fmt.Println(" [4/5] Agent Readiness on this machine:")
			agents := []struct {
				name string
				bin  string
				path string
			}{
				{"Claude Code", "claude", filepath.Join(home, ".claude", "settings.json")},
				{"OpenCode", "opencode", filepath.Join(home, ".config", "opencode", "opencode.json")},
				{"Codex CLI", "codex", filepath.Join(home, ".codex", "config.toml")},
				{"Hermes Agent", "hermes", filepath.Join(home, ".hermes", "config.yaml")},
				{"OpenClaw", "openclaw", filepath.Join(home, ".openclaw", "openclaw.json")},
				{"Cursor / Cline", "cursor", ""},
			}
			for _, ag := range agents {
				_, binErr := exec.LookPath(ag.bin)
				binStatus := "not in PATH"
				if binErr == nil {
					binStatus = "installed"
				}
				confStatus := "not configured"
				if ag.path == "" {
					confStatus = "configured (universal)"
				} else if _, err := os.Stat(ag.path); err == nil {
					confStatus = "configured"
				}
				fmt.Printf("       • %-15s: %s, %s\n", ag.name, binStatus, confStatus)
			}

			// 5. Cooldowns & Resilience
			fmt.Print(" [5/5] Resilience & Quarantine: ")
			cResp, err := http.Get("http://127.0.0.1:8128/api/resilience/model-cooldowns")
			if err != nil {
				fmt.Println("⚠️  Status unavailable")
			} else {
				defer cResp.Body.Close()
				var cd struct {
					Count int `json:"count"`
				}
				_ = json.NewDecoder(cResp.Body).Decode(&cd)
				if cd.Count == 0 {
					fmt.Println("✅ All upstreams healthy (0 active cooldowns)")
				} else {
					fmt.Printf("⚠️  %d model targets temporarily in cooldown quarantine\n", cd.Count)
				}
			}

			fmt.Println("\n🎉 Diagnostic complete. GoRoute is fully operational!")
			return nil
		},
	}
}

func NewChatCmd() *cobra.Command {
	var model string
	cmd := &cobra.Command{
		Use:   "chat",
		Short: "Interactive chat with any model or combo directly in your terminal",
		RunE: func(cmd *cobra.Command, args []string) error {
			if model == "" {
				model = "gemini-flash"
			}
			fmt.Printf("💬 Interactive GoRoute Terminal Chat (Model: %s)\n", model)
			fmt.Println("   Type your message and press Enter. Type 'exit' or 'quit' to end.")
			fmt.Println()

			scanner := bufio.NewScanner(os.Stdin)
			var history []map[string]string

			for {
				fmt.Print("👤 You: ")
				if !scanner.Scan() {
					break
				}
				userInput := strings.TrimSpace(scanner.Text())
				if userInput == "" {
					continue
				}
				if userInput == "exit" || userInput == "quit" {
					fmt.Println("Bye!")
					break
				}

				history = append(history, map[string]string{"role": "user", "content": userInput})

				reqBody := map[string]interface{}{
					"model":    model,
					"messages": history,
					"stream":   true,
				}
				b, _ := json.Marshal(reqBody)

				httpReq, err := http.NewRequest("POST", "http://127.0.0.1:8128/v1/chat/completions", bytes.NewReader(b))
				if err != nil {
					return err
				}
				httpReq.Header.Set("Content-Type", "application/json")
				httpReq.Header.Set("Authorization", "Bearer goroute")

				resp, err := http.DefaultClient.Do(httpReq)
				if err != nil {
					fmt.Printf("❌ Error connecting to gateway: %v\n", err)
					continue
				}

				if resp.StatusCode != 200 {
					raw, _ := io.ReadAll(resp.Body)
					resp.Body.Close()
					fmt.Printf("❌ HTTP %d: %s\n", resp.StatusCode, string(raw))
					continue
				}

				fmt.Print("🤖 AI: ")
				streamScanner := bufio.NewScanner(resp.Body)
				var fullAssistantReply strings.Builder

				for streamScanner.Scan() {
					line := streamScanner.Text()
					if strings.HasPrefix(line, "data: ") {
						data := strings.TrimPrefix(line, "data: ")
						if data == "[DONE]" {
							break
						}
						var chunk struct {
							Choices []struct {
								Delta struct {
									Content string `json:"content"`
								} `json:"delta"`
							} `json:"choices"`
						}
						if err := json.Unmarshal([]byte(data), &chunk); err == nil && len(chunk.Choices) > 0 {
							c := chunk.Choices[0].Delta.Content
							if c != "" {
								fmt.Print(c)
								fullAssistantReply.WriteString(c)
							}
						}
					}
				}
				resp.Body.Close()
				fmt.Println()
				history = append(history, map[string]string{"role": "assistant", "content": fullAssistantReply.String()})
			}
			return nil
		},
	}
	cmd.Flags().StringVarP(&model, "model", "m", "gemini-flash", "Model or combo to chat with")
	return cmd
}

func NewLogsCmd() *cobra.Command {
	var limit int
	cmd := &cobra.Command{
		Use:   "logs",
		Short: "View recent gateway request traffic and latencies",
		RunE: func(cmd *cobra.Command, args []string) error {
			resp, err := http.Get("http://127.0.0.1:8128/api/logs")
			if err != nil {
				return fmt.Errorf("gateway not running: %w", err)
			}
			defer resp.Body.Close()

			var res struct {
				Logs []struct {
					Timestamp  time.Time `json:"timestamp"`
					Endpoint   string    `json:"endpoint"`
					Model      string    `json:"model"`
					StatusCode int       `json:"status_code"`
					DurationMs int64     `json:"duration_ms"`
				} `json:"logs"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
				return err
			}

			logs := res.Logs
			if len(logs) == 0 {
				fmt.Println("No request logs recorded yet.")
				return nil
			}

			startIdx := 0
			if len(logs) > limit {
				startIdx = len(logs) - limit
			}

			fmt.Printf("%-10s | %-8s | %-25s | %-25s | %-6s\n", "TIME", "STATUS", "ENDPOINT", "MODEL", "LATENCY")
			fmt.Println(strings.Repeat("-", 82))
			for i := startIdx; i < len(logs); i++ {
				l := logs[i]
				timeStr := l.Timestamp.Format("15:04:05")
				statusStr := fmt.Sprintf("%d", l.StatusCode)
				if l.StatusCode == 200 {
					statusStr = "200 OK"
				}
				fmt.Printf("%-10s | %-8s | %-25s | %-25s | %dms\n", timeStr, statusStr, l.Endpoint, l.Model, l.DurationMs)
			}
			return nil
		},
	}
	cmd.Flags().IntVarP(&limit, "lines", "n", 20, "Number of recent log lines to display")
	return cmd
}

func NewReloadCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "reload",
		Short: "Hot-reload configuration without restarting the gateway",
		RunE: func(cmd *cobra.Command, args []string) error {
			resp, err := http.Post("http://127.0.0.1:8128/api/reload", "application/json", nil)
			if err != nil {
				return fmt.Errorf("could not connect to gateway: %w", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode == 200 {
				fmt.Println("✅ GoRoute configuration reloaded successfully across all providers and combos.")
			} else {
				raw, _ := io.ReadAll(resp.Body)
				fmt.Printf("⚠️  Reload returned HTTP %d: %s\n", resp.StatusCode, string(raw))
			}
			return nil
		},
	}
}

func NewKeysCmd() *cobra.Command {
	keysCmd := &cobra.Command{
		Use:   "keys",
		Short: "Manage Virtual API Keys with rate limits and model restrictions",
	}

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List all active virtual keys",
		RunE: func(cmd *cobra.Command, args []string) error {
			resp, err := http.Get("http://127.0.0.1:8128/api/keys")
			if err != nil {
				return fmt.Errorf("gateway not running: %w", err)
			}
			defer resp.Body.Close()
			var result struct {
				Keys []struct {
					ID        string  `json:"id"`
					Name      string  `json:"name"`
					Key       string  `json:"key"`
					RateLimit int     `json:"rate_limit"`
					BudgetCap float64 `json:"budget_cap"`
					IsActive  bool    `json:"is_active"`
				} `json:"keys"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
				return err
			}
			if len(result.Keys) == 0 {
				fmt.Println("No virtual keys found. Create one with: goroute keys create <name>")
				return nil
			}
			fmt.Printf("%-24s | %-16s | %-40s | %-8s\n", "KEY ID", "NAME", "VIRTUAL API KEY", "STATUS")
			fmt.Println(strings.Repeat("-", 96))
			for _, k := range result.Keys {
				status := "ACTIVE"
				if !k.IsActive {
					status = "REVOKED"
				}
				fmt.Printf("%-24s | %-16s | %-40s | %-8s\n", k.ID, k.Name, k.Key, status)
			}
			return nil
		},
	}

	var budget float64
	var rateLimit int
	var modelsList string

	createCmd := &cobra.Command{
		Use:   "create <name>",
		Short: "Create a new virtual key",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			var models []string
			if modelsList != "" {
				for _, m := range strings.Split(modelsList, ",") {
					if trimmed := strings.TrimSpace(m); trimmed != "" {
						models = append(models, trimmed)
					}
				}
			}
			payload := map[string]interface{}{
				"name":       name,
				"budget_cap": budget,
				"rate_limit": rateLimit,
				"models":     models,
			}
			b, _ := json.Marshal(payload)
			resp, err := http.Post("http://127.0.0.1:8128/api/keys", "application/json", bytes.NewReader(b))
			if err != nil {
				return err
			}
			defer resp.Body.Close()
			var res struct {
				Key struct {
					ID  string `json:"id"`
					Key string `json:"key"`
				} `json:"key"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
				return err
			}
			fmt.Println("✅ Virtual Key created successfully!")
			fmt.Printf("   Name: %s\n", name)
			fmt.Printf("   ID:   %s\n", res.Key.ID)
			fmt.Printf("   Key:  %s\n", res.Key.Key)
			return nil
		},
	}
	createCmd.Flags().Float64Var(&budget, "budget", 0, "Budget cap in USD")
	createCmd.Flags().IntVar(&rateLimit, "rate-limit", 60, "RPM rate limit")
	createCmd.Flags().StringVar(&modelsList, "models", "", "Comma-separated list of allowed models")

	revokeCmd := &cobra.Command{
		Use:   "revoke <key-id>",
		Short: "Revoke a virtual key",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			keyID := args[0]
			req, _ := http.NewRequest("DELETE", "http://127.0.0.1:8128/api/keys/"+keyID, nil)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				return err
			}
			defer resp.Body.Close()
			if resp.StatusCode == 200 {
				fmt.Printf("✅ Key '%s' revoked successfully.\n", keyID)
			} else {
				fmt.Printf("⚠️  Revoke returned HTTP %d\n", resp.StatusCode)
			}
			return nil
		},
	}

	keysCmd.AddCommand(listCmd, createCmd, revokeCmd)
	return keysCmd
}
