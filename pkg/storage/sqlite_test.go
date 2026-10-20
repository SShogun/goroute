package storage

import (
	"path/filepath"
	"testing"
	"time"
)

func TestSQLiteCallLogs(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := InitDB(dbPath)
	if err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	defer store.Close()

	log := CallLog{
		ID:           "test-req-1",
		Timestamp:    time.Now().UTC(),
		Model:        "claude-3-7-sonnet",
		Provider:     "antigravity",
		AccountID:    "acc-123",
		InputTokens:  120,
		OutputTokens: 350,
		CostUSD:      0.0025,
		LatencyMs:    450,
		StatusCode:   200,
	}

	if err := store.RecordCall(log); err != nil {
		t.Fatalf("RecordCall failed: %v", err)
	}

	logs, err := store.QueryRecentLogs(10, 0)
	if err != nil {
		t.Fatalf("QueryRecentLogs failed: %v", err)
	}

	if len(logs) != 1 {
		t.Fatalf("expected 1 log, got %d", len(logs))
	}
	if logs[0].Model != "claude-3-7-sonnet" {
		t.Errorf("expected claude-3-7-sonnet, got %s", logs[0].Model)
	}
	if logs[0].CostUSD != 0.0025 {
		t.Errorf("expected 0.0025 cost, got %f", logs[0].CostUSD)
	}
}
