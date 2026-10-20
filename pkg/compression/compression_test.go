package compression

import (
	"strings"
	"testing"
)

func TestCompressCavemanPreservesCodeBlocks(t *testing.T) {
	input := "Could you please review this code:\n```go\nfunc please(thankYou string) {\n    fmt.Println(thankYou)\n}\n```\nThank you so much!"
	compressed, stats := CompressText(input)

	if !strings.Contains(compressed, "func please(thankYou string)") {
		t.Fatalf("code block was corrupted: %s", compressed)
	}

	if strings.Contains(compressed, "Could you please") {
		t.Fatalf("polite framing was not removed: %s", compressed)
	}

	if strings.Contains(compressed, "Thank you so much") {
		t.Fatalf("pleasantries were not removed: %s", compressed)
	}

	if stats.CharsSaved <= 0 {
		t.Fatalf("expected positive chars saved, got %d", stats.CharsSaved)
	}
}

func TestCompressInlineCodeAndLinks(t *testing.T) {
	input := "Please inspect `git commit -m 'please'` and visit [Please Link](https://example.com/please)."
	compressed, _ := CompressText(input)

	if !strings.Contains(compressed, "`git commit -m 'please'`") {
		t.Fatalf("inline code backticks corrupted: %s", compressed)
	}

	if !strings.Contains(compressed, "[Please Link](https://example.com/please)") {
		t.Fatalf("markdown link was corrupted: %s", compressed)
	}
}

func TestCompressIntensities(t *testing.T) {
	input := "Due to the fact that I need you to write an in-depth explanation of Go, please do it in order to help me."

	lite, _ := CompressWithOptions(input, "lite")
	if !strings.Contains(lite, "Due to the fact that") {
		t.Fatalf("lite intensity should not remove connectives: %s", lite)
	}

	ultra, statsUltra := CompressWithOptions(input, "ultra")
	if strings.Contains(ultra, "Due to the fact that") {
		t.Fatalf("ultra intensity should replace 'Due to the fact that': %s", ultra)
	}
	if !strings.Contains(ultra, "because") {
		t.Fatalf("ultra intensity should replace with 'because': %s", ultra)
	}

	if statsUltra.PercentSaved < 20 {
		t.Fatalf("ultra should save significant tokens, got %f%%", statsUltra.PercentSaved)
	}
}
