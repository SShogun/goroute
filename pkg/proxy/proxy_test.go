package proxy

import (
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

type dummyProvider struct {
	id string
}

func (d *dummyProvider) ID() string   { return d.id }
func (d *dummyProvider) Type() string { return "dummy" }
func (d *dummyProvider) IsHealthy() bool {
	return true
}

func (d *dummyProvider) ChatCompletion(ctx context.Context, req *providers.ChatRequest) (io.ReadCloser, bool, int, error) {
	if req.Stream {
		sseData := "data: {\"id\":\"chatcmpl-test\",\"choices\":[{\"delta\":{\"content\":\"streamed content\"}}]}\n\ndata: [DONE]\n\n"
		return io.NopCloser(strings.NewReader(sseData)), true, http.StatusOK, nil
	}
	resp := `{"id":"chatcmpl-test","choices":[{"message":{"role":"assistant","content":"json content"}}]}`
	return io.NopCloser(strings.NewReader(resp)), false, http.StatusOK, nil
}

func (d *dummyProvider) Models(ctx context.Context) ([]providers.ModelInfo, error) {
	return []providers.ModelInfo{{ID: "dummy-model", Object: "model"}}, nil
}

func TestProxyServerChatCompletions(t *testing.T) {
	r := router.NewRouter()
	r.RegisterProvider(&dummyProvider{id: "dummy"})
	limiter := quota.NewLimiter()

	srv := NewServer(r, limiter, true)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// 1. Test non-streaming
	reqBody := `{"model":"dummy/dummy-model","messages":[{"role":"user","content":"hi"}]}`
	resp, err := http.Post(ts.URL+"/v1/chat/completions", "application/json", strings.NewReader(reqBody))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "json content") {
		t.Errorf("expected json content, got: %s", string(body))
	}

	// 2. Test streaming
	streamReqBody := `{"model":"dummy/dummy-model","messages":[{"role":"user","content":"hi"}],"stream":true}`
	respStream, err := http.Post(ts.URL+"/v1/chat/completions", "application/json", strings.NewReader(streamReqBody))
	if err != nil {
		t.Fatalf("stream request failed: %v", err)
	}
	defer respStream.Body.Close()

	if !strings.Contains(respStream.Header.Get("Content-Type"), "text/event-stream") {
		t.Errorf("expected text/event-stream, got %s", respStream.Header.Get("Content-Type"))
	}
	streamBytes, _ := io.ReadAll(respStream.Body)
	if !strings.Contains(string(streamBytes), "streamed content") {
		t.Errorf("expected streamed content, got: %s", string(streamBytes))
	}

	// 3. Test /v1/models
	modelsResp, err := http.Get(ts.URL + "/v1/models")
	if err != nil {
		t.Fatalf("models request failed: %v", err)
	}
	defer modelsResp.Body.Close()

	var modelsData struct {
		Data []providers.ModelInfo `json:"data"`
	}
	json.NewDecoder(modelsResp.Body).Decode(&modelsData)
	if len(modelsData.Data) == 0 {
		t.Errorf("expected at least 1 model in /v1/models")
	}
}
