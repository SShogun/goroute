package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAgentSetups(t *testing.T) {
	tempHome := t.TempDir()

	for name, cfg := range agentConfigurators {
		t.Run(name, func(t *testing.T) {
			err := cfg.Run(tempHome)
			if err != nil {
				t.Fatalf("setup for %s failed: %v", name, err)
			}
		})
	}

	// Verify key configuration files were written
	expectedFiles := []string{
		filepath.Join(tempHome, ".claude", "settings.json"),
		filepath.Join(tempHome, ".codex", "goroute.config.toml"),
		filepath.Join(tempHome, ".config", "opencode", "opencode.json"),
		filepath.Join(tempHome, ".cline", "data", "secrets.json"),
		filepath.Join(tempHome, ".local", "share", "kilo", "auth.json"),
		filepath.Join(tempHome, ".aider.conf.yml"),
		filepath.Join(tempHome, ".continue", "config.yaml"),
		filepath.Join(tempHome, ".config", "crush", "crush.json"),
		filepath.Join(tempHome, ".config", "goose", "config.yaml"),
		filepath.Join(tempHome, ".qwen", "settings.json"),
		filepath.Join(tempHome, ".roo", "settings-import.json"),
		filepath.Join(tempHome, ".hermes", "config.yaml"),
		filepath.Join(tempHome, ".openclaw", "openclaw.json"),
	}

	for _, file := range expectedFiles {
		if _, err := os.Stat(file); os.IsNotExist(err) {
			t.Errorf("expected config file was not created: %s", file)
		}
	}
}
