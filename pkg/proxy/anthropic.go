package proxy

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/ziuus/goroute/pkg/providers"
)

// AnthropicMessageRequest represents an incoming /v1/messages POST request
type AnthropicMessageRequest struct {
	Model       string                 `json:"model"`
	Messages    []AnthropicMessage     `json:"messages"`
	System      interface{}            `json:"system,omitempty"` // string or []contentBlock
	MaxTokens   int                    `json:"max_tokens,omitempty"`
	Stream      bool                   `json:"stream,omitempty"`
	Temperature *float64               `json:"temperature,omitempty"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

type AnthropicMessage struct {
	Role    string      `json:"role"`
	Content interface{} `json:"content"` // string or []AnthropicContentBlock
}

type AnthropicContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

func (s *Server) handleMessages(w http.ResponseWriter, r *http.Request) {
	var req AnthropicMessageRequest
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "failed to read body: "+err.Error(), http.StatusBadRequest)
		return
	}

	if err := json.Unmarshal(bodyBytes, &req); err != nil {
		http.Error(w, "invalid Anthropic messages JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	var maxTokensPtr *int
	if req.MaxTokens > 0 {
		maxTokensPtr = &req.MaxTokens
	}

	// Translate Anthropic format to OpenAI ChatRequest
	chatReq := providers.ChatRequest{
		Model:       req.Model,
		Stream:      req.Stream,
		MaxTokens:   maxTokensPtr,
		Temperature: req.Temperature,
		Messages:    make([]providers.ChatMessage, 0, len(req.Messages)+1),
	}

	// Extract system message
	if req.System != nil {
		var sysText string
		switch v := req.System.(type) {
		case string:
			sysText = v
		case []interface{}:
			for _, item := range v {
				if m, ok := item.(map[string]interface{}); ok {
					if txt, ok := m["text"].(string); ok {
						sysText += txt + "\n"
					}
				}
			}
		}
		if sysText != "" {
			chatReq.Messages = append(chatReq.Messages, providers.ChatMessage{
				Role:    "system",
				Content: strings.TrimSpace(sysText),
			})
		}
	}

	// Extract messages
	for _, m := range req.Messages {
		var contentStr string
		switch c := m.Content.(type) {
		case string:
			contentStr = c
		case []interface{}:
			for _, item := range c {
				if block, ok := item.(map[string]interface{}); ok {
					if txt, ok := block["text"].(string); ok {
						contentStr += txt
					}
				}
			}
		}

		chatReq.Messages = append(chatReq.Messages, providers.ChatMessage{
			Role:    m.Role,
			Content: contentStr,
		})
	}

	// Rate limiting / quota
	if !s.limiter.Allow(chatReq.Model) {
		http.Error(w, "Rate limit exceeded (cooling down)", http.StatusTooManyRequests)
		return
	}

	respBody, isStream, statusCode, err := s.router.Execute(r.Context(), &chatReq)
	if err != nil {
		http.Error(w, err.Error(), statusCode)
		return
	}
	defer respBody.Close()

	if isStream {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.WriteHeader(statusCode)

		flusher, _ := w.(http.Flusher)
		translateOpenAIToAnthropicSSE(respBody, w, flusher, req.Model)
		return
	}

	// Non-streaming response translation
	var openAIResp struct {
		ID      string `json:"id"`
		Choices []struct {
			Message struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
	}

	respRaw, err := io.ReadAll(respBody)
	if err != nil {
		http.Error(w, "failed to read upstream response: "+err.Error(), http.StatusBadGateway)
		return
	}

	if err := json.Unmarshal(respRaw, &openAIResp); err != nil {
		// Pass-through as fallback
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(statusCode)
		w.Write(respRaw)
		return
	}

	replyText := ""
	if len(openAIResp.Choices) > 0 {
		replyText = openAIResp.Choices[0].Message.Content
	}

	anthropicResp := map[string]interface{}{
		"id":           "msg_" + fmt.Sprintf("%d", time.Now().UnixNano()),
		"type":         "message",
		"role":         "assistant",
		"model":        req.Model,
		"content":      []map[string]string{{"type": "text", "text": replyText}},
		"stop_reason":  "end_turn",
		"stop_sequence": nil,
		"usage": map[string]int{
			"input_tokens":  openAIResp.Usage.PromptTokens,
			"output_tokens": openAIResp.Usage.CompletionTokens,
		},
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(anthropicResp)
}

func translateOpenAIToAnthropicSSE(upstream io.Reader, w io.Writer, flusher http.Flusher, model string) {
	msgID := fmt.Sprintf("msg_%d", time.Now().UnixNano())

	// Send message_start
	fmt.Fprintf(w, "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"%s\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"%s\",\"content\":[],\"stop_reason\":null,\"stop_sequence\":null,\"usage\":{\"input_tokens\":1,\"output_tokens\":1}}}\n\n", msgID, model)
	// Send content_block_start
	fmt.Fprintf(w, "event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n")
	if flusher != nil {
		flusher.Flush()
	}

	scanner := bufio.NewScanner(upstream)
	outputTokens := 0

	for scanner.Scan() {
		line := scanner.Bytes()
		if !bytes.HasPrefix(line, []byte("data: ")) {
			continue
		}
		data := bytes.TrimSpace(bytes.TrimPrefix(line, []byte("data: ")))
		if bytes.Equal(data, []byte("[DONE]")) {
			break
		}

		var chunk struct {
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
			} `json:"choices"`
		}

		if err := json.Unmarshal(data, &chunk); err == nil && len(chunk.Choices) > 0 {
			text := chunk.Choices[0].Delta.Content
			if text != "" {
				outputTokens++
				deltaJSON, _ := json.Marshal(map[string]interface{}{
					"type":  "content_block_delta",
					"index": 0,
					"delta": map[string]string{
						"type": "text_delta",
						"text": text,
					},
				})
				fmt.Fprintf(w, "event: content_block_delta\ndata: %s\n\n", string(deltaJSON))
				if flusher != nil {
					flusher.Flush()
				}
			}
		}
	}

	// Send content_block_stop
	fmt.Fprintf(w, "event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n")
	// Send message_delta
	fmt.Fprintf(w, "event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\",\"stop_sequence\":null},\"usage\":{\"output_tokens\":%d}}\n\n", outputTokens)
	// Send message_stop
	fmt.Fprintf(w, "event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
	if flusher != nil {
		flusher.Flush()
	}
}
