package proxy

import (
	"encoding/json"
	"fmt"
	"github.com/ziuus/goroute/pkg/providers"
)

const (
	// DefaultMaxToolResultChars limits single tool output to ~50k chars (~12k tokens)
	// to prevent context-overflow 400 errors from upstream providers.
	DefaultMaxToolResultChars = 50000
	headKeepChars             = 30000
	tailKeepChars             = 15000
)

// SanitizeAndGuardrailMessages compresses excessively large tool outputs and cleans historical thinking blocks
func SanitizeAndGuardrailMessages(messages []providers.ChatMessage, maxChars int) []providers.ChatMessage {
	if maxChars <= 0 {
		maxChars = DefaultMaxToolResultChars
	}

	sanitized := make([]providers.ChatMessage, len(messages))

	for i, msg := range messages {
		// 1. Tool role with string content
		if msg.Role == "tool" {
			if strContent, ok := msg.Content.(string); ok && len(strContent) > maxChars {
				msg.Content = truncateString(strContent, maxChars)
			}
			sanitized[i] = msg
			continue
		}

		// 2. User or Assistant role with array content blocks (Anthropic / multimodal format)
		if sliceContent, ok := msg.Content.([]interface{}); ok {
			var newSlice []interface{}
			for _, block := range sliceContent {
				m, isMap := block.(map[string]interface{})
				if !isMap {
					newSlice = append(newSlice, block)
					continue
				}

				// Skip historical thinking blocks from non-latest turns
				if m["type"] == "thinking" && i < len(messages)-1 {
					continue
				}

				// Truncate tool_result blocks
				if m["type"] == "tool_result" {
					if contentStr, ok := m["content"].(string); ok && len(contentStr) > maxChars {
						cloned := make(map[string]interface{}, len(m))
						for k, v := range m {
							cloned[k] = v
						}
						cloned["content"] = truncateString(contentStr, maxChars)
						newSlice = append(newSlice, cloned)
						continue
					}
					// Handle nested content array inside tool_result
					if contentArr, ok := m["content"].([]interface{}); ok {
						var newSubArr []interface{}
						for _, subItem := range contentArr {
							if subM, ok := subItem.(map[string]interface{}); ok {
								if subM["type"] == "text" {
									if textStr, ok := subM["text"].(string); ok && len(textStr) > maxChars {
										subCloned := make(map[string]interface{}, len(subM))
										for k, v := range subM {
											subCloned[k] = v
										}
										subCloned["text"] = truncateString(textStr, maxChars)
										newSubArr = append(newSubArr, subCloned)
										continue
									}
								}
							}
							newSubArr = append(newSubArr, subItem)
						}
						cloned := make(map[string]interface{}, len(m))
						for k, v := range m {
							cloned[k] = v
						}
						cloned["content"] = newSubArr
						newSlice = append(newSlice, cloned)
						continue
					}
				}

				newSlice = append(newSlice, block)
			}
			msg.Content = newSlice
			sanitized[i] = msg
			continue
		}

		// 3. Fallback check for raw JSON array / string
		sanitized[i] = msg
	}

	return sanitized
}

func truncateString(s string, maxChars int) string {
	if len(s) <= maxChars {
		return s
	}

	head := headKeepChars
	tail := tailKeepChars
	if head+tail >= maxChars {
		head = maxChars / 2
		tail = maxChars / 2
	}

	truncatedCount := len(s) - (head + tail)
	if truncatedCount <= 0 {
		return s[:maxChars]
	}

	return fmt.Sprintf("%s\n\n[... goroute context guard: truncated %d characters (%d tokens approx) to prevent upstream context overflow ...]\n\n%s",
		s[:head],
		truncatedCount,
		truncatedCount/4,
		s[len(s)-tail:],
	)
}

// EstimateMessageTokens provides an approximation of token count
func EstimateMessageTokens(messages []providers.ChatMessage) int {
	totalChars := 0
	for _, m := range messages {
		switch v := m.Content.(type) {
		case string:
			totalChars += len(v)
		default:
			b, _ := json.Marshal(v)
			totalChars += len(b)
		}
	}
	tokens := totalChars / 4
	if tokens == 0 && totalChars > 0 {
		tokens = 1
	}
	return tokens
}
