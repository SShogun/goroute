package guardrails

import (
	"testing"
)

func TestRedactCredentials(t *testing.T) {
	input := "My OpenAI key is sk-proj-1234567890abcdefghijklmn and anthropic is sk-ant-api03-abcdefghijklmnopqrstuvwxyz12345"
	res := RedactCredentials(input)

	if !res.Modified {
		t.Errorf("expected modified=true")
	}
	if res.CleanedText == input {
		t.Errorf("expected cleaned text to differ from input")
	}
	if res.Detections["openai_proj"] != 1 {
		t.Errorf("expected 1 openai_proj detection, got %d", res.Detections["openai_proj"])
	}
	if res.Detections["anthropic"] != 1 {
		t.Errorf("expected 1 anthropic detection, got %d", res.Detections["anthropic"])
	}
}

func TestRedactPII(t *testing.T) {
	input := "Contact developer at noelyt101@gmail.com or 415-555-2671"
	res := RedactPII(input)

	if !res.Modified {
		t.Errorf("expected modified=true")
	}
	if res.Detections["email"] != 1 {
		t.Errorf("expected 1 email detection, got %d", res.Detections["email"])
	}
	expected := "Contact developer at [EMAIL_REDACTED] or [PHONE_REDACTED]"
	if res.CleanedText != expected {
		t.Errorf("expected %q, got %q", expected, res.CleanedText)
	}
}
