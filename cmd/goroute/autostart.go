package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/spf13/cobra"
)

func NewAutostartCmd() *cobra.Command {
	autostartCmd := &cobra.Command{
		Use:   "autostart [enable|disable|status]",
		Short: "Manage GoRoute autostart as a background systemd user service",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				fmt.Println("Usage: goroute autostart <enable|disable|status>")
				fmt.Println("  goroute autostart enable   - Auto-start GoRoute on system boot")
				fmt.Println("  goroute autostart disable  - Stop and remove the systemd service")
				fmt.Println("  goroute autostart status   - View current background service status")
				return nil
			}

			action := args[0]
			home, err := os.UserHomeDir()
			if err != nil {
				return err
			}

			systemdDir := filepath.Join(home, ".config", "systemd", "user")
			servicePath := filepath.Join(systemdDir, "goroute.service")

			switch action {
			case "enable":
				_ = os.MkdirAll(systemdDir, 0755)

				binPath, err := exec.LookPath("goroute")
				if err != nil {
					binPath = filepath.Join(home, ".local", "bin", "goroute")
				}

				serviceContent := fmt.Sprintf(`[Unit]
Description=GoRoute - High-Performance LLM Gateway
After=network.target

[Service]
Type=simple
ExecStart=%s serve --no-dashboard
Restart=always
RestartSec=3
Environment=HOME=%s

[Install]
WantedBy=default.target
`, binPath, home)

				if err := os.WriteFile(servicePath, []byte(serviceContent), 0644); err != nil {
					return fmt.Errorf("failed to write service file: %w", err)
				}

				_ = exec.Command("systemctl", "--user", "daemon-reload").Run()
				if err := exec.Command("systemctl", "--user", "enable", "--now", "goroute.service").Run(); err != nil {
					return fmt.Errorf("failed to enable and start service: %w", err)
				}

				fmt.Println("✅ GoRoute systemd background service enabled and running!")
				fmt.Printf("   Service unit: %s\n", servicePath)
				fmt.Println("   GoRoute will now run silently in the background on boot (< 20MB RAM).")

			case "disable":
				_ = exec.Command("systemctl", "--user", "disable", "--now", "goroute.service").Run()
				_ = os.Remove(servicePath)
				_ = exec.Command("systemctl", "--user", "daemon-reload").Run()
				fmt.Println("🛑 GoRoute systemd service disabled and stopped.")

			case "status":
				c := exec.Command("systemctl", "--user", "status", "goroute.service")
				c.Stdout = os.Stdout
				c.Stderr = os.Stderr
				_ = c.Run()

			default:
				return fmt.Errorf("unknown action '%s'. Use 'enable', 'disable', or 'status'", action)
			}

			return nil
		},
	}

	return autostartCmd
}
