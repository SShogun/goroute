package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/ziuus/goroute/pkg/providers"
)

type Provider struct {
	id      string
	baseURL string
	apiKeys []string
	counter uint64
	headers map[string]string
	client  *http.Client
	mu      sync.RWMutex
}

func NewProvider(id, baseURL, apiKey string, headers map[string]string) *Provider {
	var keys []string
	if apiKey != "" {
		keys = append(keys, apiKey)
	}
	return NewProviderWithKeys(id, baseURL, keys, headers)
}

func NewProviderWithKeys(id, baseURL string, apiKeys []string, headers map[string]string) *Provider {
	baseURL = strings.TrimSuffix(baseURL, "/")
	if headers == nil {
		headers = make(map[string]string)
	}
	if id == "agentrouter" {
		if _, ok := headers["User-Agent"]; !ok {
			headers["User-Agent"] = "codex_cli_rs/0.156.1"
		}
		if _, ok := headers["originator"]; !ok {
			headers["originator"] = "codex_cli_rs"
		}
	}
	return &Provider{
		id:      id,
		baseURL: baseURL,
		apiKeys: apiKeys,
		headers: headers,
		client: &http.Client{
			Timeout: 120 * time.Second,
		},
	}
}

func (p *Provider) nextKey() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.apiKeys) == 0 {
		return ""
	}
	k := p.apiKeys[p.counter%uint64(len(p.apiKeys))]
	p.counter++
	return k
}

func (p *Provider) ID() string {
	return p.id
}

func (p *Provider) Type() string {
	return "openai"
}

func (p *Provider) IsHealthy() bool {
	return true
}

func (p *Provider) ChatCompletion(ctx context.Context, req *providers.ChatRequest) (io.ReadCloser, bool, int, error) {
	payload, err := json.Marshal(req)
	if err != nil {
		return nil, false, 0, fmt.Errorf("marshal request: %w", err)
	}

	url := p.baseURL + "/chat/completions"

	p.mu.RLock()
	keyCount := len(p.apiKeys)
	p.mu.RUnlock()

	attempts := keyCount
	if attempts <= 0 {
		attempts = 1
	}

	var lastErr error
	var lastStatusCode int

	for i := 0; i < attempts; i++ {
		key := p.nextKey()
		httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(payload))
		if err != nil {
			return nil, false, 0, fmt.Errorf("create request: %w", err)
		}

		httpReq.Header.Set("Content-Type", "application/json")
		if key != "" {
			httpReq.Header.Set("Authorization", "Bearer "+key)
		}
		for k, v := range p.headers {
			httpReq.Header.Set(k, v)
		}

		resp, err := p.client.Do(httpReq)
		if err != nil {
			lastErr = fmt.Errorf("upstream call failed: %w", err)
			lastStatusCode = http.StatusBadGateway
			continue
		}

		if resp.StatusCode == 429 || resp.StatusCode == 401 || resp.StatusCode >= 500 {
			if attempts > 1 && i+1 < attempts {
				log.Printf("[openai] Provider %s key hit %d, failing over to next key (%d/%d)", p.id, resp.StatusCode, i+1, attempts)
				resp.Body.Close()
				continue
			}
		}

		isStream := strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream")
		return resp.Body, isStream, resp.StatusCode, nil
	}

	return nil, false, lastStatusCode, lastErr
}

func (p *Provider) SupportsAnthropic() bool {
	return p.id == "agentrouter" || strings.Contains(p.baseURL, "agentrouter.org")
}

func (p *Provider) AnthropicMessages(ctx context.Context, body []byte, incomingHeaders http.Header) (io.ReadCloser, bool, int, error) {
	url := p.baseURL + "/messages"

	p.mu.RLock()
	keyCount := len(p.apiKeys)
	p.mu.RUnlock()

	attempts := keyCount
	if attempts <= 0 {
		attempts = 1
	}

	var lastErr error
	var lastStatusCode int

	for i := 0; i < attempts; i++ {
		key := p.nextKey()
		httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
		if err != nil {
			return nil, false, 0, fmt.Errorf("create request: %w", err)
		}

		httpReq.Header.Set("Content-Type", "application/json")
		if key != "" {
			httpReq.Header.Set("Authorization", "Bearer "+key)
		}
		for k, v := range p.headers {
			httpReq.Header.Set(k, v)
		}
		if v := incomingHeaders.Get("anthropic-version"); v != "" {
			httpReq.Header.Set("anthropic-version", v)
		} else {
			httpReq.Header.Set("anthropic-version", "2023-06-01")
		}
		if v := incomingHeaders.Get("anthropic-beta"); v != "" {
			httpReq.Header.Set("anthropic-beta", v)
		}

		resp, err := p.client.Do(httpReq)
		if err != nil {
			lastErr = fmt.Errorf("upstream call failed: %w", err)
			lastStatusCode = http.StatusBadGateway
			continue
		}

		if resp.StatusCode == 429 || resp.StatusCode == 401 || resp.StatusCode >= 500 {
			if attempts > 1 && i+1 < attempts {
				log.Printf("[openai] Provider %s key hit %d on AnthropicMessages, failing over to next key (%d/%d)", p.id, resp.StatusCode, i+1, attempts)
				resp.Body.Close()
				continue
			}
		}

		isStream := strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream")
		return resp.Body, isStream, resp.StatusCode, nil
	}

	return nil, false, lastStatusCode, lastErr
}

func (p *Provider) Models(ctx context.Context) ([]providers.ModelInfo, error) {
	url := p.baseURL + "/models"
	httpReq, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	if key := p.nextKey(); key != "" {
		httpReq.Header.Set("Authorization", "Bearer "+key)
	}
	for k, v := range p.headers {
		httpReq.Header.Set(k, v)
	}

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch models, status: %d", resp.StatusCode)
	}

	var data struct {
		Data []providers.ModelInfo `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
	}

	return data.Data, nil
}

func (p *Provider) Embeddings(ctx context.Context, req *providers.EmbeddingRequest) (*providers.EmbeddingResponse, error) {
	payload, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal embedding request: %w", err)
	}

	url := p.baseURL + "/embeddings"
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("create embedding request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	if key := p.nextKey(); key != "" {
		httpReq.Header.Set("Authorization", "Bearer "+key)
	}

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("execute embedding request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("upstream embeddings error (%d): %s", resp.StatusCode, string(errBody))
	}

	var embResp providers.EmbeddingResponse
	if err := json.NewDecoder(resp.Body).Decode(&embResp); err != nil {
		return nil, fmt.Errorf("decode embedding response: %w", err)
	}

	return &embResp, nil
}
