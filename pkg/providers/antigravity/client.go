package antigravity

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/ziuus/goroute/pkg/providers"
)

type Provider struct {
	id       string
	accounts []*Account
	idx      uint64
	baseURL  string
	client   *http.Client
}

func NewProvider(id string, accounts []*Account) *Provider {
	return &Provider{
		id:       id,
		accounts: accounts,
		baseURL:  CloudCodeAPIBaseURL,
		client: &http.Client{
			Timeout: 120 * time.Second,
		},
	}
}

func (p *Provider) ID() string {
	return p.id
}

func (p *Provider) Type() string {
	return "antigravity"
}

func (p *Provider) IsHealthy() bool {
	return len(p.accounts) > 0
}

func (p *Provider) NextAccount() (*Account, error) {
	if len(p.accounts) == 0 {
		return nil, fmt.Errorf("no accounts configured for provider %s", p.id)
	}
	next := atomic.AddUint64(&p.idx, 1)
	return p.accounts[next%uint64(len(p.accounts))], nil
}

type geminiPart struct {
	Text string `json:"text"`
}

type geminiContent struct {
	Role  string       `json:"role"`
	Parts []geminiPart `json:"parts"`
}

type antigravityPayload struct {
	Project string `json:"project,omitempty"`
	Model   string `json:"model"`
	Request struct {
		Contents []geminiContent `json:"contents"`
	} `json:"request"`
}

func (p *Provider) ChatCompletion(ctx context.Context, req *providers.ChatRequest) (io.ReadCloser, bool, int, error) {
	acc, err := p.NextAccount()
	if err != nil {
		return nil, false, 0, err
	}

	token, err := acc.GetValidAccessToken(ctx)
	if err != nil {
		return nil, false, 0, fmt.Errorf("failed to obtain access token: %w", err)
	}

	payload := antigravityPayload{
		Project: acc.ProjectID,
		Model:   req.Model,
	}

	for _, msg := range req.Messages {
		role := msg.Role
		if role == "assistant" {
			role = "model"
		}
		var text string
		switch v := msg.Content.(type) {
		case string:
			text = v
		default:
			b, _ := json.Marshal(v)
			text = string(b)
		}

		payload.Request.Contents = append(payload.Request.Contents, geminiContent{
			Role:  role,
			Parts: []geminiPart{{Text: text}},
		})
	}

	reqBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, false, 0, fmt.Errorf("marshal antigravity payload: %w", err)
	}

	url := p.baseURL + "/v1internal:streamGenerateContent?alt=sse"
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(reqBytes))
	if err != nil {
		return nil, false, 0, err
	}

	httpReq.Header.Set("Authorization", "Bearer "+token)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	httpReq.Header.Set("User-Agent", "antigravity/cli/1.0.0 (darwin/arm64)")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, false, 0, fmt.Errorf("antigravity upstream request failed: %w", err)
	}
	if resp.StatusCode == http.StatusOK {
		return TranslateGeminiSSEToOpenAI(resp.Body, req.Model), true, resp.StatusCode, nil
	}

	return resp.Body, true, resp.StatusCode, nil
}

func (p *Provider) Models(ctx context.Context) ([]providers.ModelInfo, error) {
	// Standard models available on Antigravity
	return []providers.ModelInfo{
		{ID: "claude-3-7-sonnet", Object: "model", OwnedBy: "antigravity"},
		{ID: "gemini-2.5-pro", Object: "model", OwnedBy: "antigravity"},
		{ID: "gemini-2.5-flash", Object: "model", OwnedBy: "antigravity"},
	}, nil
}
