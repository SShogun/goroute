package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"github.com/ziuus/goroute/pkg/config"
	"github.com/ziuus/goroute/pkg/providers"
	"github.com/ziuus/goroute/pkg/providers/antigravity"
	"github.com/ziuus/goroute/pkg/providers/openai"
	"github.com/ziuus/goroute/pkg/proxy"
	"github.com/ziuus/goroute/pkg/quota"
	"github.com/ziuus/goroute/pkg/router"
	"github.com/ziuus/goroute/pkg/store"
	"github.com/spf13/cobra"
)

func init() {
	// Add subcommands to rootCmd in main
}

func defaultPaths() (configPath, storePath string) {
	home, _ := os.UserHomeDir()
	baseDir := filepath.Join(home, ".goroute")
	return filepath.Join(baseDir, "config.yaml"), filepath.Join(baseDir, "vault.json")
}

func openBrowser(url string) {
	switch runtime.GOOS {
	case "linux":
		_ = exec.Command("xdg-open", url).Start()
	case "darwin":
		_ = exec.Command("open", url).Start()
	case "windows":
		_ = exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	}
}

func NewServeCmd() *cobra.Command {
	var port int
	var cfgPath string

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start the GoRoute gateway server",
		RunE: func(cmd *cobra.Command, args []string) error {
			defaultCfg, defaultStore := defaultPaths()
			if cfgPath == "" {
				cfgPath = defaultCfg
			}

			// Load config if exists, otherwise create default
			cfg, err := config.Load(cfgPath)
			if err != nil {
				fmt.Printf("Notice: No config at %s, using defaults.\n", cfgPath)
				cfg = config.DefaultConfig()
				_ = cfg.Save(cfgPath)
			}

			if port != 0 {
				cfg.Server.Port = port
			}

			st, _ := store.NewStore(defaultStore, "goroute-default-key")
			r := router.NewRouter()
			limiter := quota.NewLimiter()

			// Register configured providers
			for _, pCfg := range cfg.Providers {
				switch pCfg.Type {
				case "openai":
					prov := openai.NewProvider(pCfg.ID, pCfg.BaseURL, pCfg.APIKey)
					r.RegisterProvider(prov)
				case "antigravity":
					var accounts []*antigravity.Account
					for _, a := range pCfg.Accounts {
						refToken := a.RefreshToken
						if refToken == "" && st != nil {
							sec, _ := st.GetSecret(a.ID)
							refToken = sec
						}
						accounts = append(accounts, &antigravity.Account{
							ID:           a.ID,
							Email:        a.Email,
							RefreshToken: refToken,
							AccessToken:  a.AccessToken,
							ExpiresAt:    a.ExpiresAt,
							ProjectID:    a.ProjectID,
						})
					}
					prov := antigravity.NewProvider(pCfg.ID, accounts)
					r.RegisterProvider(prov)
				}
			}

			// Register configured combos
			for _, combo := range cfg.Combos {
				var targets []router.Target
				for _, t := range combo.Targets {
					targets = append(targets, router.Target{
						ProviderID: t.ProviderID,
						Model:      t.Model,
					})
				}
				r.RegisterCombo(combo.Name, combo.Strategy, targets)
			}

			srv := proxy.NewServer(r, limiter)
			addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)

			fmt.Printf("⚡ GoRoute starting on http://%s\n", addr)
			fmt.Printf("   Universal OpenAI endpoint: http://%s/v1\n", addr)
			fmt.Printf("   Dashboard available at:    http://%s/\n", addr)
			fmt.Printf("   Memory footprint:          < 15 MB RAM\n")

			return http.ListenAndServe(addr, srv.Handler())
		},
	}

	cmd.Flags().IntVarP(&port, "port", "p", 20128, "Port to listen on")
	cmd.Flags().StringVarP(&cfgPath, "config", "c", "", "Config file path")
	return cmd
}

