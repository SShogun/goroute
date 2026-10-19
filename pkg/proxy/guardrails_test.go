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
					"type": "thinking",
					"thinking": "some internal thought",
				},
				map[string]interface{}{
					"type": "tool_result",
					"tool_use_id": "toolu_123",
					"content": hugeToolOutput,
				},
			},
		},
		{
			Role: "user",
			Content: "follow up prompt",
		},
	}

	sanitized := SanitizeAndGuardrailMessages(msgs, 50000)

	// Verify thinking was pruned from turn 0 (non-latest turn)
	firstTurnBlocks, ok := sanitized[0].Content.([]interface{})
	if !ok {
		t.Fatalf("expected []interface{} for content")
	}

	if len(firstTurnBlocks) != 1 {
		t.Fatalf("expected thinking block to be stripped, got %d blocks", len(firstTurnBlocks))
	}

	toolResultBlock := firstTurnBlocks[0].(map[string]interface{})
	contentStr := toolResultBlock["content"].(string)

	if len(contentStr) >= 100000 {
		t.Fatalf("expected tool_result content to be truncated, got %d chars", len(contentStr))
	}

	if !strings.Contains(contentStr, "goroute context guard: truncated") {
		t.Fatalf("expected truncation marker in output")
	}
}
