package storage

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// DiscoveryRecord captures findings from automatic provider model discovery.
type DiscoveryRecord struct {
	ID           int       `json:"id"`
	ProviderID   string    `json:"providerId"`
	Method       string    `json:"method"`    // "free_tier", "web_cookie", "public_api", "trial"
	Endpoint     string    `json:"endpoint,omitempty"`
	AuthType     string    `json:"authType"`  // "none", "cookie", "api_key", "oauth"
	Models       []string  `json:"models"`
	RateLimit    string    `json:"rateLimit,omitempty"`
	Feasibility  int       `json:"feasibility"` // 0-100
	RiskLevel    string    `json:"riskLevel"`   // "none", "low", "medium", "high"
	Status       string    `json:"status"`      // "pending", "testing", "verified", "rejected"
	Notes        string    `json:"notes,omitempty"`
	DiscoveredAt time.Time `json:"discoveredAt"`
	VerifiedAt   *time.Time `json:"verifiedAt,omitempty"`
}

// DiscoveryStore manages discovery_results table in SQLite.
type DiscoveryStore struct {
	db *DBStore
	mu sync.RWMutex
}

var (
	globalDiscoveryStore *DiscoveryStore
	discoveryOnce        sync.Once
)

func GetDiscoveryStore() *DiscoveryStore {
	discoveryOnce.Do(func() {
		globalDiscoveryStore = &DiscoveryStore{
			db: GetDB(),
		}
		globalDiscoveryStore.ensureTable()
	})
	return globalDiscoveryStore
}

func (s *DiscoveryStore) ensureTable() {
	if s.db == nil || s.db.db == nil {
		return
	}
	_, _ = s.db.db.Exec(`
		CREATE TABLE IF NOT EXISTS discovery_results (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			provider_id TEXT NOT NULL,
			method TEXT NOT NULL,
			endpoint TEXT,
			auth_type TEXT,
			models TEXT,
			rate_limit TEXT,
			feasibility INTEGER DEFAULT 50,
			risk_level TEXT DEFAULT 'none',
			status TEXT NOT NULL DEFAULT 'pending',
			notes TEXT,
			discovered_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			verified_at DATETIME
		);

		CREATE INDEX IF NOT EXISTS idx_discovery_provider ON discovery_results(provider_id);
		CREATE INDEX IF NOT EXISTS idx_discovery_status ON discovery_results(status);
	`)
}

func (s *DiscoveryStore) ListResults(providerID string) ([]DiscoveryRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var results []DiscoveryRecord
	if s.db == nil || s.db.db == nil {
		return results, nil
	}

	var query string
	var args []any
	if providerID != "" {
		query = `SELECT id, provider_id, method, endpoint, auth_type, models, rate_limit, feasibility, risk_level, status, notes, discovered_at, verified_at FROM discovery_results WHERE provider_id = ? ORDER BY discovered_at DESC`
		args = append(args, providerID)
	} else {
		query = `SELECT id, provider_id, method, endpoint, auth_type, models, rate_limit, feasibility, risk_level, status, notes, discovered_at, verified_at FROM discovery_results ORDER BY discovered_at DESC`
	}

	rows, err := s.db.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var d DiscoveryRecord
		var endp, auth, mods, rl, notes, verStr *string
		var feas *int
		var risk *string
		var discStr string

		if err := rows.Scan(
			&d.ID,
			&d.ProviderID,
			&d.Method,
			&endp,
			&auth,
			&mods,
			&rl,
			&feas,
			&risk,
			&d.Status,
			&notes,
			&discStr,
			&verStr,
		); err == nil {
			if endp != nil {
				d.Endpoint = *endp
			}
			if auth != nil {
				d.AuthType = *auth
			}
			if mods != nil {
				_ = json.Unmarshal([]byte(*mods), &d.Models)
			}
			if rl != nil {
				d.RateLimit = *rl
			}
			if feas != nil {
				d.Feasibility = *feas
			}
			if risk != nil {
				d.RiskLevel = *risk
			}
			if notes != nil {
				d.Notes = *notes
			}
			d.DiscoveredAt, _ = time.Parse(time.RFC3339, discStr)
			if verStr != nil {
				t, _ := time.Parse(time.RFC3339, *verStr)
				d.VerifiedAt = &t
			}
			results = append(results, d)
		}
	}
	return results, nil
}

func (s *DiscoveryStore) RecordDiscovery(rec DiscoveryRecord) (DiscoveryRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.db == nil || s.db.db == nil {
		return rec, fmt.Errorf("database not initialized")
	}

	modsBytes, _ := json.Marshal(rec.Models)
	rec.DiscoveredAt = time.Now().UTC()
	if rec.Status == "" {
		rec.Status = "pending"
	}
	if rec.RiskLevel == "" {
		rec.RiskLevel = "none"
	}

	res, err := s.db.db.Exec(`
		INSERT INTO discovery_results (provider_id, method, endpoint, auth_type, models, rate_limit, feasibility, risk_level, status, notes, discovered_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, rec.ProviderID, rec.Method, rec.Endpoint, rec.AuthType, string(modsBytes), rec.RateLimit, rec.Feasibility, rec.RiskLevel, rec.Status, rec.Notes, rec.DiscoveredAt.Format(time.RFC3339))
	if err != nil {
		return rec, err
	}

	id, _ := res.LastInsertId()
	rec.ID = int(id)
	return rec, nil
}
