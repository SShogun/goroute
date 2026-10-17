package router

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/ziuus/goroute/pkg/providers"
)

type mockProvider struct {
	id         string
	shouldFail bool
	failCode   int
	callCount  int
}

func (m *mockProvider) ID() string   { return m.id }
func (m *mockProvider) Type() string { return "mock" }
func (m *mockProvider) IsHealthy() bool {
	return true
}

func (m *mockProvider) ChatCompletion(ctx context.Context, req *providers.ChatRequest) (io.ReadCloser, bool, int, error) {
	m.callCount++
	if m.shouldFail {
		return io.NopCloser(bytes.NewReader([]byte("rate limited"))), false, m.failCode, nil
	}
	return io.NopCloser(bytes.NewReader([]byte("success from " + m.id))), false, http.StatusOK, nil
}

func (m *mockProvider) Models(ctx context.Context) ([]providers.ModelInfo, error) {
	return []providers.ModelInfo{{ID: "claude-3-7-sonnet", Object: "model"}}, nil
}

func TestRouterFailoverCascade(t *testing.T) {
	primary := &mockProvider{id: "primary", shouldFail: true, failCode: http.StatusTooManyRequests} // 429
	backup := &mockProvider{id: "backup", shouldFail: false}

	r := NewRouter()
	r.RegisterProvider(primary)
	r.RegisterProvider(backup)

	r.RegisterCombo("smart-model", "failover", []Target{
		{ProviderID: "primary", Model: "claude-3-7-sonnet"},
		{ProviderID: "backup", Model: "claude-3-7-sonnet"},
	})

	req := &providers.ChatRequest{
		Model: "smart-model",
		Messages: []providers.ChatMessage{
			{Role: "user", Content: "hi"},
		},
	}

	body, _, status, err := r.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer body.Close()

	if status != http.StatusOK {
		t.Errorf("expected status 200, got %d", status)
	}

	resp, _ := io.ReadAll(body)
	if !strings.Contains(string(resp), "success from backup") {
		t.Errorf("expected response from backup provider, got: %s", string(resp))
	}

	if primary.callCount != 1 || backup.callCount != 1 {
		t.Errorf("expected both providers called, primary=%d, backup=%d", primary.callCount, backup.callCount)
	}
}
