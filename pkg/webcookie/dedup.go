package webcookie

import (
	"sort"
	"strings"
)

var webCookieProviders = map[string]bool{
	"chatgpt-web":             true,
	"claude-web":              true,
	"grok-web":                true,
	"deepseek-web":            true,
	"qwen-web":                true,
	"gemini-web":              true,
	"kimi-web":                true,
	"stepfun-web":             true,
	"baichuan-web":            true,
	"coze-web":                true,
	"zhipu-web":               true,
	"minimax-web":             true,
	"lingyi-web":              true,
	"ollama-cloud":            true,
	"volc-console":            true,
	"alibaba-console":         true,
	"xiaomi-mimo":             true,
}

// IsWebCookieProvider reports whether the provider uses cookie/session authentication.
func IsWebCookieProvider(provider string) bool {
	return webCookieProviders[strings.ToLower(strings.TrimSpace(provider))]
}

var preferredCredentialKeys = []string{
	"cookie",
	"token",
	"sessionToken",
	"session-token",
	"sso",
	"access_token",
	"accessToken",
	"authCookie",
	"qwenCloudCookie",
	"volcConsoleCookie",
	"alibabaConsoleCookie",
	"xiaomiMimoConsoleCookie",
	"ollamaCloudCookie",
	"ollamaCloudUsageCookie",
	"ollamaUsageCookie",
	"usageCookie",
}

// WebSessionCredentialKey extracts a comparable secret token from providerSpecificData.
func WebSessionCredentialKey(psd map[string]interface{}) string {
	if len(psd) == 0 {
		return ""
	}

	for _, key := range preferredCredentialKeys {
		if val, ok := psd[key]; ok {
			if s, ok := val.(string); ok && strings.TrimSpace(s) != "" {
				return strings.TrimSpace(s)
			}
		}
	}

	// Alphabetical fallback
	var keys []string
	for k := range psd {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		if val, ok := psd[k]; ok {
			if s, ok := val.(string); ok && strings.TrimSpace(s) != "" {
				return strings.TrimSpace(s)
			}
		}
	}
	return ""
}
