package antigravity

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"
)

type geminiStreamChunk struct {
	Response struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text             string              `json:"text"`
					ReasoningContent string              `json:"thought"`
					FunctionCall     *geminiFunctionCall `json:"functionCall,omitempty"`
				} `json:"parts"`
			} `json:"content"`
			FinishReason string `json:"finishReason"`
		} `json:"candidates"`
	} `json:"response"`
}

type openAIStreamChunk struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	Model   string `json:"model"`
	Choices []struct {
		Index int `json:"index"`
		Delta struct {
			Content          string `json:"content,omitempty"`
			ReasoningContent string `json:"reasoning_content,omitempty"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
}

// TranslateGeminiSSEToOpenAI wraps a raw Gemini SSE stream and emits standard OpenAI SSE lines
func TranslateGeminiSSEToOpenAI(rawStream io.ReadCloser, modelName string) io.ReadCloser {
	pr, pw := io.Pipe()

	go func() {
		defer rawStream.Close()
		defer pw.Close()

		scanner := bufio.NewScanner(rawStream)
		buf := make([]byte, 64*1024)
		scanner.Buffer(buf, 1024*1024)

		now := time.Now().Unix()

		for scanner.Scan() {
			line := scanner.Text()
			trimmed := strings.TrimSpace(line)

			if !strings.HasPrefix(trimmed, "data:") {
				continue
			}

			payload := strings.TrimSpace(strings.TrimPrefix(trimmed, "data:"))
			if payload == "" || payload == "[DONE]" {
				continue
			}

			var gChunk geminiStreamChunk
			if err := json.Unmarshal([]byte(payload), &gChunk); err != nil {
				continue
			}

			if len(gChunk.Response.Candidates) == 0 {
				continue
			}

			candidate := gChunk.Response.Candidates[0]
			for _, part := range candidate.Content.Parts {
				if part.FunctionCall != nil {
					argsBytes, _ := json.Marshal(part.FunctionCall.Args)
					toolCallChunk := map[string]interface{}{
						"id":      "chatcmpl-goroute",
						"object":  "chat.completion.chunk",
						"created": now,
						"model":   modelName,
						"choices": []map[string]interface{}{
							{
								"index": 0,
								"delta": map[string]interface{}{
									"tool_calls": []map[string]interface{}{
										{
											"index": 0,
											"id":    fmt.Sprintf("toolu_%d", time.Now().UnixNano()),
											"type":  "function",
											"function": map[string]string{
												"name":      part.FunctionCall.Name,
												"arguments": string(argsBytes),
											},
										},
									},
								},
								"finish_reason": "tool_calls",
							},
						},
					}
					data, _ := json.Marshal(toolCallChunk)
					fmt.Fprintf(pw, "data: %s\n\n", data)
					continue
				}

				if part.Text == "" && part.ReasoningContent == "" {
					continue
				}

				chunk := openAIStreamChunk{
					ID:      "chatcmpl-goroute",
					Object:  "chat.completion.chunk",
					Created: now,
					Model:   modelName,
					Choices: make([]struct {
						Index int `json:"index"`
						Delta struct {
							Content          string `json:"content,omitempty"`
							ReasoningContent string `json:"reasoning_content,omitempty"`
						} `json:"delta"`
						FinishReason *string `json:"finish_reason"`
					}, 1),
				}
				chunk.Choices[0].Index = 0
				chunk.Choices[0].Delta.Content = part.Text
				chunk.Choices[0].Delta.ReasoningContent = part.ReasoningContent

				data, _ := json.Marshal(chunk)
				fmt.Fprintf(pw, "data: %s\n\n", data)
			}
		}

		// Emit final finish_reason chunk required by OpenAI spec before [DONE]
		stopReason := "stop"
		finalChunk := openAIStreamChunk{
			ID:      "chatcmpl-goroute",
			Object:  "chat.completion.chunk",
			Created: now,
			Model:   modelName,
			Choices: make([]struct {
				Index int `json:"index"`
				Delta struct {
					Content          string `json:"content,omitempty"`
					ReasoningContent string `json:"reasoning_content,omitempty"`
				} `json:"delta"`
				FinishReason *string `json:"finish_reason"`
			}, 1),
		}
		finalChunk.Choices[0].Index = 0
		finalChunk.Choices[0].FinishReason = &stopReason
		if data, err := json.Marshal(finalChunk); err == nil {
			fmt.Fprintf(pw, "data: %s\n\n", data)
		}

		fmt.Fprint(pw, "data: [DONE]\n\n")
	}()

	return pr
}

// ConvertGeminiSSEToNonStreaming aggregates a Gemini SSE stream into a standard OpenAI non-streaming JSON
func ConvertGeminiSSEToNonStreaming(rawStream io.ReadCloser, modelName string) (io.ReadCloser, error) {
	defer rawStream.Close()

	scanner := bufio.NewScanner(rawStream)
	buf := make([]byte, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	var contentBuilder strings.Builder

	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)

		if !strings.HasPrefix(trimmed, "data:") {
			continue
		}

		payload := strings.TrimSpace(strings.TrimPrefix(trimmed, "data:"))
		if payload == "" || payload == "[DONE]" {
			continue
		}

		var gChunk geminiStreamChunk
		if err := json.Unmarshal([]byte(payload), &gChunk); err != nil {
			continue
		}

		if len(gChunk.Response.Candidates) == 0 {
			continue
		}

		candidate := gChunk.Response.Candidates[0]
		for _, part := range candidate.Content.Parts {
			contentBuilder.WriteString(part.Text)
		}
	}

	fullText := contentBuilder.String()
	tokens := len(fullText) / 4
	if tokens == 0 && len(fullText) > 0 {
		tokens = 1
	}

	resp := map[string]interface{}{
		"id":      fmt.Sprintf("chatcmpl-goroute-%d", time.Now().UnixNano()),
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   modelName,
		"choices": []map[string]interface{}{
			{
				"index": 0,
				"message": map[string]interface{}{
					"role":    "assistant",
					"content": fullText,
				},
				"finish_reason": "stop",
			},
		},
		"usage": map[string]interface{}{
			"prompt_tokens":     10,
			"completion_tokens": tokens,
			"total_tokens":      10 + tokens,
		},
	}

	data, err := json.Marshal(resp)
	if err != nil {
		return nil, err
	}

	return io.NopCloser(bytes.NewReader(data)), nil
}
