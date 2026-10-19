package proxy

import (
	"testing"
)

func TestExtractXmlToolCalls(t *testing.T) {
	// 1. Test <tool_use>
	input := `Let me check the files first.
<tool_use>
{"name":"Bash","id":"toolu_01YHGZoSbJB58KJRj7Ak3VsN","input":{"command":"cat /home/zius/Projects/liquid-glass/src/main.rs"}}
</tool_use>
<tool_use>
{"name":"Bash","id":"toolu_01PxNDVi1r8R1FbmJMBZzD3d","input":{"command":"cat /home/zius/Projects/liquid-glass/Cargo.toml"}}
</tool_use>`

	cleaned, calls := ExtractXmlToolCalls(input)
	if cleaned != "Let me check the files first." {
		t.Errorf("unexpected cleaned text: %q", cleaned)
	}
	if len(calls) != 2 {
		t.Fatalf("expected 2 tool calls, got %d", len(calls))
	}
	if calls[0].Name != "Bash" || calls[0].ID != "toolu_01YHGZoSbJB58KJRj7Ak3VsN" {
		t.Errorf("tool 0 mismatch: %+v", calls[0])
	}
	if calls[0].Input["command"] != "cat /home/zius/Projects/liquid-glass/src/main.rs" {
		t.Errorf("tool 0 input mismatch: %+v", calls[0].Input)
	}
	if calls[1].Name != "Bash" || calls[1].ID != "toolu_01PxNDVi1r8R1FbmJMBZzD3d" {
		t.Errorf("tool 1 mismatch: %+v", calls[1])
	}

	// 2. Test <invoke>
	invokeInput := `Here is what I will run:
<invoke name="Bash">
<parameter name="command">ls -la</parameter>
</invoke>`
	cleaned2, calls2 := ExtractXmlToolCalls(invokeInput)
	if cleaned2 != "Here is what I will run:" {
		t.Errorf("unexpected cleaned text: %q", cleaned2)
	}
	if len(calls2) != 1 || calls2[0].Name != "Bash" || calls2[0].Input["command"] != "ls -la" {
		t.Errorf("unexpected invoke tool call: %+v", calls2)
	}

	// 3. Test <tool_call>
	toolCallInput := `Running now:
<tool_call>
{"name": "Read", "arguments": {"path": "main.go"}}
</tool_call>`
	cleaned3, calls3 := ExtractXmlToolCalls(toolCallInput)
	if cleaned3 != "Running now:" {
		t.Errorf("unexpected cleaned text: %q", cleaned3)
	}
	if len(calls3) != 1 || calls3[0].Name != "Read" || calls3[0].Input["path"] != "main.go" {
		t.Errorf("unexpected tool_call: %+v", calls3)
	}

	// 4. Test streaming across chunk boundaries
	parser := NewXmlStreamParser()
	var allCalls []ExtractedToolCall
	var fullText string

	c1, t1 := parser.Feed("I am going to read ")
	fullText += c1
	allCalls = append(allCalls, t1...)

	c2, t2 := parser.Feed("<tool_use>\n{\"name\":")
	fullText += c2
	allCalls = append(allCalls, t2...)

	c3, t3 := parser.Feed("\"Bash\",\"input\":{\"command\":\"cargo check\"}}\n</tool_use>")
	fullText += c3
	allCalls = append(allCalls, t3...)

	fullText += parser.Flush()

	if fullText != "I am going to read " {
		t.Errorf("expected text 'I am going to read ', got %q", fullText)
	}
	if len(allCalls) != 1 {
		t.Fatalf("expected 1 call across chunks, got %d", len(allCalls))
	}
	if allCalls[0].Name != "Bash" || allCalls[0].Input["command"] != "cargo check" {
		t.Errorf("mismatched streaming call: %+v", allCalls[0])
	}
}
