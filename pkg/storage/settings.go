package storage

import (
	"encoding/json"
	"fmt"
	"sync"
)

// SystemSettings matches OmniRoute's complete key_value runtime settings map.
type SystemSettings struct {
	CloudEnabled                            bool              `json:"cloudEnabled"`
	TailscaleEnabled                        bool              `json:"tailscaleEnabled"`
	TailscaleURL                            string            `json:"tailscaleUrl"`
	StickyRoundRobinLimit                   int               `json:"stickyRoundRobinLimit"`
	DisableSessionStickiness                bool              `json:"disableSessionStickiness"`
	ConnectionAwareExpansion                bool              `json:"connectionAwareExpansion"`
	PromptCacheAffinityEnabled              bool              `json:"promptCacheAffinityEnabled"`
	ComboStrategy                           string            `json:"comboStrategy"`
	ComboStickyRoundRobinLimit              *int              `json:"comboStickyRoundRobinLimit,omitempty"`
	ProviderStrategies                      map[string]string `json:"providerStrategies"`
	RequestRetry                            int               `json:"requestRetry"`
	MaxRetryIntervalSec                     int               `json:"maxRetryIntervalSec"`
	RequireLogin                            bool              `json:"requireLogin"`
	CredentialRedactionEnabled              bool              `json:"credentialRedactionEnabled"`
	PiiRedactionEnabled                     bool              `json:"piiRedactionEnabled"`
	AutoRefreshProviderQuota                bool              `json:"autoRefreshProviderQuota"`
	AutoRefreshProviderQuotaInterval        int               `json:"autoRefreshProviderQuotaInterval"`
	ComboConfigMode                         string            `json:"comboConfigMode"`
	ResponsesPreviousResponseIDMode         string            `json:"responsesPreviousResponseIdMode"`
	DebugMode                               bool              `json:"debugMode"`
	ProxyEnabled                            bool              `json:"proxyEnabled"`
	CustomSystemPromptEnabled               bool              `json:"customSystemPromptEnabled"`
	CustomSystemPrompt                      string            `json:"customSystemPrompt"`
	PreferClaudeCodeForUnprefixedClaude     bool              `json:"preferClaudeCodeForUnprefixedClaudeModels"`
	ClaudeClassifierCompat                  string            `json:"claudeClassifierCompat"`
	SessionAffinityTTLMs                    int               `json:"sessionAffinityTtlMs"`
	LocalOnlyManageScopeBypassEnabled       bool              `json:"localOnlyManageScopeBypassEnabled"`
	HiddenSidebarItems                      []string          `json:"hiddenSidebarItems"`
}

// DefaultSettings returns full default values conforming to OmniRoute core defaults.
func DefaultSettings() SystemSettings {
	return SystemSettings{
		CloudEnabled:                       false,
		TailscaleEnabled:                   false,
		TailscaleURL:                       "",
		StickyRoundRobinLimit:              3,
		DisableSessionStickiness:           false,
		ConnectionAwareExpansion:           false,
		PromptCacheAffinityEnabled:         true,
		ComboStrategy:                      "fallback",
		ProviderStrategies:                 make(map[string]string),
		RequestRetry:                       3,
		MaxRetryIntervalSec:                30,
		RequireLogin:                       false,
		CredentialRedactionEnabled:         true,
		PiiRedactionEnabled:                true,
		AutoRefreshProviderQuota:           false,
		AutoRefreshProviderQuotaInterval:   180,
		ComboConfigMode:                    "guided",
		ResponsesPreviousResponseIDMode:    "off",
		DebugMode:                          false,
		ProxyEnabled:                       true,
		CustomSystemPromptEnabled:          false,
		CustomSystemPrompt:                 "",
		PreferClaudeCodeForUnprefixedClaude: true,
		ClaudeClassifierCompat:             "off",
		SessionAffinityTTLMs:               300000,
		LocalOnlyManageScopeBypassEnabled:  true,
		HiddenSidebarItems:                 []string{},
	}
}

