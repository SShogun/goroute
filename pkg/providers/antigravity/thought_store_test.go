package antigravity

import (
	"testing"
)

func TestThoughtStore(t *testing.T) {
	callID := "toolu_test_12345"
	sig := "sig_abc_xyz_987"

	StoreThoughtSignature(callID, sig)

	got := GetThoughtSignature(callID)
	if got != sig {
		t.Fatalf("expected signature %s, got %s", sig, got)
	}

	// Non-existent key
	if empty := GetThoughtSignature("toolu_non_existent"); empty != "" {
		t.Fatalf("expected empty string for non-existent key, got %s", empty)
	}
}
