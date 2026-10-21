package storage

import (
	"os"
	"testing"
)

func TestDomainBudgets(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "goroute-budget-test-*.db")
	if err != nil {
		t.Fatal(err)
	}
	tmpPath := tmpFile.Name()
	tmpFile.Close()
	defer os.Remove(tmpPath)

	store, err := InitDB(tmpPath)
	if err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	defer store.Close()

	keyID := "test-key-1"
	b := DomainBudget{
		APIKeyID:         keyID,
		DailyLimitUSD:    5.0,
		WeeklyLimitUSD:   25.0,
		MonthlyLimitUSD:  100.0,
		WarningThreshold: 0.8,
		ResetInterval:    "daily",
	}

	if err := store.SetBudget(b); err != nil {
		t.Fatalf("SetBudget failed: %v", err)
	}

	// Record some costs
	if err := store.RecordDomainCost(keyID, 2.50, "gpt-4o", "openai"); err != nil {
		t.Fatalf("RecordDomainCost failed: %v", err)
	}

	summary, err := store.GetCostSummary(keyID)
	if err != nil {
		t.Fatalf("GetCostSummary failed: %v", err)
	}

	if summary.TotalCostToday != 2.50 {
		t.Errorf("expected 2.50 cost today, got %f", summary.TotalCostToday)
	}
	if summary.BudgetCheck.Exceeded {
		t.Errorf("expected budget not exceeded")
	}
	if summary.BudgetCheck.PercentUsed != 50.0 {
		t.Errorf("expected 50 percent used, got %f", summary.BudgetCheck.PercentUsed)
	}

	// Add more to exceed
	_ = store.RecordDomainCost(keyID, 3.0, "gpt-4o", "openai")
	summary2, _ := store.GetCostSummary(keyID)
	if !summary2.BudgetCheck.Exceeded {
		t.Errorf("expected budget exceeded now")
	}
	if summary2.BudgetCheck.Allowed {
		t.Errorf("expected budget disallowed when exceeded")
	}
}
