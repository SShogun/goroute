package pool

import (
	"testing"
	"time"
)

func TestAccountPoolFairShareBalancing(t *testing.T) {
	mgr := NewPoolManager()
	p := mgr.GetOrCreatePool("kiro", "balanced")

	accounts := []string{"acc-alice", "acc-bob", "acc-charlie"}
	for _, a := range accounts {
		p.RegisterAccount(a, 1.0)
	}

	// 1. Initial selection should pick any
	first := p.SelectBestAccount(accounts)
	if first == "" {
		t.Fatalf("expected valid account selection")
	}

	// 2. Simulate Alice consuming 5,000 tokens
	p.RecordUsage("acc-alice", 5000)

	// 3. Next selection must NOT pick Alice; it must pick Bob or Charlie (0 tokens)
	second := p.SelectBestAccount(accounts)
	if second == "acc-alice" {
		t.Fatalf("expected fair share to avoid heavy user acc-alice, got %s", second)
	}

	// 4. Simulate Bob consuming 3,000 tokens
	p.RecordUsage("acc-bob", 3000)

	// 5. Next selection must pick Charlie (0 tokens)
	third := p.SelectBestAccount(accounts)
	if third != "acc-charlie" {
		t.Fatalf("expected acc-charlie (lowest usage), got %s", third)
	}

	// 6. Test cooldown
	p.SetCooldown("acc-charlie", 5*time.Minute)
	afterCooldown := p.SelectBestAccount(accounts)
	if afterCooldown == "acc-charlie" {
		t.Fatalf("expected cooling down account not to be selected")
	}
}
