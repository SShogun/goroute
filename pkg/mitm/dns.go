package mitm

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// AgentBridgeTargets maps tools to their default API endpoints that can be intercepted via /etc/hosts or DNS.
var AgentBridgeTargets = map[string][]string{
	"antigravity": {
		"daily-cloudcode-pa.googleapis.com",
		"cloudcode-pa.googleapis.com",
		"daily-cloudcode-pa.sandbox.googleapis.com",
		"autopush-cloudcode-pa.sandbox.googleapis.com",
	},
	"copilot": {
		"api.githubcopilot.com",
		"copilot-proxy.githubusercontent.com",
	},
	"claude-code": {
		"api.anthropic.com",
	},
	"cursor": {
		"api2.cursor.sh",
	},
	"codex": {
		"api.openai.com",
	},
}

const (
	HostsHeader = "# GoRoute AgentBridge DNS Interception Begin"
	HostsFooter = "# GoRoute AgentBridge DNS Interception End"
)

// GenerateHostsEntries generates hosts file content mapping targets to 127.0.0.1.
func GenerateHostsEntries(targets []string) string {
	var sb strings.Builder
	sb.WriteString(HostsHeader + "\n")
	for _, target := range targets {
		sb.WriteString(fmt.Sprintf("127.0.0.1 %s\n", target))
	}
	sb.WriteString(HostsFooter + "\n")
	return sb.String()
}

// UpdateHostsContent adds or updates GoRoute entries in a raw hosts file content.
func UpdateHostsContent(existingContent string, targets []string) string {
	lines := strings.Split(existingContent, "\n")
	var result []string
	inSection := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == HostsHeader {
			inSection = true
			continue
		}
		if trimmed == HostsFooter {
			inSection = false
			continue
		}
		if !inSection {
			result = append(result, line)
		}
	}

	cleaned := strings.TrimRight(strings.Join(result, "\n"), "\n")
	if len(targets) == 0 {
		return cleaned + "\n"
	}

	entrySection := GenerateHostsEntries(targets)
	if cleaned == "" {
		return entrySection
	}
	return cleaned + "\n\n" + entrySection
}

// ReadHosts reads the system hosts file.
func ReadHosts(path string) (string, error) {
	if path == "" {
		path = "/etc/hosts"
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// ParseHostsLines returns all hosts currently managed by GoRoute in the given hosts file.
func ParseManagedHosts(content string) []string {
	scanner := bufio.NewScanner(strings.NewReader(content))
	var managed []string
	inSection := false

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == HostsHeader {
			inSection = true
			continue
		}
		if line == HostsFooter {
			inSection = false
			continue
		}
		if inSection && strings.HasPrefix(line, "127.0.0.1 ") {
			host := strings.TrimSpace(strings.TrimPrefix(line, "127.0.0.1 "))
			if host != "" {
				managed = append(managed, host)
			}
		}
	}
	return managed
}
