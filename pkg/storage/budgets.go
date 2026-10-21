package storage

import (
	"database/sql"
	"fmt"
	"sync"
	"time"
)

// DomainBudget stores spend constraints for a specific API key or domain.
type DomainBudget struct {
	APIKeyID          string  `json:"apiKeyId"`
	DailyLimitUSD     float64 `json:"dailyLimitUsd"`
	WeeklyLimitUSD    float64 `json:"weeklyLimitUsd"`
	MonthlyLimitUSD   float64 `json:"monthlyLimitUsd"`
	WarningThreshold  float64 `json:"warningThreshold"`
	ResetInterval     string  `json:"resetInterval"`
	ResetTime         string  `json:"resetTime"`
	BudgetResetAt     int64   `json:"budgetResetAt"`
	LastBudgetResetAt int64   `json:"lastBudgetResetAt"`
}

// BudgetCheckResult describes the quota status for a key.
type BudgetCheckResult struct {
	Allowed      bool    `json:"allowed"`
	Exceeded     bool    `json:"exceeded"`
	Warning      bool    `json:"warning"`
	PercentUsed  float64 `json:"percentUsed"`
	RemainingUSD float64 `json:"remainingUsd"`
}

// BudgetSummary is the comprehensive cost and budget status.
type BudgetSummary struct {
	APIKeyID          string            `json:"apiKeyId"`
	DailyLimitUSD     float64           `json:"dailyLimitUsd"`
	WeeklyLimitUSD    float64           `json:"weeklyLimitUsd"`
	MonthlyLimitUSD   float64           `json:"monthlyLimitUsd"`
	WarningThreshold  float64           `json:"warningThreshold"`
	ResetInterval     string            `json:"resetInterval"`
	ResetTime         string            `json:"resetTime"`
	BudgetResetAt     int64             `json:"budgetResetAt"`
	LastBudgetResetAt int64             `json:"lastBudgetResetAt"`
	TotalCostToday    float64           `json:"totalCostToday"`
	TotalCostMonth    float64           `json:"totalCostMonth"`
	TotalCostPeriod   float64           `json:"totalCostPeriod"`
	ActiveLimitUSD    float64           `json:"activeLimitUsd"`
	NextResetAt       string            `json:"nextResetAt"`
	PeriodStartAt     string            `json:"periodStartAt"`
	BudgetCheck       BudgetCheckResult `json:"budgetCheck"`
}

var budgetTableInit sync.Once

func initBudgetTables(db *sql.DB) error {
	schema := `
	CREATE TABLE IF NOT EXISTS domain_budgets (
		api_key_id TEXT PRIMARY KEY,
		daily_limit_usd REAL DEFAULT 0,
		weekly_limit_usd REAL DEFAULT 0,
		monthly_limit_usd REAL DEFAULT 0,
		warning_threshold REAL DEFAULT 0.8,
		reset_interval TEXT DEFAULT 'daily',
		reset_time TEXT DEFAULT '00:00',
		budget_reset_at INTEGER DEFAULT 0,
		last_budget_reset_at INTEGER DEFAULT 0,
		warning_emitted_at INTEGER DEFAULT 0,
		warning_period_start INTEGER DEFAULT 0,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS domain_cost_history (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		api_key_id TEXT NOT NULL,
		cost_usd REAL NOT NULL,
		model TEXT,
		provider TEXT,
		timestamp DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_cost_history_key_time ON domain_cost_history(api_key_id, timestamp);
	`
	_, err := db.Exec(schema)
	return err
}

func ensureBudgetInit(db *sql.DB) {
	budgetTableInit.Do(func() {
		_ = initBudgetTables(db)
	})
}

// RecordDomainCost records spend for an API key.
func (s *DBStore) RecordDomainCost(apiKeyID string, costUSD float64, model, provider string) error {
	if s == nil || s.db == nil || apiKeyID == "" || costUSD <= 0 {
		return nil
	}
	ensureBudgetInit(s.db)

	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec(`
		INSERT INTO domain_cost_history (api_key_id, cost_usd, model, provider)
		VALUES (?, ?, ?, ?)
	`, apiKeyID, costUSD, model, provider)
	return err
}

// SetBudget persists or updates budget limits for a key.
func (s *DBStore) SetBudget(b DomainBudget) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("database not initialized")
	}
	ensureBudgetInit(s.db)

	if b.ResetInterval == "" {
		b.ResetInterval = "daily"
	}
	if b.ResetTime == "" {
		b.ResetTime = "00:00"
	}
	if b.WarningThreshold <= 0 {
		b.WarningThreshold = 0.8
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec(`
		INSERT INTO domain_budgets (
			api_key_id, daily_limit_usd, weekly_limit_usd, monthly_limit_usd,
			warning_threshold, reset_interval, reset_time, budget_reset_at,
			last_budget_reset_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(api_key_id) DO UPDATE SET
			daily_limit_usd = excluded.daily_limit_usd,
			weekly_limit_usd = excluded.weekly_limit_usd,
			monthly_limit_usd = excluded.monthly_limit_usd,
			warning_threshold = excluded.warning_threshold,
			reset_interval = excluded.reset_interval,
			reset_time = excluded.reset_time,
			budget_reset_at = excluded.budget_reset_at,
			last_budget_reset_at = excluded.last_budget_reset_at,
			updated_at = CURRENT_TIMESTAMP
	`, b.APIKeyID, b.DailyLimitUSD, b.WeeklyLimitUSD, b.MonthlyLimitUSD,
		b.WarningThreshold, b.ResetInterval, b.ResetTime, b.BudgetResetAt, b.LastBudgetResetAt)
	return err
}