func NewAuthCmd() *cobra.Command {
	authCmd := &cobra.Command{
		Use:   "auth",
		Short: "Manage authentication and OAuth provider logins",
	}

	loginCmd := &cobra.Command{
		Use:   "login [provider]",
		Short: "Login to an OAuth provider (e.g. antigravity)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			providerName := args[0]
			if providerName != "antigravity" && providerName != "google" {
				return fmt.Errorf("currently supported OAuth providers: antigravity")
			}

			defaultCfg, defaultStore := defaultPaths()
			st, err := store.NewStore(defaultStore, "goroute-default-key")
			if err != nil {
				return err
			}

			verifier, challenge, err := antigravity.GeneratePKCE()
			if err != nil {
				return err
			}

			redirectURI := "http://127.0.0.1:20129/callback"
			state := "goroute-auth-" + fmt.Sprintf("%d", time.Now().UnixNano())
			authURL := antigravity.BuildAuthURL(redirectURI, state, challenge)

			codeChan := make(chan string, 1)
			errChan := make(chan error, 1)

			listener, err := net.Listen("tcp", "127.0.0.1:20129")
			if err != nil {
				return fmt.Errorf("failed to start local auth listener on port 20129: %w", err)
			}
			defer listener.Close()

			mux := http.NewServeMux()
			mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
				q := r.URL.Query()
				if q.Get("state") != state {
					http.Error(w, "Invalid state", http.StatusBadRequest)
					errChan <- fmt.Errorf("OAuth state mismatch")
					return
				}
				code := q.Get("code")
				if code == "" {
					http.Error(w, "No code provided", http.StatusBadRequest)
					errChan <- fmt.Errorf("no authorization code in callback")
					return
				}

				w.Header().Set("Content-Type", "text/html")
				w.Write([]byte(`<h2>Authentication Successful!</h2><p>You can close this tab and return to your terminal.</p>`))
				codeChan <- code
			})

			server := &http.Server{Handler: mux}
			go func() {
				if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
					errChan <- err
				}
			}()

			fmt.Println("👉 Please open the following URL in your browser to sign in:")
			fmt.Println(authURL)
			fmt.Println("\nWaiting for browser callback on http://127.0.0.1:20129/callback...")
			openBrowser(authURL)

			var code string
			select {
			case code = <-codeChan:
			case err := <-errChan:
				return err
			case <-time.After(3 * time.Minute):
				return fmt.Errorf("OAuth login timed out after 3 minutes")
			}

			_ = server.Shutdown(context.Background())

			fmt.Println("⏳ Exchanging code for credentials & discovering project...")
			acc, err := antigravity.ExchangeCode(context.Background(), code, redirectURI, verifier)
			if err != nil {
				return fmt.Errorf("token exchange failed: %w", err)
			}

			accID := fmt.Sprintf("antigravity-%s", acc.Email)
			acc.ID = accID

			// Save to encrypted vault
			if acc.RefreshToken != "" {
				_ = st.SetSecret(accID, acc.RefreshToken)
			}

			// Update config.yaml
			cfg, _ := config.Load(defaultCfg)
			if cfg == nil {
				cfg = config.DefaultConfig()
			}

			// Find or create antigravity provider
			found := false
			for i, p := range cfg.Providers {
				if p.Type == "antigravity" {
					cfg.Providers[i].Accounts = append(cfg.Providers[i].Accounts, config.AccountConfig{
						ID:          accID,
						Email:       acc.Email,
						ProjectID:   acc.ProjectID,
						AccessToken: acc.AccessToken,
						ExpiresAt:   acc.ExpiresAt,
					})
					found = true
					break
				}
			}
			if !found {
				cfg.Providers = append(cfg.Providers, config.ProviderConfig{
					ID:   "antigravity",
					Type: "antigravity",
					Accounts: []config.AccountConfig{
						{
							ID:          accID,
							Email:       acc.Email,
							ProjectID:   acc.ProjectID,
							AccessToken: acc.AccessToken,
							ExpiresAt:   acc.ExpiresAt,
						},
					},
				})
			}

			_ = cfg.Save(defaultCfg)

			fmt.Println("✅ Successfully connected Antigravity account!")
			fmt.Printf("   Account ID: %s\n", acc.ID)
			fmt.Printf("   Email:      %s\n", acc.Email)
			fmt.Printf("   Project ID: %s\n", acc.ProjectID)
			return nil
		},
	}

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List configured providers and accounts",
		RunE: func(cmd *cobra.Command, args []string) error {
			defaultCfg, _ := defaultPaths()
			cfg, err := config.Load(defaultCfg)
			if err != nil {
				fmt.Println("No configuration found. Run 'goroute serve' or 'goroute auth login antigravity'")
				return nil
			}

			fmt.Println("Configured Providers:")
			for _, p := range cfg.Providers {
				fmt.Printf("• Provider: %s (Type: %s)\n", p.ID, p.Type)
				for _, acc := range p.Accounts {
					fmt.Printf("   - Account: %s (%s) [Project: %s]\n", acc.ID, acc.Email, acc.ProjectID)
				}
			}

			if len(cfg.Combos) > 0 {
				fmt.Println("\nConfigured Combos:")
				for _, c := range cfg.Combos {
					fmt.Printf("• Combo: %s [Strategy: %s]\n", c.Name, c.Strategy)
					for _, t := range c.Targets {
						fmt.Printf("   -> %s / %s\n", t.ProviderID, t.Model)
					}
				}
			}
			return nil
		},
	}

	authCmd.AddCommand(loginCmd, listCmd)
	return authCmd
}

func NewStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Check running GoRoute gateway status",
		RunE: func(cmd *cobra.Command, args []string) error {
			resp, err := http.Get("http://127.0.0.1:20128/health")
			if err != nil {
				fmt.Println("❌ GoRoute server is NOT running on port 20128.")
				fmt.Println("   Start it with: goroute serve")
				return nil
			}
			defer resp.Body.Close()

			fmt.Println("✅ GoRoute is ACTIVE on http://127.0.0.1:20128")
			modelsResp, err := http.Get("http://127.0.0.1:20128/v1/models")
			if err == nil {
				defer modelsResp.Body.Close()
				var data struct {
					Data []providers.ModelInfo `json:"data"`
				}
				if json.NewDecoder(modelsResp.Body).Decode(&data) == nil {
					fmt.Printf("   Active Models/Combos: %d\n", len(data.Data))
					for _, m := range data.Data {
						fmt.Printf("   • %s (%s)\n", m.ID, m.OwnedBy)
					}
				}
			}
			return nil
		},
	}
}
