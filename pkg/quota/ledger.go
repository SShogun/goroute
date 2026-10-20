package quota

import (
	"strings"
	"sync"
	"time"
)

type ModelPricing struct {
	PromptPer1M     float64 `json:"prompt_per_1m"`
	CompletionPer1M float64 `json:"completion_per_1m"`
}

var DefaultPricing = map[string]ModelPricing{
	// Frontier & Anthropic
	"claude-3-7-sonnet": {PromptPer1M: 3.00, CompletionPer1M: 15.00},
	"claude-3-5-sonnet": {PromptPer1M: 3.00, CompletionPer1M: 15.00},
	"claude-sonnet-4-5": {PromptPer1M: 3.00, CompletionPer1M: 15.00},
	"claude-3-5-haiku":  {PromptPer1M: 0.80, CompletionPer1M: 4.00},
	"claude-haiku-4-5":  {PromptPer1M: 0.80, CompletionPer1M: 4.00},
	"claude-opus":       {PromptPer1M: 15.00, CompletionPer1M: 75.00},

	// OpenAI
	"gpt-4o":            {PromptPer1M: 2.50, CompletionPer1M: 10.00},
	"gpt-4o-mini":       {PromptPer1M: 0.15, CompletionPer1M: 0.60},
	"o1":                {PromptPer1M: 15.00, CompletionPer1M: 60.00},
	"o3":                {PromptPer1M: 10.00, CompletionPer1M: 40.00},
	"o3-mini":           {PromptPer1M: 1.10, CompletionPer1M: 4.40},

	// Google Gemini
	"gemini-2.5-pro":    {PromptPer1M: 1.25, CompletionPer1M: 5.00},
	"gemini-3.1-pro":    {PromptPer1M: 1.25, CompletionPer1M: 5.00},
	"gemini-2.5-flash":  {PromptPer1M: 0.075, CompletionPer1M: 0.30},
	"gemini-3.8-flash":  {PromptPer1M: 0.075, CompletionPer1M: 0.30},

	// DeepSeek
	"deepseek-chat":     {PromptPer1M: 0.14, CompletionPer1M: 0.28},
	"deepseek-reasoner": {PromptPer1M: 0.55, CompletionPer1M: 2.19},
}

type UsageRecord struct {
	Timestamp        time.Time `json:"timestamp"`
	ProviderID       string    `json:"provider_id"`
	Model            string    `json:"model"`
	PromptTokens     int       `json:"prompt_tokens"`
	CompletionTokens int       `json:"completion_tokens"`
	TotalTokens      int       `json:"total_tokens"`
	EstimatedCostUSD float64   `json:"estimated_cost_usd"`
	IsFreeOrOAuth    bool      `json:"is_free_or_oauth"`
}

type LedgerSummary struct {
	TotalRequests     int64              `json:"total_requests"`
	TotalTokens       int64              `json:"total_tokens"`
	TotalCostUSD      float64            `json:"total_cost_usd"`
	TotalSavedUSD     float64            `json:"total_saved_usd"`
	ModelDistribution map[string]int64   `json:"model_distribution"`
	ProviderSpendUSD  map[string]float64 `json:"provider_spend_usd"`
}

type CostLedger struct {
	mu      sync.RWMutex
	records []UsageRecord
	maxSize int
}

var GlobalLedger = NewCostLedger(5000)

func NewCostLedger(maxSize int) *CostLedger {
	return &CostLedger{
		records: make([]UsageRecord, 0, maxSize),
		maxSize: maxSize,
	}
}

func (l *CostLedger) Record(providerID, model string, promptToks, complToks int) {
	l.mu.Lock()
	defer l.mu.Unlock()

	modelLower := strings.ToLower(model)
	// Match pricing
	pricing, ok := DefaultPricing[modelLower]
	if !ok {
		for k, p := range DefaultPricing {
			if strings.Contains(modelLower, k) {
				pricing = p
				ok = true
				break
			}
		}
	}
	if !ok {
		pricing = ModelPricing{PromptPer1M: 1.0, CompletionPer1M: 3.0}
	}

	cost := (float64(promptToks)/1_000_000.0)*pricing.PromptPer1M + (float64(complToks)/1_000_000.0)*pricing.CompletionPer1M
	isOAuthOrFree := providerID == "antigravity" || providerID == "kiro" || providerID == "copilot" || providerID == "codex" || strings.Contains(modelLower, "free")

	rec := UsageRecord{
		Timestamp:        time.Now(),
		ProviderID:       providerID,
		Model:            model,
		PromptTokens:     promptToks,
		CompletionTokens: complToks,
		TotalTokens:      promptToks + complToks,
		EstimatedCostUSD: cost,
		IsFreeOrOAuth:    isOAuthOrFree,
	}

	if len(l.records) >= l.maxSize {
		l.records = l.records[1:]
	}
	l.records = append(l.records, rec)
}

func (l *CostLedger) Summary() LedgerSummary {
	l.mu.RLock()
	defer l.mu.RUnlock()

	summary := LedgerSummary{
		ModelDistribution: make(map[string]int64),
		ProviderSpendUSD:  make(map[string]float64),
	}

	for _, r := range l.records {
		summary.TotalRequests++
		summary.TotalTokens += int64(r.TotalTokens)
		if r.IsFreeOrOAuth {
			summary.TotalSavedUSD += r.EstimatedCostUSD
		} else {
			summary.TotalCostUSD += r.EstimatedCostUSD
		}
		summary.ModelDistribution[r.Model] += int64(r.TotalTokens)
		summary.ProviderSpendUSD[r.ProviderID] += r.EstimatedCostUSD
	}

	return summary
}
