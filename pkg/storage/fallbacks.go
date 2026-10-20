package storage

import (
	"encoding/json"
	"sync"
)

// FallbackChainMap stores model -> []targetModel failover sequences.
type FallbackChainMap struct {
	Chains map[string][]string `json:"chains"`
}

var (
	fallbackMu sync.RWMutex
	fallbacks  = make(map[string][]string)
)

func GetAllFallbackChains() map[string][]string {
	fallbackMu.RLock()
	defer fallbackMu.RUnlock()
	res := make(map[string][]string)
	for k, v := range fallbacks {
		cp := make([]string, len(v))
		copy(cp, v)
		res[k] = cp
	}
	return res
}

func RegisterFallbackChain(db *DBStore, model string, chain []string) error {
	fallbackMu.Lock()
	defer fallbackMu.Unlock()

	fallbacks[model] = chain
	if db != nil && db.db != nil {
		bytes, err := json.Marshal(fallbacks)
		if err != nil {
			return err
		}
		_, err = db.db.Exec(`
			INSERT OR REPLACE INTO key_value (namespace, key, value)
			VALUES ('settings', 'fallbackChains', ?)
		`, string(bytes))
		return err
	}
	return nil
}

func RemoveFallbackChain(db *DBStore, model string) (bool, error) {
	fallbackMu.Lock()
	defer fallbackMu.Unlock()

	_, exists := fallbacks[model]
	if !exists {
		return false, nil
	}
	delete(fallbacks, model)

	if db != nil && db.db != nil {
		bytes, err := json.Marshal(fallbacks)
		if err != nil {
			return true, err
		}
		_, err = db.db.Exec(`
			INSERT OR REPLACE INTO key_value (namespace, key, value)
			VALUES ('settings', 'fallbackChains', ?)
		`, string(bytes))
		return true, err
	}
	return true, nil
}

func InitFallbackChains(db *DBStore) {
	if db == nil || db.db == nil {
		return
	}
	var val string
	err := db.db.QueryRow(`SELECT value FROM key_value WHERE namespace = 'settings' AND key = 'fallbackChains'`).Scan(&val)
	if err == nil && val != "" {
		var m map[string][]string
		if json.Unmarshal([]byte(val), &m) == nil {
			fallbackMu.Lock()
			fallbacks = m
			fallbackMu.Unlock()
		}
	}
}

// ResolveFallbackModel checks if a model has a fallback chain configured.
func ResolveFallbackChain(model string) []string {
	fallbackMu.RLock()
	defer fallbackMu.RUnlock()
	if chain, exists := fallbacks[model]; exists {
		return chain
	}
	return nil
}
