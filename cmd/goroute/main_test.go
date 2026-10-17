package main

import (
	"bytes"
	"testing"
)

func TestRootCommand(t *testing.T) {
	cmd := NewRootCmd("0.1.0")
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"--version"})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	out := buf.String()
	if out == "" {
		t.Errorf("expected version output, got empty")
	}
}
