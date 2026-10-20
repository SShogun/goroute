package cursor

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ziuus/goroute/pkg/providers"
)

type Account struct {
	ID           string    `json:"id"`
	Pool         string    `json:"pool,omitempty"`
	Email        string    `json:"email,omitempty"`
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	WorkOSID     string    `json:"workos_id,omitempty"`
	ExpiresAt    time.Time `json:"expires_at,omitempty"`
	mu           sync.RWMutex
}

func (a *Account) GetValidAccessToken(ctx context.Context, client *http.Client) (string, error) {
	a.mu.RLock()
	if a.AccessToken != "" && (a.ExpiresAt.IsZero() || time.Now().Before(a.ExpiresAt.Add(-2*time.Minute))) {
		token := a.AccessToken
		a.mu.RUnlock()
		return token, nil
	}
	a.mu.RUnlock()

	if a.RefreshToken == "" {
		a.mu.RLock()
		defer a.mu.RUnlock()
		return a.AccessToken, nil
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	// Double check
	if a.AccessToken != "" && !a.ExpiresAt.IsZero() && time.Now().Before(a.ExpiresAt.Add(-2*time.Minute)) {
		return a.AccessToken, nil
	}

	// Exchange refresh token at https://api2.cursor.sh/auth/exchange_user_api_key
	req, err := http.NewRequestWithContext(ctx, "POST", "https://api2.cursor.sh/auth/exchange_user_api_key", bytes.NewReader([]byte("{}")))
	if err != nil {
		return "", fmt.Errorf("create cursor refresh request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+a.RefreshToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("cursor refresh request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("cursor token refresh rejected (%d): %s", resp.StatusCode, string(body))
	}

	var data struct {
		AccessToken  string `json:"accessToken"`
		RefreshToken string `json:"refreshToken"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return "", fmt.Errorf("decode cursor refresh response: %w", err)
	}

	if data.AccessToken == "" {
		return "", fmt.Errorf("cursor refresh response missing access token")
	}

	a.AccessToken = data.AccessToken
	if data.RefreshToken != "" {
		a.RefreshToken = data.RefreshToken
	}
	a.ExpiresAt = time.Now().Add(1 * time.Hour)
	return a.AccessToken, nil
}

type Provider struct {
	id         string
	accounts   []*Account
	accIndex   int
	mu         sync.Mutex
	httpClient *http.Client
}

func NewProvider(id string, accounts []*Account) *Provider {
	return &Provider{
		id:       id,
		accounts: accounts,
		httpClient: &http.Client{
			Transport: &http.Transport{
				ResponseHeaderTimeout: 300 * time.Second,
				IdleConnTimeout:       120 * time.Second,
			},
			Timeout: 0,
		},
	}
}

func (p *Provider) ID() string {
	return p.id
}

func (p *Provider) Type() string {
	return "cursor"
}

func (p *Provider) IsHealthy() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.accounts) > 0
}

func (p *Provider) Models(ctx context.Context) ([]providers.ModelInfo, error) {
	return []providers.ModelInfo{
		{ID: "auto", Object: "model", OwnedBy: "cursor"},
		{ID: "composer-2", Object: "model", OwnedBy: "cursor"},
		{ID: "claude-4.6-sonnet-medium", Object: "model", OwnedBy: "cursor"},
		{ID: "claude-4.6-opus-high-thinking-fast", Object: "model", OwnedBy: "cursor"},
		{ID: "gpt-5.4-low-fast", Object: "model", OwnedBy: "cursor"},
		{ID: "gpt-5.3-codex", Object: "model", OwnedBy: "cursor"},
		{ID: "gemini-3.1-pro", Object: "model", OwnedBy: "cursor"},
		{ID: "grok-4.6-medium", Object: "model", OwnedBy: "cursor"},
	}, nil
}

func (p *Provider) Embeddings(ctx context.Context, req *providers.EmbeddingRequest) (*providers.EmbeddingResponse, error) {
	return nil, fmt.Errorf("embeddings not supported on cursor provider")
}

func (p *Provider) ChatCompletion(ctx context.Context, req *providers.ChatRequest) (io.ReadCloser, bool, int, error) {
	return p.Execute(ctx, req)
}

func (p *Provider) TestConnection(ctx context.Context) error {
	valid, msg, _, err := p.TestAccount(ctx, "")
	if !valid || err != nil {
		if err != nil {
			return err
		}
		return fmt.Errorf("%s", msg)
	}
	return nil
}

func (p *Provider) getNextAccount() (*Account, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if len(p.accounts) == 0 {
		return nil, fmt.Errorf("no accounts configured for cursor provider %s", p.id)
	}

	acc := p.accounts[p.accIndex]
	p.accIndex = (p.accIndex + 1) % len(p.accounts)
	return acc, nil
}

func (p *Provider) Execute(ctx context.Context, req *providers.ChatRequest) (io.ReadCloser, bool, int, error) {
	acc, err := p.getNextAccount()
	if err != nil {
		return nil, false, http.StatusServiceUnavailable, err
	}

	token, err := acc.GetValidAccessToken(ctx, p.httpClient)
	if err != nil {
		return nil, false, http.StatusUnauthorized, fmt.Errorf("cursor auth error: %w", err)
	}

	payload, err := json.Marshal(req)
	if err != nil {
		return nil, false, http.StatusBadRequest, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", "https://api2.cursor.sh/v1/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return nil, false, http.StatusInternalServerError, err
	}

	httpReq.Header.Set("Authorization", "Bearer "+token)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("User-Agent", "Cursor/0.45.0")
	httpReq.Header.Set("connect-protocol-version", "1")
	httpReq.Header.Set("connect-accept-encoding", "gzip")

	resp, err := p.httpClient.Do(httpReq)
	if err != nil {
		return nil, false, http.StatusBadGateway, err
	}

	isStream := strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream") || req.Stream
	return resp.Body, isStream, resp.StatusCode, nil
}

// TestAccount checks connection against Cursor API
func (p *Provider) TestAccount(ctx context.Context, accID string) (bool, string, int64, error) {
	start := time.Now()
	var targetAcc *Account
	p.mu.Lock()
	for _, a := range p.accounts {
		if a.ID == accID || accID == "" {
			targetAcc = a
			break
		}
	}
	p.mu.Unlock()

	if targetAcc == nil {
		return false, "Account not found", 0, fmt.Errorf("account not found")
	}

	token, err := targetAcc.GetValidAccessToken(ctx, p.httpClient)
	if err != nil {
		return false, fmt.Sprintf("Authentication failed: %v", err), time.Since(start).Milliseconds(), nil
	}

	req, err := http.NewRequestWithContext(ctx, "POST", "https://api2.cursor.sh/aiserver.v1.DashboardService/GetCurrentPeriodUsage", bytes.NewReader([]byte("{}")))
	if err != nil {
		return false, err.Error(), time.Since(start).Milliseconds(), err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "goroute-cursor-quota")

	resp, err := p.httpClient.Do(req)
	latency := time.Since(start).Milliseconds()
	if err != nil {
		return false, err.Error(), latency, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		return true, "Connection verified successfully", latency, nil
	}

	body, _ := io.ReadAll(resp.Body)
	return false, fmt.Sprintf("Cursor API error (%d): %s", resp.StatusCode, string(body)), latency, nil
}

// AutoImportCursor detects and imports Cursor credentials from ~/.config/cursor/auth.json
func AutoImportCursor() (*Account, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}

	authPath := filepath.Join(home, ".config", "cursor", "auth.json")
	if fi, err := os.Stat(authPath); err == nil && !fi.IsDir() {
		data, err := os.ReadFile(authPath)
		if err == nil {
			var parsed struct {
				AccessToken  string `json:"accessToken"`
				RefreshToken string `json:"refreshToken"`
			}
			if err := json.Unmarshal(data, &parsed); err == nil && (parsed.AccessToken != "" || parsed.RefreshToken != "") {
				email := "cursor-user"
				// Try decode JWT email / sub
				if parsed.AccessToken != "" {
					parts := strings.Split(parsed.AccessToken, ".")
					if len(parts) == 3 {
						if seg, err := base64.RawURLEncoding.DecodeString(parts[1]); err == nil {
							var claims struct {
								Email string `json:"email"`
								Sub   string `json:"sub"`
							}
							if err := json.Unmarshal(seg, &claims); err == nil {
								if claims.Email != "" {
									email = claims.Email
								} else if claims.Sub != "" {
									email = claims.Sub
								}
							}
						}
					}
				}

				return &Account{
					ID:           "cursor-" + email,
					Email:        email,
					AccessToken:  parsed.AccessToken,
					RefreshToken: parsed.RefreshToken,
					ExpiresAt:    time.Now().Add(24 * time.Hour),
				}, nil
			}
		}
	}

	return nil, fmt.Errorf("no Cursor credentials found in ~/.config/cursor/auth.json")
}
