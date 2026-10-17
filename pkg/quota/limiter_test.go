package quota

import (
	"testing"
	"time"
)

func TestLimiterCooldown(t *testing.T) {
	limiter := NewLimiter()

	account := "acc-1"
	if !limiter.Allow(account) {
		t.Fatalf("expected account to be allowed initially")
	}

	// Trigger 429 cooldown
	limiter.Record429(account, 100*time.Millisecond)

	if limiter.Allow(account) {
		t.Errorf("expected account to be blocked during cooldown")
	}

	time.Sleep(120 * time.Millisecond)

	if !limiter.Allow(account) {
		t.Errorf("expected account to be allowed after cooldown expires")
	}
}

func TestLimiterRPM(t *testing.T) {
	limiter := NewLimiter()
	account := "acc-rpm"
	limiter.SetRPM(account, 3)

	if !limiter.Allow(account) {
		t.Errorf("call 1 should be allowed")
	}
	if !limiter.Allow(account) {
		t.Errorf("call 2 should be allowed")
	}
	if !limiter.Allow(account) {
		t.Errorf("call 3 should be allowed")
	}
	if limiter.Allow(account) {
		t.Errorf("call 4 should exceed RPM limit")
	}
}
