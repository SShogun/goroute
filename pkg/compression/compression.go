package compression

import (
	"regexp"
	"strings"
)

var (
	multiNewlineRegex = regexp.MustCompile(`\n{3,}`)
	multiSpaceRegex   = regexp.MustCompile(`[ \t]{2,}`)
	trailingSpaceRegex = regexp.MustCompile(`(?m)[ \t]+$`)
)

type CompressionStats struct {
	OriginalChars   int     `json:"original_chars"`
	CompressedChars int     `json:"compressed_chars"`
	CharsSaved      int     `json:"chars_saved"`
	PercentSaved    float64 `json:"percent_saved"`
	EstTokensSaved  int     `json:"est_tokens_saved"`
}

// CompressText optimizes prompt text by removing excess whitespace, runs of newlines, and trailing spaces
func CompressText(input string) (string, CompressionStats) {
	origLen := len(input)
	if origLen == 0 {
		return input, CompressionStats{}
	}

	// 1. Strip trailing spaces on lines
	res := trailingSpaceRegex.ReplaceAllString(input, "")

	// 2. Collapse runs of 3+ newlines down to 2
	res = multiNewlineRegex.ReplaceAllString(res, "\n\n")

	// 3. Normalize runs of tabs and spaces
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
	}

	return strings.TrimSpace(res), stats
}
