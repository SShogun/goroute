package openai

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ziuus/goroute/pkg/providers"
)

func TestOpenAIProviderChatCompletion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-secret" {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		if r.URL.Path != "/chat/completions" {
			http.Error(w, "Not found", http.StatusNotFound)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"id":"chatcmpl-123","choices":[{"message":{"role":"assistant","content":"hello"}}]}`))
	}))
	defer server.Close()

	prov := NewProvider("test-provider", server.URL, "test-secret", nil)

	req := &providers.ChatRequest{
		Model: "gpt-4o",
		Messages: []providers.ChatMessage{
			{Role: "user", Content: "hi"},
		},
	}

	body, isStream, statusCode, err := prov.ChatCompletion(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer body.Close()

	if statusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", statusCode)
	}
	if isStream {
		t.Errorf("expected isStream false")
	}

	respBytes, _ := io.ReadAll(body)
	if len(respBytes) == 0 {
		t.Errorf("expected non-empty response")
	}
}
