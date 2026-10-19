package proxy

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestLogsHub(t *testing.T) {
	RecordRequest(RequestLog{
		ID:         "test_req_1",
		Timestamp:  time.Now(),
		Endpoint:   "/v1/messages",
		Model:      "claude-sonnet-4-6",
		StatusCode: 200,
		DurationMs: 150,
	})

	logs := GetRecentLogs()
	if len(logs) == 0 {
		t.Fatalf("expected at least 1 log entry")
	}

	found := false
	for _, l := range logs {
		if l.ID == "test_req_1" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected to find test_req_1 in recent logs")
	}

	// Test HTTP handler
	server := &Server{}
	req := httptest.NewRequest("GET", "/api/logs", nil)
	w := httptest.NewRecorder()
	server.handleLogs(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	var resp struct {
		Total int          `json:"total"`
		Logs  []RequestLog `json:"logs"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode logs response: %v", err)
	}
	if resp.Total == 0 || len(resp.Logs) == 0 {
		t.Fatalf("expected non-empty logs from /api/logs")
	}
}