// SettingsStore manages persistent settings in SQLite key_value table.
type SettingsStore struct {
	db *DBStore
	mu sync.RWMutex
}

var (
	globalSettingsStore *SettingsStore
	settingsOnce        sync.Once
)

// GetSettingsStore returns the singleton settings store.
func GetSettingsStore() *SettingsStore {
	settingsOnce.Do(func() {
		globalSettingsStore = &SettingsStore{
			db: GetDB(),
		}
		globalSettingsStore.ensureTable()
	})
	return globalSettingsStore
}

func (s *SettingsStore) ensureTable() {
	if s.db == nil || s.db.db == nil {
		return
	}
	_, _ = s.db.db.Exec(`
		CREATE TABLE IF NOT EXISTS key_value (
			namespace TEXT NOT NULL,
			key TEXT NOT NULL,
			value TEXT NOT NULL,
			PRIMARY KEY (namespace, key)
		);
	`)
}

// LoadSettings reads merged settings (defaults + SQLite overrides).
func (s *SettingsStore) LoadSettings() (SystemSettings, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	settings := DefaultSettings()
	if s.db == nil || s.db.db == nil {
		return settings, nil
	}

	rows, err := s.db.db.Query(`SELECT key, value FROM key_value WHERE namespace = 'settings'`)
	if err != nil {
		return settings, err
	}
	defer rows.Close()

	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			continue
		}
		switch k {
		case "cloudEnabled":
			settings.CloudEnabled = (v == "true")
		case "tailscaleEnabled":
			settings.TailscaleEnabled = (v == "true")
		case "tailscaleUrl":
			settings.TailscaleURL = v
		case "stickyRoundRobinLimit":
			var limit int
			fmt.Sscanf(v, "%d", &limit)
			if limit > 0 {
				settings.StickyRoundRobinLimit = limit
			}
		case "disableSessionStickiness":
			settings.DisableSessionStickiness = (v == "true")
		case "promptCacheAffinityEnabled":
			settings.PromptCacheAffinityEnabled = (v == "true")
		case "comboStrategy":
			settings.ComboStrategy = v
		case "credentialRedactionEnabled":
			settings.CredentialRedactionEnabled = (v == "true")
		case "piiRedactionEnabled":
			settings.PiiRedactionEnabled = (v == "true")
		case "proxyEnabled":
			settings.ProxyEnabled = (v == "true")
		case "debugMode":
			settings.DebugMode = (v == "true")
		case "customSystemPromptEnabled":
			settings.CustomSystemPromptEnabled = (v == "true")
		case "customSystemPrompt":
			settings.CustomSystemPrompt = v
		case "claudeClassifierCompat":
			settings.ClaudeClassifierCompat = v
		case "sessionAffinityTtlMs":
			var ttl int
			fmt.Sscanf(v, "%d", &ttl)
			if ttl > 0 {
				settings.SessionAffinityTTLMs = ttl
			}
		case "providerStrategies":
			var m map[string]string
			if err := json.Unmarshal([]byte(v), &m); err == nil {
				settings.ProviderStrategies = m
			}
		}
	}

	return settings, nil
}

// UpdateKey writes a specific setting key into key_value table.
func (s *SettingsStore) UpdateKey(key string, val any) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.db == nil || s.db.db == nil {
		return fmt.Errorf("database not initialized")
	}

	var strVal string
	switch v := val.(type) {
	case string:
		strVal = v
	case bool:
		strVal = fmt.Sprintf("%v", v)
	case int, int64, float64:
		strVal = fmt.Sprintf("%v", v)
	default:
		bytes, err := json.Marshal(val)
		if err != nil {
			return err
		}
		strVal = string(bytes)
	}

	_, err := s.db.db.Exec(`
		INSERT OR REPLACE INTO key_value (namespace, key, value)
		VALUES ('settings', ?, ?)
	`, key, strVal)

	return err
}
