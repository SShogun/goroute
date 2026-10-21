package oauth

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// CliProxyTypeToProvider maps CLIProxyAPI types to GoRoute provider IDs.
var CliProxyTypeToProvider = map[string]string{
	"anthropic":   "claude",
	"claude":      "claude",
	"codex":       "codex",
	"antigravity": "antigravity",
	"kimi":        "kimi",
	"meta":        "muse-code",
}

// ParsedCliProxyAuth represents a normalized credential from CLIProxyAPI.
type ParsedCliProxyAuth struct {
	Provider             string                 `json:"provider"`
	Type                 string                 `json:"type"`
	Email                string                 `json:"email,omitempty"`
	AccessToken          string                 `json:"access_token,omitempty"`
	RefreshToken         string                 `json:"refresh_token,omitempty"`
	ExpiresAt            string                 `json:"expires_at,omitempty"`
	ProjectID            string                 `json:"project_id,omitempty"`
	ProviderSpecificData map[string]interface{} `json:"provider_specific_data,omitempty"`
}

// DefaultCliProxyDir returns ~/.cli-proxy-api or CLIPROXYAPI_CONFIG_DIR env var.
func DefaultCliProxyDir() string {
	if custom := os.Getenv("CLIPROXYAPI_CONFIG_DIR"); custom != "" {
		return custom
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".cli-proxy-api"
	}
	return filepath.Join(home, ".cli-proxy-api")
}

// resolveExpiry converts expiry timestamp or relative seconds to ISO8601 string.
func resolveExpiry(raw map[string]interface{}, now time.Time) string {
	if exp, ok := raw["expired"]; ok && exp != nil {
		switch v := exp.(type) {
		case string:
			if t, err := time.Parse(time.RFC3339, strings.TrimSpace(v)); err == nil {
				return t.Format(time.RFC3339)
			}
		case float64:
			if v > 0 {
				var t time.Time
				if v < 1e12 {
					t = time.Unix(int64(v), 0)
				} else {
					t = time.UnixMilli(int64(v))
				}
				return t.Format(time.RFC3339)
			}
		}
	}

	if expIn, ok := raw["expires_in"]; ok && expIn != nil {
		if sec, ok := expIn.(float64); ok && sec > 0 {
			return now.Add(time.Duration(sec) * time.Second).Format(time.RFC3339)
		}
	}
	return ""
}

// ParseCliProxyAuthRecord parses one json raw map into a ParsedCliProxyAuth or returns nil.
func ParseCliProxyAuthRecord(raw map[string]interface{}, now time.Time) *ParsedCliProxyAuth {
	rawType, _ := raw["type"].(string)
	rawType = strings.ToLower(strings.TrimSpace(rawType))
	if rawType == "" {
		return nil
	}

	provider, exists := CliProxyTypeToProvider[rawType]
	if !exists {
		return nil
	}

	email, _ := raw["email"].(string)
	accessToken, _ := raw["access_token"].(string)
	refreshToken, _ := raw["refresh_token"].(string)
	projectID, _ := raw["project_id"].(string)
	if projectID == "" {
		projectID, _ = raw["projectId"].(string)
	}

	if provider == "muse-code" {
		dcaToken, _ := raw["dca_token"].(string)
		if dcaToken == "" && strings.HasPrefix(accessToken, "dca:") {
			dcaToken = accessToken
		}
		apiKey, _ := raw["api_key"].(string)
		inferenceKey := ""
		if apiKey != "" && !strings.HasPrefix(apiKey, "dca:") {
			inferenceKey = apiKey
		} else if accessToken != "" && !strings.HasPrefix(accessToken, "dca:") {
			inferenceKey = accessToken
		}

		if inferenceKey == "" && dcaToken == "" {
			return nil
		}

		expiresAt := ""
		if inferenceKey == "" {
			expiresAt = resolveExpiry(raw, now)
		}

		psd := map[string]interface{}{
			"importedFrom": "cliproxyapi",
			"authKind":     "oauth",
		}
		if dcaToken != "" {
			psd["dcaToken"] = dcaToken
		}
		if base, ok := raw["base_url"].(string); ok && base != "" {
			psd["baseUrl"] = base
		}
		if email != "" {
			psd["email"] = email
		}
		if name, ok := raw["name"].(string); ok && name != "" {
			psd["name"] = name
		}

		finalToken := inferenceKey
		if finalToken == "" {
			finalToken = dcaToken
		}

		return &ParsedCliProxyAuth{
			Provider:             provider,
			Type:                 rawType,
			Email:                email,
			AccessToken:          finalToken,
			RefreshToken:         dcaToken,
			ExpiresAt:            expiresAt,
			ProviderSpecificData: psd,
		}
	}

	if strings.TrimSpace(accessToken) == "" {
		return nil
	}

	return &ParsedCliProxyAuth{
		Provider:     provider,
		Type:         rawType,
		Email:        email,
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		ExpiresAt:    resolveExpiry(raw, now),
		ProjectID:    projectID,
		ProviderSpecificData: map[string]interface{}{
			"importedFrom": "cliproxyapi",
			"importedAt":   time.Now().UTC().Format(time.RFC3339),
		},
	}
}

// ScanCliProxyAuthDir scans the given directory for all *.json credentials.
func ScanCliProxyAuthDir(dir string) ([]ParsedCliProxyAuth, int, int, error) {
	if dir == "" {
		dir = DefaultCliProxyDir()
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []ParsedCliProxyAuth{}, 0, 0, nil
		}
		return nil, 0, 0, fmt.Errorf("failed to read dir: %w", err)
	}

	var candidates []ParsedCliProxyAuth
	scanned := 0
	skipped := 0
	now := time.Now()

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".json") {
			continue
		}
		scanned++

		filePath := filepath.Join(dir, entry.Name())
		data, err := os.ReadFile(filePath)
		if err != nil {
			skipped++
			continue
		}

		var raw map[string]interface{}
		if err := json.Unmarshal(data, &raw); err != nil {
			skipped++
			continue
		}

		parsed := ParseCliProxyAuthRecord(raw, now)
		if parsed == nil {
			skipped++
			continue
		}

		candidates = append(candidates, *parsed)
	}

	return candidates, skipped, scanned, nil
}
