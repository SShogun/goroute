package storage

import (
	"path/filepath"
	"testing"
)

func TestSettingsStore(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "settings_test.db")

	db, err := InitDB(dbPath)
	if err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	defer db.Close()

	store := &SettingsStore{db: db}
	store.ensureTable()

	s, err := store.LoadSettings()
	if err != nil {
		t.Fatalf("LoadSettings failed: %v", err)
	}

	if s.StickyRoundRobinLimit != 3 {
		t.Errorf("expected default 3, got %d", s.StickyRoundRobinLimit)
	}

	if err := store.UpdateKey("stickyRoundRobinLimit", 7); err != nil {
		t.Fatalf("UpdateKey failed: %v", err)
	}

	if err := store.UpdateKey("customSystemPrompt", "You are an expert coder"); err != nil {
		t.Fatalf("UpdateKey failed: %v", err)
	}

	updated, err := store.LoadSettings()
	if err != nil {
		t.Fatalf("LoadSettings updated failed: %v", err)
	}

	if updated.StickyRoundRobinLimit != 7 {
		t.Errorf("expected 7, got %d", updated.StickyRoundRobinLimit)
	}
	if updated.CustomSystemPrompt != "You are an expert coder" {
		t.Errorf("expected custom prompt, got %s", updated.CustomSystemPrompt)
	}
}
