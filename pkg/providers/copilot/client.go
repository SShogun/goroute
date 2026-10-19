package copilot

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ziuus/goroute/pkg/providers"
)

const (
	CopilotInternalTokenURL = "https://api.github.com/copilot_internal/v2/token"
	CopilotDefaultBaseURL   = "https://api.githubcopilot.com"
	CopilotClientID         = "Iv1.b507a08c87ecfe98"
	CopilotUserAgent        = "GithubCopilot/1.155.0"
	CopilotChatUserAgent    = "GitHubCopilotChat/0.17.0"
)

type Account struct {
	ID           string
	GitHubToken  string
	CopilotToken string
	ExpiresAt    time.Time
	User         string
	mu           sync.RWMutex
}

func (a *Account) GetValidCopilotToken(ctx context.Context, client *http.Client) (string, error) {
	a.mu.RLock()
	if a.CopilotToken != "" && time.Now().Before(a.ExpiresAt.Add(-2*time.Minute)) {
		token := a.CopilotToken
		a.mu.RUnlock()
		return token, nil
	}
	a.mu.RUnlock()

	a.mu.Lock()
	defer a.mu.Unlock()

	if a.CopilotToken != "" && time.Now().Before(a.ExpiresAt.Add(-2*time.Minute)) {
		return a.CopilotToken, nil
	}

	req, err := http.NewRequestWithContext(ctx, "GET", CopilotInternalTokenURL, nil)
	if err != nil {
		return "", fmt.Errorf("create copilot token request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+a.GitHubToken)
	req.Header.Set("User-Agent", CopilotUserAgent)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-GitHub-Api-Version", "2024-04-01")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("exchange copilot token: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("copilot token exchange failed (%d): %s", resp.StatusCode, string(body))
	}

	var res struct {
		Token     string `json:"token"`
		ExpiresAt int64  `json:"expires_at"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return "", fmt.Errorf("decode copilot token response: %w", err)
	}

	if res.Token == "" {
		return "", fmt.Errorf("empty token returned from copilot token exchange")
	}

	a.CopilotToken = res.Token
	if res.ExpiresAt > 0 {
		a.ExpiresAt = time.Unix(res.ExpiresAt, 0)
	} else {
		a.ExpiresAt = time.Now().Add(30 * time.Minute)
	}

	return a.CopilotToken, nil
}

type Provider struct {
	id       string
	accounts []*Account
	idx      uint64
	client   *http.Client
	mu       sync.RWMutex
}

func NewProvider(id string, accounts []*Account) *Provider {
	return &Provider{
		id:       id,
		accounts: accounts,
		client: &http.Client{
			Timeout: 120 * time.Second,
		},
	}
}

func (p *Provider) ID() string {
	return p.id
}

func (p *Provider) Type() string {
	return "copilot"
}

func (p *Provider) IsHealthy() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.accounts) > 0
}

func (p *Provider) Accounts() []*Account {
	p.mu.RLock()
	defer p.mu.RUnlock()
	res := make([]*Account, len(p.accounts))
	copy(res, p.accounts)
	return res
}

func (p *Provider) TestAccount(ctx context.Context, accountID string) error {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if len(p.accounts) == 0 {
		return fmt.Errorf("no accounts configured for copilot provider")
	}
	if accountID == "" {
		_, err := p.accounts[0].GetValidCopilotToken(ctx, p.client)
		return err
	}
	for _, a := range p.accounts {
		if a.ID == accountID {
			_, err := a.GetValidCopilotToken(ctx, p.client)
			return err
		}
	}
	return fmt.Errorf("account %q not found in copilot provider", accountID)
}

func (p *Provider) TestConnection(ctx context.Context) error {
	return p.TestAccount(ctx, "")
}

func (p *Provider) Models(ctx context.Context) ([]providers.ModelInfo, error) {
	return []providers.ModelInfo{
		{ID: "gpt-4o", Object: "model", OwnedBy: "copilot"},
		{ID: "claude-3.5-sonnet", Object: "model", OwnedBy: "copilot"},
		{ID: "o1-preview", Object: "model", OwnedBy: "copilot"},
		{ID: "o1-mini", Object: "model", OwnedBy: "copilot"},
	}, nil
}

func (p *Provider) Embeddings(ctx context.Context, req *providers.EmbeddingRequest) (*providers.EmbeddingResponse, error) {
	return nil, fmt.Errorf("embeddings not supported on copilot provider")
}

func (p *Provider) ChatCompletion(ctx context.Context, req *providers.ChatRequest) (io.ReadCloser, bool, int, error) {
	p.mu.RLock()
	accs := p.accounts
	p.mu.RUnlock()

	if len(accs) == 0 {
		return nil, false, http.StatusServiceUnavailable, fmt.Errorf("no copilot accounts configured")
	}

	acc := accs[0]
	copilotToken, err := acc.GetValidCopilotToken(ctx, p.client)
	if err != nil {
		return nil, false, http.StatusUnauthorized, fmt.Errorf("copilot token exchange error: %w", err)
	}

	payload, err := json.Marshal(req)
	if err != nil {
		return nil, false, http.StatusBadRequest, fmt.Errorf("marshal copilot request: %w", err)
	}

	url := CopilotDefaultBaseURL + "/chat/completions"
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(payload))
	if err != nil {
		return nil, false, http.StatusInternalServerError, err
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+copilotToken)
	httpReq.Header.Set("Editor-Version", "vscode/1.90.0")
	httpReq.Header.Set("Editor-Plugin-Version", "copilot-chat/0.17.0")
	httpReq.Header.Set("User-Agent", CopilotChatUserAgent)
	httpReq.Header.Set("Openai-Organization", "github-copilot")
	httpReq.Header.Set("Openai-Intent", "conversation-panel")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, false, http.StatusBadGateway, fmt.Errorf("copilot upstream request failed: %w", err)
	}

	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
		log.Printf("[copilot] Upstream returned HTTP %d", resp.StatusCode)
	}

	isStream := strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream")
	return resp.Body, isStream, resp.StatusCode, nil
}

// ReadCopilotCLIAuth parses ~/.config/github-copilot/hosts.json or hosts.bak
func ReadCopilotCLIAuth() (*Account, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}

	paths := []string{
		filepath.Join(home, ".config", "github-copilot", "hosts.json"),
		filepath.Join(home, ".config", "github-copilot", "hosts.bak"),
	}

	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}

		var parsed map[string]struct {
			OAuthToken string `json:"oauth_token"`
			User       string `json:"user"`
		}
		if err := json.Unmarshal(data, &parsed); err == nil {
			for host, hostData := range parsed {
				if strings.Contains(host, "github.com") && hostData.OAuthToken != "" {
					return &Account{
						ID:          "copilot-" + hostData.User,
						GitHubToken: hostData.OAuthToken,
						User:        hostData.User,
					}, nil
				}
			}
		}
	}

	return nil, fmt.Errorf("no GitHub Copilot oauth token found in ~/.config/github-copilot")
}
