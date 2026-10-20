package affinity

import (
	"testing"
	"time"

	"github.com/ziuus/goroute/pkg/providers"
)

func TestAffinityLifecycle(t *testing.T) {
	mgr := NewManager(10 * time.Minute)

	req := &providers.ChatRequest{
		Messages: []providers.ChatMessage{
			{Role: "user", Content: "Hello world from test session"},
		},
	}
	key := DeriveSessionKey(nil, req)
	if key == "" {
		t.Fatalf("expected non-empty derived session key")
	}

	// 1. Initially no affinity
	if acc := mgr.GetAffinity(key, "kiro"); acc != "" {
		t.Fatalf("expected empty initial affinity, got %s", acc)
	}

	// 2. Set affinity
	mgr.SetAffinity(key, "kiro", "acc-kiro-primary")
	if acc := mgr.GetAffinity(key, "kiro"); acc != "acc-kiro-primary" {
		t.Fatalf("expected pinned account acc-kiro-primary, got %s", acc)
	}

	// 3. Different provider should have separate key
	if acc := mgr.GetAffinity(key, "antigravity"); acc != "" {
		t.Fatalf("expected no affinity for antigravity, got %s", acc)
	}

	// 4. Invalidate affinity on 429
	mgr.InvalidateAffinity(key, "kiro")
	if acc := mgr.GetAffinity(key, "kiro"); acc != "" {
		t.Fatalf("expected invalidated affinity, got %s", acc)
	}
}
