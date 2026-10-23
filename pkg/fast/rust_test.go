package fast

import (
	"testing"
)

func TestRustBridge(t *testing.T) {
	avail := IsAvailable()
	t.Logf("Rust Available: %v", avail)
	t.Logf("Rust Version: %s", Version())

	if !avail {
		t.Skip("Rust library not found, skipping FFI test")
	}

	testPrompt := "Hello there, could you please provide a detailed explanation of Go routines? Thank you so much!"
	compressed, ok := Compress(testPrompt, 2)
	if !ok {
		t.Fatalf("Rust Compress failed")
	}
	t.Logf("Original:   %s", testPrompt)
	t.Logf("Compressed: %s", compressed)

	tokens, tokOk := CountTokens(testPrompt)
	if !tokOk || tokens <= 0 {
		t.Fatalf("Rust CountTokens failed or returned %d", tokens)
	}
	t.Logf("Estimated tokens: %d", tokens)
}
