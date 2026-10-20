package storage

import (
	"fmt"
	"sync"
	"time"
)

// QuotaGroup defines a pooled quota allocation group.
type QuotaGroup struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"createdAt"`
}

// TokenHealthSummary reports aggregate OAuth token statuses.
type TokenHealthSummary struct {
	Total       int        `json:"total"`
	Healthy     int        `json:"healthy"`
	Errored     int        `json:"errored"`
	Warning     int        `json:"warning"`
	LastCheckAt *time.Time `json:"lastCheckAt,omitempty"`
	Status      string     `json:"status"` // "healthy", "warning", "error", "unknown"
}

// QuotaGroupStore manages quota_groups table in SQLite.
type QuotaGroupStore struct {
	db *DBStore
	mu sync.RWMutex
}

var (
	globalQuotaGroupStore *QuotaGroupStore
	quotaGroupOnce        sync.Once
)

func GetQuotaGroupStore() *QuotaGroupStore {
	quotaGroupOnce.Do(func() {
		globalQuotaGroupStore = &QuotaGroupStore{
			db: GetDB(),
		}
		globalQuotaGroupStore.ensureTable()
	})
	return globalQuotaGroupStore
}

func (s *QuotaGroupStore) ensureTable() {
	if s.db == nil || s.db.db == nil {
		return
	}
	_, _ = s.db.db.Exec(`
		CREATE TABLE IF NOT EXISTS quota_groups (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL UNIQUE,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
	`)
}

func (s *QuotaGroupStore) ListGroups() ([]QuotaGroup, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var groups []QuotaGroup
	if s.db == nil || s.db.db == nil {
		return groups, nil
	}

	rows, err := s.db.db.Query(`SELECT id, name, created_at FROM quota_groups ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var g QuotaGroup
		var tsStr string
		if err := rows.Scan(&g.ID, &g.Name, &tsStr); err == nil {
			g.CreatedAt, _ = time.Parse(time.RFC3339, tsStr)
			groups = append(groups, g)
		}
	}
	return groups, nil
}

func (s *QuotaGroupStore) CreateGroup(name string) (QuotaGroup, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	g := QuotaGroup{
		ID:        fmt.Sprintf("qg_%d", time.Now().UnixNano()),
		Name:      name,
		CreatedAt: time.Now().UTC(),
	}

	if s.db == nil || s.db.db == nil {
		return g, fmt.Errorf("database not initialized")
	}

	_, err := s.db.db.Exec(`
		INSERT INTO quota_groups (id, name, created_at)
		VALUES (?, ?, ?)
	`, g.ID, g.Name, g.CreatedAt.Format(time.RFC3339))
	return g, err
}
