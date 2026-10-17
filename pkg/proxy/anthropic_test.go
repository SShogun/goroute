package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ziuus/goroute/pkg/providers"
	"github.com/ziuus/goroute/pkg/quota"
	"github.com/ziuus/goroute/pkg/router"
)

type mockAnthropicProvider struct{}

func (m *mockAnthropicProvider) ID() string   { return "mock" }
func (m *mockAnthropicProvider) Type() string { return "openai" }
func (m *mockAnthropicProvider) IsHealthy() bool { return true }
func (m *mockAnthropicProvider) Models(ctx context.Context) ([]providers.ModelInfo, error) {
	return []providers.ModelInfo{{ID: "claude-3-7-sonnet"}}, nil
}
func (m *mockAnthropicProvider) Embeddings(ctx context.Context, req *providers.EmbeddingRequest) (*providers.EmbeddingResponse, error) {
	return &providers.EmbeddingResponse{
		Object: "list",
		Model:  req.Model,
		Data:   []providers.EmbeddingItem{{Object: "embedding", Index: 0, Embedding: []float64{0.1, 0.2}}},
	}, nil
}
func (m *mockAnthropicProvider) ChatCompletion(ctx context.Context, req *providers.ChatRequest) (io.ReadCloser, bool, int, error) {
	if req.Stream {
		sseData := "data: {\"choices\":[{\"delta\":{\"content\":\"Hello world\"}}]}\n\ndata: [DONE]\n\n"
		return io.NopCloser(strings.NewReader(sseData)), true, http.StatusOK, nil
	}
	resp := map[string]interface{}{
		"id": "chatcmpl-test",
		"choices": []map[string]interface{}{
			{
				"message": map[string]string{
					"role":    "assistant",
					"content": "Hello from mock",
				},
				"finish_reason": "stop",
			},
		},
		"usage": map[string]int{
			"prompt_tokens":     10,
			"completion_tokens": 15,
		},
	}
	b, _ := json.Marshal(resp)
	return io.NopCloser(bytes.NewReader(b)), false, http.StatusOK, nil
}

func TestAnthropicMessagesHandler(t *testing.T) {
	r := router.NewRouter()
	r.RegisterProvider(&mockAnthropicProvider{})
	limiter := quota.NewLimiter()
	srv := NewServer(r, limiter, false)

	// Test Non-streaming request
	body := `{"model":"claude-3-7-sonnet","messages":[{"role":"user","content":"Hi"}],"stream":false}`
	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(body))
	w := httptest.NewRecorder()

	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", w.Code, w.Body.String())
	}

	var anthropicResp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &anthropicResp); err != nil {
		t.Fatalf("invalid json response: %v", err)
	}

	if anthropicResp["type"] != "message" {
		t.Errorf("expected type 'message', got %v", anthropicResp["type"])
	}

	// Test Streaming request
	streamBody := `{"model":"claude-3-7-sonnet","messages":[{"role":"user","content":"Hi"}],"stream":true}`
	reqStream := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(streamBody))
	wStream := httptest.NewRecorder()

	srv.Handler().ServeHTTP(wStream, reqStream)

	if wStream.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", wStream.Code)
	}
	respStr := wStream.Body.String()
	if !strings.Contains(respStr, "event: message_start") {
		t.Errorf("expected message_start SSE event, got: %s", respStr)
	}
	if !strings.Contains(respStr, "content_block_delta") {
		t.Errorf("expected content_block_delta SSE event, got: %s", respStr)
	}
}
