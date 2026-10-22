package proxy

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/ziuus/goroute/pkg/storage"
)

const maxLogEntries = 200

// RequestLog represents a single tracked request through GoRoute
type RequestLog struct {
	ID         string    `json:"id"`
	Timestamp  time.Time `json:"timestamp"`
	Endpoint   string    `json:"endpoint"`
	Model      string    `json:"model"`
	Provider   string    `json:"provider,omitempty"`
	AccountID  string    `json:"account_id,omitempty"`
	StatusCode int       `json:"status_code"`
	DurationMs int64     `json:"duration_ms"`
	Tokens     int       `json:"tokens,omitempty"`
	Error      string    `json:"error,omitempty"`
}

// LogHub manages the in-memory request ring buffer and live SSE subscribers
type LogHub struct {
	mu          sync.RWMutex
	logs        []RequestLog
	subscribers map[chan RequestLog]struct{}
}

var globalLogHub = &LogHub{
	logs:        make([]RequestLog, 0, maxLogEntries),
	subscribers: make(map[chan RequestLog]struct{}),
}

// RecordRequest records a completed request into the ring buffer and notifies SSE subscribers
func RecordRequest(entry RequestLog) {
	globalLogHub.mu.Lock()
	if len(globalLogHub.logs) >= maxLogEntries {
		globalLogHub.logs = globalLogHub.logs[1:]
	}
	globalLogHub.logs = append(globalLogHub.logs, entry)

	// Broadcast to active SSE subscribers
	for ch := range globalLogHub.subscribers {
		select {
		case ch <- entry:
		default:
			// Non-blocking write to avoid lagging clients slowing down proxy
		}
	}
	globalLogHub.mu.Unlock()

	// Asynchronously record into SQLite persistent store
	go func(logEntry RequestLog) {
		db := storage.GetDB()
		if db != nil {
			_ = db.RecordCall(storage.CallLog{
				ID:           logEntry.ID,
				Timestamp:    logEntry.Timestamp,
				Model:        logEntry.Model,
				Provider:     logEntry.Provider,
				AccountID:    logEntry.AccountID,
				OutputTokens: logEntry.Tokens,
				LatencyMs:    logEntry.DurationMs,
				StatusCode:   logEntry.StatusCode,
				ErrorMessage: logEntry.Error,
			})
		}
	}(entry)
}

// GetRecentLogs returns a copy of recent request logs
func GetRecentLogs() []RequestLog {
	globalLogHub.mu.RLock()
	defer globalLogHub.mu.RUnlock()

	res := make([]RequestLog, len(globalLogHub.logs))
	copy(res, globalLogHub.logs)
	return res
}

func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) {
	logs := GetRecentLogs()
	if len(logs) == 0 {
		// Fallback to SQLite persistent store so history is immediately visible after restart
		db := storage.GetDB()
		if db != nil {
			callLogs, _ := db.QueryRecentLogs(20, 0)
			for _, cl := range callLogs {
				logs = append(logs, RequestLog{
					ID:         cl.ID,
					Timestamp:  cl.Timestamp,
					Model:      cl.Model,
					Provider:   cl.Provider,
					AccountID:  cl.AccountID,
					StatusCode: cl.StatusCode,
					DurationMs: cl.LatencyMs,
					Tokens:     cl.OutputTokens,
					Error:      cl.ErrorMessage,
				})
			}
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"total": len(logs),
		"logs":  logs,
	})
}

func (s *Server) handleLogsStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	logCh := make(chan RequestLog, 50)
	globalLogHub.mu.Lock()
	globalLogHub.subscribers[logCh] = struct{}{}
	globalLogHub.mu.Unlock()

	defer func() {
		globalLogHub.mu.Lock()
		delete(globalLogHub.subscribers, logCh)
		globalLogHub.mu.Unlock()
		close(logCh)
	}()

	notify := r.Context().Done()
	for {
		select {
		case <-notify:
			return
		case entry := <-logCh:
			data, err := json.Marshal(entry)
			if err == nil {
				fmt.Fprintf(w, "data: %s\n\n", data)
				flusher.Flush()
			}
		}
	}
}
