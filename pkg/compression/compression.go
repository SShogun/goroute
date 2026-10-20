package compression

import (
	"regexp"
	"strings"
)

var (
	multiNewlineRegex  = regexp.MustCompile(`\n{3,}`)
	multiSpaceRegex    = regexp.MustCompile(`[ \t]{2,}`)
	trailingSpaceRegex = regexp.MustCompile(`(?m)[ \t]+$`)
	codeBlockRegex     = regexp.MustCompile("(?s)```.*?```")

	// Caveman linguistic reduction rules (filler & conversational noise removal)
	pleasantriesRegex = regexp.MustCompile(`(?i)\b(?:thank you|thanks|no problem|you'?re welcome|i'?d be happy to|i would be glad to|glad to help|of course|certainly)\b[,.!?\s]*`)
	politeRegex       = regexp.MustCompile(`(?i)\b(?:could you please|would you please|can you please|please|kindly|i would like you to|i need you to)\b\s*`)
	hedgingRegex      = regexp.MustCompile(`(?i)\b(?:it seems like|it appears that|i think that|i believe that|probably|possibly)\b\s*`)
	verboseRegex      = regexp.MustCompile(`(?i)\b(?:provide a detailed explanation of|give me a comprehensive explanation of|write an in-depth explanation of|explain in detail)\b\s*`)
	redundantRegex    = regexp.MustCompile(`(?i)\b(?:due to the fact that|the reason is because)\b\s*`)
)

type CompressionStats struct {
	OriginalChars   int     `json:"original_chars"`
	CompressedChars int     `json:"compressed_chars"`
	CharsSaved      int     `json:"chars_saved"`
	PercentSaved    float64 `json:"percent_saved"`
	EstTokensSaved  int     `json:"est_tokens_saved"`
	Techniques      []string `json:"techniques"`
}

// CompressCaveman applies OmniRoute-style Caveman linguistic reduction to prose while keeping code blocks intact
func CompressCaveman(input string) string {
	// Preserve code blocks verbatim
	codeBlocks := codeBlockRegex.FindAllString(input, -1)
	placeholders := make([]string, len(codeBlocks))
	for i := range codeBlocks {
		placeholders[i] = "___CODE_BLOCK_" + string(rune('A'+i)) + "___"
	}

	prose := input
	for i, cb := range codeBlocks {
		prose = strings.Replace(prose, cb, placeholders[i], 1)
	}

	// Apply linguistic compression rules to prose
	prose = pleasantriesRegex.ReplaceAllString(prose, "")
	prose = politeRegex.ReplaceAllString(prose, "")
	prose = hedgingRegex.ReplaceAllString(prose, "")
	prose = verboseRegex.ReplaceAllString(prose, "explain ")
	prose = redundantRegex.ReplaceAllString(prose, "because ")

	// Restore code blocks
	for i, ph := range placeholders {
		prose = strings.Replace(prose, ph, codeBlocks[i], 1)
	}

	return prose
}

// CompressText combines structural whitespace optimization with Caveman linguistic compression
func CompressText(input string) (string, CompressionStats) {
	origLen := len(input)
	if origLen == 0 {
		return input, CompressionStats{}
	}

	// 1. Caveman linguistic reduction
	res := CompressCaveman(input)

	// 2. Strip trailing spaces on lines
	res = trailingSpaceRegex.ReplaceAllString(res, "")

	// 3. Collapse runs of 3+ newlines down to 2
	res = multiNewlineRegex.ReplaceAllString(res, "\n\n")

	// 4. Normalize runs of tabs and spaces
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

	stats := CompressionStats{
		OriginalChars:   origLen,
		CompressedChars: compLen,
		CharsSaved:      saved,
		PercentSaved:    percent,
		EstTokensSaved:  saved / 4,
		Techniques:      []string{"caveman-linguistic", "whitespace-collapse"},
	}

	return strings.TrimSpace(res), stats
}
