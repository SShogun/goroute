package proxy

import (
	"encoding/json"
	"fmt"

	"github.com/ziuus/goroute/pkg/providers"
)

const (
	// DefaultMaxToolResultChars limits single tool output to ~20k chars (~5k tokens)
	DefaultMaxToolResultChars = 20000
	headKeepChars             = 12000
	tailKeepChars             = 6000

	// DefaultMaxTotalContextChars limits entire conversation history to ~160k chars (~40k tokens)
	// to prevent AWS Kiro 413 / ValidationException and Google Cloud Code 400 errors on long agent runs.
	DefaultMaxTotalContextChars = 160000
)

// SanitizeAndGuardrailMessages compresses tool outputs, strips historical thinking, and prunes older turns if oversized
func SanitizeAndGuardrailMessages(messages []providers.ChatMessage, maxToolChars int) []providers.ChatMessage {
	if maxToolChars <= 0 {
		maxToolChars = DefaultMaxToolResultChars
	}

	sanitized := make([]providers.ChatMessage, len(messages))

	for i, msg := range messages {
		// 1. Tool role with string content
		if msg.Role == "tool" {
			if strContent, ok := msg.Content.(string); ok && len(strContent) > maxToolChars {
				msg.Content = truncateString(strContent, maxToolChars)
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
					if contentStr, ok := m["content"].(string); ok && len(contentStr) > maxToolChars {
						cloned := make(map[string]interface{}, len(m))
						for k, v := range m {
							cloned[k] = v
						}
						cloned["content"] = truncateString(contentStr, maxToolChars)
						newSlice = append(newSlice, cloned)
						continue
					}
					// Handle nested content array inside tool_result
					if contentArr, ok := m["content"].([]interface{}); ok {
						var newSubArr []interface{}
						for _, subItem := range contentArr {
							if subM, ok := subItem.(map[string]interface{}); ok {
								if subM["type"] == "text" {
									if textStr, ok := subM["text"].(string); ok && len(textStr) > maxToolChars {
										subCloned := make(map[string]interface{}, len(subM))
										for k, v := range subM {
											subCloned[k] = v
										}
										subCloned["text"] = truncateString(textStr, maxToolChars)
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

		sanitized[i] = msg
	}

	// Apply conversation history pruning if total characters exceed threshold
	return PruneHistoryToFit(sanitized, DefaultMaxTotalContextChars)
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

	return fmt.Sprintf("%s\n\n[... goroute context guard: truncated %d characters to fit window ...]\n\n%s",
		s[:head],
		truncatedCount,
		s[len(s)-tail:],
	)
}

// PruneHistoryToFit keeps the leading system prompt and the latest N messages within maxChars
func PruneHistoryToFit(messages []providers.ChatMessage, maxChars int) []providers.ChatMessage {
	if len(messages) <= 2 || maxChars <= 0 {
		return messages
	}

	totalChars := 0
	for _, m := range messages {
		totalChars += getMessageChars(m)
	}

	if totalChars <= maxChars {
		return messages
	}

	// Separate system message(s) from conversation turns
	var systemMessages []providers.ChatMessage
	var turns []providers.ChatMessage

	for _, m := range messages {
		if m.Role == "system" || m.Role == "developer" {
			systemMessages = append(systemMessages, m)
		} else {
			turns = append(turns, m)
		}
	}

	// Calculate budget for turns
	sysChars := 0
	for _, sm := range systemMessages {
		sysChars += getMessageChars(sm)
	}
	turnsBudget := maxChars - sysChars
	if turnsBudget < 30000 {
		turnsBudget = 30000
	}

	// Keep the most recent turns that fit within turnsBudget
	currentTurnChars := 0
	keepIdx := len(turns) - 1

	for i := len(turns) - 1; i >= 0; i-- {
		ch := getMessageChars(turns[i])
		if currentTurnChars+ch > turnsBudget && i < len(turns)-2 {
			break
		}
		currentTurnChars += ch
		keepIdx = i
	}

	// Ensure we start on a user turn if possible
	for keepIdx < len(turns)-1 && turns[keepIdx].Role != "user" {
		keepIdx++
	}

	retainedTurns := turns[keepIdx:]

	// Find all tool_use IDs defined in retained assistant turns
	toolUseIDs := make(map[string]bool)
	for _, t := range retainedTurns {
		if t.Role == "assistant" {
			if blocks, ok := t.Content.([]interface{}); ok {
				for _, b := range blocks {
					if m, ok := b.(map[string]interface{}); ok && m["type"] == "tool_use" {
						if id, ok := m["id"].(string); ok && id != "" {
							toolUseIDs[id] = true
						}
					}
				}
			}
		}
	}

	// Neutralize orphan tool_result blocks whose tool_use was pruned away
	for i, t := range retainedTurns {
		if t.Role == "user" {
			if blocks, ok := t.Content.([]interface{}); ok {
				var fixedBlocks []interface{}
				changed := false
				for _, b := range blocks {
					if m, ok := b.(map[string]interface{}); ok && m["type"] == "tool_result" {
						id, _ := m["tool_use_id"].(string)
						if !toolUseIDs[id] {
							// Convert orphan tool_result to text block so upstream doesn't 400
							changed = true
							resStr := ""
							switch c := m["content"].(type) {
							case string:
								resStr = c
							default:
								bRaw, _ := json.Marshal(c)
								resStr = string(bRaw)
							}
							if len(resStr) > 2000 {
								resStr = resStr[:2000] + "... [truncated historical context]"
							}
							fixedBlocks = append(fixedBlocks, map[string]interface{}{
								"type": "text",
								"text": fmt.Sprintf("[Historical tool result: %s]", resStr),
							})
							continue
						}
					}
					fixedBlocks = append(fixedBlocks, b)
				}
				if changed {
					t.Content = fixedBlocks
					retainedTurns[i] = t
				}
			}
		}
	}

	result := make([]providers.ChatMessage, 0, len(systemMessages)+len(retainedTurns))
	result = append(result, systemMessages...)
	result = append(result, retainedTurns...)

	return result
}

func getMessageChars(m providers.ChatMessage) int {
	switch v := m.Content.(type) {
	case string:
		return len(v)
	default:
		b, _ := json.Marshal(v)
		return len(b)
	}
}

// EstimateMessageTokens provides an approximation of token count
func EstimateMessageTokens(messages []providers.ChatMessage) int {
	totalChars := 0
	for _, m := range messages {
		totalChars += getMessageChars(m)
	}
	tokens := totalChars / 4
	if tokens == 0 && totalChars > 0 {
		tokens = 1
	}
	return tokens
}
