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
