# GoRoute: Ultra-Efficient LLM Gateway & Multi-Provider Router

- **Date**: 2026-10-17
- **Author**: Antigravity & zius
- **Status**: Approved Design Spec

## 1. Executive Summary

`goroute` is a high-performance, ultra-resource-efficient AI gateway written in Go. It directly solves the excessive resource usage of heavy Node/Electron gateways (which consume 500MB–1.5GB of RAM and significant idle CPU) while providing robust multi-agent routing capabilities:
1. **Interactive OAuth Logins & Auto-Refresh** (e.g. Google Antigravity CLI with multiple accounts).
2. **Multi-Account Rotation** for the same provider (round-robin, least-loaded).
3. **Automatic Fallback Cascades** (combos) when rate limits (429) or upstream errors (5xx) occur.
4. **Quota and Rate Limit Tracking** (RPM, TPM, cooldowns).
5. **Universal OpenAI-Compatible API** (`/v1/chat/completions`, `/v1/models`) with full SSE streaming.

Target footprint: **< 15MB RAM idle**, **0% idle CPU**, single static binary.

---

## 2. Architecture & Modules

```
goroute/
├── cmd/
│   └── goroute/              # CLI entry point (Cobra: serve, login, status, config)
├── pkg/
│   ├── config/               # YAML configuration loader & validation
│   ├── store/                # Encrypted SQLite storage for OAuth tokens & metadata
│   ├── proxy/                # HTTP server (Chi), OpenAI spec handler, SSE streaming
│   ├── providers/            # Provider adapter implementations
│   │   ├── provider.go       # Core Provider interface
│   │   ├── openai/           # OpenAI-compatible adapter (TokenRouter, ApinexBond, etc.)
│   │   └── antigravity/      # Antigravity OAuth PKCE, token refresh, project discovery
│   ├── router/               # Model routing, combo cascades, account balancer
│   └── quota/                # Sliding window RPM/TPM tracking & cooldowns
├── web/                      # Embedded single-file dashboard (Preact / Vanilla HTML)
├── docs/                     # Documentation & guides
├── go.mod
└── README.md
```

### 2.1 Core Components

1. **Proxy Engine (`pkg/proxy`)**:
   - Built on Go's `net/http` and `chi`.
   - Streaming SSE support with `http.Flusher`.
   - Translates incoming requests into upstream provider payloads and normalizes streaming tokens.

2. **Provider Abstraction (`pkg/providers`)**:
   ```go
   type Provider interface {
       Name() string
       ID() string
       ChatCompletion(ctx context.Context, req *ChatRequest) (*ChatResponseStream, error)
       Models(ctx context.Context) ([]ModelInfo, error)
       IsHealthy() bool
   }
   ```
   - **Antigravity Provider**: Implements local loopback listener for Google OAuth PKCE, obtains refresh tokens, queries Cloud Code Assist endpoints, auto-refreshes tokens before expiration.
   - **OpenAI Compatible Provider**: Handles direct base URL + API key routing.

3. **Routing & Combos (`pkg/router`)**:
   - Combos group multiple provider accounts into a virtual model (e.g., `cascade-sonnet` routes to Antigravity account 1 -> Antigravity account 2 -> TokenRouter).
   - On HTTP 429 / 5xx, immediately retries against the next available provider in the combo without dropping the client connection.

4. **Quota & Rate Limiting (`pkg/quota`)**:
   - In-memory sliding window counters for RPM and TPM.
   - Temporary cooldowns for accounts that report rate limits.

5. **Token Security & Storage (`pkg/store`)**:
   - Stores OAuth refresh tokens encrypted with AES-256-GCM.
   - Key derived from machine ID or user-specified master key in `~/.goroute/config.yaml`.

6. **Embedded UI (`web/`)**:
   - Embedded into binary using Go `embed.FS`.
   - Zero background connections when dashboard is closed. Only polls when actively viewed in a browser.

---

## 3. Configuration Format (`~/.goroute/config.yaml`)

```yaml
server:
  port: 20128
  host: "127.0.0.1"

providers:
  - id: "antigravity-pool"
    type: "antigravity"
    accounts:
      - id: "acc-1"
        email: "account1@gmail.com"
      - id: "acc-2"
        email: "account2@gmail.com"

  - id: "tokenrouter-main"
    type: "openai"
    base_url: "https://api.tokenrouter.io/v1"
    api_key: "${TOKENROUTER_API_KEY}"

  - id: "apinexbond-main"
    type: "openai"
    base_url: "https://api.apinexbond.com/v1"
    api_key: "${APINEXBOND_API_KEY}"

combos:
  - name: "claude-code"
    strategy: "failover" # failover | round_robin
    targets:
      - provider: "antigravity-pool"
        model: "claude-3-7-sonnet"
      - provider: "tokenrouter-main"
        model: "claude-3-7-sonnet"
```

---

## 4. CLI Experience

- `goroute serve`: Starts the gateway daemon.
- `goroute auth login antigravity`: Launches browser PKCE flow to link a new Google Antigravity account.
- `goroute auth list`: Displays linked accounts and token statuses.
- `goroute status`: Displays active routes, quotas, and memory consumption.

---

## 5. Verification & Testing Plan

1. **Unit Tests**:
   - SSE streaming parser and forwarder.
   - Token refresh loop and expiry handler.
   - Failover combo cascade upon simulated 429/500 errors.
2. **Integration Tests**:
   - OpenAI compatible client (curl / python openai sdk) hitting `localhost:20128/v1/chat/completions`.
   - Antigravity OAuth authentication round-trip.
3. **Resource Benchmark**:
   - Memory profile under 100 concurrent requests (must remain < 25MB RAM).