// GetCostSummary computes current spend and compares against configured limits.
func (s *DBStore) GetCostSummary(apiKeyID string) (BudgetSummary, error) {
	summary := BudgetSummary{
		APIKeyID:         apiKeyID,
		WarningThreshold: 0.8,
		ResetInterval:    "daily",
		ResetTime:        "00:00",
		NextResetAt:      time.Now().UTC().Truncate(24 * time.Hour).Add(24 * time.Hour).Format(time.RFC3339),
		PeriodStartAt:    time.Now().UTC().Truncate(24 * time.Hour).Format(time.RFC3339),
		BudgetCheck: BudgetCheckResult{
			Allowed:  true,
			Exceeded: false,
		},
	}

	if s == nil || s.db == nil {
		return summary, nil
	}
	ensureBudgetInit(s.db)

	s.mu.Lock()
	defer s.mu.Unlock()

	// 1. Fetch budget config if present
	row := s.db.QueryRow(`
		SELECT daily_limit_usd, weekly_limit_usd, monthly_limit_usd,
		       warning_threshold, reset_interval, reset_time, budget_reset_at, last_budget_reset_at
		FROM domain_budgets WHERE api_key_id = ?
	`, apiKeyID)

	_ = row.Scan(
		&summary.DailyLimitUSD,
		&summary.WeeklyLimitUSD,
		&summary.MonthlyLimitUSD,
		&summary.WarningThreshold,
		&summary.ResetInterval,
		&summary.ResetTime,
		&summary.BudgetResetAt,
		&summary.LastBudgetResetAt,
	)

	// 2. Query today's cost
	var costToday float64
	_ = s.db.QueryRow(`
		SELECT COALESCE(SUM(cost_usd), 0.0)
		FROM domain_cost_history
		WHERE api_key_id = ? AND timestamp >= datetime('now', 'start of day')
	`, apiKeyID).Scan(&costToday)
	summary.TotalCostToday = costToday

	// 3. Query monthly cost
	var costMonth float64
	_ = s.db.QueryRow(`
		SELECT COALESCE(SUM(cost_usd), 0.0)
		FROM domain_cost_history
		WHERE api_key_id = ? AND timestamp >= datetime('now', 'start of month')
	`, apiKeyID).Scan(&costMonth)
	summary.TotalCostMonth = costMonth

	// Active limit & period calculation
	switch summary.ResetInterval {
	case "weekly":
		summary.ActiveLimitUSD = summary.WeeklyLimitUSD
		var costWeek float64
		_ = s.db.QueryRow(`
			SELECT COALESCE(SUM(cost_usd), 0.0)
			FROM domain_cost_history
			WHERE api_key_id = ? AND timestamp >= datetime('now', '-7 days')
		`, apiKeyID).Scan(&costWeek)
		summary.TotalCostPeriod = costWeek
	case "monthly":
		summary.ActiveLimitUSD = summary.MonthlyLimitUSD
		summary.TotalCostPeriod = summary.TotalCostMonth
	default: // daily
		summary.ActiveLimitUSD = summary.DailyLimitUSD
		summary.TotalCostPeriod = summary.TotalCostToday
	}

	// Budget check
	if summary.ActiveLimitUSD > 0 {
		summary.BudgetCheck.PercentUsed = (summary.TotalCostPeriod / summary.ActiveLimitUSD) * 100
		summary.BudgetCheck.RemainingUSD = summary.ActiveLimitUSD - summary.TotalCostPeriod
		if summary.BudgetCheck.RemainingUSD < 0 {
			summary.BudgetCheck.RemainingUSD = 0
		}
		if summary.TotalCostPeriod >= summary.ActiveLimitUSD {
			summary.BudgetCheck.Exceeded = true
			summary.BudgetCheck.Allowed = false
		}
		if summary.TotalCostPeriod >= (summary.ActiveLimitUSD * summary.WarningThreshold) {
			summary.BudgetCheck.Warning = true
		}
	} else {
		summary.BudgetCheck.Allowed = true
		summary.BudgetCheck.RemainingUSD = 999999.0
	}

	return summary, nil
}

// GetAllBudgetSummaries returns summaries for all configured domain budgets.
func (s *DBStore) GetAllBudgetSummaries() (map[string]BudgetSummary, error) {
	res := make(map[string]BudgetSummary)
	if s == nil || s.db == nil {
		return res, nil
	}
	ensureBudgetInit(s.db)

	s.mu.Lock()
	rows, err := s.db.Query(`SELECT api_key_id FROM domain_budgets`)
	s.mu.Unlock()
	if err != nil {
		return res, err
	}
	defer rows.Close()

	var keys []string
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err == nil && k != "" {
			keys = append(keys, k)
		}
	}

	for _, k := range keys {
		sum, err := s.GetCostSummary(k)
		if err == nil {
			res[k] = sum
		}
	}
	return res, nil
}
