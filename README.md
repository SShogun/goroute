# ⚡ GoRoute

<div align="center">

**Ultra-Lightweight, Production-Grade LLM Gateway & Multi-Provider Router**  
*The high-performance, single-binary alternative to heavy Node/Electron proxies.*

[![Go Version](https://img.shields.io/badge/go-1.24%2B-blue.svg)](https://golang.org)
[![License: MIT](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)
[![Release](https://img.shields.io/github/v/release/ziuus/goroute)](https://github.com/ziuus/goroute/releases)
[![Memory Footprint](https://img.shields.io/badge/memory-%3C%2015MB%20RAM-success)](#benchmarks)

</div>

---

## ⚡ 60-Second Quickstart

### 1. Install

**Option A: 1-Line Installer (Linux & macOS)**
```bash
curl -fsSL https://raw.githubusercontent.com/ziuus/goroute/main/install.sh | bash
```

**Option B: npm / npx (Zero Go dependencies needed)**
```bash
npm install -g goroute
# OR run directly:
npx goroute setup all
```

**Option C: Go Toolchain**
```bash
go install github.com/ziuus/goroute/cmd/goroute@latest
```

---

### 2. Connect Your Providers

**If migrating from OmniRoute (1-second import):**
```bash
goroute import omniroute
```
*Instantly imports all your Antigravity Google accounts, custom provider keys, and combos into encrypted storage.*

**Or login fresh with Google Antigravity (OAuth PKCE):**
```bash
goroute auth login antigravity
```
*Opens your browser to sign in. Auto-refreshes tokens and auto-discovers your project ID.*

---

### 3. Connect All Your Coding Agents (1-Click)

```bash
goroute setup all
```
*Automatically configures **14 agents** (Claude Code, OpenAI Codex, OpenCode, Cursor, Aider, Cline, Roo Code, Continue, Goose, Crush, Qwen, Kilo, etc.) to point at GoRoute.*

---

### 4. Start the Gateway

```bash
# Run in background as a systemd service on Linux boot (uses ~5MB RAM):
goroute autostart enable

# Or run manually in terminal:
goroute serve
```

---

## 📊 Benchmarks vs OmniRoute

| Metric | OmniRoute (Node/Next.js) | GoRoute (Pure Go) | Delta |
| :--- | :--- | :--- | :--- |
| **Idle Memory (RSS)** | 580 MB – 1.5 GB | **5.1 MB – 15 MB** | **> 98% less RAM** |
| **Heap Under 500 Req Load**| > 200 MB | **0.84 MB** | **> 99% less Heap** |
| **Idle CPU Usage** | 1% – 5% background | **0.0%** | **Pure event sleeping** |
| **Binary / Package Size** | ~180 MB (`node_modules`) | **13 MB** (Single static binary) | **14x smaller** |
| **Cold Start Time** | 3,200 ms | **11 ms** | **290x faster** |
| **Default Port** | `20128` | **`8128`** | **No port collision** |

---

## 🎯 Supported Coding Agents

Run `goroute setup all` or configure individual tools:

| Agent | Command | Target Config File |
| :--- | :--- | :--- |
| **Claude Code** | `goroute setup claude` | `~/.claude/settings.json` |
| **OpenAI Codex CLI** | `goroute setup codex` | `~/.codex/goroute.config.toml` |
| **OpenCode** | `goroute setup opencode` | `~/.config/opencode/opencode.json` |
| **Cursor IDE** | `goroute setup cursor` | `~/.config/Cursor/User/settings.json` |
| **Aider** | `goroute setup aider` | `~/.aider.conf.yml` |
| **Cline** | `goroute setup cline` | `~/.cline/data/secrets.json` |
| **Roo Code** | `goroute setup roo` | `~/.roo/settings-import.json` |
| **Continue (cn CLI)** | `goroute setup continue` | `~/.continue/config.yaml` |
| **Kilo Code** | `goroute setup kilo` | `~/.local/share/kilo/auth.json` |
| **Charm Crush** | `goroute setup crush` | `~/.config/crush/crush.json` |
| **Block Goose** | `goroute setup goose` | `~/.config/goose/config.yaml` |
| **Qwen Code** | `goroute setup qwen` | `~/.qwen/settings.json` |
| **Google Gemini CLI** | `goroute setup gemini` | Shell env recipe |
| **5dive Fleet** | `goroute setup 5dive` | 5dive auth profile recipe |

### Universal CLI Runner
Launch any agent with all OpenAI and Anthropic proxy environment variables automatically injected:
```bash
goroute run claude
goroute run codex --profile goroute
goroute run aider --model openai/claude-3-7-sonnet
goroute run opencode
goroute run goose
```

---

## 🔌 API Endpoints & Capabilities

- **OpenAI Chat Completions** (`POST /v1/chat/completions`):
  Full OpenAI protocol support with real-time SSE chunk streaming.
- **Anthropic Messages API** (`POST /v1/messages`):
  Native Anthropic protocol support with `content_block_delta` SSE streaming (for Claude Code).
- **Multimodal Vision & Media**:
  Supports images, diagrams, screenshots, and base64 data URIs (`image_url` and Anthropic `image` blocks). Auto-translates to Gemini `inlineData`.
- **Vector Embeddings** (`POST /v1/embeddings`):
  Full vector embeddings proxy for Cursor codebase indexing, Continue, and local RAG.
- **In-Memory Zero-Alloc Response Cache**:
  Repeated queries return instant `X-Cache: HIT` (0 ms latency, 0 tokens spent).
- **Multi-Account Failover & Rotation**:
  Rotates across 4+ Google Antigravity accounts. On HTTP 429 (quota) or 403 (license), automatically cascades to the next account or backup provider.
- **Model Discovery** (`GET /v1/models`):
  Lists all live models, combos, and provider aliases.
- **Health Check** (`GET /health`):
  Fast JSON status endpoint for monitoring and uptime probes.

---

## ⚙️ Configuration (`~/.goroute/config.yaml`)

```yaml
server:
  port: 8128
  host: "127.0.0.1"

providers:
  # Antigravity OAuth pool (populated via 'goroute auth login antigravity')
  - id: "antigravity"
    type: "antigravity"
    accounts:
      - id: "acc-1"
        email: "user1@gmail.com"
      - id: "acc-2"
        email: "user2@gmail.com"

  # Standard OpenAI-compatible endpoints (DeepSeek, Groq, TokenRouter, etc.)
  - id: "tokenrouter"
    type: "openai"
    base_url: "https://api.tokenrouter.io/v1"
    api_key: "${TOKENROUTER_API_KEY}"

combos:
  # Automatic failover cascade: tries Antigravity acc-1 -> acc-2 -> TokenRouter backup
  - name: "claude-3-7-sonnet"
    strategy: "failover"
    targets:
      - provider_id: "antigravity"
        model: "claude-3-7-sonnet"
      - provider_id: "tokenrouter"
        model: "claude-3-7-sonnet"
```

---

## 📖 CLI Cheatsheet

```bash
# Service Management
goroute serve                      # Start proxy server (port 8128)
goroute serve --no-dashboard       # Start purely headless (< 10MB RAM)
goroute autostart enable           # Enable Linux systemd background service
goroute autostart status           # Check background service status
goroute autostart disable          # Disable systemd background service
goroute status                     # Probe live gateway & show active models

# Agent Configuration
goroute setup all                  # Configure all 14 coding agents
goroute setup <agent>              # Configure specific agent (claude, codex, etc.)
goroute run <command> [args...]    # Run any CLI agent through proxy

# Providers & Authentication
goroute auth login antigravity     # Sign in with Google Antigravity (PKCE)
goroute auth list                  # List stored credentials
goroute import omniroute           # 1-click import from OmniRoute
goroute catalog                    # Browse 25+ built-in provider presets
```

---

## 📄 License

MIT License. Contributions welcome!
