package storage

import (
	"encoding/json"
	"fmt"
	"sync"
)

// ModelPricing represents token rates per million tokens.
type ModelPricing struct {
	Input  float64 `json:"input"`
	Output float64 `json:"output"`
	Cached float64 `json:"cached,omitempty"`
}

// PricingStore manages default and custom token pricing per provider and model.
type PricingStore struct {
	db *DBStore
	mu sync.RWMutex
}

var (
	globalPricingStore *PricingStore
	pricingOnce        sync.Once

	defaultPricing = map[string]map[string]ModelPricing{
		"openai": {
			"gpt-4o":      {Input: 2.50, Output: 10.00, Cached: 1.25},
			"gpt-4o-mini": {Input: 0.15, Output: 0.60, Cached: 0.075},
			"o1":          {Input: 15.00, Output: 60.00, Cached: 7.50},
			"o3-mini":     {Input: 1.10, Output: 4.40, Cached: 0.55},
		},
		"anthropic": {
			"claude-3-7-sonnet": {Input: 3.00, Output: 15.00, Cached: 0.30},
			"claude-3-5-sonnet": {Input: 3.00, Output: 15.00, Cached: 0.30},
			"claude-3-5-haiku":  {Input: 0.80, Output: 4.00, Cached: 0.08},
			"claude-opus-4-8":   {Input: 15.00, Output: 75.00, Cached: 1.50},
		},
		"google": {
			"gemini-2.5-pro":       {Input: 1.25, Output: 5.00, Cached: 0.31},
			"gemini-2.5-flash":     {Input: 0.075, Output: 0.30, Cached: 0.018},
			"gemini-3.1-flash-lite": {Input: 0.05, Output: 0.20, Cached: 0.01},
		},
		"deepseek": {
			"deepseek-chat":   {Input: 0.14, Output: 0.28, Cached: 0.014},
			"deepseek-reasoner": {Input: 0.55, Output: 2.19, Cached: 0.14},
		},
	}
)

func GetPricingStore() *PricingStore {
	pricingOnce.Do(func() {
		globalPricingStore = &PricingStore{
			db: GetDB(),
		}
	})
	return globalPricingStore
}

func (s *PricingStore) GetPricing() map[string]map[string]ModelPricing {
	s.mu.RLock()
	defer s.mu.RUnlock()

	merged := make(map[string]map[string]ModelPricing)
	for p, models := range defaultPricing {
		merged[p] = make(map[string]ModelPricing)
		for m, rate := range models {
			merged[p][m] = rate
		}
	}

	if s.db == nil || s.db.db == nil {
		return merged
	}

	var rawUser string
	err := s.db.db.QueryRow(`SELECT value FROM key_value WHERE namespace = 'settings' AND key = 'userPricing'`).Scan(&rawUser)
	if err == nil && rawUser != "" {
		var userOverrides map[string]map[string]ModelPricing
		if json.Unmarshal([]byte(rawUser), &userOverrides) == nil {
			for p, models := range userOverrides {
				if _, ok := merged[p]; !ok {
					merged[p] = make(map[string]ModelPricing)
				}
				for m, rate := range models {
					merged[p][m] = rate
				}
			}
		}
	}

	return merged
}

func (s *PricingStore) UpdatePricing(overrides map[string]map[string]ModelPricing) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.db == nil || s.db.db == nil {
		return fmt.Errorf("database not initialized")
	}

	bytes, err := json.Marshal(overrides)
	if err != nil {
		return err
	}

	_, err = s.db.db.Exec(`
		INSERT OR REPLACE INTO key_value (namespace, key, value)
		VALUES ('settings', 'userPricing', ?)
	`, string(bytes))
	return err
}
