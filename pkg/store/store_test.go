package store

import (
	"path/filepath"
	"testing"
)

func TestStoreEncryption(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "vault.json")
	secretKey := "12345678901234567890123456789012" // 32 bytes for AES-256

	st, err := NewStore(dbPath, secretKey)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	err = st.SetSecret("account-1", "my-refresh-token-xyz")
	if err != nil {
		t.Fatalf("failed to set secret: %v", err)
	}

	val, err := st.GetSecret("account-1")
	if err != nil {
		t.Fatalf("failed to get secret: %v", err)
	}

	if val != "my-refresh-token-xyz" {
		t.Errorf("expected 'my-refresh-token-xyz', got '%s'", val)
	}

	// Verify persistence: create new store instance pointing to same file
	st2, err := NewStore(dbPath, secretKey)
	if err != nil {
		t.Fatalf("failed to load store again: %v", err)
	}

	val2, err := st2.GetSecret("account-1")
	if err != nil || val2 != "my-refresh-token-xyz" {
		t.Errorf("failed to reload secret from disk: val=%s, err=%v", val2, err)
	}
}
