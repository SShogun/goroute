package storage

import (
	"encoding/json"
	"fmt"
	"sync"
)

// ModelCapabilityOverride represents an operator's per-model runtime override.
type ModelCapabilityOverride struct {
	Provider    string `json:"provider"`
	ModelID     string `json:"modelId"`
	Target      string `json:"target"`
	Key         string `json:"key"`   // "context_length", "max_input_tokens", "max_output_tokens", "reasoning_efforts"
	Value       any    `json:"value"` // number or []string
	RefreshedAt string `json:"refreshedAt"`
}

// CapabilityStore manages model overrides persisted in SQLite.
type CapabilityStore struct {
	db *DBStore
	mu sync.RWMutex
}

var (
	globalCapabilityStore *CapabilityStore
	capOnce               sync.Once
)

// GetCapabilityStore returns the singleton capability store.
func GetCapabilityStore() *CapabilityStore {
	capOnce.Do(func() {
		globalCapabilityStore = &CapabilityStore{
			db: GetDB(),
		}
		globalCapabilityStore.ensureTable()
	})
	return globalCapabilityStore
}

func (s *CapabilityStore) ensureTable() {
	if s.db == nil || s.db.db == nil {
		return
	}
	_, _ = s.db.db.Exec(`
		CREATE TABLE IF NOT EXISTS model_capability_overrides (
			provider TEXT NOT NULL,
			model_id TEXT NOT NULL,
			override_key TEXT NOT NULL,
			override_value TEXT NOT NULL,
			refreshed_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (provider, model_id, override_key)
		);
	`)
}

// ListOverrides returns all capability overrides.
func (s *CapabilityStore) ListOverrides() ([]ModelCapabilityOverride, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []ModelCapabilityOverride
	if s.db == nil || s.db.db == nil {
		return result, nil
	}

	rows, err := s.db.db.Query(`
		SELECT provider, model_id, override_key, override_value, refreshed_at
		FROM model_capability_overrides
		ORDER BY provider, model_id, override_key
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var o ModelCapabilityOverride
		var valStr string
		var refStr string
		if err := rows.Scan(&o.Provider, &o.ModelID, &o.Key, &valStr, &refStr); err != nil {
			continue
		}
		o.Target = fmt.Sprintf("%s/%s", o.Provider, o.ModelID)
		o.RefreshedAt = refStr

		var parsed any
		if err := json.Unmarshal([]byte(valStr), &parsed); err == nil {
			o.Value = parsed
		} else {
			o.Value = valStr
		}
		result = append(result, o)
	}

	return result, nil
}

// SetOverride inserts or updates a model capability override.
func (s *CapabilityStore) SetOverride(provider, modelID, key string, val any) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.db == nil || s.db.db == nil {
		return fmt.Errorf("database not initialized")
	}

	valBytes, err := json.Marshal(val)
	if err != nil {
		return err
	}

	_, err = s.db.db.Exec(`
		INSERT INTO model_capability_overrides (provider, model_id, override_key, override_value, refreshed_at)
		VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(provider, model_id, override_key) DO UPDATE SET
			override_value = excluded.override_value,
			refreshed_at = CURRENT_TIMESTAMP
	`, provider, modelID, key, string(valBytes))

	return err
}

// DeleteOverride removes an override.
func (s *CapabilityStore) DeleteOverride(provider, modelID, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.db == nil || s.db.db == nil {
		return fmt.Errorf("database not initialized")
	}

	_, err := s.db.db.Exec(`
		DELETE FROM model_capability_overrides
		WHERE provider = ? AND model_id = ? AND override_key = ?
	`, provider, modelID, key)

	return err
}
