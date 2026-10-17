package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/ziuus/goroute/pkg/config"
	"github.com/ziuus/goroute/pkg/store"
	"github.com/spf13/cobra"
)

type omniExportItem struct {
	ID           string `json:"id"`
	Provider     string `json:"provider"`
	Name         string `json:"name"`
	AuthType     string `json:"authType"`
	APIKey       string `json:"apiKey"`
	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"refreshToken"`
}

func NewImportCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "import [source]",
		Short: "Import existing credentials and accounts from OmniRoute",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			source := args[0]
			if source != "omniroute" {
				return fmt.Errorf("currently supported import source: 'omniroute'")
			}

			tmpFile := filepath.Join(os.TempDir(), fmt.Sprintf("goroute_import_%d.json", time.Now().UnixNano()))
			defer os.Remove(tmpFile)

			cmdExport := exec.Command("omniroute", "auth", "export", "--format", "json", "--force", "--out", tmpFile)
			if err := cmdExport.Run(); err != nil {
				return fmt.Errorf("failed to run 'omniroute auth export': %w (ensure omniroute is installed)", err)
			}

			rawBytes, err := os.ReadFile(tmpFile)
			if err != nil {
				return fmt.Errorf("failed to read export file: %w", err)
			}

			var items []omniExportItem
			if err := json.Unmarshal(rawBytes, &items); err != nil {
				return fmt.Errorf("failed to parse OmniRoute export JSON: %w", err)
			}

			defaultCfg, defaultStore := defaultPaths()
			st, err := store.NewStore(defaultStore, "goroute-default-key")
			if err != nil {
				return err
			}

			cfg, _ := config.Load(defaultCfg)
			if cfg == nil {
				cfg = config.DefaultConfig()
			}

			var agyAccounts []config.AccountConfig
			importedCount := 0

			for _, it := range items {
				switch it.Provider {
				case "agy", "antigravity":
					accID := fmt.Sprintf("antigravity-%s", it.Name)
					if it.RefreshToken != "" {
						_ = st.SetSecret(accID, it.RefreshToken)
					}
					agyAccounts = append(agyAccounts, config.AccountConfig{
						ID:          accID,
						Email:       it.Name,
						AccessToken: it.AccessToken,
						ExpiresAt:   time.Now().Add(1 * time.Hour),
					})
					importedCount++
					fmt.Printf("   ✔ Imported Antigravity Account: %s\n", it.Name)

				case "tokenrouter":
					cfg.Providers = append(cfg.Providers, config.ProviderConfig{
						ID:      "tokenrouter",
						Type:    "openai",
						BaseURL: "https://api.tokenrouter.io/v1",
						APIKey:  it.APIKey,
					})
					importedCount++
					fmt.Printf("   ✔ Imported TokenRouter (%s)\n", it.Name)

				case "agentrouter":
					cfg.Providers = append(cfg.Providers, config.ProviderConfig{
						ID:      "agentrouter",
						Type:    "openai",
						BaseURL: "https://api.agentrouter.org/v1",
						APIKey:  it.APIKey,
					})
					importedCount++
					fmt.Printf("   ✔ Imported AgentRouter (%s)\n", it.Name)

				default:
					if strings.HasPrefix(it.Provider, "openai-compatible") {
						cfg.Providers = append(cfg.Providers, config.ProviderConfig{
							ID:      "apinexbond",
							Type:    "openai",
							BaseURL: "https://api.apinexbond.com/v1",
							APIKey:  it.APIKey,
						})
						importedCount++
						fmt.Printf("   ✔ Imported OpenAI-Compatible/ApinexBond (%s)\n", it.Name)
					}
				}
			}

			if len(agyAccounts) > 0 {
				cfg.Providers = append(cfg.Providers, config.ProviderConfig{
					ID:       "antigravity",
					Type:     "antigravity",
					Accounts: agyAccounts,
				})
			}

			// Configure default smart combos
			cfg.Combos = []config.ComboConfig{
				{
					Name:     "claude-3-7-sonnet",
					Strategy: "failover",
					Targets: []config.ComboTarget{
						{ProviderID: "antigravity", Model: "claude-3-7-sonnet"},
						{ProviderID: "tokenrouter", Model: "claude-3-7-sonnet"},
						{ProviderID: "agentrouter", Model: "claude-3-7-sonnet"},
					},
				},
				{
					Name:     "gemini-2.5-pro",
					Strategy: "failover",
					Targets: []config.ComboTarget{
						{ProviderID: "antigravity", Model: "gemini-2.5-pro"},
						{ProviderID: "agentrouter", Model: "gemini-2.5-pro"},
					},
				},
			}

			if err := cfg.Save(defaultCfg); err != nil {
				return fmt.Errorf("failed to save config: %w", err)
			}

			fmt.Printf("\n🎉 Successfully imported %d connections into GoRoute!\n", importedCount)
			fmt.Printf("   Config saved to: %s\n", defaultCfg)
			fmt.Println("   You can now start GoRoute with: goroute serve")
			return nil
		},
	}

	return cmd
}
