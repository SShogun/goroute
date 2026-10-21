package storage

import (
	"fmt"
	"sync"
	"time"
)

// EvalSuiteRecord represents an evaluation suite.
type EvalSuiteRecord struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	CaseCount   int             `json:"caseCount"`
	Cases       []EvalCaseRecord `json:"cases,omitempty"`
	CreatedAt   time.Time       `json:"createdAt"`
	UpdatedAt   time.Time       `json:"updatedAt"`
}

// EvalCaseRecord represents a single test prompt and expected response predicate.
type EvalCaseRecord struct {
	ID        string    `json:"id"`
	SuiteID   string    `json:"suiteId"`
	Name      string    `json:"name"`
	Model     string    `json:"model,omitempty"`
	Input     string    `json:"input"`    // prompt or json messages
	Strategy  string    `json:"strategy"` // "contains", "exact", "regex"
	Expected  string    `json:"expected"`
	CreatedAt time.Time `json:"createdAt"`
}

// EvalRunRecord captures results of a run against a model or combo target.
type EvalRunRecord struct {
	ID        string    `json:"id"`
	SuiteID   string    `json:"suiteId"`
	Target    string    `json:"target"`
	Total     int       `json:"total"`
	Passed    int       `json:"passed"`
	Failed    int       `json:"failed"`
	PassRate  float64   `json:"passRate"`
	DurationMs int64    `json:"durationMs"`
	CreatedAt time.Time `json:"createdAt"`
}

// LocalCorpusConfig holds the configured root path for RAG index ingestion.
type LocalCorpusConfig struct {
	Configured bool   `json:"configured"`
	RootPath   string `json:"rootPath"`
	Status     string `json:"status"` // "ready", "indexing", "idle"
}

// EvalCorpusStore manages evals and local corpus in SQLite.
type EvalCorpusStore struct {
	db *DBStore
	mu sync.RWMutex
}

var (
	globalEvalCorpusStore *EvalCorpusStore
	evalCorpusOnce        sync.Once
)

func GetEvalCorpusStore() *EvalCorpusStore {
	evalCorpusOnce.Do(func() {
		globalEvalCorpusStore = &EvalCorpusStore{
			db: GetDB(),
		}
		globalEvalCorpusStore.ensureTables()
	})
	return globalEvalCorpusStore
}

func (s *EvalCorpusStore) ensureTables() {
	if s.db == nil || s.db.db == nil {
		return
	}
	_, _ = s.db.db.Exec(`
		CREATE TABLE IF NOT EXISTS eval_suites (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL UNIQUE,
			description TEXT,
			case_count INTEGER DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);

		CREATE TABLE IF NOT EXISTS eval_cases (
			id TEXT PRIMARY KEY,
			suite_id TEXT NOT NULL,
			name TEXT NOT NULL,
			model TEXT,
			input TEXT NOT NULL,
			strategy TEXT NOT NULL DEFAULT 'contains',
			expected TEXT NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (suite_id) REFERENCES eval_suites(id) ON DELETE CASCADE
		);

		CREATE TABLE IF NOT EXISTS eval_runs (
			id TEXT PRIMARY KEY,
			suite_id TEXT NOT NULL,
			target TEXT NOT NULL,
			total INTEGER DEFAULT 0,
			passed INTEGER DEFAULT 0,
			failed INTEGER DEFAULT 0,
			pass_rate REAL DEFAULT 0.0,
			duration_ms INTEGER DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);

		CREATE INDEX IF NOT EXISTS idx_eval_runs_suite ON eval_runs(suite_id);
	`)
}

func (s *EvalCorpusStore) ListSuites() ([]EvalSuiteRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var suites []EvalSuiteRecord
	if s.db == nil || s.db.db == nil {
		return suites, nil
	}

	rows, err := s.db.db.Query(`SELECT id, name, description, case_count, created_at, updated_at FROM eval_suites ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var e EvalSuiteRecord
		var desc *string
		var crStr, upStr string
		if err := rows.Scan(&e.ID, &e.Name, &desc, &e.CaseCount, &crStr, &upStr); err == nil {
			if desc != nil {
				e.Description = *desc
			}
			e.CreatedAt, _ = time.Parse(time.RFC3339, crStr)
			e.UpdatedAt, _ = time.Parse(time.RFC3339, upStr)
			suites = append(suites, e)
		}
	}
	return suites, nil
}

func (s *EvalCorpusStore) CreateSuite(name, description string) (EvalSuiteRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	e := EvalSuiteRecord{
		ID:          fmt.Sprintf("suite_%d", time.Now().UnixNano()),
		Name:        name,
		Description: description,
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}

	if s.db == nil || s.db.db == nil {
		return e, fmt.Errorf("database not initialized")
	}

	_, err := s.db.db.Exec(`
		INSERT INTO eval_suites (id, name, description, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?)
	`, e.ID, e.Name, e.Description, e.CreatedAt.Format(time.RFC3339), e.UpdatedAt.Format(time.RFC3339))
	return e, err
}

func (s *EvalCorpusStore) RecordRun(run EvalRunRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.db == nil || s.db.db == nil {
		return fmt.Errorf("database not initialized")
	}
	if run.ID == "" {
		run.ID = fmt.Sprintf("run_%d", time.Now().UnixNano())
	}
	if run.CreatedAt.IsZero() {
		run.CreatedAt = time.Now().UTC()
	}

	_, err := s.db.db.Exec(`
		INSERT INTO eval_runs (id, suite_id, target, total, passed, failed, pass_rate, duration_ms, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, run.ID, run.SuiteID, run.Target, run.Total, run.Passed, run.Failed, run.PassRate, run.DurationMs, run.CreatedAt.Format(time.RFC3339))
	return err
}

func (s *EvalCorpusStore) ListRuns(limit int) ([]EvalRunRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var runs []EvalRunRecord
	if s.db == nil || s.db.db == nil {
		return runs, nil
	}
	if limit <= 0 {
		limit = 20
	}

	rows, err := s.db.db.Query(`
		SELECT id, suite_id, target, total, passed, failed, pass_rate, duration_ms, created_at
		FROM eval_runs
		ORDER BY created_at DESC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var r EvalRunRecord
		var crStr string
		if err := rows.Scan(&r.ID, &r.SuiteID, &r.Target, &r.Total, &r.Passed, &r.Failed, &r.PassRate, &r.DurationMs, &crStr); err == nil {
			r.CreatedAt, _ = time.Parse(time.RFC3339, crStr)
			runs = append(runs, r)
		}
	}
	return runs, nil
}

// SetLocalCorpusRoot stores the corpus directory path in SQLite.
func (s *EvalCorpusStore) SetLocalCorpusRoot(path string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.db == nil || s.db.db == nil {
		return fmt.Errorf("database not initialized")
	}

	_, err := s.db.db.Exec(`
		INSERT OR REPLACE INTO key_value (namespace, key, value)
		VALUES ('settings', 'localCorpusRoot', ?)
	`, path)
	return err
}

// GetLocalCorpusConfig reads the configured local corpus settings.
func (s *EvalCorpusStore) GetLocalCorpusConfig() LocalCorpusConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()

	cfg := LocalCorpusConfig{Configured: false, RootPath: "", Status: "idle"}
	if s.db == nil || s.db.db == nil {
		return cfg
	}

	var root string
	err := s.db.db.QueryRow(`SELECT value FROM key_value WHERE namespace = 'settings' AND key = 'localCorpusRoot'`).Scan(&root)
	if err == nil && root != "" {
		cfg.Configured = true
		cfg.RootPath = root
		cfg.Status = "ready"
	}
	return cfg
}
