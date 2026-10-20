package storage

import (
	"database/sql"
	"fmt"
	"sync"
	"time"
)

// ProxyRecord represents a proxy registration in proxy_registry.
type ProxyRecord struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Type      string    `json:"type"` // "http", "https", "socks5"
	Host      string    `json:"host"`
	Port      int       `json:"port"`
	Username  string    `json:"username,omitempty"`
	Password  string    `json:"password,omitempty"`
	Region    string    `json:"region,omitempty"`
	Notes     string    `json:"notes,omitempty"`
	Status    string    `json:"status"` // "active", "inactive", "error"
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// ProxyAssignment represents a route mapping (global, per-provider, or per-connection).
type ProxyAssignment struct {
	ID        int64     `json:"id"`
	ProxyID   string    `json:"proxyId"`
	Scope     string    `json:"scope"` // "global", "provider", "connection"
	ScopeID   string    `json:"scopeId,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// ProxyStore manages proxy registrations and route assignments in SQLite.
type ProxyStore struct {
	db *DBStore
	mu sync.RWMutex
}

var (
	globalProxyStore *ProxyStore
	proxyOnce        sync.Once
)

// GetProxyStore returns the singleton proxy store.
func GetProxyStore() *ProxyStore {
	proxyOnce.Do(func() {
		globalProxyStore = &ProxyStore{
			db: GetDB(),
		}
		globalProxyStore.ensureTables()
	})
	return globalProxyStore
}

func (s *ProxyStore) ensureTables() {
	if s.db == nil || s.db.db == nil {
		return
	}
	_, _ = s.db.db.Exec(`
		CREATE TABLE IF NOT EXISTS proxy_registry (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			type TEXT NOT NULL,
			host TEXT NOT NULL,
			port INTEGER NOT NULL,
			username TEXT,
			password TEXT,
			region TEXT,
			notes TEXT,
			status TEXT NOT NULL DEFAULT 'active',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);

		CREATE INDEX IF NOT EXISTS idx_proxy_registry_status ON proxy_registry(status);

		CREATE TABLE IF NOT EXISTS proxy_assignments (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			proxy_id TEXT NOT NULL,
			scope TEXT NOT NULL,
			scope_id TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(scope, scope_id)
		);

		CREATE INDEX IF NOT EXISTS idx_proxy_assignments_scope ON proxy_assignments(scope, scope_id);
	`)
}

// ListProxies returns all registered proxies.
func (s *ProxyStore) ListProxies() ([]ProxyRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []ProxyRecord
	if s.db == nil || s.db.db == nil {
		return result, nil
	}

	rows, err := s.db.db.Query(`
		SELECT id, name, type, host, port, username, password, region, notes, status, created_at, updated_at
		FROM proxy_registry
		ORDER BY created_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var p ProxyRecord
		var u, pw, reg, n sql.NullString
		var caStr, uaStr string

		if err := rows.Scan(
			&p.ID,
			&p.Name,
			&p.Type,
			&p.Host,
			&p.Port,
			&u,
			&pw,
			&reg,
			&n,
			&p.Status,
			&caStr,
			&uaStr,
		); err != nil {
			continue
		}

		if u.Valid {
			p.Username = u.String
		}
		if pw.Valid {
			p.Password = pw.String
		}
		if reg.Valid {
			p.Region = reg.String
		}
		if n.Valid {
			p.Notes = n.String
		}
		p.CreatedAt, _ = time.Parse(time.RFC3339, caStr)
		p.UpdatedAt, _ = time.Parse(time.RFC3339, uaStr)

		result = append(result, p)
	}

	return result, nil
}

// CreateProxy adds a new proxy into the registry.
func (s *ProxyStore) CreateProxy(p ProxyRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.db == nil || s.db.db == nil {
		return fmt.Errorf("database not initialized")
	}

	_, err := s.db.db.Exec(`
		INSERT INTO proxy_registry (id, name, type, host, port, username, password, region, notes, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
	`, p.ID, p.Name, p.Type, p.Host, p.Port, p.Username, p.Password, p.Region, p.Notes, p.Status)

	return err
}

// DeleteProxy removes a proxy from the registry and any assignments referencing it.
func (s *ProxyStore) DeleteProxy(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.db == nil || s.db.db == nil {
		return fmt.Errorf("database not initialized")
	}

	_, _ = s.db.db.Exec(`DELETE FROM proxy_assignments WHERE proxy_id = ?`, id)
	_, err := s.db.db.Exec(`DELETE FROM proxy_registry WHERE id = ?`, id)
	return err
}
