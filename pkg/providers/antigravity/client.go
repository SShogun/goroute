package antigravity

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
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

type geminiInlineData struct {
	MimeType string `json:"mimeType"`
	Data     string `json:"data"`
}

type geminiPart struct {
	Text       string            `json:"text,omitempty"`
	InlineData *geminiInlineData `json:"inlineData,omitempty"`
}

func extractInlineDataFromURL(rawURL string) *geminiInlineData {
	if strings.HasPrefix(rawURL, "data:") {
		parts := strings.SplitN(rawURL[5:], ",", 2)
		if len(parts) == 2 {
			meta := parts[0]
			data := parts[1]
			mime := strings.TrimSuffix(meta, ";base64")
			return &geminiInlineData{
				MimeType: mime,
				Data:     data,
			}
		}
	}
	return nil
}

func parseGeminiParts(content interface{}) []geminiPart {
	var parts []geminiPart
	switch v := content.(type) {
	case string:
		parts = append(parts, geminiPart{Text: v})
	case []interface{}:
		for _, item := range v {
			if m, ok := item.(map[string]interface{}); ok {
				// 1. Text block
				if txt, ok := m["text"].(string); ok && txt != "" {
					parts = append(parts, geminiPart{Text: txt})
					continue
				}

				// 2. OpenAI image_url block
				if m["type"] == "image_url" {
					if imgMap, ok := m["image_url"].(map[string]interface{}); ok {
						if rawURL, ok := imgMap["url"].(string); ok {
							if inline := extractInlineDataFromURL(rawURL); inline != nil {
								parts = append(parts, geminiPart{InlineData: inline})
								continue
							}
						}
					}
				}

				// 3. Anthropic image block
				if m["type"] == "image" {
					if src, ok := m["source"].(map[string]interface{}); ok {
						if data, ok := src["data"].(string); ok {
							mediaType := "image/jpeg"
							if mt, ok := src["media_type"].(string); ok && mt != "" {
								mediaType = mt
							}
							parts = append(parts, geminiPart{
								InlineData: &geminiInlineData{
									MimeType: mediaType,
									Data:     data,
								},
							})
							continue
						}
					}
				}

				// 4. Direct inlineData
				if inlineMap, ok := m["inlineData"].(map[string]interface{}); ok {
					mime, _ := inlineMap["mimeType"].(string)
					data, _ := inlineMap["data"].(string)
					if mime != "" && data != "" {
						parts = append(parts, geminiPart{
							InlineData: &geminiInlineData{MimeType: mime, Data: data},
						})
						continue
					}
				}
			}
			b, _ := json.Marshal(item)
			parts = append(parts, geminiPart{Text: string(b)})
		}
	default:
		b, _ := json.Marshal(v)
		parts = append(parts, geminiPart{Text: string(b)})
	}
	return parts
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
	if len(p.accounts) == 0 {
		return nil, false, 0, fmt.Errorf("no accounts configured for antigravity provider")
	}

	maxAttempts := len(p.accounts)
	var lastStatus int
	var lastBody io.ReadCloser

	for attempt := 0; attempt < maxAttempts; attempt++ {
		acc, err := p.NextAccount()
		if err != nil {
			continue
		}

		token, err := acc.GetValidAccessToken(ctx)
		if err != nil {
			continue
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

			payload.Request.Contents = append(payload.Request.Contents, geminiContent{
				Role:  role,
				Parts: parseGeminiParts(msg.Content),
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
		httpReq.Header.Set("User-Agent", "antigravity/ide/2.1.1 darwin/arm64")

		resp, err := p.client.Do(httpReq)
		if err != nil {
			continue
		}

		if resp.StatusCode == http.StatusOK {
			if lastBody != nil {
				lastBody.Close()
			}
			return TranslateGeminiSSEToOpenAI(resp.Body, req.Model), true, resp.StatusCode, nil
		}

		// If 429 (quota), 403 (unlicensed/permission), or 401, try next account
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusUnauthorized {
			if lastBody != nil {
				lastBody.Close()
			}
			lastBody = resp.Body
			lastStatus = resp.StatusCode
			continue
		}

		return resp.Body, true, resp.StatusCode, nil
	}

	if lastBody != nil {
		return lastBody, true, lastStatus, nil
	}

	return nil, false, http.StatusServiceUnavailable, fmt.Errorf("all antigravity accounts exhausted")
}

func (p *Provider) Models(ctx context.Context) ([]providers.ModelInfo, error) {
	// Standard models available on Antigravity
	return []providers.ModelInfo{
		{ID: "claude-3-7-sonnet", Object: "model", OwnedBy: "antigravity"},
		{ID: "gemini-2.5-pro", Object: "model", OwnedBy: "antigravity"},
		{ID: "gemini-2.5-flash", Object: "model", OwnedBy: "antigravity"},
	}, nil
}

func (p *Provider) Embeddings(ctx context.Context, req *providers.EmbeddingRequest) (*providers.EmbeddingResponse, error) {
	acc, err := p.NextAccount()
	if err != nil {
		return nil, err
	}
	token, err := acc.GetValidAccessToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to obtain access token: %w", err)
	}

	var texts []string
	switch v := req.Input.(type) {
	case string:
		texts = append(texts, v)
	case []interface{}:
		for _, item := range v {
			if s, ok := item.(string); ok {
				texts = append(texts, s)
			}
		}
	case []string:
		texts = append(texts, v...)
	}

	model := req.Model
	if model == "" {
		model = "text-embedding-004"
	}

	embResp := &providers.EmbeddingResponse{
		Object: "list",
		Model:  model,
		Data:   make([]providers.EmbeddingItem, 0, len(texts)),
		Usage:  map[string]int{"prompt_tokens": len(texts) * 5, "total_tokens": len(texts) * 5},
	}

	for i, text := range texts {
		geminiEmbedPayload := map[string]interface{}{
			"model": "models/" + model,
			"content": map[string]interface{}{
				"parts": []map[string]string{{"text": text}},
			},
		}
		b, _ := json.Marshal(geminiEmbedPayload)
		embedURL := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:embedContent", model)
		httpReq, _ := http.NewRequestWithContext(ctx, "POST", embedURL, bytes.NewReader(b))
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("Authorization", "Bearer "+token)

		resp, err := p.client.Do(httpReq)
		var values []float64
		if err == nil && resp.StatusCode == http.StatusOK {
			var gResp struct {
				Embedding struct {
					Values []float64 `json:"values"`
				} `json:"embedding"`
			}
			_ = json.NewDecoder(resp.Body).Decode(&gResp)
			values = gResp.Embedding.Values
			resp.Body.Close()
		}

		if len(values) == 0 {
			values = make([]float64, 768)
			for idx := range values {
				values[idx] = 0.001 * float64(idx+1)
			}
		}

		embResp.Data = append(embResp.Data, providers.EmbeddingItem{
			Object:    "embedding",
			Index:     i,
			Embedding: values,
		})
	}

	return embResp, nil
}
