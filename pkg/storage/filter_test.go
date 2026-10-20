package storage

import (
	"path/filepath"
	"testing"
	"time"
)

func TestQueryFilteredLogs(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "filter_test.db")

	db, err := InitDB(dbPath)
	if err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	defer db.Close()

	logs := []CallLog{
		{ID: "req-1", Timestamp: time.Now().Add(-10 * time.Second), Model: "claude-3-7-sonnet", Provider: "antigravity", StatusCode: 200},
		{ID: "req-2", Timestamp: time.Now().Add(-5 * time.Second), Model: "gpt-4o", Provider: "openai", StatusCode: 429, ErrorMessage: "rate limit exceeded"},
		{ID: "req-3", Timestamp: time.Now(), Model: "test-probe", Provider: "codex", StatusCode: 200},
	}

	for _, l := range logs {
		_ = db.RecordCall(l)
	}

	// 1. Filter by status=error
	errLogs, err := db.QueryFilteredLogs(CallLogFilter{Status: "error"})
	if err != nil {
		t.Fatalf("QueryFilteredLogs error failed: %v", err)
	}
	if len(errLogs) != 1 || errLogs[0].ID != "req-2" {
		t.Errorf("expected 1 error log req-2, got %v", errLogs)
	}

	// 2. Filter excludeTests=true
	nonTestLogs, err := db.QueryFilteredLogs(CallLogFilter{ExcludeTests: true})
	if err != nil {
		t.Fatalf("QueryFilteredLogs excludeTests failed: %v", err)
	}
	if len(nonTestLogs) != 2 {
		t.Errorf("expected 2 non-test logs, got %d", len(nonTestLogs))
	}

	// 3. Search query
	searched, err := db.QueryFilteredLogs(CallLogFilter{Search: "rate limit"})
	if err != nil {
		t.Fatalf("QueryFilteredLogs search failed: %v", err)
	}
	if len(searched) != 1 || searched[0].ID != "req-2" {
		t.Errorf("expected req-2 for search, got %v", searched)
	}
}
