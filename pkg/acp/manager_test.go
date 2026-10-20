package acp

import (
	"context"
	"testing"
	"time"
)

func TestAcpLifecycle(t *testing.T) {
	mgr := NewManager()

	// Spawn cat process as mock agent
	session, err := mgr.Spawn("mock-cat", "cat", nil, nil)
	if err != nil {
		t.Fatalf("failed to spawn mock session: %v", err)
	}

	if !session.Alive {
		t.Fatalf("expected session to be alive")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	resp, err := session.SendPrompt(ctx, "echo-ping")
	if err != nil {
		t.Fatalf("send prompt failed: %v", err)
	}
	if len(resp) == 0 {
		t.Logf("cat output collected: %q", resp)
	}

	sessions := mgr.ListSessions()
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(sessions))
	}

	if err := mgr.KillSession(session.ID); err != nil {
		t.Fatalf("kill session failed: %v", err)
	}

	if len(mgr.ListSessions()) != 0 {
		t.Fatalf("expected 0 sessions after kill")
	}
}
