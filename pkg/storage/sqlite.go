package storage

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// CallLog represents a persistent historical proxy query record.
type CallLog struct {
	ID           string    `json:"id"`
	Timestamp    time.Time `json:"timestamp"`
	Model        string    `json:"model"`
	Provider     string    `json:"provider"`
	AccountID    string    `json:"account_id"`
	InputTokens  int       `json:"input_tokens"`
	OutputTokens int       `json:"output_tokens"`
	CostUSD      float64   `json:"cost_usd"`
	LatencyMs    int64     `json:"latency_ms"`
	StatusCode   int       `json:"status_code"`
	ErrorMessage string    `json:"error_message,omitempty"`
}

// DBStore wraps SQLite database operations for GoRoute.
type DBStore struct {
	db *sql.DB
	mu sync.RWMutex
}

var (
	globalDB   *DBStore
	dbInitOnce sync.Once
)

// InitDB initializes SQLite storage in ~/.goroute/goroute.db
func InitDB(dbPath string) (*DBStore, error) {
	if dbPath == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		dir := filepath.Join(home, ".goroute")
		if err := os.MkdirAll(dir, 0700); err != nil {
			return nil, err
		}
		dbPath = filepath.Join(dir, "goroute.db")
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite: %w", err)
	}

	// High concurrency tuning for SQLite in WAL mode
	if _, err := db.Exec(`
		PRAGMA journal_mode = WAL;
		PRAGMA synchronous = NORMAL;
		PRAGMA foreign_keys = ON;
		PRAGMA busy_timeout = 5000;
	`); err != nil {
		return nil, fmt.Errorf("failed to execute pragma: %w", err)
	}

	schema := `
	CREATE TABLE IF NOT EXISTS call_logs (
		id TEXT PRIMARY KEY,
		timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
		model TEXT NOT NULL,
		provider TEXT NOT NULL,
		account_id TEXT,
		input_tokens INTEGER DEFAULT 0,
		output_tokens INTEGER DEFAULT 0,
		cost_usd REAL DEFAULT 0.0,
		latency_ms INTEGER DEFAULT 0,
		status_code INTEGER DEFAULT 200,
		error_message TEXT
	);

	CREATE INDEX IF NOT EXISTS idx_call_logs_timestamp ON call_logs(timestamp DESC);
	CREATE INDEX IF NOT EXISTS idx_call_logs_provider ON call_logs(provider);
	CREATE INDEX IF NOT EXISTS idx_call_logs_model ON call_logs(model);

	CREATE TABLE IF NOT EXISTS domain_policies (
		domain TEXT PRIMARY KEY,
		action TEXT NOT NULL,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	`

	if _, err := db.Exec(schema); err != nil {
		return nil, fmt.Errorf("failed to execute schema: %w", err)
	}

	store := &DBStore{db: db}
	globalDB = store
	return store, nil
}

// GetDB returns the initialized global DB store.
func GetDB() *DBStore {
	dbInitOnce.Do(func() {
		if globalDB == nil {
			InitDB("")
		}
	})
	return globalDB
}

// RecordCall logs an LLM request execution to SQLite.
func (s *DBStore) RecordCall(log CallLog) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	query := `
	INSERT INTO call_logs (id, timestamp, model, provider, account_id, input_tokens, output_tokens, cost_usd, latency_ms, status_code, error_message)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`
	_, err := s.db.Exec(query,
		log.ID,
		log.Timestamp.Format(time.RFC3339),
		log.Model,
		log.Provider,
		log.AccountID,
		log.InputTokens,
		log.OutputTokens,
		log.CostUSD,
		log.LatencyMs,
		log.StatusCode,
		log.ErrorMessage,
	)
	return err
}

// QueryRecentLogs retrieves recent call logs with pagination.
func (s *DBStore) QueryRecentLogs(limit, offset int) ([]CallLog, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if limit <= 0 {
		limit = 50
	}
	query := `
	SELECT id, timestamp, model, provider, account_id, input_tokens, output_tokens, cost_usd, latency_ms, status_code, error_message
	FROM call_logs
	ORDER BY timestamp DESC
	LIMIT ? OFFSET ?
	`
	rows, err := s.db.Query(query, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var logs []CallLog
	for rows.Next() {
		var l CallLog
		var tsStr string
		var errMsg sql.NullString
		var accID sql.NullString

		if err := rows.Scan(
			&l.ID,
			&tsStr,
			&l.Model,
			&l.Provider,
			&accID,
			&l.InputTokens,
			&l.OutputTokens,
			&l.CostUSD,
			&l.LatencyMs,
			&l.StatusCode,
			&errMsg,
		); err != nil {
			return nil, err
		}

		l.Timestamp, _ = time.Parse(time.RFC3339, tsStr)
		if errMsg.Valid {
			l.ErrorMessage = errMsg.String
		}
		if accID.Valid {
			l.AccountID = accID.String
		}
		logs = append(logs, l)
	}

	return logs, nil
}

// Close closes the database connection.
func (s *DBStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}
