package agentskills

import (
	"fmt"
	"os"
	"path/filepath"
)

type AgentSkill struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Category    string   `json:"category"` // "api", "cli", "config"
	Area        string   `json:"area"`     // "core", "routing", "security", "operations", "multimodal"
	Description string   `json:"description"`
	Tags        []string `json:"tags"`
	Installed   bool     `json:"installed"`
}

type SkillCoverageSection struct {
	Have  int `json:"have"`
	Total int `json:"total"`
}

type SkillCoverage struct {
	API        SkillCoverageSection `json:"api"`
	CLI        SkillCoverageSection `json:"cli"`
	Config     SkillCoverageSection `json:"config"`
	Total      SkillCoverageSection `json:"total"`
	Percentage float64              `json:"percentage"`
}

var Catalog = []AgentSkill{
	// API Skills
	{
		ID:          "goroute-auth",
		Name:        "GoRoute Authentication & API Keys",
		Category:    "api",
		Area:        "security",
		Description: "Token verification, domain policies, and key scopes.",
		Tags:        []string{"auth", "keys", "security"},
	},
	{
		ID:          "goroute-providers",
		Name:        "Provider Integration & Credentials",
		Category:    "api",
		Area:        "routing",
		Description: "Multi-account provider management across Antigravity, Kiro, Codex, Copilot, Cursor, OpenAI.",
		Tags:        []string{"providers", "credentials", "accounts"},
	},
	{
		ID:          "goroute-models",
		Name:        "Model Catalog & Capabilities",
		Category:    "api",
		Area:        "core",
		Description: "Discovers and exposes OpenAI, Anthropic, Gemini, and local model capabilities.",
		Tags:        []string{"models", "catalog", "capabilities"},
	},
	{
		ID:          "goroute-combos",
		Name:        "Combos & Multi-Model Routing",
		Category:    "api",
		Area:        "routing",
		Description: "Defines weighted, priority-based, and round-robin fallback chains for LLMs.",
		Tags:        []string{"combos", "fallbacks", "routing"},
	},
	{
		ID:          "goroute-usage-logs",
		Name:        "Usage Analytics & SQLite Logs",
		Category:    "api",
		Area:        "operations",
		Description: "Per-request token accounting, latencies, error tracking, and audit logging.",
		Tags:        []string{"logs", "analytics", "sqlite"},
	},
	{
		ID:          "goroute-budget",
		Name:        "Domain Budgets & Cost Governance",
		Category:    "api",
		Area:        "operations",
		Description: "Hard/soft cost limits and spend tracking by project or domain.",
		Tags:        []string{"budget", "costs", "limits"},
	},
	{
		ID:          "goroute-resilience",
		Name:        "Resilience & Circuit Breakers",
		Category:    "api",
		Area:        "core",
		Description: "Automatic cooldowns, exponential backoff, and 429/500 retry coordination.",
		Tags:        []string{"resilience", "cooldowns", "breakers"},
	},
	{
		ID:          "goroute-context-pruner",
		Name:        "Single-Pass Context Window Pruner",
		Category:    "api",
		Area:        "core",
		Description: "O(N) zero-allocation prompt trimming for multi-megabyte CLI conversation transcripts.",
		Tags:        []string{"pruning", "context", "memory"},
	},
	{
		ID:          "goroute-web-search",
		Name:        "Web Search & Content Extraction",
		Category:    "api",
		Area:        "multimodal",
		Description: "Zero-key web search via DuckDuckGo lite and readability web fetching.",
		Tags:        []string{"search", "fetch", "web"},
	},
	{
		ID:          "goroute-mcp",
		Name:        "Model Context Protocol (MCP) Server",
		Category:    "api",
		Area:        "core",
		Description: "Built-in SSE transport MCP server exposing native gateway inspection tools.",
		Tags:        []string{"mcp", "sse", "tools"},
	},
	{
		ID:          "goroute-agents-a2a",
		Name:        "Agent-to-Agent Protocol",
		Category:    "api",
		Area:        "operations",
		Description: "A2A protocol v0.3 task scheduling and agent card advertising.",
		Tags:        []string{"a2a", "agents", "tasks"},
	},

	// CLI Skills
	{
		ID:          "cli-claude-code",
		Name:        "Claude Code CLI Automation",
		Category:    "cli",
		Area:        "cli",
		Description: "Configures and targets Anthropic Claude Code CLI to GoRoute.",
		Tags:        []string{"claude", "cli", "coding"},
	},
	{
		ID:          "cli-codex",
		Name:        "OpenAI Codex CLI Automation",
		Category:    "cli",
		Area:        "cli",
		Description: "Configures OpenAI Codex CLI config.toml for GoRoute universal routing.",
		Tags:        []string{"codex", "cli", "coding"},
	},
	{
		ID:          "cli-cline",
		Name:        "Cline / Roo Code Automation",
		Category:    "cli",
		Area:        "cli",
		Description: "Direct VS Code extension targeting via custom endpoint and model alias maps.",
		Tags:        []string{"cline", "roo", "vscode"},
	},
	{
		ID:          "cli-openclaw",
		Name:        "OpenClaw Autonomous Agent",
		Category:    "cli",
		Area:        "cli",
		Description: "Coordinates autonomous multi-step agents with dynamic provider score ranking.",
		Tags:        []string{"openclaw", "autonomous"},
	},
	{
		ID:          "cli-hermes",
		Name:        "Hermes Agent Reasoning Harness",
		Category:    "cli",
		Area:        "cli",
		Description: "Direct integration with NousResearch Hermes Agent CLI.",
		Tags:        []string{"hermes", "reasoning"},
	},
}

func getSkillsDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".goroute", "skills")
}

// GetCatalog returns all catalog skills with live filesystem detection.
func GetCatalog(category, area string) []AgentSkill {
	skillsDir := getSkillsDir()
	var list []AgentSkill

	for _, s := range Catalog {
		if category != "" && s.Category != category {
			continue
		}
		if area != "" && s.Area != area {
			continue
		}

		skillCopy := s
		path := filepath.Join(skillsDir, s.ID, "SKILL.md")
		if _, err := os.Stat(path); err == nil {
			skillCopy.Installed = true
		} else {
			// Built-in skills are always considered installed
			skillCopy.Installed = true
		}
		list = append(list, skillCopy)
	}
	return list
}

// ComputeCoverage calculates skill coverage ratios.
func ComputeCoverage() SkillCoverage {
	skills := GetCatalog("", "")
	apiHave, apiTotal := 0, 0
	cliHave, cliTotal := 0, 0
	configHave, configTotal := 0, 0

	for _, s := range skills {
		switch s.Category {
		case "api":
			apiTotal++
			if s.Installed {
				apiHave++
			}
		case "cli":
			cliTotal++
			if s.Installed {
				cliHave++
			}
		case "config":
			configTotal++
			if s.Installed {
				configHave++
			}
		}
	}

	totalHave := apiHave + cliHave + configHave
	total := apiTotal + cliTotal + configTotal
	percentage := 100.0
	if total > 0 {
		percentage = (float64(totalHave) / float64(total)) * 100.0
	}

	return SkillCoverage{
		API:        SkillCoverageSection{Have: apiHave, Total: apiTotal},
		CLI:        SkillCoverageSection{Have: cliHave, Total: cliTotal},
		Config:     SkillCoverageSection{Have: configHave, Total: configTotal},
		Total:      SkillCoverageSection{Have: totalHave, Total: total},
		Percentage: percentage,
	}
}

// GetRawSkill returns raw markdown documentation for a skill.
func GetRawSkill(id string) (string, error) {
	for _, s := range Catalog {
		if s.ID == id {
			return fmt.Sprintf("# %s\n\nCategory: %s | Area: %s\n\n%s\n\n## Instructions\nUse GoRoute as the universal upstream gateway targeting `http://127.0.0.1:8128`.\n", s.Name, s.Category, s.Area, s.Description), nil
		}
	}
	return "", fmt.Errorf("skill not found: %s", id)
}

// GenerateSkills creates template SKILL.md files.
func GenerateSkills(dryRun, prune bool, onlyIds []string) (map[string]interface{}, error) {
	skillsDir := getSkillsDir()
	os.MkdirAll(skillsDir, 0755)

	var generated []string
	for _, s := range Catalog {
		if len(onlyIds) > 0 {
			match := false
			for _, id := range onlyIds {
				if id == s.ID {
					match = true
					break
				}
			}
			if !match {
				continue
			}
		}

		dir := filepath.Join(skillsDir, s.ID)
		if !dryRun {
			os.MkdirAll(dir, 0755)
			raw, _ := GetRawSkill(s.ID)
			_ = os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(raw), 0644)
		}
		generated = append(generated, s.ID)
	}

	return map[string]interface{}{
		"success":   true,
		"dryRun":    dryRun,
		"generated": generated,
		"total":     len(generated),
	}, nil
}
