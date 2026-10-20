package webcookie

import (
	"strings"
	"testing"
)

func TestParseJsonCookiesToHeader(t *testing.T) {
	jsonInput := `[
		{"name": "sessionKey", "value": "sk-ant-sid01-xxx", "domain": ".claude.ai"},
		{"name": "cf_clearance", "value": "clear123", "path": "/"}
	]`

	header, err := ParseJsonCookiesToHeader(jsonInput)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := "sessionKey=sk-ant-sid01-xxx; cf_clearance=clear123"
	if header != expected {
		t.Errorf("expected %q, got %q", expected, header)
	}

	rawString := "Cookie: sessionKey=sk-ant-sid01-xxx; __Secure-next-auth.session-token=tok123"
	cleaned, err := ParseJsonCookiesToHeader(rawString)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(cleaned, "sessionKey=") {
		t.Errorf("expected stripped cookie header, got %q", cleaned)
	}

	sk := ExtractNamedCookie(cleaned, "sessionKey")
	if sk != "sk-ant-sid01-xxx" {
		t.Errorf("expected extracted sessionKey, got %q", sk)
	}
}
