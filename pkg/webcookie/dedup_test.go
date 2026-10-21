package webcookie

import (
	"testing"
)

func TestWebCookieDedup(t *testing.T) {
	if !IsWebCookieProvider("chatgpt-web") {
		t.Errorf("expected chatgpt-web to be recognized")
	}
	if IsWebCookieProvider("openai") {
		t.Errorf("expected openai not to be recognized as webcookie")
	}

	data := map[string]interface{}{
		"username": "testuser",
		"token":    "my-secret-session-token",
	}

	key := WebSessionCredentialKey(data)
	if key != "my-secret-session-token" {
		t.Errorf("expected my-secret-session-token, got %s", key)
	}
}
