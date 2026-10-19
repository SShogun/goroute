package kiro

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/ziuus/goroute/pkg/providers"
)

type Provider struct {
	id           string
	refreshToken string
	profileARN   string
	accessToken  string
	expiresAt    time.Time
	mu           sync.RWMutex
	client       *http.Client
}

func NewProvider(id, refreshToken, profileARN string) *Provider {
	if profileARN == "" {
		profileARN = "arn:aws:codewhisperer:us-east-1:699475941385:profile/EHGA3GRVQMUK"
	}
	return &Provider{
		id:           id,
		refreshToken: refreshToken,
		profileARN:   profileARN,
		client: &http.Client{
			Timeout: 120 * time.Second,
		},
	}
}

func (p *Provider) ID() string {
	return p.id
}

func (p *Provider) Type() string {
	return "kiro"
}

func (p *Provider) IsHealthy() bool {
	return p.refreshToken != ""
}

func (p *Provider) getValidAccessToken(ctx context.Context) (string, error) {
	p.mu.RLock()
	if p.accessToken != "" && time.Now().Before(p.expiresAt.Add(-2*time.Minute)) {
		token := p.accessToken
		p.mu.RUnlock()
		return token, nil
	}
	p.mu.RUnlock()

	p.mu.Lock()
	defer p.mu.Unlock()

	// Double check
	if p.accessToken != "" && time.Now().Before(p.expiresAt.Add(-2*time.Minute)) {
		return p.accessToken, nil
	}

	refreshPayload := map[string]string{
		"refreshToken": p.refreshToken,
	}
	reqBytes, err := json.Marshal(refreshPayload)
	if err != nil {
		return "", fmt.Errorf("marshal refresh payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", "https://prod.us-east-1.auth.desktop.kiro.dev/refreshToken", bytes.NewReader(reqBytes))
	if err != nil {
		return "", fmt.Errorf("create refresh request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("execute token refresh: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("token refresh failed with status %d: %s", resp.StatusCode, string(body))
	}

	var res struct {
		AccessToken string `json:"accessToken"`
		ExpiresIn   int    `json:"expiresIn"`
		ProfileArn  string `json:"profileArn"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return "", fmt.Errorf("decode token refresh response: %w", err)
	}

	if res.AccessToken == "" {
		return "", fmt.Errorf("empty access token returned from Kiro refresh")
	}

	p.accessToken = res.AccessToken
	expiresIn := res.ExpiresIn
	if expiresIn <= 0 {
		expiresIn = 3600
	}
	p.expiresAt = time.Now().Add(time.Duration(expiresIn) * time.Second)
	if res.ProfileArn != "" {
		p.profileARN = res.ProfileArn
	}

	return p.accessToken, nil
}

type kiroToolSpec struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	InputSchema map[string]interface{} `json:"inputSchema"`
}

type kiroToolItem struct {
	ToolSpecification kiroToolSpec `json:"toolSpecification"`
}

type kiroUserInputContext struct {
	Tools       []kiroToolItem           `json:"tools,omitempty"`
	ToolResults []map[string]interface{} `json:"toolResults,omitempty"`
}

type kiroUserMsg struct {
	Content                 string                `json:"content"`
	ModelID                 string                `json:"modelId"`
	Origin                  string                `json:"origin"`
	UserInputMessageContext *kiroUserInputContext `json:"userInputMessageContext,omitempty"`
}

type kiroAssistMsg struct {
	Content string `json:"content"`
}

type kiroHistoryTurn struct {
	UserInputMessage        *kiroUserMsg   `json:"userInputMessage,omitempty"`
	AssistantResponseMessage *kiroAssistMsg `json:"assistantResponseMessage,omitempty"`
}

type kiroCurrentMsg struct {
	UserInputMessage kiroUserMsg `json:"userInputMessage"`
}

type kiroConversationState struct {
	CurrentMessage  kiroCurrentMsg    `json:"currentMessage"`
	ChatTriggerType string            `json:"chatTriggerType"`
	ConversationID  string            `json:"conversationId"`
	History         []kiroHistoryTurn `json:"history,omitempty"`
}

type kiroRequestPayload struct {
	ConversationState kiroConversationState `json:"conversationState"`
	ProfileArn        string                `json:"profileArn"`
}

func randomUUID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

func (p *Provider) ChatCompletion(ctx context.Context, req *providers.ChatRequest) (io.ReadCloser, bool, int, error) {
	token, err := p.getValidAccessToken(ctx)
	if err != nil {
		return nil, false, http.StatusUnauthorized, fmt.Errorf("kiro auth failed: %w", err)
	}

	modelID := "claude-sonnet-4.5"
	lowerModel := strings.ToLower(req.Model)
	if strings.Contains(lowerModel, "haiku") {
		modelID = "claude-haiku-4.5"
	} else if strings.Contains(lowerModel, "sonnet-5") {
		modelID = "claude-sonnet-5"
	} else if strings.Contains(lowerModel, "deepseek") {
		modelID = "deepseek-3.2"
	} else if strings.Contains(lowerModel, "luna") {
		modelID = "gpt-5.6-luna"
	} else if strings.Contains(lowerModel, "qwen") {
		modelID = "qwen3-coder-next"
	}

	// Prepare messages
	var userContent string
	var history []kiroHistoryTurn

	for i, m := range req.Messages {
		text := ""
		switch v := m.Content.(type) {
		case string:
			text = v
		default:
			b, _ := json.Marshal(v)
			text = string(b)
		}

		if i == len(req.Messages)-1 && (m.Role == "user" || m.Role == "system") {
			userContent = text
		} else {
			if m.Role == "assistant" {
				history = append(history, kiroHistoryTurn{
					AssistantResponseMessage: &kiroAssistMsg{Content: text},
				})
			} else {
				history = append(history, kiroHistoryTurn{
					UserInputMessage: &kiroUserMsg{
						Content: text,
						ModelID: modelID,
						Origin:  "AI_EDITOR",
					},
				})
			}
		}
	}

	if userContent == "" {
		userContent = "(empty prompt)"
	}

	var kiroTools []kiroToolItem
	for _, t := range req.Tools {
		m, ok := t.(map[string]interface{})
		if !ok {
			continue
		}
		name, _ := m["name"].(string)
		desc, _ := m["description"].(string)
		schema := m["input_schema"]
		if schema == nil {
			if fn, ok := m["function"].(map[string]interface{}); ok {
				name, _ = fn["name"].(string)
				desc, _ = fn["description"].(string)
				schema = fn["parameters"]
			}
		}
		schemaMap, ok := schema.(map[string]interface{})
		if !ok {
			schemaMap = map[string]interface{}{"type": "object"}
		}
		kiroTools = append(kiroTools, kiroToolItem{
			ToolSpecification: kiroToolSpec{
				Name:        name,
				Description: desc,
				InputSchema: map[string]interface{}{
					"json": schemaMap,
				},
			},
		})
	}

	var userCtx *kiroUserInputContext
	if len(kiroTools) > 0 {
		userCtx = &kiroUserInputContext{
			Tools: kiroTools,
		}
	}

	payload := kiroRequestPayload{
		ConversationState: kiroConversationState{
			CurrentMessage: kiroCurrentMsg{
				UserInputMessage: kiroUserMsg{
					Content:                 userContent,
					ModelID:                 modelID,
					Origin:                  "AI_EDITOR",
					UserInputMessageContext: userCtx,
				},
			},
			ChatTriggerType: "MANUAL",
			ConversationID:  randomUUID(),
			History:         history,
		},
		ProfileArn: p.profileARN,
	}

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, false, 0, fmt.Errorf("marshal kiro payload: %w", err)
	}

	url := "https://codewhisperer.us-east-1.amazonaws.com/generateAssistantResponse"
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, false, 0, fmt.Errorf("create kiro request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/vnd.amazon.eventstream")
	httpReq.Header.Set("X-Amz-Target", "AmazonCodeWhispererStreamingService.GenerateAssistantResponse")
	httpReq.Header.Set("Authorization", "Bearer "+token)
	httpReq.Header.Set("User-Agent", "AWS-SDK-JS/3.0.0 kiro-ide/1.0.0")
	httpReq.Header.Set("X-Amz-User-Agent", "aws-sdk-js/3.0.0 kiro-ide/1.0.0")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, false, 0, fmt.Errorf("kiro upstream call failed: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, false, resp.StatusCode, fmt.Errorf("kiro upstream error %d: %s", resp.StatusCode, string(raw))
	}

	pr, pw := io.Pipe()

	go func() {
		defer resp.Body.Close()
		defer pw.Close()

		created := time.Now().Unix()
		respID := fmt.Sprintf("chatcmpl-kiro-%d", time.Now().UnixNano())

		// Read buffer for eventstream
		buf := make([]byte, 0, 64*1024)
		readBuf := make([]byte, 4096)

		for {
			n, rErr := resp.Body.Read(readBuf)
			if n > 0 {
				buf = append(buf, readBuf[:n]...)

				// Process complete event frames
				for len(buf) >= 16 {
					totalLen := binary.BigEndian.Uint32(buf[0:4])
					if totalLen < 16 || totalLen > 10*1024*1024 {
						// Malformed, skip
						buf = buf[1:]
						continue
					}
					if uint32(len(buf)) < totalLen {
						// Wait for complete frame
						break
					}

					headersLen := binary.BigEndian.Uint32(buf[4:8])
					frame := buf[:totalLen]
					buf = buf[totalLen:]

					// Parse headers
					var eventType string
					offset := 12
					headerEnd := int(12 + headersLen)
					for offset < headerEnd && offset < len(frame) {
						nameLen := int(frame[offset])
						offset++
						if offset+nameLen > len(frame) {
							break
						}
						headerName := string(frame[offset : offset+nameLen])
						offset += nameLen
						if offset >= len(frame) {
							break
						}
						headerType := frame[offset]
						offset++
						if headerType == 7 { // String type
							if offset+2 > len(frame) {
								break
							}
							valLen := int(binary.BigEndian.Uint16(frame[offset : offset+2]))
							offset += 2
							if offset+valLen > len(frame) {
								break
							}
							val := string(frame[offset : offset+valLen])
							offset += valLen
							if headerName == ":event-type" {
								eventType = val
							}
						} else {
							break
						}
					}

					// Parse payload
					payloadStart := 12 + int(headersLen)
					payloadEnd := int(totalLen) - 4
					if payloadEnd > payloadStart && payloadEnd <= len(frame) {
						payloadBytes := frame[payloadStart:payloadEnd]
						if eventType == "assistantResponseEvent" {
							var evt struct {
								Content string `json:"content"`
							}
							if err := json.Unmarshal(payloadBytes, &evt); err == nil && evt.Content != "" {
								chunk := map[string]interface{}{
									"id":      respID,
									"object":  "chat.completion.chunk",
									"created": created,
									"model":   modelID,
									"choices": []map[string]interface{}{
										{
											"index": 0,
											"delta": map[string]string{
												"content": evt.Content,
											},
											"finish_reason": nil,
										},
									},
								}
								chunkBytes, _ := json.Marshal(chunk)
								fmt.Fprintf(pw, "data: %s\n\n", string(chunkBytes))
							}
						} else if eventType == "toolUseEvent" {
							var evt struct {
								ToolUseId string      `json:"toolUseId"`
								Name      string      `json:"name"`
								Input     interface{} `json:"input"`
							}
							if err := json.Unmarshal(payloadBytes, &evt); err == nil && (evt.Name != "" || evt.ToolUseId != "") {
								argsStr := ""
								switch v := evt.Input.(type) {
								case string:
									if v != "null" {
										argsStr = v
									}
								case nil:
									argsStr = ""
								default:
									b, _ := json.Marshal(v)
									s := string(b)
									if s != "null" {
										argsStr = s
									}
								}
								chunk := map[string]interface{}{
									"id":      respID,
									"object":  "chat.completion.chunk",
									"created": created,
									"model":   modelID,
									"choices": []map[string]interface{}{
										{
											"index": 0,
											"delta": map[string]interface{}{
												"tool_calls": []map[string]interface{}{
													{
														"index": 0,
														"id":    evt.ToolUseId,
														"type":  "function",
														"function": map[string]string{
															"name":      evt.Name,
															"arguments": argsStr,
														},
													},
												},
											},
											"finish_reason": "tool_calls",
										},
									},
								}
								chunkBytes, _ := json.Marshal(chunk)
								fmt.Fprintf(pw, "data: %s\n\n", string(chunkBytes))
							}
						}
					}
				}
			}
			if rErr != nil {
				break
			}
		}

		// Emit final finish chunk and DONE
		finishChunk := map[string]interface{}{
			"id":      respID,
			"object":  "chat.completion.chunk",
			"created": created,
			"model":   modelID,
			"choices": []map[string]interface{}{
				{
					"index":         0,
					"delta":         map[string]string{},
					"finish_reason": "stop",
				},
			},
		}
		chunkBytes, _ := json.Marshal(finishChunk)
		fmt.Fprintf(pw, "data: %s\n\n", string(chunkBytes))
		fmt.Fprintf(pw, "data: [DONE]\n\n")
	}()

	return pr, true, http.StatusOK, nil
}

func (p *Provider) Embeddings(ctx context.Context, req *providers.EmbeddingRequest) (*providers.EmbeddingResponse, error) {
	return nil, fmt.Errorf("embeddings not supported by Kiro")
}

func (p *Provider) Models(ctx context.Context) ([]providers.ModelInfo, error) {
	return []providers.ModelInfo{
		{ID: "claude-sonnet-4.5", Object: "model", OwnedBy: "kiro"},
		{ID: "claude-haiku-4.5", Object: "model", OwnedBy: "kiro"},
		{ID: "deepseek-3.2", Object: "model", OwnedBy: "kiro"},
		{ID: "qwen3-coder-next", Object: "model", OwnedBy: "kiro"},
		{ID: "gpt-5.6-luna", Object: "model", OwnedBy: "kiro"},
	}, nil
}
