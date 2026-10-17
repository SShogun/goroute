package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfig(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	os.Setenv("TEST_KEY", "secret-token-123")
	defer os.Unsetenv("TEST_KEY")

	yamlContent := `
server:
  port: 20128
  host: "127.0.0.1"
  storage_dir: "` + tmpDir + `"

providers:
  - id: "test-openai"
    type: "openai"
    base_url: "https://api.tokenrouter.io/v1"
    api_key: "${TEST_KEY}"

combos:
  - name: "claude-combo"
    strategy: "failover"
    targets:
      - provider_id: "test-openai"
        model: "claude-3-7-sonnet"
`
	if err := os.WriteFile(configPath, []byte(yamlContent), 0600); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("unexpected error loading config: %v", err)
	}

	if cfg.Server.Port != 20128 {
		t.Errorf("expected port 20128, got %d", cfg.Server.Port)
	}

	if len(cfg.Providers) != 1 {
		t.Fatalf("expected 1 provider, got %d", len(cfg.Providers))
	}

	if cfg.Providers[0].APIKey != "secret-token-123" {
		t.Errorf("expected expanded env var, got %s", cfg.Providers[0].APIKey)
	}

	if len(cfg.Combos) != 1 || cfg.Combos[0].Name != "claude-combo" {
		t.Errorf("expected combo 'claude-combo', got %+v", cfg.Combos)
	}
}
