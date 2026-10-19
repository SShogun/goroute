package antigravity

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ziuus/goroute/pkg/providers"
)

func TestAntigravityTokenRefresh(t *testing.T) {
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{
			"access_token": "new-refreshed-token",
			"expires_in": 3600
		}`))
	}))
	defer tokenServer.Close()

	acc := &Account{
		ID:           "test-acc-1",
		RefreshToken: "initial-refresh-token",
		AccessToken:  "old-token",
		ExpiresAt:    time.Now().Add(-1 * time.Hour), // Expired!
		TokenURL:     tokenServer.URL,
	}

	token, err := acc.GetValidAccessToken(context.Background())
	if err != nil {
		t.Fatalf("unexpected error getting token: %v", err)
	}

	if token != "new-refreshed-token" {
		t.Errorf("expected 'new-refreshed-token', got '%s'", token)
	}
}

func TestAntigravityChatCompletion(t *testing.T) {
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer valid-token" {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`data: {"response":{"candidates":[{"content":{"parts":[{"text":"Hello from Antigravity!"}]}}]}}` + "\n\n"))
	}))
	defer apiServer.Close()

	acc := &Account{
		ID:          "test-acc-1",
		AccessToken: "valid-token",
		ExpiresAt:   time.Now().Add(1 * time.Hour),
		ProjectID:   "test-project-123",
	}

	prov := NewProvider("antigravity-test", []*Account{acc})
	prov.baseURL = apiServer.URL

	req := &providers.ChatRequest{
		Model: "gemini-2.5-flash",
		Messages: []providers.ChatMessage{
			{Role: "user", Content: "hello"},
		},
	}

	body, _, statusCode, err := prov.ChatCompletion(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer body.Close()

	if statusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", statusCode)
	}

	respBytes, _ := io.ReadAll(body)
	if len(respBytes) == 0 {
		t.Errorf("expected non-empty response")
	}
}

func TestMultimodalMediaParsing(t *testing.T) {
	// Test data URI extraction
	inline := extractInlineDataFromURL("data:image/png;base64,iVBORw0KGgoAAAANSUhEUg==")
	if inline == nil {
		t.Fatalf("expected extracted inline data, got nil")
	}
	if inline.MimeType != "image/png" {
		t.Errorf("expected image/png, got %s", inline.MimeType)
	}
	if inline.Data != "iVBORw0KGgoAAAANSUhEUg==" {
		t.Errorf("expected base64 data, got %s", inline.Data)
	}

	// Test OpenAI multimodal array
	openAIMedia := []interface{}{
		map[string]interface{}{"type": "text", "text": "Describe this image:"},
		map[string]interface{}{
			"type": "image_url",
			"image_url": map[string]interface{}{
				"url": "data:image/jpeg;base64,12345678",
			},
		},
	}
	parts := parseGeminiParts(openAIMedia, nil)
	if len(parts) != 2 {
		t.Fatalf("expected 2 parts, got %d", len(parts))
	}
	if parts[0].Text != "Describe this image:" {
		t.Errorf("expected text part, got: %v", parts[0].Text)
	}
	if parts[1].InlineData == nil || parts[1].InlineData.MimeType != "image/jpeg" {
		t.Errorf("expected image/jpeg inline data, got: %v", parts[1].InlineData)
	}

	// Test Anthropic multimodal block
	anthropicMedia := []interface{}{
		map[string]interface{}{"type": "text", "text": "Analyze diagram:"},
		map[string]interface{}{
			"type": "image",
			"source": map[string]interface{}{
				"type":       "base64",
				"media_type": "image/webp",
				"data":       "abcdef==",
			},
		},
	}
	anthropicParts := parseGeminiParts(anthropicMedia, nil)
	if len(anthropicParts) != 2 {
		t.Fatalf("expected 2 parts, got %d", len(anthropicParts))
	}
	if anthropicParts[1].InlineData == nil || anthropicParts[1].InlineData.MimeType != "image/webp" {
		t.Errorf("expected image/webp inline data, got: %v", anthropicParts[1].InlineData)
	}
}
