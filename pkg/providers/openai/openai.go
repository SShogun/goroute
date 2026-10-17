package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/ziuus/goroute/pkg/providers"
)

type Provider struct {
	id      string
	baseURL string
	apiKey  string
	client  *http.Client
}

func NewProvider(id, baseURL, apiKey string) *Provider {
	baseURL = strings.TrimSuffix(baseURL, "/")
	return &Provider{
		id:      id,
		baseURL: baseURL,
		apiKey:  apiKey,
		client: &http.Client{
			Timeout: 120 * time.Second,
		},
	}
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
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(payload))
	if err != nil {
		return nil, false, 0, fmt.Errorf("create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	if p.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	}

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, false, 0, fmt.Errorf("upstream call failed: %w", err)
	}

	isStream := strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream")
	return resp.Body, isStream, resp.StatusCode, nil
}

func (p *Provider) Models(ctx context.Context) ([]providers.ModelInfo, error) {
	url := p.baseURL + "/models"
	httpReq, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	if p.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
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
