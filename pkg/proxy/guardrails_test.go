package proxy

import (
	"strings"
	"testing"

	"github.com/ziuus/goroute/pkg/providers"
)

func TestSanitizeAndGuardrailMessages(t *testing.T) {
	hugeToolOutput := strings.Repeat("A", 100000)

	msgs := []providers.ChatMessage{
		{
			Role: "user",
			Content: []interface{}{
				map[string]interface{}{
					"type":     "thinking",
					"thinking": "some internal thought",
				},
				map[string]interface{}{
					"type":        "tool_result",
					"tool_use_id": "toolu_123",
					"content":     hugeToolOutput,
				},
			},
		},
		{
			Role:    "user",
			Content: "follow up prompt",
		},
	}

	sanitized := SanitizeAndGuardrailMessages(msgs, 0)

	// Verify thinking was pruned from turn 0 (non-latest turn, required by Anthropic)
	firstTurnBlocks, ok := sanitized[0].Content.([]interface{})
	if !ok {
		t.Fatalf("expected []interface{} for content")
	}

	if len(firstTurnBlocks) != 1 {
		t.Fatalf("expected thinking block to be stripped, got %d blocks", len(firstTurnBlocks))
	}

	toolResultBlock := firstTurnBlocks[0].(map[string]interface{})
	contentStr := toolResultBlock["content"].(string)

	// Verify full fidelity passthrough: NO artificial truncation
	if len(contentStr) != 100000 {
		t.Fatalf("expected full 100000 chars preserved without artificial limit, got %d", len(contentStr))
	}

	if len(sanitized) != 2 {
		t.Fatalf("expected all conversation turns preserved, got %d", len(sanitized))
	}
}
