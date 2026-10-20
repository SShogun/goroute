package rtk

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"regexp"
	"sort"
	"strings"
	"sync"
)

//go:embed filters/*.json
var filtersFS embed.FS

type FilterRule struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Description string `json:"description"`
	Category    string `json:"category"`
	Priority    int    `json:"priority"`
	Match       struct {
		Commands []string `json:"commands"`
		Patterns []string `json:"patterns"`
	} `json:"match"`
	Rules struct {
		IncludePatterns []string `json:"includePatterns"`
		DropPatterns    []string `json:"dropPatterns"`
		MaxLines        int      `json:"maxLines"`
		HeadLines       int      `json:"headLines"`
		TailLines       int      `json:"tailLines"`
	} `json:"rules"`

	compiledCommands []*regexp.Regexp
	compiledPatterns []*regexp.Regexp
	compiledIncludes []*regexp.Regexp
	compiledDrops    []*regexp.Regexp
}

type Engine struct {
	mu      sync.RWMutex
	filters []*FilterRule
}

var GlobalEngine = NewEngine()

func NewEngine() *Engine {
	e := &Engine{}
	e.loadFilters()
	return e
}

func (e *Engine) loadFilters() {
	entries, err := fs.ReadDir(filtersFS, "filters")
	if err != nil {
		return
	}

	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		data, err := filtersFS.ReadFile("filters/" + entry.Name())
		if err != nil {
			continue
		}

		var f FilterRule
		if err := json.Unmarshal(data, &f); err != nil {
			continue
		}

		// Compile regexes
		for _, cmd := range f.Match.Commands {
			if r, err := regexp.Compile("(?i)" + cmd); err == nil {
				f.compiledCommands = append(f.compiledCommands, r)
			}
		}
		for _, pat := range f.Match.Patterns {
			if r, err := regexp.Compile("(?i)" + pat); err == nil {
				f.compiledPatterns = append(f.compiledPatterns, r)
			}
		}
		for _, inc := range f.Rules.IncludePatterns {
			if r, err := regexp.Compile("(?i)" + inc); err == nil {
				f.compiledIncludes = append(f.compiledIncludes, r)
			}
		}
		for _, drop := range f.Rules.DropPatterns {
			if r, err := regexp.Compile("(?i)" + drop); err == nil {
				f.compiledDrops = append(f.compiledDrops, r)
			}
		}

		e.filters = append(e.filters, &f)
	}

	// Sort by priority descending
	sort.Slice(e.filters, func(i, j int) bool {
		return e.filters[i].Priority > e.filters[j].Priority
	})
}

type FilterResult struct {
	OriginalLines   int      `json:"original_lines"`
	FilteredLines   int      `json:"filtered_lines"`
	LinesSaved      int      `json:"lines_saved"`
	PercentSaved    float64  `json:"percent_saved"`
	MatchedFilterID string   `json:"matched_filter_id,omitempty"`
	Output          string   `json:"output"`
	Techniques      []string `json:"techniques"`
}

// FilterCommandOutput checks if text matches any command filter and optimizes it
func (e *Engine) FilterCommandOutput(command, output string) FilterResult {
	origLines := strings.Split(output, "\n")
	origCount := len(origLines)

	res := FilterResult{
		OriginalLines: origCount,
		FilteredLines: origCount,
		Output:        output,
	}

	if origCount <= 4 {
		return res
	}

	var matched *FilterRule

	// 1. Try matching by command name
	if command != "" {
		for _, f := range e.filters {
			for _, cr := range f.compiledCommands {
				if cr.MatchString(command) {
					matched = f
					break
				}
			}
			if matched != nil {
				break
			}
		}
	}

	// 2. Try matching by content patterns if command didn't match
	if matched == nil {
		firstFew := strings.Join(origLines[:min(origCount, 8)], "\n")
		for _, f := range e.filters {
			for _, pr := range f.compiledPatterns {
				if pr.MatchString(firstFew) {
					matched = f
					break
				}
			}
			if matched != nil {
				break
			}
		}
	}

	if matched == nil {
		return res
	}

	res.MatchedFilterID = matched.ID
	var filtered []string

	for _, line := range origLines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}

		// Check drop patterns
		drop := false
		for _, dr := range matched.compiledDrops {
			if dr.MatchString(line) {
				drop = true
				break
			}
		}
		if drop {
			continue
		}

		// If include patterns exist, line must match at least one include pattern
		if len(matched.compiledIncludes) > 0 {
			include := false
			for _, ir := range matched.compiledIncludes {
				if ir.MatchString(line) {
					include = true
					break
				}
			}
			if !include {
				continue
			}
		}

		filtered = append(filtered, line)
	}

	// Apply headLines / tailLines windowing if still exceeds maxLines
	maxLines := matched.Rules.MaxLines
	if maxLines <= 0 {
		maxLines = 60
	}

	if len(filtered) > maxLines {
		head := matched.Rules.HeadLines
		if head <= 0 {
			head = 15
		}
		tail := matched.Rules.TailLines
		if tail <= 0 {
			tail = 20
		}
		if head+tail < len(filtered) {
			skipped := len(filtered) - (head + tail)
			windowed := append([]string{}, filtered[:head]...)
			windowed = append(windowed, fmt.Sprintf("... [RTK: %d repetitive lines skipped to preserve context] ...", skipped))
			windowed = append(windowed, filtered[len(filtered)-tail:]...)
			filtered = windowed
			res.Techniques = append(res.Techniques, "head-tail-windowing")
		}
	}

	res.FilteredLines = len(filtered)
	res.LinesSaved = origCount - len(filtered)
	if origCount > 0 {
		res.PercentSaved = (float64(res.LinesSaved) / float64(origCount)) * 100.0
	}
	res.Output = strings.Join(filtered, "\n")
	res.Techniques = append(res.Techniques, fmt.Sprintf("rtk-%s", matched.ID))

	return res
}

func (e *Engine) ListFilters() []map[string]interface{} {
	var list []map[string]interface{}
	for _, f := range e.filters {
		list = append(list, map[string]interface{}{
			"id":          f.ID,
			"label":       f.Label,
			"description": f.Description,
			"category":    f.Category,
			"priority":    f.Priority,
			"commands":    f.Match.Commands,
		})
	}
	return list
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
