package webcookie

import (
	"encoding/json"
	"fmt"
	"strings"
)

// RawCookie represents a cookie entry exported by standard DevTools or browser extensions.
type RawCookie struct {
	Name     string `json:"name"`
	Value    string `json:"value"`
	Domain   string `json:"domain,omitempty"`
	Path     string `json:"path,omitempty"`
	Expires  any    `json:"expires,omitempty"`
	HttpOnly bool   `json:"httpOnly,omitempty"`
	Secure   bool   `json:"secure,omitempty"`
	SameSite string `json:"sameSite,omitempty"`
}

// StripCookieInputPrefix trims leading Bearer or Cookie: labels from user input.
func StripCookieInputPrefix(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}

	if strings.HasPrefix(strings.ToLower(trimmed), "bearer ") {
		trimmed = strings.TrimSpace(trimmed[7:])
	}
	if strings.HasPrefix(strings.ToLower(trimmed), "cookie:") {
		trimmed = strings.TrimSpace(trimmed[7:])
	}
	return trimmed
}

// ParseJsonCookiesToHeader converts raw JSON cookie export into standard "Cookie: name=val; name2=val2" header.
func ParseJsonCookiesToHeader(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if !strings.HasPrefix(trimmed, "[") {
		return StripCookieInputPrefix(raw), nil
	}

	var cookies []RawCookie
	if err := json.Unmarshal([]byte(trimmed), &cookies); err != nil {
		return StripCookieInputPrefix(raw), nil
	}

	var parts []string
	for i, c := range cookies {
		if c.Name == "" {
			return "", fmt.Errorf("invalid cookie JSON at index %d: missing name", i)
		}
		parts = append(parts, fmt.Sprintf("%s=%s", c.Name, c.Value))
	}

	return strings.Join(parts, "; "), nil
}

// ExtractSessionCookie retrieves a specific named cookie from a header string or session string.
func ExtractNamedCookie(cookieHeader string, targetName string) string {
	parts := strings.Split(cookieHeader, ";")
	for _, p := range parts {
		kv := strings.SplitN(strings.TrimSpace(p), "=", 2)
		if len(kv) == 2 && strings.EqualFold(kv[0], targetName) {
			return kv[1]
		}
	}
	return ""
}
