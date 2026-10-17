package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/ziuus/goroute/pkg/config"
)

type AgentConfigurator func(home string) error

var agentConfigurators = map[string]struct {
	Desc string
	Run  AgentConfigurator
}{
	"claude": {
		Desc: "Claude Code CLI (~/.claude/settings.json)",
		Run:  setupClaude,
	},
	"codex": {
		Desc: "OpenAI Codex CLI (~/.codex/config.toml)",
		Run:  setupCodex,
	},
	"opencode": {
		Desc: "OpenCode CLI (~/.config/opencode/opencode.json)",
		Run:  setupOpenCode,
	},
	"cursor": {
		Desc: "Cursor IDE (settings and connection instructions)",
		Run:  setupCursor,
	},
	"cline": {
		Desc: "Cline Agent (~/.cline/data/secrets.json & globalState.json)",
		Run:  setupCline,
	},
	"roo": {
		Desc: "Roo Code (~/.roo/settings-import.json)",
		Run:  setupRoo,
	},
	"kilo": {
		Desc: "Kilo Code (~/.local/share/kilo/auth.json)",
		Run:  setupKilo,
	},
	"aider": {
		Desc: "Aider Assistant (~/.aider.conf.yml)",
		Run:  setupAider,
	},
	"continue": {
		Desc: "Continue.dev & cn CLI (~/.continue/config.yaml)",
		Run:  setupContinue,
	},
	"crush": {
		Desc: "Charm Crush (~/.config/crush/crush.json)",
		Run:  setupCrush,
	},
	"goose": {
		Desc: "Block Goose (~/.config/goose/config.yaml)",
		Run:  setupGoose,
	},
	"qwen": {
		Desc: "Qwen Code (~/.qwen/settings.json)",
		Run:  setupQwen,
	},
	"gemini": {
		Desc: "Google Gemini CLI (environment & command setup)",
		Run:  setupGemini,
	},
	"5dive": {
		Desc: "5dive Agent Fleet (auth profile setup)",
		Run:  setup5dive,
	},
}

func NewSetupCmd() *cobra.Command {
	setupCmd := &cobra.Command{
		Use:   "setup [agent|all]",
		Short: "Configure any AI coding agent to connect to GoRoute (Claude, Codex, OpenCode, Aider, etc.)",
		RunE: func(cmd *cobra.Command, args []string) error {
			home, err := os.UserHomeDir()
			if err != nil {
				return fmt.Errorf("could not determine user home directory: %w", err)
			}

			if len(args) == 0 {
				fmt.Println("🤖 Available Agent Setups in GoRoute:")
				for k, v := range agentConfigurators {
					fmt.Printf("  goroute setup %-10s - %s\n", k, v.Desc)
				}
				fmt.Println("  goroute setup all        - Configure ALL supported agents in one click")
				fmt.Println("\nRun any agent directly through GoRoute:")
				fmt.Println("  goroute run <command> [args...]")
				fmt.Println("  goroute launch <agent> [args...]")
				return nil
			}

			target := strings.ToLower(args[0])

			if target == "all" {
				fmt.Println("🚀 Configuring all supported AI coding agents for GoRoute...")
				successCount := 0
				for k, v := range agentConfigurators {
					fmt.Printf("\n▶ Setting up %s...\n", k)
					if err := v.Run(home); err != nil {
						fmt.Printf("  ⚠️  Failed to setup %s: %v\n", k, err)
					} else {
						successCount++
					}
				}
				fmt.Printf("\n🎉 Setup complete! %d/%d agents successfully configured.\n", successCount, len(agentConfigurators))
				return nil
			}

			cfg, exists := agentConfigurators[target]
			if !exists {
				return fmt.Errorf("unknown agent '%s'. Run 'goroute setup' to view supported agents", target)
			}

			return cfg.Run(home)
		},
	}

	return setupCmd
}

func setupClaude(home string) error {
	claudeDir := filepath.Join(home, ".claude")
	_ = os.MkdirAll(claudeDir, 0700)
	settingsPath := filepath.Join(claudeDir, "settings.json")

	data := map[string]interface{}{
		"customBaseUrl": "http://127.0.0.1:8128",
	}
	bytes, _ := json.MarshalIndent(data, "", "  ")
	if err := os.WriteFile(settingsPath, bytes, 0600); err != nil {
		return err
	}

	fmt.Println("✅ Configured Claude Code!")
	fmt.Printf("   Wrote settings to: %s\n", settingsPath)
	fmt.Println("\nLaunch with:")
	fmt.Println("  goroute run claude")
	fmt.Println("  OR: ANTHROPIC_BASE_URL=\"http://127.0.0.1:8128\" ANTHROPIC_AUTH_TOKEN=\"goroute\" claude")
	return nil
}

