package storage

import (
	"fmt"
	"sync"
	"time"
)

// QuotaPool defines a shared quota allocation pool.
type QuotaPool struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	GroupID      string    `json:"groupId,omitempty"`
	ConnectionID string    `json:"connectionId,omitempty"`
	Strategy     string    `json:"strategy"` // "weight", "cap", "fair_share"
	MaxTokens    int64     `json:"maxTokens,omitempty"`
	CreatedAt    time.Time `json:"createdAt"`
}

// QuotaPoolStore manages the quota_pools table in SQLite.
type QuotaPoolStore struct {
	db *DBStore
	mu sync.RWMutex
}

var (
	globalQuotaPoolStore *QuotaPoolStore
	quotaPoolOnce        sync.Once
)

func GetQuotaPoolStore() *QuotaPoolStore {
	quotaPoolOnce.Do(func() {
		globalQuotaPoolStore = &QuotaPoolStore{
			db: GetDB(),
		}
		globalQuotaPoolStore.ensureTable()
	})
	return globalQuotaPoolStore
}

func (s *QuotaPoolStore) ensureTable() {
	if s.db == nil || s.db.db == nil {
		return
	}
	_, _ = s.db.db.Exec(`
		CREATE TABLE IF NOT EXISTS quota_pools (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			group_id TEXT,
			connection_id TEXT,
			strategy TEXT NOT NULL DEFAULT 'weight',
			max_tokens INTEGER DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
	`)
}

func (s *QuotaPoolStore) ListPools() ([]QuotaPool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var pools []QuotaPool
	if s.db == nil || s.db.db == nil {
		return pools, nil
	}

	rows, err := s.db.db.Query(`SELECT id, name, group_id, connection_id, strategy, max_tokens, created_at FROM quota_pools ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var p QuotaPool
		var gID, cID *string
		var tsStr string
		if err := rows.Scan(&p.ID, &p.Name, &gID, &cID, &p.Strategy, &p.MaxTokens, &tsStr); err == nil {
			if gID != nil {
				p.GroupID = *gID
			}
			if cID != nil {
				p.ConnectionID = *cID
			}
			p.CreatedAt, _ = time.Parse(time.RFC3339, tsStr)
			pools = append(pools, p)
		}
	}
	return pools, nil
}

func (s *QuotaPoolStore) CreatePool(name, groupID, connectionID, strategy string, maxTokens int64) (QuotaPool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if strategy == "" {
		strategy = "weight"
	}

	p := QuotaPool{
		ID:           fmt.Sprintf("qp_%d", time.Now().UnixNano()),
		Name:         name,
		GroupID:      groupID,
		ConnectionID: connectionID,
		Strategy:     strategy,
		MaxTokens:    maxTokens,
		CreatedAt:    time.Now().UTC(),
	}

	if s.db == nil || s.db.db == nil {
		return p, fmt.Errorf("database not initialized")
	}

	_, err := s.db.db.Exec(`
		INSERT INTO quota_pools (id, name, group_id, connection_id, strategy, max_tokens, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, p.ID, p.Name, p.GroupID, p.ConnectionID, p.Strategy, p.MaxTokens, p.CreatedAt.Format(time.RFC3339))
	return p, err
}
