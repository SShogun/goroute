package storage

import (
	"os"
	"testing"
)

func TestResponsesContinuation(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "goroute-cont-test-*.db")
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

	respID := "resp_123456"
	apiKeyID := "key_abcdef"
	input := []map[string]string{
		{"role": "user", "content": "Hello world"},
	}
	output := []map[string]string{
		{"role": "assistant", "content": "Hi there!"},
	}

	err = store.SaveContinuationState(respID, apiKeyID, "call_999", "gpt-4o", "openai", input, output)
	if err != nil {
		t.Fatalf("SaveContinuationState failed: %v", err)
	}

	state, err := store.GetContinuationState(respID, apiKeyID)
	if err != nil {
		t.Fatalf("GetContinuationState failed: %v", err)
	}

	if state.ResponseID != respID {
		t.Errorf("expected %s, got %s", respID, state.ResponseID)
	}
	if state.Model != "gpt-4o" {
		t.Errorf("expected gpt-4o, got %s", state.Model)
	}
}
