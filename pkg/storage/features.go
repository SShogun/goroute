package storage

import (
	"encoding/json"
	"net"
	"strings"
	"sync"
)

// SystemPromptConfig holds prefix, suffix, and master toggle for prompt injection.
type SystemPromptConfig struct {
	Enabled      bool   `json:"enabled"`
	PrefixPrompt string `json:"prefixPrompt"`
	SuffixPrompt string `json:"suffixPrompt"`
}

var (
	systemPromptMu sync.RWMutex
	systemPrompt   = SystemPromptConfig{
		Enabled:      false,
		PrefixPrompt: "",
		SuffixPrompt: "",
	}
)

func GetSystemPromptConfig() SystemPromptConfig {
	systemPromptMu.RLock()
	defer systemPromptMu.RUnlock()
	return systemPrompt
}

func SetSystemPromptConfig(db *DBStore, cfg SystemPromptConfig) error {
	systemPromptMu.Lock()
	defer systemPromptMu.Unlock()
	systemPrompt = cfg
	if db != nil && db.db != nil {
		bytes, err := json.Marshal(cfg)
		if err != nil {
			return err
		}
		_, err = db.db.Exec(`
			INSERT OR REPLACE INTO key_value (namespace, key, value)
			VALUES ('settings', 'systemPrompt', ?)
		`, string(bytes))
		return err
	}
	return nil
}

// IPFilterConfig holds blacklist, whitelist, and IP enforcement mode.
type IPFilterConfig struct {
	Enabled   bool     `json:"enabled"`
	Mode      string   `json:"mode"` // "blacklist" or "whitelist"
	Blacklist []string `json:"blacklist"`
	Whitelist []string `json:"whitelist"`
}

var (
	ipFilterMu sync.RWMutex
	ipFilter   = IPFilterConfig{
		Enabled:   false,
		Mode:      "blacklist",
		Blacklist: []string{},
		Whitelist: []string{},
	}
)

func GetIPFilterConfig() IPFilterConfig {
	ipFilterMu.RLock()
	defer ipFilterMu.RUnlock()
	return ipFilter
}

func SetIPFilterConfig(db *DBStore, cfg IPFilterConfig) error {
	ipFilterMu.Lock()
	defer ipFilterMu.Unlock()
	ipFilter = cfg
	if db != nil && db.db != nil {
		bytes, err := json.Marshal(cfg)
		if err != nil {
			return err
		}
		_, err = db.db.Exec(`
			INSERT OR REPLACE INTO key_value (namespace, key, value)
			VALUES ('settings', 'ipFilter', ?)
		`, string(bytes))
		return err
	}
	return nil
}

func IsIPAllowed(ipStr string) bool {
	ipFilterMu.RLock()
	defer ipFilterMu.RUnlock()

	if !ipFilter.Enabled {
		return true
	}

	host, _, err := net.SplitHostPort(ipStr)
	if err == nil {
		ipStr = host
	}
	ipStr = strings.TrimSpace(ipStr)

	if ipFilter.Mode == "whitelist" {
		for _, w := range ipFilter.Whitelist {
			if w == ipStr {
				return true
			}
		}
		return false
	}

	// Mode is blacklist
	for _, b := range ipFilter.Blacklist {
		if b == ipStr {
			return false
		}
	}
	return true
}

// CacheConfig matches OmniRoute's semantic cache & prompt cache settings.
type CacheConfig struct {
	SemanticCacheEnabled   bool    `json:"semanticCacheEnabled"`
	SemanticCacheTTL       int     `json:"semanticCacheTTL"`
	SemanticCacheThreshold float64 `json:"semanticCacheThreshold"`
	PromptCacheEnabled     bool    `json:"promptCacheEnabled"`
	PromptCacheStrategy    string  `json:"promptCacheStrategy"` // "auto", "system-only", "manual"
	IdempotencyWindowMs    int     `json:"idempotencyWindowMs"`
}

var (
	cacheMu     sync.RWMutex
	cacheConfig = CacheConfig{
		SemanticCacheEnabled:   false,
		SemanticCacheTTL:       3600,
		SemanticCacheThreshold: 0.95,
		PromptCacheEnabled:     true,
		PromptCacheStrategy:    "auto",
		IdempotencyWindowMs:    5000,
	}
)

func GetCacheConfig() CacheConfig {
	cacheMu.RLock()
	defer cacheMu.RUnlock()
	return cacheConfig
}

func SetCacheConfig(db *DBStore, cfg CacheConfig) error {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	cacheConfig = cfg
	if db != nil && db.db != nil {
		bytes, err := json.Marshal(cfg)
		if err != nil {
			return err
		}
		_, err = db.db.Exec(`
			INSERT OR REPLACE INTO key_value (namespace, key, value)
			VALUES ('settings', 'cacheConfig', ?)
		`, string(bytes))
		return err
	}
	return nil
}
