package antigravity

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

type geminiStreamChunk struct {
	Response struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text             string `json:"text"`
					ReasoningContent string `json:"thought"`
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
		// Large buffer for large context chunks
		buf := make([]byte, 64*1024)
		scanner.Buffer(buf, 1024*1024)

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
				if part.Text == "" && part.ReasoningContent == "" {
					continue
				}

				chunk := openAIStreamChunk{
					ID:      "chatcmpl-goroute",
					Object:  "chat.completion.chunk",
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

		fmt.Fprint(pw, "data: [DONE]\n\n")
	}()

	return pr
}
