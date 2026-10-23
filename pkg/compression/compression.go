package compression

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/ziuus/goroute/pkg/fast"
)

var (
	multiNewlineRegex  = regexp.MustCompile(`\n{3,}`)
	multiSpaceRegex    = regexp.MustCompile(`[ \t]{2,}`)
	trailingSpaceRegex = regexp.MustCompile(`(?m)[ \t]+$`)

	// Protection regexes for syntax and literals
	codeBlockRegex  = regexp.MustCompile("(?s)```.*?```")
	inlineCodeRegex = regexp.MustCompile("`[^`\n]+`")
	markdownLinkRegex = regexp.MustCompile(`!?\[.*?\]\(.*?\)`)

	// Intensity: Lite (Polite framing & greetings)
	pleasantriesRegex = regexp.MustCompile(`(?i)\b(?:thank you so much|thank you|thanks a lot|thanks|no problem|you'?re welcome|i'?d be happy to|i would be glad to|glad to help|of course|certainly|cheers)\b[,.!?\s]*`)
	politeRegex       = regexp.MustCompile(`(?i)\b(?:could you please|would you please|can you please|would you kindly|could you kindly|please|kindly|i would like you to|i need you to|i want you to|i would be (?:very )?grateful if you could|i would appreciate it if you could)\b\s*`)

	// Intensity: Full (Hedging & verbose prompts)
	hedgingRegex = regexp.MustCompile(`(?i)\b(?:it seems like|it appears that|i think that|i believe that|in my opinion|probably|possibly)\b\s*`)
	verboseRegex = regexp.MustCompile(`(?i)\b(?:provide a detailed explanation of|give me a comprehensive explanation of|write an in-depth explanation of|give me an in-depth breakdown of|explain in detail|provide a step-by-step guide to)\b\s*`)

	// Intensity: Ultra (Redundant connectives & filler words)
	connectivesRegex = regexp.MustCompile(`(?i)\b(?:due to the fact that|the reason is because|owing to the fact that)\b\s*`)
	inOrderToRegex   = regexp.MustCompile(`(?i)\b(?:in order to|for the purpose of)\b\s*`)
	asWellAsRegex    = regexp.MustCompile(`(?i)\b(?:as well as)\b\s*`)
)

type CompressionStats struct {
	OriginalChars   int      `json:"original_chars"`
	CompressedChars int      `json:"compressed_chars"`
	CharsSaved      int      `json:"chars_saved"`
	PercentSaved    float64  `json:"percent_saved"`
	EstTokensSaved  int      `json:"est_tokens_saved"`
	Techniques      []string `json:"techniques"`
	Intensity       string   `json:"intensity"`
}

// CompressCaveman applies Caveman linguistic reduction with intensity level (lite, full, ultra)
func CompressCaveman(input string, intensity string) (string, []string) {
	if len(input) == 0 {
		return input, nil
	}
	if intensity == "" {
		intensity = "full"
	}

	// High-speed Rust SIMD acceleration when available
	if fast.IsAvailable() {
		level := int32(2)
		if intensity == "lite" {
			level = 1
		} else if intensity == "ultra" {
			level = 3
		}
		if res, ok := fast.Compress(input, level); ok && res != input {
			return res, []string{"rust-simd-caveman", "rust-zero-alloc-pruning"}
		}
	}

	var techniques []string

	// 1. Extract and protect code blocks
	var protected []string
	protectPlaceholder := func(match string) string {
		idx := len(protected)
		protected = append(protected, match)
		return fmt.Sprintf("___GR_PROT_%d___", idx)
	}

	prose := codeBlockRegex.ReplaceAllStringFunc(input, protectPlaceholder)
	prose = inlineCodeRegex.ReplaceAllStringFunc(prose, protectPlaceholder)
	prose = markdownLinkRegex.ReplaceAllStringFunc(prose, protectPlaceholder)

	// 2. Apply rules based on intensity
	// Level: Lite
	if pleasantriesRegex.MatchString(prose) || politeRegex.MatchString(prose) {
		prose = pleasantriesRegex.ReplaceAllString(prose, "")
		prose = politeRegex.ReplaceAllString(prose, "")
		techniques = append(techniques, "filler-removal")
	}

	// Level: Full
	if intensity == "full" || intensity == "ultra" {
		if hedgingRegex.MatchString(prose) || verboseRegex.MatchString(prose) {
			prose = hedgingRegex.ReplaceAllString(prose, "")
			prose = verboseRegex.ReplaceAllString(prose, "explain ")
			techniques = append(techniques, "instruction-condensing")
		}
	}

	// Level: Ultra
	if intensity == "ultra" {
		if connectivesRegex.MatchString(prose) || inOrderToRegex.MatchString(prose) || asWellAsRegex.MatchString(prose) {
			prose = connectivesRegex.ReplaceAllString(prose, "because ")
			prose = inOrderToRegex.ReplaceAllString(prose, "to ")
			prose = asWellAsRegex.ReplaceAllString(prose, "and ")
			techniques = append(techniques, "connective-reduction")
		}
	}

	// 3. Restore protected literals
	for i, orig := range protected {
		ph := fmt.Sprintf("___GR_PROT_%d___", i)
		prose = strings.Replace(prose, ph, orig, 1)
	}

	return prose, techniques
}

// CompressText combines structural whitespace optimization with Caveman linguistic compression
func CompressText(input string) (string, CompressionStats) {
	return CompressWithOptions(input, "full")
}

// CompressWithOptions provides configurable intensity compression
func CompressWithOptions(input string, intensity string) (string, CompressionStats) {
	origLen := len(input)
	if origLen == 0 {
		return input, CompressionStats{Intensity: intensity}
	}

	// 1. Caveman linguistic reduction
	res, techniques := CompressCaveman(input, intensity)

	// 2. Structural whitespace optimization
	res = trailingSpaceRegex.ReplaceAllString(res, "")
	res = multiNewlineRegex.ReplaceAllString(res, "\n\n")
	res = multiSpaceRegex.ReplaceAllString(res, " ")

	compLen := len(res)
	saved := origLen - compLen
	if saved < 0 {
		saved = 0
	}

	percent := 0.0
	if origLen > 0 {
		percent = (float64(saved) / float64(origLen)) * 100.0
	}

	techniques = append(techniques, "whitespace-collapse")

	stats := CompressionStats{
		OriginalChars:   origLen,
		CompressedChars: compLen,
		CharsSaved:      saved,
		PercentSaved:    percent,
		EstTokensSaved:  saved / 4,
		Techniques:      techniques,
		Intensity:       intensity,
	}

	return strings.TrimSpace(res), stats
}
