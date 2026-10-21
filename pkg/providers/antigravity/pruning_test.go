package antigravity

import (
	"strings"
	"testing"

	"github.com/ziuus/goroute/pkg/providers"
)

func TestTokenEstimationAndPruning(t *testing.T) {
	// 1. Tool result message with complex structure
	toolResultMsg := providers.ChatMessage{
		Role: "tool",
		Content: []interface{}{
			map[string]interface{}{
				"type":    "tool_result",
				"content": strings.Repeat("Large file output ", 10000), // ~180,000 chars
			},
		},
	}

	tokens := estimateMessageTokens(toolResultMsg)
	if tokens < 50000 {
		t.Fatalf("expected at least 50k tokens estimated, got %d", tokens)
	}

	// 2. Test sanitizeMessageSizes
	msgs := []providers.ChatMessage{
		{
			Role:    "user",
			Content: strings.Repeat("a", 50000),
		},
	}
	sanitized := sanitizeMessageSizes(msgs, 1000)
	if s, ok := sanitized[0].Content.(string); !ok || len(s) > 1100 {
		t.Fatalf("expected message to be truncated to ~1000 chars, got length %d", len(s))
	}

	// 3. Test pruneMessagesForContextWindow
	var conversation []providers.ChatMessage
	conversation = append(conversation, providers.ChatMessage{Role: "system", Content: "System prompt"})
	for i := 0; i < 10; i++ {
		conversation = append(conversation, providers.ChatMessage{
			Role:    "user",
			Content: strings.Repeat("Large turn content ", 5000), // ~95,000 chars per turn
		})
		conversation = append(conversation, providers.ChatMessage{
			Role:    "assistant",
			Content: "Response turn",
		})
	}
	conversation = append(conversation, providers.ChatMessage{Role: "user", Content: "Final prompt"})

	pruned := pruneMessagesForContextWindow(conversation, 100000)
	if len(pruned) >= len(conversation) {
		t.Fatalf("expected conversation to be pruned from %d messages, got %d", len(conversation), len(pruned))
	}
	if pruned[0].Role != "system" {
		t.Fatalf("expected first message to remain system prompt, got %s", pruned[0].Role)
	}
	if pruned[len(pruned)-1].Content != "Final prompt" {
		t.Fatalf("expected last message to remain final prompt, got %v", pruned[len(pruned)-1].Content)
	}
}