func setupCodex(home string) error {
	codexDir := filepath.Join(home, ".codex")
	_ = os.MkdirAll(codexDir, 0700)
	profilePath := filepath.Join(codexDir, "goroute.config.toml")
	configPath := filepath.Join(codexDir, "config.toml")

	tomlContent := `# GoRoute Configuration for OpenAI Codex CLI
model = "claude-3-7-sonnet"
model_provider = "goroute"
model_context_window = 131072
model_auto_compact_token_limit = 112000
tool_output_token_limit = 32768

[model_providers.goroute]
name = "GoRoute"
base_url = "http://127.0.0.1:8128/v1"
env_key = "OPENAI_API_KEY"
wire_api = "responses"
requires_openai_auth = false
`
	if err := os.WriteFile(profilePath, []byte(tomlContent), 0600); err != nil {
		return err
	}

	// Also write or append to config.toml if it doesn't have goroute
	existing, _ := os.ReadFile(configPath)
	if !strings.Contains(string(existing), "[model_providers.goroute]") {
		f, err := os.OpenFile(configPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if err == nil {
			_, _ = f.WriteString("\n" + tomlContent)
			f.Close()
		}
	}

	fmt.Println("✅ Configured OpenAI Codex CLI!")
	fmt.Printf("   Wrote profile to: %s\n", profilePath)
	fmt.Println("\nLaunch with:")
	fmt.Println("  goroute run codex")
	fmt.Println("  OR: codex --profile goroute")
	return nil
}

func getAllGoRouteModels(home string) map[string]string {
	models := map[string]string{
		"agy/gemini-flash-3-8":      "Gemini 3.8 Flash High (Antigravity)",
		"agy/claude-opus-4-6":       "Claude Opus 4.6 Thinking (Antigravity)",
		"agy/claude-sonnet-4-6":     "Claude Sonnet 4.6 (Antigravity)",
		"agy/gemini-pro-agent":      "Gemini Pro Agent (Antigravity)",
		"agy/gemini-3-flash":        "Gemini 3 Flash (Antigravity)",
		"agy/gemini-3.1-flash-lite": "Gemini 3.1 Flash Lite (Antigravity)",
		"agy/gpt-oss-120b-medium":   "GPT-OSS 120B Medium (Antigravity)",
		"claude-3-7-sonnet":         "Claude 3.7 Sonnet (Auto/Failover)",
		"claude-3-5-sonnet":         "Claude 3.5 Sonnet",
		"claude-sonnet-4-6":         "Claude Sonnet 4.6 (Live Native)",
		"claude-opus-4-6-thinking":  "Claude Opus 4.6 (Thinking)",
		"gemini-2.5-pro":            "Gemini 2.5 Pro (Deep Reasoning)",
		"gemini-2.5-flash":          "Gemini 2.5 Flash (Sub-Second)",
		"gemini-3.8-flash-high":     "Gemini 3.8 Flash High",
		"gemini-3-flash":            "Gemini 3 Flash",
		"gemini-3.1-flash-lite":     "Gemini 3.1 Flash Lite",
		"gpt-oss-120b-medium":       "GPT-OSS 120B (Open Weights)",
	}

	cfgPath := filepath.Join(home, ".goroute", "config.yaml")
	if cfg, err := config.Load(cfgPath); err == nil {
		for _, combo := range cfg.Combos {
			if _, exists := models[combo.Name]; !exists {
				models[combo.Name] = fmt.Sprintf("%s (GoRoute Combo)", combo.Name)
			}
		}
		for _, p := range cfg.Providers {
			if p.Type == "openai" {
				models[p.ID+"/default"] = fmt.Sprintf("%s (Upstream Provider)", p.ID)
			}
		}
	}
	return models
}

func setupOpenCode(home string) error {
	// 1. Register gr and goroute credentials in ~/.local/share/opencode/auth.json
	authDir := filepath.Join(home, ".local", "share", "opencode")
	_ = os.MkdirAll(authDir, 0700)
	authPath := filepath.Join(authDir, "auth.json")
	authExisting := make(map[string]interface{})
	if raw, err := os.ReadFile(authPath); err == nil {
		_ = json.Unmarshal(raw, &authExisting)
	}
	authExisting["gr"] = map[string]interface{}{
		"type": "api",
		"key":  "goroute",
	}
	authExisting["goroute"] = map[string]interface{}{
		"type": "api",
		"key":  "goroute",
	}
	if aBytes, err := json.MarshalIndent(authExisting, "", "  "); err == nil {
		_ = os.WriteFile(authPath, aBytes, 0600)
	}

	// 2. Discover all models from GoRoute
	allModels := getAllGoRouteModels(home)
	modelsMap := make(map[string]interface{})
	for modelID, modelName := range allModels {
		modelsMap[modelID] = map[string]interface{}{
			"name": modelName,
		}
	}

	// 3. Configure ~/.config/opencode/opencode.json
	opencodeDir := filepath.Join(home, ".config", "opencode")
	_ = os.MkdirAll(opencodeDir, 0700)
	configPath := filepath.Join(opencodeDir, "opencode.json")

	existing := make(map[string]interface{})
	if raw, err := os.ReadFile(configPath); err == nil {
		_ = json.Unmarshal(raw, &existing)
	}

	providers, ok := existing["provider"].(map[string]interface{})
	if !ok || providers == nil {
		providers = make(map[string]interface{})
	}

	grProvider := map[string]interface{}{
		"name": "GoRoute",
		"npm":  "@ai-sdk/openai-compatible",
		"options": map[string]interface{}{
			"baseURL": "http://127.0.0.1:8128/v1",
			"apiKey":  "goroute",
		},
		"models": modelsMap,
	}

	providers["gr"] = grProvider
	providers["goroute"] = grProvider

	existing["provider"] = providers
	existing["model"] = "gr/claude-3-7-sonnet"
	existing["small_model"] = "gr/gemini-2.5-flash"
	existing["agent"] = map[string]interface{}{
		"build": map[string]interface{}{
			"model": "gr/claude-3-7-sonnet",
		},
		"plan": map[string]interface{}{
			"model": "gr/claude-3-7-sonnet",
		},
	}

	bytes, _ := json.MarshalIndent(existing, "", "  ")
	if err := os.WriteFile(configPath, bytes, 0600); err != nil {
		return err
	}

	// 4. Also sync to opencode.jsonc if present
	jsoncPath := filepath.Join(opencodeDir, "opencode.jsonc")
	if raw, err := os.ReadFile(jsoncPath); err == nil {
		var existingJsonc map[string]interface{}
		if err := json.Unmarshal(raw, &existingJsonc); err == nil {
			p, ok := existingJsonc["provider"].(map[string]interface{})
			if !ok || p == nil {
				p = make(map[string]interface{})
			}
			p["gr"] = grProvider
			p["goroute"] = grProvider
			existingJsonc["provider"] = p
			existingJsonc["model"] = "gr/claude-3-7-sonnet"
			existingJsonc["small_model"] = "gr/gemini-2.5-flash"
			if jBytes, err := json.MarshalIndent(existingJsonc, "", "  "); err == nil {
				_ = os.WriteFile(jsoncPath, jBytes, 0600)
			}
		}
	}

	fmt.Println("✅ Configured OpenCode!")
	fmt.Printf("   Configured %d GoRoute models across providers:\n", len(modelsMap))
	for m := range allModels {
		fmt.Printf("   • gr/%s\n", m)
	}
	fmt.Printf("   Wrote configuration to: %s\n", configPath)
	fmt.Printf("   Authenticated provider key in: %s\n", authPath)
	fmt.Println("   Active model set to: gr/claude-3-7-sonnet")
	fmt.Println("   Uniform format: gr/agy/gemini-flash-3-8, gr/agy/claude-opus-4-6, etc.")
	fmt.Println("   Launch with: opencode (or goroute run opencode)")
	return nil
}

func setupCursor(home string) error {
	cursorDir := filepath.Join(home, ".config", "Cursor", "User")
	settingsPath := filepath.Join(cursorDir, "settings.json")

	if _, err := os.Stat(cursorDir); err == nil {
		existing := make(map[string]interface{})
		if raw, err := os.ReadFile(settingsPath); err == nil {
			_ = json.Unmarshal(raw, &existing)
		}
		existing["cursor.openaiBaseUrl"] = "http://127.0.0.1:8128/v1"
		existing["cursor.openaiApiKey"] = "goroute"
		if bytes, err := json.MarshalIndent(existing, "", "  "); err == nil {
			_ = os.WriteFile(settingsPath, bytes, 0600)
			fmt.Printf("✅ Updated Cursor settings at: %s\n", settingsPath)
		}
	}

	fmt.Println("✅ Cursor IDE Configuration Details:")
	fmt.Println("  1. Open Cursor Settings (Ctrl+,) → Models")
	fmt.Println("  2. Turn on 'Override OpenAI Base URL' and enter:")
	fmt.Println("     http://127.0.0.1:8128/v1")
	fmt.Println("  3. Set 'OpenAI API Key' to:")
	fmt.Println("     goroute")
	fmt.Println("  4. Add model: claude-3-7-sonnet or gemini-2.5-pro")
	return nil
}

func setupCline(home string) error {
	clineDataDir := filepath.Join(home, ".cline", "data")
	_ = os.MkdirAll(clineDataDir, 0700)

	secretsPath := filepath.Join(clineDataDir, "secrets.json")
	statePath := filepath.Join(clineDataDir, "globalState.json")

	secrets := map[string]string{"openAiApiKey": "goroute"}
	sBytes, _ := json.MarshalIndent(secrets, "", "  ")
	_ = os.WriteFile(secretsPath, sBytes, 0600)

	state := map[string]interface{}{
		"actModeApiProvider":     "openai",
		"planModeApiProvider":    "openai",
		"openAiBaseUrl":          "http://127.0.0.1:8128",
		"openAiModelId":          "claude-3-7-sonnet",
		"planModeOpenAiModelId":  "claude-3-7-sonnet",
	}
	stBytes, _ := json.MarshalIndent(state, "", "  ")
	_ = os.WriteFile(statePath, stBytes, 0600)

	fmt.Println("✅ Configured Cline CLI & Standalone Mode!")
	fmt.Printf("   Wrote secrets to: %s\n", secretsPath)
	fmt.Println("\nFor VS Code Extension UI:")
	fmt.Println("  - Provider: OpenAI Compatible")
	fmt.Println("  - Base URL: http://127.0.0.1:8128")
	fmt.Println("  - API Key:  goroute")
	return nil
}

func setupRoo(home string) error {
	rooDir := filepath.Join(home, ".roo")
	_ = os.MkdirAll(rooDir, 0700)
	importPath := filepath.Join(rooDir, "settings-import.json")

	importDoc := map[string]interface{}{
		"providerProfiles": map[string]interface{}{
			"currentApiConfigName": "GoRoute",
			"apiConfigs": map[string]interface{}{
				"GoRoute": map[string]interface{}{
					"apiProvider":   "openai",
					"openAiBaseUrl": "http://127.0.0.1:8128/v1",
					"openAiApiKey":  "goroute",
					"openAiModelId": "claude-3-7-sonnet",
				},
			},
		},
	}
	bytes, _ := json.MarshalIndent(importDoc, "", "  ")
	if err := os.WriteFile(importPath, bytes, 0600); err != nil {
		return err
	}

	fmt.Println("✅ Generated Roo Code Import Document!")
	fmt.Printf("   Wrote file to: %s\n", importPath)
	fmt.Println("   In Roo Code: Click Settings → Import Settings → Select this file.")
	return nil
}

func setupKilo(home string) error {
	kiloDir := filepath.Join(home, ".local", "share", "kilo")
	_ = os.MkdirAll(kiloDir, 0700)
	authPath := filepath.Join(kiloDir, "auth.json")

	auth := map[string]interface{}{
		"openai-compatible": map[string]string{
			"apiKey":  "goroute",
			"baseUrl": "http://127.0.0.1:8128/v1",
			"model":   "claude-3-7-sonnet",
		},
	}
	bytes, _ := json.MarshalIndent(auth, "", "  ")
	if err := os.WriteFile(authPath, bytes, 0600); err != nil {
		return err
	}

	fmt.Println("✅ Configured Kilo Code!")
	fmt.Printf("   Wrote configuration to: %s\n", authPath)
	return nil
}

func setupAider(home string) error {
	aiderConf := filepath.Join(home, ".aider.conf.yml")
	content := `openai-api-base: http://127.0.0.1:8128/v1
openai-api-key: goroute
model: openai/claude-3-7-sonnet
`
	if err := os.WriteFile(aiderConf, []byte(content), 0600); err != nil {
		return err
	}

	fmt.Println("✅ Configured Aider!")
	fmt.Printf("   Wrote configuration to: %s\n", aiderConf)
	fmt.Println("   Launch with: goroute run aider")
	return nil
}

func setupContinue(home string) error {
	contDir := filepath.Join(home, ".continue")
	_ = os.MkdirAll(contDir, 0700)
	confPath := filepath.Join(contDir, "config.yaml")

	content := `name: GoRoute Config
version: "1.0"
schema: v1
models:
  - name: "GoRoute: claude-3-7-sonnet"
    provider: openai
    model: claude-3-7-sonnet
    apiBase: http://127.0.0.1:8128/v1
    apiKey: goroute
    roles: [chat, edit, apply]
  - name: "GoRoute: gemini-2.5-pro"
    provider: openai
    model: gemini-2.5-pro
    apiBase: http://127.0.0.1:8128/v1
    apiKey: goroute
    roles: [chat, edit, apply]
`
	if err := os.WriteFile(confPath, []byte(content), 0600); err != nil {
		return err
	}

	fmt.Println("✅ Configured Continue (continue.dev & cn CLI)!")
	fmt.Printf("   Wrote configuration to: %s\n", confPath)
	fmt.Println("   Run with: cn -p \"hello\"")
	return nil
}

func setupCrush(home string) error {
	crushDir := filepath.Join(home, ".config", "crush")
	_ = os.MkdirAll(crushDir, 0700)
	confPath := filepath.Join(crushDir, "crush.json")

	content := `{
  "providers": {
    "goroute": {
      "type": "openai-compat",
      "base_url": "http://127.0.0.1:8128/v1",
      "api_key": "goroute",
      "models": [
        {"id": "claude-3-7-sonnet", "name": "GoRoute Claude 3.7 Sonnet"},
        {"id": "gemini-2.5-pro", "name": "GoRoute Gemini 2.5 Pro"}
      ]
    }
  }
}
`
	if err := os.WriteFile(confPath, []byte(content), 0600); err != nil {
		return err
	}

	fmt.Println("✅ Configured Charm Crush!")
	fmt.Printf("   Wrote configuration to: %s\n", confPath)
	fmt.Println("   Launch with: goroute run crush")
	return nil
}

func setupGoose(home string) error {
	gooseDir := filepath.Join(home, ".config", "goose")
	_ = os.MkdirAll(gooseDir, 0700)
	confPath := filepath.Join(gooseDir, "config.yaml")

	content := `GOOSE_PROVIDER: openai
OPENAI_HOST: http://127.0.0.1:8128
GOOSE_MODEL: claude-3-7-sonnet
`
	if err := os.WriteFile(confPath, []byte(content), 0600); err != nil {
		return err
	}

	fmt.Println("✅ Configured Goose!")
	fmt.Printf("   Wrote configuration to: %s\n", confPath)
	fmt.Println("   Launch with: goroute run goose")
	return nil
}

func setupQwen(home string) error {
	qwenDir := filepath.Join(home, ".qwen")
	_ = os.MkdirAll(qwenDir, 0700)
	confPath := filepath.Join(qwenDir, "settings.json")

	data := map[string]string{
		"baseUrl": "http://127.0.0.1:8128/v1",
		"apiKey":  "goroute",
		"model":   "claude-3-7-sonnet",
	}
	bytes, _ := json.MarshalIndent(data, "", "  ")
	if err := os.WriteFile(confPath, bytes, 0600); err != nil {
		return err
	}

	fmt.Println("✅ Configured Qwen Code!")
	fmt.Printf("   Wrote configuration to: %s\n", confPath)
	fmt.Println("   Launch with: goroute run qwen")
	return nil
}

func setupGemini(home string) error {
	fmt.Println("✅ Google Gemini CLI Configuration:")
	fmt.Println("   Set the following environment variables in your shell:")
	fmt.Println("     export GOOGLE_GEMINI_BASE_URL=\"http://127.0.0.1:8128\"")
	fmt.Println("     export GEMINI_API_KEY=\"goroute\"")
	fmt.Println("\n   Or run directly with:")
	fmt.Println("     goroute run gemini")
	return nil
}

func setup5dive(home string) error {
	fmt.Println("✅ 5dive Agent Fleet Configuration:")
	fmt.Println("   To connect your 5dive agent fleet to GoRoute, run:")
	fmt.Println("     5dive agent auth set claude \\")
	fmt.Println("       --provider=openai \\")
	fmt.Println("       --base-url=http://127.0.0.1:8128 \\")
	fmt.Println("       --auth-profile=goroute \\")
	fmt.Println("       --model=claude-3-7-sonnet")
	return nil
}
