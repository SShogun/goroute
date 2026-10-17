# ⚡ GoRoute

> **Ultra-Lightweight, Production-Grade LLM Gateway & Multi-Provider Router**  
> Written in Go. Consumes **< 15MB RAM** (vs 600MB–1.5GB in OmniRoute) with 0% idle CPU.

---

## Why GoRoute?

Gateways like OmniRoute provide great features (OAuth flows, multi-account rotation, fallbacks), but run on heavy Electron/Node.js stacks with constant background websocket polling, in-memory vector/FTS indexing, and heavy compression pipelines.

**GoRoute** extracts the features you actually need into a **single statically linked Go binary**:

- 🚀 **Ultra-Low Resource**: 2.9 MB heap under load; < 15MB RSS.
- 🔑 **Native OAuth PKCE Support**: Login with Google Antigravity directly via `goroute auth login antigravity`. Handles token refresh & project discovery automatically.
- 🔄 **Multi-Account Rotation**: Connect multiple accounts for the same provider; GoRoute rotates requests across them.
- 🔀 **Smart Combos & Failover**: Automatic cascading retry on HTTP 429 (rate-limit) or 5xx upstream errors without failing the client request.
- 📊 **Sliding-Window Quota & Cooldowns**: Automatically pauses accounts that hit limits.
- 🔌 **Universal OpenAI Endpoint**: Drop-in compatible with Claude Code, Cursor, Cline, OpenCode, Codex, and standard OpenAI SDKs (`http://localhost:20128/v1`).
- 🖥️ **Embedded Zero-Overhead Dashboard**: Modern status UI served from binary with zero background sockets.

---

## Benchmark Comparison

| Metric | OmniRoute (Node/Electron) | GoRoute (Go) | Improvement |
| :--- | :--- | :--- | :--- |
| **Idle Memory (RSS)** | ~500 MB – 1.5 GB | **< 15 MB** | **~98% reduction** |
| **Heap Under Load (500 reqs)** | > 200 MB | **2.93 MB** | **> 98% reduction** |
| **Idle CPU** | 1% – 5% continuous | **0.0%** | **Instant sleeping** |
| **Binary Size** | ~180 MB | **14 MB** | **Single binary** |
| **Startup Time** | ~3,500 ms | **< 20 ms** | **175x faster** |

---

## Installation

### 1. Direct Go Install
```bash
go install github.com/ziuus/goroute/cmd/goroute@latest
```

### 2. From Source
```bash
git clone https://github.com/ziuus/goroute.git
cd goroute
go build -o goroute ./cmd/goroute
sudo mv goroute /usr/local/bin/
```

### 3. Docker
```bash
docker run -d -p 20128:20128 -v ~/.goroute:/root/.goroute goroute:latest
```

---

## Quickstart

### 1. Login with Google Antigravity (OAuth PKCE)
```bash
goroute auth login antigravity
```
*Opens your browser to sign in. Automatically retrieves refresh tokens, fetches your email, and discovers your Cloud Code project ID.*

### 2. Start the Gateway
```bash
goroute serve
```
Output:
```
⚡ GoRoute starting on http://127.0.0.1:20128
   Universal OpenAI endpoint: http://127.0.0.1:20128/v1
   Dashboard available at:    http://127.0.0.1:20128/
   Memory footprint:          < 15 MB RAM
```

### 3. Connect Your Tools
Point your IDE or CLI to:
- **Base URL**: `http://127.0.0.1:20128/v1`
- **API Key**: `any-key`

---

## Configuration (`~/.goroute/config.yaml`)

```yaml
server:
  port: 20128
  host: "127.0.0.1"

providers:
  # Antigravity OAuth pool (accounts populated via 'goroute auth login antigravity')
  - id: "antigravity"
    type: "antigravity"
    accounts:
      - id: "acc-1"
        email: "user1@gmail.com"
      - id: "acc-2"
        email: "user2@gmail.com"

  # Standard OpenAI-compatible endpoints (TokenRouter, ApinexBond, DeepSeek, etc.)
  - id: "tokenrouter"
    type: "openai"
    base_url: "https://api.tokenrouter.io/v1"
    api_key: "${TOKENROUTER_API_KEY}"

combos:
  # Automatic failover cascade: tries Antigravity acc-1 -> acc-2 -> TokenRouter
  - name: "claude-cascade"
    strategy: "failover"
    targets:
      - provider_id: "antigravity"
        model: "claude-3-7-sonnet"
      - provider_id: "tokenrouter"
        model: "claude-3-7-sonnet"
```

---

## CLI Reference

- `goroute serve`: Starts the proxy server.
- `goroute auth login <provider>`: Authenticates OAuth provider (PKCE browser flow).
- `goroute auth list`: Lists all configured providers and accounts.
- `goroute status`: Displays live gateway status and available models.

---

## License

MIT License. Contributions welcome!
