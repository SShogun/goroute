package oauth

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestParseCliProxyAuthRecord(t *testing.T) {
	now := time.Now()
	raw := map[string]interface{}{
		"type":          "claude",
		"email":         "dev@example.com",
		"access_token":  "sk-ant-test-token",
		"refresh_token": "sk-ant-refresh",
		"expires_in":    3600.0,
	}

	parsed := ParseCliProxyAuthRecord(raw, now)
	if parsed == nil {
		t.Fatal("expected non-nil parsed record")
	}
	if parsed.Provider != "claude" {
		t.Errorf("expected provider claude, got %s", parsed.Provider)
	}
	if parsed.Email != "dev@example.com" {
		t.Errorf("expected email dev@example.com, got %s", parsed.Email)
	}
	if parsed.AccessToken != "sk-ant-test-token" {
		t.Errorf("expected token sk-ant-test-token, got %s", parsed.AccessToken)
	}
}

func TestScanCliProxyAuthDir(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "cliproxy-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	item := map[string]interface{}{
		"type":         "codex",
		"email":        "codex@example.com",
		"access_token": "ghu_codex_12345",
	}
	bytes, _ := json.Marshal(item)
	if err := os.WriteFile(filepath.Join(tmpDir, "codex.json"), bytes, 0644); err != nil {
		t.Fatal(err)
	}

	// invalid file
	if err := os.WriteFile(filepath.Join(tmpDir, "invalid.json"), []byte("{invalid json"), 0644); err != nil {
		t.Fatal(err)
	}

	candidates, skipped, scanned, err := ScanCliProxyAuthDir(tmpDir)
	if err != nil {
		t.Fatalf("scan error: %v", err)
	}
	if scanned != 2 {
		t.Errorf("expected 2 scanned, got %d", scanned)
	}
	if skipped != 1 {
		t.Errorf("expected 1 skipped, got %d", skipped)
	}
	if len(candidates) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(candidates))
	}
	if candidates[0].Provider != "codex" {
		t.Errorf("expected provider codex, got %s", candidates[0].Provider)
	}
}
