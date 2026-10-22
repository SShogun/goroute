<div align="center">

<img src="./assets/banner.svg" alt="GoRoute Banner" width="100%" />

<br/>

### The Ultra-Lightweight LLM Gateway & Multi-Provider Router for Coding Agents
**Sub-10ms Latency • < 12MB RAM • 35 CLI Agents • A2A Protocol • Zero Node.js Overhead**

[![Go Version](https://img.shields.io/badge/go-1.24%2B-00ADD8?style=flat-square&logo=go)](https://golang.org)
[![NPM Version](https://img.shields.io/npm/v/goroute?style=flat-square&logo=npm&color=CB3837)](https://www.npmjs.com/package/goroute)
[![GitHub stars](https://img.shields.io/github/stars/ziuus/goroute?style=flat-square&logo=github&color=gold)](https://github.com/ziuus/goroute/stargazers)
[![License: MIT](https://img.shields.io/badge/license-MIT-3DA639?style=flat-square)](LICENSE)
[![Release](https://img.shields.io/github/v/release/ziuus/goroute?style=flat-square&color=8A2BE2)](https://github.com/ziuus/goroute/releases)
[![Memory Footprint](https://img.shields.io/badge/memory-%3C%2012MB%20RAM-00C853?style=flat-square)](#-benchmarks)
[![Platform](https://img.shields.io/badge/platform-linux%20%7C%20macos%20%7C%20windows-lightgrey?style=flat-square)](#-cross-platform-matrix)

<p align="center">
  <a href="#-quickstart">Quickstart</a> •
  <a href="#-architecture">Architecture</a> •
  <a href="#-supported-coding-agents">Supported Agents</a> •
  <a href="#-benchmarks">Benchmarks</a> •
  <a href="#-features">Features</a> •
  <a href="#-cli-cheatsheet">Cheatsheet</a>
</p>

</div>

---

## ⚡ What is GoRoute?

**GoRoute** is an ultra-fast, single-binary LLM gateway engineered specifically for developers using autonomous AI coding agents (**OpenCode, Claude Code, Cursor, Codex CLI, Aider, Cline**, etc.).

Instead of running bloated Node/Electron proxies that consume **600MB to 1.5GB of RAM**, GoRoute delivers identical multi-provider pooling, Google Antigravity OAuth rotation, Anthropic-to-OpenAI protocol normalization, and response caching inside a tiny **5.1MB footprint**.

---

## 🏗️ Architecture

```
                ┌─────────────────────────────────────────────────────────────┐
                │                     YOUR CODING AGENTS                      │
                │  OpenCode • Claude Code • Cursor • Codex CLI • Aider • Roo  │
                └──────────────────────────────┬──────────────────────────────┘
                                               │
                               http://127.0.0.1:8128
                         (OpenAI /v1 & Anthropic /v1/messages)
                                               │
                                               ▼
┌───────────────────────────────────────────────────────────────────────────────────────────┐
│                                     ⚡ GOROUTE CORE                                       │
│                                                                                           │
│  ┌───────────────────────┐  ┌───────────────────────┐  ┌───────────────────────────────┐  │
│  │   Dual-Protocol SSE   │  │   55 RTK Filters      │  │    Session Account Affinity   │  │
│  │  OpenAI ⇆ Anthropic   │  │   70-90% Noise Cut    │  │  Prompt Cache Preservation    │  │
│  └───────────────────────┘  └───────────────────────┘  └───────────────────────────────┘  │
│                                                                                           │
│  ┌─────────────────────────────────────────────────────────────────────────────────────┐  │
│  │                     Autonomous Token Health Scheduler & Router                      │  │
│  │         (Cascades across accounts on 429 quota or 503 limits; proactive refresh)    │  │
│  └─────────────────────────────────────────────────────────────────────────────────────┘  │
└──────────────────────────────────────────────┬────────────────────────────────────────────┘
                                               │
               ┌───────────────────────────────┼───────────────────────────────┐
               ▼                               ▼                               ▼
 ┌───────────────────────────┐   ┌───────────────────────────┐   ┌───────────────────────────┐
 │ Google Antigravity Pool   │   │  AWS CodeWhisperer (Kiro) │   │  OpenAI / Codex Direct    │
 │ Multi-Account • Sonnet    │   │  Context Auto-Pruning     │   │  OpenAI Compatible API    │
 └───────────────────────────┘   └───────────────────────────┘   └───────────────────────────┘
```

---

## 🚀 Quickstart

### 1. Install (Choose One)

#### 📦 Option A: via npm / npx (Zero Go needed)
```bash
npm install -g goroute

# Or run directly via npx:
npx goroute setup opencode
```

#### ⚡ Option B: 1-Line Native Installer (Linux & macOS)
```bash
curl -fsSL https://raw.githubusercontent.com/ziuus/goroute/main/install.sh | bash
```

#### 🍺 Option C: Homebrew (macOS & Linux)
```bash
brew install ziuus/goroute/goroute
```

#### 🛠️ Option D: Go Toolchain
```bash
go install github.com/ziuus/goroute/cmd/goroute@latest
```

---

### 2. Connect Your Accounts

#### Sign in with Google Antigravity (PKCE OAuth):
```bash
goroute auth login antigravity
```
*Securely links your Antigravity accounts into GoRoute's encrypted local storage (`~/.goroute/vault.json`).*

---

### 3. Connect Any Coding Agent

Auto-configure your favorite agent with one command:

```bash
# Configure OpenCode (writes config + registers provider credentials)
goroute setup opencode

# Configure Claude Code
goroute setup claude

# Configure OpenAI Codex CLI
goroute setup codex

# Configure ALL 18 supported agents in one click
goroute setup all
```

---

### 4. Run Seamlessly

Run GoRoute as a background service:

```bash
# Enable Linux background autostart (systemd user unit, ~5MB RAM):
goroute autostart enable

# Verify gateway status:
goroute status
```

Or launch any agent with zero configuration using the universal proxy injector:

```bash
goroute run opencode
goroute run claude
goroute run aider --model openai/claude-3-7-sonnet
```

---

## 📊 Benchmarks vs Traditional Gateways

Tested on Linux x86_64 under 500 concurrent streaming requests:

| Benchmark Metric | Traditional Gateways *(Node/Next.js/Python)* | GoRoute *(Pure Go)* | Efficiency Gain |
| :--- | :--- | :--- | :--- |
| **Idle Memory (RSS)** | `580 MB – 1,450 MB` | **`5.1 MB`** | **99.1% less RAM** |
| **Memory Under 500 Req Load** | `850 MB – 2.1 GB` | **`16.5 MB`** | **98.0% less RAM** |
| **Idle CPU Utilization** | `1.5% – 6.0%` background | **`0.0%`** (Sleep until epoll) | **Zero background waste** |
| **Disk Footprint** | `~180 MB` (`node_modules`) | **`13 MB`** (Static single binary)| **14x smaller** |
| **Cold Startup Time** | `3,200 ms` | **`11 ms`** | **290x faster** |
| **Gateway Port** | `20128` | **`8128`** | **Collision-free** |

---

## 🤖 Supported Coding Agents

`goroute setup` automatically configures paths, headers, authentication keys, and provider manifests for:

| Agent | Command | Target Config File | Features Supported |
| :--- | :--- | :--- | :--- |
| **Claude Code** | `goroute setup claude` | `~/.claude/settings.json` | Anthropic `/v1/messages` protocol + thinking |
| **OpenCode** | `goroute setup opencode` | `~/.config/opencode/opencode.json` | Full models catalog, `auth.json` registration |
| **Cursor IDE** | `goroute setup cursor` | `~/.config/Cursor/User/settings.json`| Custom OpenAI Base URL + `/v1/embeddings` |
| **OpenAI Codex CLI** | `goroute setup codex` | `~/.codex/goroute.config.toml` | Custom profile, auto-compaction |
| **Cline** | `goroute setup cline` | `~/.cline/data/secrets.json` | Act & Plan modes |
| **Roo Code** | `goroute setup roo` | `~/.roo/settings-import.json` | Custom OpenAI compatible profile |
| **OpenClaw** | `goroute setup openclaw` | `~/.openclaw/config.yaml` | Multi-agent execution harness |
| **Hermes Agent** | `goroute setup hermes` | `~/.hermes/config.json` | In-flight reasoning inspection |
| **Droid** | `goroute setup droid` | `~/.droid/settings.json` | Autonomous workspace workflows |
| **Grok-Build** | `goroute setup grok-build` | `~/.grok/build.json` | Native reasoning compiler |
| **Aider** | `goroute setup aider` | `~/.aider.conf.yml` | OpenAI base URL + git workflow routing |
| **Kilo Code** | `goroute setup kilo` | `~/.local/share/kilo/auth.json` | Custom gateway key |
| **Zed Editor** | `goroute setup zed` | `~/.config/zed/settings.json` | Assistant panel routing |
| **Letta / MemGPT** | `goroute setup letta` | `~/.letta/config.json` | Stateful agent memory gateway |
| **Warp Terminal** | `goroute setup warp` | `~/.warp/settings.json` | AI terminal command generator |
| **DeepSeek TUI** | `goroute setup deepseek-tui` | `~/.deepseek/config.toml` | Interactive terminal reasoning |
| **Charm Crush** | `goroute setup crush` | `~/.config/crush/crush.json` | Custom host setup |
| **Continue (cn CLI)** | `goroute setup continue` | `~/.continue/config.yaml` | Chat completions + vector embeddings |
| *+ 17 more tools* | `goroute setup all` | All local tools | 35 CLI agents configured in parallel with auto-backups |

---

## 💎 Features

- **Universal Multi-Protocol Gateway**:
  - `POST /v1/chat/completions` (OpenAI format + SSE chunks)
  - `POST /v1/messages` (Native Anthropic format + `content_block_delta` SSE streaming)
  - `POST /api/chat` (Native Ollama emulation)
  - `GET /mcp` (Model Context Protocol native SSE server)
  - `GET /.well-known/agent.json` & `POST /api/a2a/tasks` (A2A Protocol v0.3)
- **Zero-Alloc In-Memory Cache**:
  - Repeated agent queries (e.g. title generation, linters, duplicate prompts) hit the internal cache and return `X-Cache: HIT` with **0ms latency** and **0 upstream tokens**.
- **DuckDuckGo Web Search & Readability Scraper**:
  - Built-in `POST /v1/web/fetch` and `GET/POST /v1/search` with zero API keys required. Converts fetched HTML to clean GitHub-flavored Markdown.
- **Smart Quota Cascade & Multi-Account Pools**:
  - Dynamically rotates across accounts with configurable strategies: `Priority / Failover`, `Round-Robin`, and `Fair-Share`.
  - Automatically isolates accounts on HTTP 429 quota exhaustion or 402 billing limits with proactive token cooldowns.
- **Context Pruning & Token Optimization**:
  - Single-pass $O(N)$ integer pruning algorithm trims duplicate prompt prefixes and keeps multi-turn transcripts within upstream limits without dropping active agent instructions.

---

## 🤝 Contributing & Good First Issues

We welcome contributors! Check out our [open Good First Issues](https://github.com/ziuus/goroute/issues?q=is%3Aissue+is%3Aopen+label%3A%22good+first+issue%22):
- Adding a new provider adapter takes under 20 lines of pure Go in `pkg/providers/`.
- Adding a new CLI tool integration is just 1 struct entry in `pkg/clitools/registry.go`.
- Read [CONTRIBUTING.md](CONTRIBUTING.md) to get started in 2 minutes.

---

## 💻 Cross-Platform Matrix

GoRoute provides pre-compiled, optimized static binaries for all major platforms:

| Platform | Architecture | Binary Format | Autostart Service |
| :--- | :--- | :--- | :--- |
| **Linux** | `x86_64`, `arm64` | Native ELF | `systemd` user service |
| **macOS** | Apple Silicon (`arm64`), Intel (`x86_64`) | Mach-O | `launchd` / CLI |
| **Windows** | `x64`, `arm64` | Native `.exe` | Windows Task / CLI |
| **NPM Wrapper** | Any Node.js 18+ environment | Universal JS runner | Auto-detects platform |

---

## 📖 CLI Cheatsheet

```bash
# Gateway Management
goroute serve                       # Start gateway server (port 8128)
goroute serve --no-dashboard        # Headless mode (< 10MB RAM)
goroute autostart enable            # Enable system background service
goroute autostart status            # Check service status
goroute autostart disable           # Stop & disable service
goroute status                      # Health check & model listing

# Agent Setups
goroute setup all                   # Configure all 14 coding agents
goroute setup <agent>               # Configure a specific agent (e.g. opencode, claude)
goroute run <agent> [args...]       # Launch agent with proxy env variables

# Providers & Accounts
goroute auth login antigravity      # Interactive PKCE OAuth login
goroute auth list                   # List connected provider accounts
goroute catalog                     # View 25+ built-in provider presets
```

---

## 📄 License

MIT © [ziuus](https://github.com/ziuus). Designed for developers who love speed.
