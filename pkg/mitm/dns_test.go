package mitm

import (
	"strings"
	"testing"
)

func TestUpdateHostsContent(t *testing.T) {
	initial := `127.0.0.1 localhost
::1 localhost ip6-localhost ip6-loopback
`
	targets := []string{"daily-cloudcode-pa.googleapis.com", "api.githubcopilot.com"}
	updated := UpdateHostsContent(initial, targets)

	if !strings.Contains(updated, HostsHeader) {
		t.Errorf("expected HostsHeader in updated content")
	}
	if !strings.Contains(updated, "127.0.0.1 daily-cloudcode-pa.googleapis.com") {
		t.Errorf("missing antigravity target in updated content")
	}

	managed := ParseManagedHosts(updated)
	if len(managed) != 2 {
		t.Fatalf("expected 2 managed hosts, got %d", len(managed))
	}
	if managed[0] != targets[0] || managed[1] != targets[1] {
		t.Errorf("managed hosts mismatch: %v", managed)
	}

	// Test removal
	cleaned := UpdateHostsContent(updated, nil)
	if strings.Contains(cleaned, HostsHeader) {
		t.Errorf("expected header removed when targets nil")
	}
	if len(ParseManagedHosts(cleaned)) != 0 {
		t.Errorf("expected 0 managed hosts after clearing")
	}
}
