package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/spf13/cobra"
)

func NewSetupCmd() *cobra.Command {
	setupCmd := &cobra.Command{
		Use:   "setup [agent]",
		Short: "Configure AI coding agents (Claude, OpenCode, Cursor, Cline, Aider) to use GoRoute",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				fmt.Println("🤖 Available Agent Setups in GoRoute:")
				fmt.Println("  goroute setup claude    - Configure Claude Code CLI")
				fmt.Println("  goroute setup opencode  - Configure OpenCode agent")
				fmt.Println("  goroute setup cursor    - Show Cursor IDE setup instructions")
				fmt.Println("  goroute setup cline     - Show Cline / Roo Code setup instructions")
				fmt.Println("  goroute setup aider     - Configure Aider coding assistant")
				fmt.Println("\nOr launch directly:")
				fmt.Println("  goroute launch claude   - Run Claude Code pointed at GoRoute")
				fmt.Println("  goroute launch aider    - Run Aider pointed at GoRoute")
				return nil
			}

			target := args[0]
			home, _ := os.UserHomeDir()

			switch target {
			case "claude":
				claudeDir := filepath.Join(home, ".claude")
				_ = os.MkdirAll(claudeDir, 0700)
				settingsPath := filepath.Join(claudeDir, "settings.json")
				data := map[string]interface{}{
					"customBaseUrl": "http://127.0.0.1:20128",
				}
				bytes, _ := json.MarshalIndent(data, "", "  ")
				_ = os.WriteFile(settingsPath, bytes, 0600)

				fmt.Println("✅ Configured Claude Code!")
				fmt.Printf("   Wrote settings to: %s\n", settingsPath)
				fmt.Println("\nTo launch Claude Code:")
				fmt.Println("  goroute launch claude")
				fmt.Println("  OR: ANTHROPIC_BASE_URL=\"http://127.0.0.1:20128\" ANTHROPIC_AUTH_TOKEN=\"goroute\" claude")

			case "opencode":
				opencodeDir := filepath.Join(home, ".config", "opencode")
				_ = os.MkdirAll(opencodeDir, 0700)
				configPath := filepath.Join(opencodeDir, "opencode.json")

				providerEntry := map[string]interface{}{
					"goroute": map[string]interface{}{
						"type":    "openai-compatible",
						"baseURL": "http://127.0.0.1:20128/v1",
						"apiKey":  "goroute",
					},
				}
				bytes, _ := json.MarshalIndent(providerEntry, "", "  ")
				_ = os.WriteFile(configPath, bytes, 0600)

				fmt.Println("✅ Configured OpenCode!")
				fmt.Printf("   Wrote configuration to: %s\n", configPath)
				fmt.Println("   Use with: opencode -m goroute/claude-3-7-sonnet")

			case "cursor":
				fmt.Println("Cursor Configuration Steps:")
				fmt.Println("  1. Open Cursor Settings (Ctrl+,) → Models")
				fmt.Println("  2. Turn on 'Override OpenAI Base URL' and set:")
				fmt.Println("     http://127.0.0.1:20128/v1")
				fmt.Println("  3. Set 'OpenAI API Key' to:")
				fmt.Println("     goroute")
				fmt.Println("  4. Add model name: claude-3-7-sonnet or gemini-2.5-pro")

			case "cline", "roo":
				fmt.Println("Cline / Roo Code Configuration:")
				fmt.Println("  1. Open Cline Settings in VS Code")
				fmt.Println("  2. API Provider: OpenAI Compatible")
				fmt.Println("  3. Base URL:     http://127.0.0.1:20128/v1")
				fmt.Println("  4. API Key:      goroute")
				fmt.Println("  5. Model ID:     claude-3-7-sonnet")

			case "aider":
				aiderConf := filepath.Join(home, ".aider.conf.yml")
				confContent := `openai-api-base: http://127.0.0.1:20128/v1
openai-api-key: goroute
model: openai/claude-3-7-sonnet
`
				_ = os.WriteFile(aiderConf, []byte(confContent), 0600)
				fmt.Println("✅ Configured Aider!")
				fmt.Printf("   Wrote configuration to: %s\n", aiderConf)
				fmt.Println("   Launch with: goroute launch aider")

			default:
				return fmt.Errorf("unknown agent '%s'. Supported: claude, opencode, cursor, cline, aider", target)
			}
			return nil
		},
	}

	return setupCmd
}

func NewLaunchCmd() *cobra.Command {
	launchCmd := &cobra.Command{
		Use:   "launch [agent] [args...]",
		Short: "Launch a CLI coding agent with GoRoute proxy environment injected",
		Args:  cobra.MinimumNArgs(1),
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			target := args[0]
			extraArgs := args[1:]

			var c *exec.Cmd

			switch target {
			case "claude":
				c = exec.Command("claude", extraArgs...)
				c.Env = append(os.Environ(),
					"ANTHROPIC_BASE_URL=http://127.0.0.1:20128",
					"ANTHROPIC_AUTH_TOKEN=goroute",
				)
			case "aider":
				c = exec.Command("aider", extraArgs...)
				c.Env = append(os.Environ(),
					"OPENAI_API_BASE=http://127.0.0.1:20128/v1",
					"OPENAI_API_KEY=goroute",
				)
			case "opencode":
				c = exec.Command("opencode", extraArgs...)
				c.Env = append(os.Environ(),
					"OPENAI_BASE_URL=http://127.0.0.1:20128/v1",
					"OPENAI_API_KEY=goroute",
				)
			default:
				c = exec.Command(target, extraArgs...)
				c.Env = append(os.Environ(),
					"OPENAI_BASE_URL=http://127.0.0.1:20128/v1",
					"OPENAI_API_KEY=goroute",
					"ANTHROPIC_BASE_URL=http://127.0.0.1:20128",
					"ANTHROPIC_AUTH_TOKEN=goroute",
				)
			}

			c.Stdin = os.Stdin
			c.Stdout = os.Stdout
			c.Stderr = os.Stderr

			return c.Run()
		},
	}

	return launchCmd
}
