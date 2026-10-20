package proxy

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/ziuus/goroute/pkg/acp"
	"github.com/ziuus/goroute/pkg/affinity"
	"github.com/ziuus/goroute/pkg/cache"
	"github.com/ziuus/goroute/pkg/catalog"
	"github.com/ziuus/goroute/pkg/clitools"
	"github.com/ziuus/goroute/pkg/compression"
	"github.com/ziuus/goroute/pkg/config"
	"github.com/ziuus/goroute/pkg/keys"
	"github.com/ziuus/goroute/pkg/mcp"
	"github.com/ziuus/goroute/pkg/pool"
	"github.com/ziuus/goroute/pkg/providers"
	"github.com/ziuus/goroute/pkg/providers/antigravity"
	"github.com/ziuus/goroute/pkg/providers/codex"
	"github.com/ziuus/goroute/pkg/providers/copilot"
	"github.com/ziuus/goroute/pkg/providers/cursor"
	"github.com/ziuus/goroute/pkg/providers/kiro"
	"github.com/ziuus/goroute/pkg/providers/openai"
	"github.com/ziuus/goroute/pkg/quota"
	"github.com/ziuus/goroute/pkg/resilience"
	"github.com/ziuus/goroute/pkg/router"
	"github.com/ziuus/goroute/pkg/rtk"
	"github.com/ziuus/goroute/pkg/store"
	"github.com/ziuus/goroute/pkg/tokens"
	"github.com/ziuus/goroute/pkg/tunnel"
	"github.com/ziuus/goroute/web"
)

type Server struct {
	router          *router.Router
	limiter         *quota.Limiter
	cache           *cache.Cache
	tunnelMgr       *tunnel.Manager
	keyMgr          *keys.Manager
	mcpServer       *mcp.Server
	enableDashboard bool
	mux             *chi.Mux
	cfgPath         string
	storePath       string
	startTime       time.Time
}

func (s *Server) SetConfigPaths(cfgPath, storePath string) {
	s.cfgPath = cfgPath
	s.storePath = storePath
	if s.keyMgr != nil {
		_ = s.keyMgr.Reload()
	} else {
		s.keyMgr = keys.NewManager(cfgPath)
	}
}

func NewServer(r *router.Router, limiter *quota.Limiter, enableDashboard bool) *Server {
	home, _ := os.UserHomeDir()
	cfgPath := filepath.Join(home, ".goroute", "config.yaml")

	s := &Server{
		router:          r,
		limiter:         limiter,
		cache:           cache.NewCache(15*time.Minute, 2000),
		tunnelMgr:       tunnel.NewManager(8128),
		keyMgr:          keys.NewManager(cfgPath),
		mcpServer:       mcp.NewServer(resilience.GlobalCooldown),
		enableDashboard: enableDashboard,
		cfgPath:         cfgPath,
		startTime:       time.Now(),
		mux:             chi.NewRouter(),
	}

	s.setupRoutes()
	tokens.InitScheduler(s.router, 3*time.Minute)
	return s
}

func (s *Server) setupRoutes() {
	s.mux.Use(middleware.RequestID)
	s.mux.Use(middleware.RealIP)
	s.mux.Use(middleware.Recoverer)

	// Enable CORS for web apps & IDE extensions
	s.mux.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS, PUT, DELETE")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Accept, X-Requested-With")
			if r.Method == "OPTIONS" {
				w.WriteHeader(http.StatusOK)
				return
			}
			next.ServeHTTP(w, r)
		})
	})

	s.mux.Get("/health", s.handleHealth)
	s.mux.Get("/api/status", s.handleAPIStatus)
	s.mux.Get("/api/catalog", s.handleCatalog)
	s.mux.Get("/api/logs", s.handleLogs)
	s.mux.Get("/api/logs/stream", s.handleLogsStream)
	s.mux.Post("/api/reload", s.handleReload)
	s.mux.Post("/api/agents/setup", s.handleAgentSetup)
	s.mux.Post("/api/providers/add", s.handleAddProvider)
	s.mux.Post("/api/providers/{id}/accounts", s.handleAddAccount)
	s.mux.Get("/api/providers/{id}/models", s.handleProviderModels)
	s.mux.Post("/api/providers/{id}/test-model", s.handleTestModel)
	s.mux.Post("/api/providers/{id}/expose-claude", s.handleExposeClaude)
	s.mux.Post("/api/playground/chat", s.handlePlaygroundChat)
	s.mux.Post("/api/providers/import-kiro", s.handleImportKiro)
	s.mux.Post("/api/providers/import-codex", s.handleImportCodex)
	s.mux.Post("/api/providers/import-copilot", s.handleImportCopilot)
	s.mux.Post("/api/providers/import-cursor", s.handleImportCursor)
	s.mux.Post("/api/providers/{id}/test", s.handleTestProvider)
	s.mux.Post("/api/providers/{id}/accounts/{accId}/test", s.handleTestAccount)
	s.mux.Delete("/api/providers/{id}", s.handleDeleteProvider)
	s.mux.Delete("/api/providers/{id}/accounts/{accId}", s.handleDeleteAccount)
	s.mux.Get("/api/resilience/model-cooldowns", s.handleModelCooldowns)
	s.mux.Post("/api/resilience/reset", s.handleResilienceReset)
	s.mux.Get("/api/usage/quota", s.handleUsageQuota)
	s.mux.Get("/api/usage/analytics", s.handleUsageAnalytics)
	s.mux.Get("/api/keys", s.handleListKeys)
	s.mux.Post("/api/keys", s.handleCreateKey)
	s.mux.Delete("/api/keys/{id}", s.handleRevokeKey)
	s.mux.Post("/api/compression/compress", s.handleCompressPrompt)
	s.mux.Get("/api/router/stats", s.handleRouterStats)
	s.mux.Post("/api/router/quota", s.handleSetRouterQuota)
	s.mux.Get("/api/cli-agents", s.handleListCliAgents)
	s.mux.Post("/api/cli-agents/{id}/configure", s.handleConfigureCliAgent)
	s.mux.Get("/api/topology", s.handleTopology)
	s.mux.Get("/api/combos", s.handleListCombos)
	s.mux.Post("/api/combos", s.handleSaveCombo)
	s.mux.Delete("/api/combos/{name}", s.handleDeleteCombo)
	s.mux.Get("/api/rtk/filters", s.handleListRTKFilters)
	s.mux.Post("/api/rtk/test", s.handleTestRTKFilter)
	s.mux.Get("/api/affinity/sessions", s.handleListAffinitySessions)
	s.mux.Get("/api/acp/sessions", s.handleListAcpSessions)
	s.mux.Post("/api/acp/spawn", s.handleSpawnAcpSession)
	s.mux.Delete("/api/acp/sessions/{id}", s.handleKillAcpSession)
	s.mux.Get("/api/tokens/health", s.handleListTokensHealth)
	s.mux.Get("/api/pool/usage", s.handleListPoolUsage)

	// Built-in MCP Server
	s.mux.Get("/sse", s.mcpServer.HandleSSE)
	s.mux.Post("/message", s.mcpServer.HandleMessage)
	s.mux.Get("/mcp/tools", s.mcpServer.HandleToolsList)
	s.mux.Post("/mcp/tools", s.mcpServer.HandleToolCall)

	s.tunnelMgr.RegisterRoutes(s.mux)
	s.mux.Get("/v1/models", s.handleModels)
	s.mux.Get("/models", s.handleModels)
	s.mux.Get("/api/models", s.handleModels)
	s.mux.Get("/api/v1/models", s.handleModels)
	s.mux.Get("/v1beta/models", s.handleModels)
	s.mux.Post("/v1/chat/completions", s.handleChatCompletions)
	s.mux.Post("/chat/completions", s.handleChatCompletions)
	s.mux.Post("/v1/messages", s.handleMessages)
	s.mux.Post("/messages", s.handleMessages)
	s.mux.Post("/v1/embeddings", s.handleEmbeddings)
	s.mux.Post("/v1/images/generations", s.handleImageGenerations)
	s.mux.Post("/v1/audio/speech", s.handleAudioSpeech)
	s.mux.Post("/v1/audio/transcriptions", s.handleAudioTranscriptions)
	s.mux.Post("/v1/rerank", s.handleRerank)
	s.mux.Post("/v1/moderations", s.handleModerations)
	if s.enableDashboard {
		s.mux.Handle("/*", web.Handler())
	}
}

func (s *Server) Handler() http.Handler {
	return s.mux
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"ok","engine":"goroute"}`))
}

func (s *Server) handleModels(w http.ResponseWriter, r *http.Request) {
	models, err := s.router.ListModels(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"object": "list",
		"data":   models,
	})
}

func (s *Server) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	var chatReq providers.ChatRequest
	if err := json.NewDecoder(r.Body).Decode(&chatReq); err != nil {
		http.Error(w, "invalid request json: "+err.Error(), http.StatusBadRequest)
		return
	}
	log.Printf("[GoRoute] Incoming /v1/chat/completions model=%s stream=%v messages=%d", chatReq.Model, chatReq.Stream, len(chatReq.Messages))

	// Validate virtual key if keys are registered
	if s.keyMgr != nil && s.keyMgr.HasKeys() {
		ok, _, errMsg := s.keyMgr.Validate(r.Header.Get("Authorization"), chatReq.Model)
		if !ok {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"error": map[string]interface{}{
					"message": errMsg,
					"type":    "invalid_request_error",
				},
			})
			return
		}
	}

	// Apply prompt token compression if requested
	if r.Header.Get("X-Compress-Prompt") == "true" {
		for i := range chatReq.Messages {
			if str, ok := chatReq.Messages[i].Content.(string); ok {
				compressed, _ := compression.CompressText(str)
				chatReq.Messages[i].Content = compressed
			}
		}
	}

	// Guardrail messages to prevent context overflow 400 errors
	chatReq.Messages = SanitizeAndGuardrailMessages(chatReq.Messages, 0)

	// Check rate limit/quota if configured
	if !s.limiter.Allow(chatReq.Model) {
		http.Error(w, "Rate limit exceeded (cooling down)", http.StatusTooManyRequests)
		return
	}

	var cacheKey string
	if !chatReq.Stream {
		cacheKey = s.cache.GenerateKey(&chatReq)
		if cached, hit := s.cache.Get(cacheKey); hit {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("X-Cache", "HIT")
			w.WriteHeader(http.StatusOK)
			w.Write(cached)
			RecordRequest(RequestLog{
				ID:         fmt.Sprintf("req_%d", time.Now().UnixNano()),
				Timestamp:  start,
				Endpoint:   "/v1/chat/completions",
				Model:      chatReq.Model,
				StatusCode: http.StatusOK,
				DurationMs: time.Since(start).Milliseconds(),
			})
			return
		}
	}

	poolHeader := r.Header.Get("x-goroute-pool")
	if poolHeader == "" {
		poolHeader = r.Header.Get("x-pool")
	}
	if poolHeader == "" {
		poolHeader = r.URL.Query().Get("pool")
	}
	ctx := r.Context()
	if poolHeader != "" {
		ctx = context.WithValue(ctx, "goroute-pool", poolHeader)
	}

	body, isStream, statusCode, err := s.router.Execute(ctx, &chatReq)
	if err != nil {
		log.Printf("[GoRoute] Execute error: %v (code %d)", err, statusCode)
		if statusCode < 100 || statusCode > 599 {
			statusCode = http.StatusInternalServerError
		}
		RecordRequest(RequestLog{
			ID:         fmt.Sprintf("req_%d", time.Now().UnixNano()),
			Timestamp:  start,
			Endpoint:   "/v1/chat/completions",
			Model:      chatReq.Model,
			StatusCode: statusCode,
			DurationMs: time.Since(start).Milliseconds(),
			Error:      err.Error(),
		})
		http.Error(w, err.Error(), statusCode)
		return
	}
	defer body.Close()
	log.Printf("[GoRoute] Upstream returned status %d isStream=%v", statusCode, isStream)

	if statusCode >= 400 {
		RecordRequest(RequestLog{
			ID:         fmt.Sprintf("req_%d", time.Now().UnixNano()),
			Timestamp:  start,
			Endpoint:   "/v1/chat/completions",
			Model:      chatReq.Model,
			StatusCode: statusCode,
			DurationMs: time.Since(start).Milliseconds(),
			Error:      fmt.Sprintf("upstream returned %d", statusCode),
		})
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(statusCode)
		io.Copy(w, body)
		return
	}

	if isStream && !chatReq.Stream {
		consolidated, err := consolidateSSEStream(body, chatReq.Model)
		if err != nil {
			http.Error(w, "failed to consolidate stream: "+err.Error(), http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(statusCode)
		w.Write(consolidated)
		if statusCode == http.StatusOK && cacheKey != "" {
			s.cache.Set(cacheKey, consolidated)
		}
		RecordRequest(RequestLog{
			ID:         fmt.Sprintf("req_%d", time.Now().UnixNano()),
			Timestamp:  start,
			Endpoint:   "/v1/chat/completions",
			Model:      chatReq.Model,
			StatusCode: statusCode,
			DurationMs: time.Since(start).Milliseconds(),
		})
		return
	}

	if isStream {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.WriteHeader(statusCode)

		flusher, ok := w.(http.Flusher)
		buf := make([]byte, 4096)
		for {
			n, readErr := body.Read(buf)
			if n > 0 {
				w.Write(buf[:n])
				if ok {
					flusher.Flush()
				}
			}
			if readErr != nil {
				break
			}
		}
		RecordRequest(RequestLog{
			ID:         fmt.Sprintf("req_%d", time.Now().UnixNano()),
			Timestamp:  start,
			Endpoint:   "/v1/chat/completions",
			Model:      chatReq.Model,
			StatusCode: statusCode,
			DurationMs: time.Since(start).Milliseconds(),
		})
		return
	}

	// Non-streaming JSON response
	respBytes, err := io.ReadAll(body)
	if err != nil {
		http.Error(w, "failed to read upstream body: "+err.Error(), http.StatusBadGateway)
		return
	}

	if statusCode == http.StatusOK && cacheKey != "" {
		s.cache.Set(cacheKey, respBytes)
	}

	RecordRequest(RequestLog{
		ID:         fmt.Sprintf("req_%d", time.Now().UnixNano()),
		Timestamp:  start,
		Endpoint:   "/v1/chat/completions",
		Model:      chatReq.Model,
		StatusCode: statusCode,
		DurationMs: time.Since(start).Milliseconds(),
	})

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Cache", "MISS")
	w.WriteHeader(statusCode)
	w.Write(respBytes)
}

func (s *Server) handleEmbeddings(w http.ResponseWriter, r *http.Request) {
	var embReq providers.EmbeddingRequest
	if err := json.NewDecoder(r.Body).Decode(&embReq); err != nil {
		http.Error(w, "invalid request json: "+err.Error(), http.StatusBadRequest)
		return
	}

	resp, err := s.router.ExecuteEmbeddings(r.Context(), &embReq)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(resp)
}

func (s *Server) handleAPIStatus(w http.ResponseWriter, r *http.Request) {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	home, _ := os.UserHomeDir()
	cfgPath := filepath.Join(home, ".goroute", "config.yaml")
	cfg, err := config.Load(cfgPath)
	if err != nil {
		cfg = &config.Config{}
	}

	models, _ := s.router.ListModels(r.Context())

	fileExists := func(p string) bool {
		_, err := os.Stat(p)
		return err == nil
	}

	// Comprehensive agent statuses across all 14 supported agents
	agentMap := map[string]interface{}{
		"claude": map[string]interface{}{
			"name":       "Claude Code",
			"desc":       "Anthropic terminal agent & subagent fleet (~/.claude/settings.json)",
			"configured": fileExists(filepath.Join(home, ".claude", "settings.json")),
			"cmd":        "goroute setup claude",
		},
		"opencode": map[string]interface{}{
			"name":       "OpenCode",
			"desc":       "Autonomous terminal coding agent (~/.config/opencode/opencode.json)",
			"configured": fileExists(filepath.Join(home, ".config", "opencode", "opencode.json")),
			"cmd":        "goroute setup opencode",
		},
		"codex": map[string]interface{}{
			"name":       "OpenAI Codex",
			"desc":       "Codex CLI agent (~/.codex/config.toml)",
			"configured": fileExists(filepath.Join(home, ".codex", "config.toml")),
			"cmd":        "goroute setup codex",
		},
		"cursor": map[string]interface{}{
			"name":       "Cursor / Cline",
			"desc":       "Universal OpenAI base: http://127.0.0.1:8128/v1",
			"configured": true,
			"cmd":        "goroute setup cursor",
		},
		"aider": map[string]interface{}{
			"name":       "Aider Assistant",
			"desc":       "Terminal AI pair programmer (~/.aider.conf.yml)",
			"configured": fileExists(filepath.Join(home, ".aider.conf.yml")),
			"cmd":        "goroute setup aider",
		},
		"cline": map[string]interface{}{
			"name":       "Cline Agent",
			"desc":       "Autonomous coding extension (~/.cline/data/secrets.json)",
			"configured": fileExists(filepath.Join(home, ".cline")),
			"cmd":        "goroute setup cline",
		},
		"roo": map[string]interface{}{
			"name":       "Roo Code",
			"desc":       "Multi-mode coding assistant (~/.roo/settings-import.json)",
			"configured": fileExists(filepath.Join(home, ".roo")),
			"cmd":        "goroute setup roo",
		},
		"continue": map[string]interface{}{
			"name":       "Continue.dev",
			"desc":       "IDE Assistant & cn CLI (~/.continue/config.yaml)",
			"configured": fileExists(filepath.Join(home, ".continue", "config.yaml")),
			"cmd":        "goroute setup continue",
		},
		"kilo": map[string]interface{}{
			"name":       "Kilo Code",
			"desc":       "Agent tool (~/.local/share/kilo/auth.json)",
			"configured": fileExists(filepath.Join(home, ".local", "share", "kilo", "auth.json")),
			"cmd":        "goroute setup kilo",
		},
		"crush": map[string]interface{}{
			"name":       "Charm Crush",
			"desc":       "Terminal agent (~/.config/crush/crush.json)",
			"configured": fileExists(filepath.Join(home, ".config", "crush", "crush.json")),
			"cmd":        "goroute setup crush",
		},
		"goose": map[string]interface{}{
			"name":       "Block Goose",
			"desc":       "Extensible on-machine agent (~/.config/goose/config.yaml)",
			"configured": fileExists(filepath.Join(home, ".config", "goose", "config.yaml")),
			"cmd":        "goroute setup goose",
		},
		"qwen": map[string]interface{}{
			"name":       "Qwen Code",
			"desc":       "Qwen modelProvider setup (~/.qwen/settings.json)",
			"configured": fileExists(filepath.Join(home, ".qwen", "settings.json")),
			"cmd":        "goroute setup qwen",
		},
		"gemini": map[string]interface{}{
			"name":       "Gemini CLI",
			"desc":       "Google Gemini terminal environment",
			"configured": fileExists(filepath.Join(home, ".gemini")),
			"cmd":        "goroute setup gemini",
		},
		"5dive": map[string]interface{}{
			"name":       "5dive Fleet",
			"desc":       "5dive agent fleet auth profile (~/.5dive/config.json)",
			"configured": fileExists(filepath.Join(home, ".5dive", "config.json")),
			"cmd":        "goroute setup 5dive",
		},
		"hermes": map[string]interface{}{
			"name":       "Hermes Agent",
			"desc":       "Nous Research Hermes tool-calling agent (~/.hermes/config.yaml)",
			"configured": fileExists(filepath.Join(home, ".hermes", "config.yaml")),
			"cmd":        "goroute setup hermes",
		},
		"openclaw": map[string]interface{}{
			"name":       "OpenClaw",
			"desc":       "Autonomous multi-channel gateway & subagent fleet (~/.openclaw/openclaw.json)",
			"configured": fileExists(filepath.Join(home, ".openclaw", "openclaw.json")),
			"cmd":        "goroute setup openclaw",
		},
	}

	// Calculate sanitized provider accounts
	type AccountSummary struct {
		ID        string `json:"id"`
		Email     string `json:"email"`
		Status    string `json:"status"`
		ProjectID string `json:"project_id,omitempty"`
	}
	type ProviderSummary struct {
		ID       string           `json:"id"`
		Type     string           `json:"type"`
		BaseURL  string           `json:"base_url,omitempty"`
		HasKey   bool             `json:"has_key"`
		Status   string           `json:"status"`
		Accounts []AccountSummary `json:"accounts,omitempty"`
	}

	var provSummaries []ProviderSummary
	for _, p := range cfg.Providers {
		ps := ProviderSummary{
			ID:      p.ID,
			Type:    p.Type,
			BaseURL: p.BaseURL,
			HasKey:  p.APIKey != "" || p.RefreshToken != "" || len(p.Accounts) > 0,
			Status:  "active",
		}
		if len(p.Accounts) > 0 {
			for _, a := range p.Accounts {
				label := a.Email
				if label == "" {
					label = a.ID
				}
				ps.Accounts = append(ps.Accounts, AccountSummary{
					ID:        a.ID,
					Email:     label,
					Status:    "active",
					ProjectID: a.ProjectID,
				})
			}
		} else if p.RefreshToken != "" || p.APIKey != "" {
			ps.Accounts = append(ps.Accounts, AccountSummary{
				ID:     p.ID,
				Email:  "Default Account",
				Status: "active",
			})
		}
		provSummaries = append(provSummaries, ps)
	}

	uptimeSec := time.Since(s.startTime).Seconds()
	if s.startTime.IsZero() {
		uptimeSec = 1.0
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":         "active",
		"engine":         "goroute",
		"port":           8128,
		"uptime":         int64(uptimeSec),
		"uptime_seconds": int64(uptimeSec),
		"memory_mb":      float64(m.Alloc) / 1024 / 1024,
		"providers":      provSummaries,
		"combos":         cfg.Combos,
		"models":         models,
		"agents":         agentMap,
	})
}

func (s *Server) handleCatalog(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "ok",
		"count":  len(catalog.Registry),
		"data":   catalog.Registry,
	})
}

func (s *Server) handleAgentSetup(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Agent string `json:"agent"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	out, err := exec.Command("goroute", "setup", req.Agent).CombinedOutput()
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "error",
			"error":  string(out),
		})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "ok",
		"message": string(out),
	})
}

func (s *Server) handleAddProvider(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID           string `json:"id"`
		Type         string `json:"type"`
		BaseURL      string `json:"base_url"`
		APIKey       string `json:"api_key"`
		RefreshToken string `json:"refresh_token"`
		ProfileARN   string `json:"profile_arn"`
		AccountID    string `json:"account_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if req.ID == "" {
		http.Error(w, "provider id required", http.StatusBadRequest)
		return
	}
	if req.Type == "" {
		req.Type = "openai"
	}

	home, _ := os.UserHomeDir()
	cfgPath := s.cfgPath
	if cfgPath == "" {
		cfgPath = filepath.Join(home, ".goroute", "config.yaml")
	}

	cfg, err := config.Load(cfgPath)
	if err != nil {
		cfg = config.DefaultConfig()
	}

	found := false
	for i, p := range cfg.Providers {
		if p.ID == req.ID {
			found = true
			if req.Type == "kiro" && req.RefreshToken != "" {
				// Append account to existing kiro provider pool
				if len(cfg.Providers[i].Accounts) == 0 && cfg.Providers[i].RefreshToken != "" {
					cfg.Providers[i].Accounts = append(cfg.Providers[i].Accounts, config.AccountConfig{
						ID:           cfg.Providers[i].ID + "-primary",
						RefreshToken: cfg.Providers[i].RefreshToken,
						ProfileARN:   cfg.Providers[i].ProfileARN,
					})
				}
				accID := req.AccountID
				if accID == "" {
					accID = fmt.Sprintf("%s-%d", req.ID, len(cfg.Providers[i].Accounts)+1)
				}
				cfg.Providers[i].Accounts = append(cfg.Providers[i].Accounts, config.AccountConfig{
					ID:           accID,
					RefreshToken: req.RefreshToken,
					ProfileARN:   req.ProfileARN,
				})
			} else {
				cfg.Providers[i].BaseURL = req.BaseURL
				cfg.Providers[i].APIKey = req.APIKey
				cfg.Providers[i].Type = req.Type
				if req.RefreshToken != "" {
					cfg.Providers[i].RefreshToken = req.RefreshToken
				}
				if req.ProfileARN != "" {
					cfg.Providers[i].ProfileARN = req.ProfileARN
				}
			}
			break
		}
	}
	if !found {
		newProv := config.ProviderConfig{
			ID:           req.ID,
			Type:         req.Type,
			BaseURL:      req.BaseURL,
			APIKey:       req.APIKey,
			RefreshToken: req.RefreshToken,
			ProfileARN:   req.ProfileARN,
		}
		if req.Type == "kiro" && req.RefreshToken != "" {
			accID := req.AccountID
			if accID == "" {
				accID = req.ID
			}
			newProv.Accounts = []config.AccountConfig{
				{
					ID:           accID,
					RefreshToken: req.RefreshToken,
					ProfileARN:   req.ProfileARN,
				},
			}
		}
		cfg.Providers = append(cfg.Providers, newProv)
	}

	if err := cfg.Save(cfgPath); err != nil {
		http.Error(w, "save config failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	_ = s.Reload()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "ok",
		"id":     req.ID,
	})
}

func (s *Server) handleImportKiro(w http.ResponseWriter, r *http.Request) {
	out, err := exec.Command("goroute", "auth", "import", "kiro").CombinedOutput()
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "error",
			"error":  string(out),
		})
		return
	}
	_ = s.Reload()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "ok",
		"output":  strings.TrimSpace(string(out)),
		"message": "Successfully imported Kiro account into active pool",
	})
}

func (s *Server) handleImportCodex(w http.ResponseWriter, r *http.Request) {
	acc, err := codex.ReadCodexCLIAuth()
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "error",
			"error":  err.Error(),
		})
		return
	}

	cfgPath := s.cfgPath
	if cfgPath == "" {
		home, _ := os.UserHomeDir()
		cfgPath = filepath.Join(home, ".goroute", "config.yaml")
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		cfg = config.DefaultConfig()
	}

	found := false
	for i, p := range cfg.Providers {
		if p.ID == "codex" {
			found = true
			updated := false
			for j, a := range cfg.Providers[i].Accounts {
				if a.ID == "codex-cli" {
					cfg.Providers[i].Accounts[j].RefreshToken = acc.RefreshToken
					cfg.Providers[i].Accounts[j].AccessToken = acc.AccessToken
					cfg.Providers[i].Accounts[j].AccountID = acc.AccountID
					updated = true
					break
				}
			}
			if !updated {
				cfg.Providers[i].Accounts = append(cfg.Providers[i].Accounts, config.AccountConfig{
					ID:           "codex-cli",
					RefreshToken: acc.RefreshToken,
					AccessToken:  acc.AccessToken,
					AccountID:    acc.AccountID,
				})
			}
			break
		}
	}
	if !found {
		cfg.Providers = append(cfg.Providers, config.ProviderConfig{
			ID:           "codex",
			Type:         "codex",
			RefreshToken: acc.RefreshToken,
			Accounts: []config.AccountConfig{
				{
					ID:           "codex-cli",
					RefreshToken: acc.RefreshToken,
					AccessToken:  acc.AccessToken,
					AccountID:    acc.AccountID,
				},
			},
		})
	}

	if err := cfg.Save(cfgPath); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	_ = s.Reload()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "ok",
		"message": "Successfully imported OpenAI Codex credentials from ~/.codex/auth.json",
	})
}

func (s *Server) handleImportCopilot(w http.ResponseWriter, r *http.Request) {
	acc, err := copilot.ReadCopilotCLIAuth()
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "error",
			"error":  err.Error(),
		})
		return
	}

	cfgPath := s.cfgPath
	if cfgPath == "" {
		home, _ := os.UserHomeDir()
		cfgPath = filepath.Join(home, ".goroute", "config.yaml")
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		cfg = config.DefaultConfig()
	}

	found := false
	for i, p := range cfg.Providers {
		if p.ID == "copilot" {
			found = true
			updated := false
			for j, a := range cfg.Providers[i].Accounts {
				if a.ID == acc.ID {
					cfg.Providers[i].Accounts[j].APIKey = acc.GitHubToken
					cfg.Providers[i].Accounts[j].Email = acc.User
					updated = true
					break
				}
			}
			if !updated {
				cfg.Providers[i].Accounts = append(cfg.Providers[i].Accounts, config.AccountConfig{
					ID:     acc.ID,
					APIKey: acc.GitHubToken,
					Email:  acc.User,
				})
			}
			break
		}
	}
	if !found {
		cfg.Providers = append(cfg.Providers, config.ProviderConfig{
			ID:     "copilot",
			Type:   "copilot",
			APIKey: acc.GitHubToken,
			Accounts: []config.AccountConfig{
				{
					ID:     acc.ID,
					APIKey: acc.GitHubToken,
					Email:  acc.User,
				},
			},
		})
	}

	if err := cfg.Save(cfgPath); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	_ = s.Reload()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "ok",
		"message": "Successfully imported GitHub Copilot credentials from ~/.config/github-copilot",
	})
}

func (s *Server) handleDeleteProvider(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		http.Error(w, "provider id required", http.StatusBadRequest)
		return
	}
	cfgPath := s.cfgPath
	if cfgPath == "" {
		home, _ := os.UserHomeDir()
		cfgPath = filepath.Join(home, ".goroute", "config.yaml")
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	var newProviders []config.ProviderConfig
	for _, p := range cfg.Providers {
		if p.ID != id {
			newProviders = append(newProviders, p)
		}
	}
	cfg.Providers = newProviders
	if err := cfg.Save(cfgPath); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = s.Reload()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"status": "ok", "deleted": id})
}

func (s *Server) handleAddAccount(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		http.Error(w, "provider id required", http.StatusBadRequest)
		return
	}
	var req struct {
		AccountID    string  `json:"account_id"`
		ID           string  `json:"id"`
		Pool         string  `json:"pool"`
		Type         string  `json:"type"`
		BaseURL      string  `json:"base_url"`
		APIKey       string  `json:"api_key"`
		RefreshToken string  `json:"refresh_token"`
		Email        string  `json:"email"`
		ProjectID    string  `json:"project_id"`
		ProfileARN   string  `json:"profile_arn"`
		Weight       float64 `json:"weight"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	accID := req.AccountID
	if accID == "" {
		accID = req.ID
	}
	if accID == "" {
		http.Error(w, "account_id is required", http.StatusBadRequest)
		return
	}
	if req.Pool == "" {
		req.Pool = "pool1"
	}
	if req.Weight <= 0 {
		req.Weight = 1.0
	}

	cfgPath := s.cfgPath
	if cfgPath == "" {
		home, _ := os.UserHomeDir()
		cfgPath = filepath.Join(home, ".goroute", "config.yaml")
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		cfg = config.DefaultConfig()
	}

	found := false
	for i, p := range cfg.Providers {
		if p.ID == id {
			found = true
			if req.BaseURL != "" {
				cfg.Providers[i].BaseURL = req.BaseURL
			}
			if len(cfg.Providers[i].Accounts) == 0 && (cfg.Providers[i].APIKey != "" || cfg.Providers[i].RefreshToken != "") {
				cfg.Providers[i].Accounts = append(cfg.Providers[i].Accounts, config.AccountConfig{
					ID:           p.ID + "-primary",
					Pool:         "pool1",
					APIKey:       p.APIKey,
					RefreshToken: p.RefreshToken,
					ProfileARN:   p.ProfileARN,
					Weight:       1.0,
				})
			}
			updated := false
			for j, a := range cfg.Providers[i].Accounts {
				if a.ID == accID {
					if req.Pool != "" {
						cfg.Providers[i].Accounts[j].Pool = req.Pool
					}
					if req.APIKey != "" {
						cfg.Providers[i].Accounts[j].APIKey = req.APIKey
					}
					if req.RefreshToken != "" {
						cfg.Providers[i].Accounts[j].RefreshToken = req.RefreshToken
					}
					if req.Email != "" {
						cfg.Providers[i].Accounts[j].Email = req.Email
					}
					if req.ProjectID != "" {
						cfg.Providers[i].Accounts[j].ProjectID = req.ProjectID
					}
					if req.ProfileARN != "" {
						cfg.Providers[i].Accounts[j].ProfileARN = req.ProfileARN
					}
					if req.Weight > 0 {
						cfg.Providers[i].Accounts[j].Weight = req.Weight
					}
					updated = true
					break
				}
			}
			if !updated {
				cfg.Providers[i].Accounts = append(cfg.Providers[i].Accounts, config.AccountConfig{
					ID:           accID,
					Pool:         req.Pool,
					APIKey:       req.APIKey,
					RefreshToken: req.RefreshToken,
					Email:        req.Email,
					ProjectID:    req.ProjectID,
					ProfileARN:   req.ProfileARN,
					Weight:       req.Weight,
				})
			}
			break
		}
	}

	if !found {
		pType := req.Type
		baseURL := req.BaseURL
		entry := catalog.FindEntry(id)
		if entry != nil {
			if pType == "" || pType == id {
				pType = entry.Type
			}
			if baseURL == "" {
				baseURL = entry.BaseURL
			}
		}
		if pType == "" {
			pType = "openai"
		}
		newProv := config.ProviderConfig{
			ID:      id,
			Type:    pType,
			BaseURL: baseURL,
			Accounts: []config.AccountConfig{
				{
					ID:           accID,
					Pool:         req.Pool,
					APIKey:       req.APIKey,
					RefreshToken: req.RefreshToken,
					Email:        req.Email,
					ProjectID:    req.ProjectID,
					ProfileARN:   req.ProfileARN,
					Weight:       req.Weight,
				},
			},
		}
		cfg.Providers = append(cfg.Providers, newProv)
	}

	if err := cfg.Save(cfgPath); err != nil {
		http.Error(w, "save config failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	pool.GlobalPool.RegisterAccount(req.Pool, id, accID, req.Weight)
	_ = s.Reload()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":     "ok",
		"provider":   id,
		"account_id": accID,
		"pool":       req.Pool,
		"message":    fmt.Sprintf("Account %s added to pool %s successfully", accID, req.Pool),
	})
}

func (s *Server) handleDeleteAccount(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	accId := chi.URLParam(r, "accId")
	if id == "" || accId == "" {
		http.Error(w, "provider id and account id required", http.StatusBadRequest)
		return
	}
	cfgPath := s.cfgPath
	if cfgPath == "" {
		home, _ := os.UserHomeDir()
		cfgPath = filepath.Join(home, ".goroute", "config.yaml")
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	for i, p := range cfg.Providers {
		if p.ID == id {
			var newAccounts []config.AccountConfig
			for _, a := range p.Accounts {
				if a.ID != accId {
					newAccounts = append(newAccounts, a)
				}
			}
			cfg.Providers[i].Accounts = newAccounts
			break
		}
	}
	if err := cfg.Save(cfgPath); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	pool.GlobalPool.RemoveAccount(id, accId)
	_ = s.Reload()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"status": "ok", "deleted_account": accId})
}

func (s *Server) handleTestProvider(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		http.Error(w, "provider id required", http.StatusBadRequest)
		return
	}
	p, ok := s.router.GetProvider(id)
	if !ok {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "error",
			"valid":  false,
			"error":  fmt.Sprintf("provider %q not found in active router", id),
		})
		return
	}

	start := time.Now()
	var err error
	ctx := r.Context()

	switch prov := p.(type) {
	case *kiro.Provider:
		err = prov.TestConnection(ctx)
	case *antigravity.Provider:
		err = prov.TestConnection(ctx)
	case *codex.Provider:
		err = prov.TestConnection(ctx)
	case *copilot.Provider:
		err = prov.TestConnection(ctx)
	case *cursor.Provider:
		valid, msg, _, terr := prov.TestAccount(ctx, "")
		if !valid || terr != nil {
			err = fmt.Errorf("%s", msg)
		}
	case *openai.Provider:
		err = prov.TestConnection(ctx)
	default:
		_, err = p.Models(ctx)
	}

	latency := time.Since(start).Milliseconds()
	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":     "error",
			"valid":      false,
			"latency_ms": latency,
			"error":      err.Error(),
		})
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":     "ok",
		"valid":      true,
		"latency_ms": latency,
		"message":    "Connection verified successfully",
	})
}

func (s *Server) handleTestAccount(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	accId := chi.URLParam(r, "accId")
	if id == "" || accId == "" {
		http.Error(w, "provider id and account id required", http.StatusBadRequest)
		return
	}

	p, ok := s.router.GetProvider(id)
	if !ok {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "error",
			"valid":  false,
			"error":  fmt.Sprintf("provider %q not found in active router", id),
		})
		return
	}

	start := time.Now()
	var err error
	ctx := r.Context()

	switch prov := p.(type) {
	case *kiro.Provider:
		err = prov.TestAccount(ctx, accId)
	case *antigravity.Provider:
		err = prov.TestAccount(ctx, accId)
	case *codex.Provider:
		err = prov.TestAccount(ctx, accId)
	case *copilot.Provider:
		err = prov.TestAccount(ctx, accId)
	case *cursor.Provider:
		valid, msg, _, terr := prov.TestAccount(ctx, accId)
		if !valid || terr != nil {
			err = fmt.Errorf("%s", msg)
		}
	default:
		err = fmt.Errorf("account-level testing not supported for %s", p.Type())
	}

	latency := time.Since(start).Milliseconds()
	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":     "error",
			"valid":      false,
			"latency_ms": latency,
			"error":      err.Error(),
		})
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":     "ok",
		"valid":      true,
		"latency_ms": latency,
		"message":    fmt.Sprintf("Account %s verified successfully", accId),
	})
}

func (s *Server) handleProviderModels(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	prov, ok := s.router.GetProvider(id)
	if !ok {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]interface{}{"status": "error", "error": "provider not found", "models": []providers.ModelInfo{}})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	models, err := prov.Models(ctx)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"status": "error", "error": err.Error(), "models": []providers.ModelInfo{}})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"status": "ok", "models": models})
}

func (s *Server) handleTestModel(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req struct {
		Model string `json:"model"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	if req.Model == "" {
		req.Model = "gemini-3.1-pro"
	}
	prov, ok := s.router.GetProvider(id)
	if !ok {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]interface{}{"status": "error", "error": "provider not found"})
		return
	}
	start := time.Now()
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	chatReq := &providers.ChatRequest{
		Model: req.Model,
		Messages: []providers.ChatMessage{
			{Role: "user", Content: "Say OK"},
		},
	}
	resp, _, code, err := prov.ChatCompletion(ctx, chatReq)
	duration := time.Since(start).Milliseconds()
	if err != nil || code >= 400 {
		errMsg := "error"
		if err != nil {
			errMsg = err.Error()
		} else {
			errMsg = fmt.Sprintf("HTTP %d", code)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":     "error",
			"error":      errMsg,
			"latency_ms": duration,
		})
		return
	}
	if resp != nil {
		resp.Close()
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":     "ok",
		"latency_ms": duration,
		"model":      req.Model,
	})
}

func (s *Server) handlePlaygroundChat(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ProviderID string `json:"provider_id"`
		Model      string `json:"model"`
		Prompt     string `json:"prompt"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if req.Prompt == "" {
		http.Error(w, "prompt required", http.StatusBadRequest)
		return
	}
	prov, ok := s.router.GetProvider(req.ProviderID)
	if !ok {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]interface{}{"status": "error", "error": "provider not found"})
		return
	}
	start := time.Now()
	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()
	chatReq := &providers.ChatRequest{
		Model: req.Model,
		Messages: []providers.ChatMessage{
			{Role: "user", Content: req.Prompt},
		},
	}
	resp, _, code, err := prov.ChatCompletion(ctx, chatReq)
	duration := time.Since(start).Milliseconds()
	if err != nil || code >= 400 {
		errMsg := "error"
		if err != nil {
			errMsg = err.Error()
		} else {
			errMsg = fmt.Sprintf("HTTP %d", code)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":      "error",
			"error":       errMsg,
			"duration_ms": duration,
		})
		return
	}
	defer resp.Close()
	bodyBytes, _ := io.ReadAll(resp)
	var parsed struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			TotalTokens int `json:"total_tokens"`
		} `json:"usage"`
	}
	json.Unmarshal(bodyBytes, &parsed)
	content := ""
	if len(parsed.Choices) > 0 {
		content = parsed.Choices[0].Message.Content
	} else {
		content = string(bodyBytes)
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":      "ok",
		"content":     content,
		"duration_ms": duration,
		"tokens":      parsed.Usage.TotalTokens,
	})
}

func (s *Server) handleExposeClaude(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req struct {
		Model string `json:"model"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	if req.Model == "" {
		req.Model = "gemini-3.1-pro"
	}
	home, _ := os.UserHomeDir()
	cfgPath := s.cfgPath
	if cfgPath == "" {
		cfgPath = filepath.Join(home, ".goroute", "config.yaml")
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		cfg = config.DefaultConfig()
	}
	comboName := req.Model
	exists := false
	for _, c := range cfg.Combos {
		if c.Name == comboName {
			exists = true
			break
		}
	}
	if !exists {
		cfg.Combos = append(cfg.Combos, config.ComboConfig{
			Name:     comboName,
			Strategy: "failover",
			Targets: []config.ComboTarget{
				{ProviderID: id, Model: req.Model},
				{ProviderID: "antigravity", Model: "gemini-3.8-flash-high"},
			},
		})
		cfg.Save(cfgPath)
		s.Reload()
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "ok",
		"message": fmt.Sprintf("Exposed %s in Claude Code catalog", comboName),
	})
}

func (s *Server) handleReload(w http.ResponseWriter, r *http.Request) {
	if err := s.Reload(); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "error",
			"error":  err.Error(),
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "ok",
		"message": "configuration reloaded successfully",
	})
}

func (s *Server) handleModelCooldowns(w http.ResponseWriter, r *http.Request) {
	cooldowns := resilience.GlobalCooldown.List()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":    "ok",
		"count":     len(cooldowns),
		"cooldowns": cooldowns,
	})
}

func (s *Server) handleResilienceReset(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Key string `json:"key"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	resilience.GlobalCooldown.Reset(req.Key)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "ok",
		"message": "cooldowns reset successfully",
	})
}

// Reload reads the configuration from disk and re-registers all providers and combos dynamically
func (s *Server) Reload() error {
	if s.cfgPath == "" {
		home, _ := os.UserHomeDir()
		s.cfgPath = filepath.Join(home, ".goroute", "config.yaml")
	}
	if s.storePath == "" {
		home, _ := os.UserHomeDir()
		s.storePath = filepath.Join(home, ".goroute", "vault.json")
	}

	cfg, err := config.Load(s.cfgPath)
	if err != nil {
		return fmt.Errorf("failed to reload config from %s: %w", s.cfgPath, err)
	}

	st, _ := store.NewStore(s.storePath, "goroute-default-key")

	s.router.Clear()

	for _, pCfg := range cfg.Providers {
		switch pCfg.Type {
		case "openai":
			var keys []string
			for _, a := range pCfg.Accounts {
				if a.APIKey != "" {
					keys = append(keys, a.APIKey)
					poolID := a.Pool
					if poolID == "" {
						poolID = "pool1"
					}
					weight := a.Weight
					if weight <= 0 {
						weight = 1.0
					}
					pool.GlobalPool.RegisterAccount(poolID, pCfg.ID, a.ID, weight)
				}
			}
			if pCfg.APIKey != "" {
				keys = append(keys, pCfg.APIKey)
				pool.GlobalPool.RegisterAccount("pool1", pCfg.ID, pCfg.ID+"-primary", 1.0)
			}
			prov := openai.NewProviderWithKeys(pCfg.ID, pCfg.BaseURL, keys, pCfg.Headers)
			s.router.RegisterProvider(prov)
		case "antigravity":
			var accounts []*antigravity.Account
			for _, a := range pCfg.Accounts {
				refToken := a.RefreshToken
				if refToken == "" && st != nil {
					sec, _ := st.GetSecret(a.ID)
					refToken = sec
				}
				poolID := a.Pool
				if poolID == "" {
					poolID = "pool1"
				}
				accounts = append(accounts, &antigravity.Account{
					ID:           a.ID,
					Pool:         poolID,
					Email:        a.Email,
					RefreshToken: refToken,
					AccessToken:  a.AccessToken,
					ExpiresAt:    a.ExpiresAt,
					ProjectID:    a.ProjectID,
				})
				weight := a.Weight
				if weight <= 0 {
					weight = 1.0
				}
				pool.GlobalPool.RegisterAccount(poolID, pCfg.ID, a.ID, weight)
			}
			prov := antigravity.NewProvider(pCfg.ID, accounts)
			s.router.RegisterProvider(prov)
		case "kiro":
			var accounts []*kiro.Account
			for _, a := range pCfg.Accounts {
				refToken := a.RefreshToken
				if refToken == "" && st != nil {
					sec, _ := st.GetSecret(a.ID)
					refToken = sec
				}
				poolID := a.Pool
				if poolID == "" {
					poolID = "pool1"
				}
				accounts = append(accounts, &kiro.Account{
					ID:           a.ID,
					Pool:         poolID,
					RefreshToken: refToken,
					ProfileARN:   a.ProfileARN,
				})
				weight := a.Weight
				if weight <= 0 {
					weight = 1.0
				}
				pool.GlobalPool.RegisterAccount(poolID, pCfg.ID, a.ID, weight)
			}
			if len(accounts) == 0 && pCfg.RefreshToken != "" {
				accounts = append(accounts, &kiro.Account{
					ID:           pCfg.ID,
					Pool:         "pool1",
					RefreshToken: pCfg.RefreshToken,
					ProfileARN:   pCfg.ProfileARN,
				})
				pool.GlobalPool.RegisterAccount("pool1", pCfg.ID, pCfg.ID, 1.0)
			}
			prov := kiro.NewProviderWithAccounts(pCfg.ID, accounts)
			s.router.RegisterProvider(prov)
		case "codex":
			var accounts []*codex.Account
			for _, a := range pCfg.Accounts {
				refToken := a.RefreshToken
				if refToken == "" && st != nil {
					sec, _ := st.GetSecret(a.ID)
					refToken = sec
				}
				poolID := a.Pool
				if poolID == "" {
					poolID = "pool1"
				}
				accounts = append(accounts, &codex.Account{
					ID:           a.ID,
					Pool:         poolID,
					RefreshToken: refToken,
					AccessToken:  a.AccessToken,
					AccountID:    a.AccountID,
					ExpiresAt:    a.ExpiresAt,
				})
				weight := a.Weight
				if weight <= 0 {
					weight = 1.0
				}
				pool.GlobalPool.RegisterAccount(poolID, pCfg.ID, a.ID, weight)
			}
			if len(accounts) == 0 && pCfg.RefreshToken != "" {
				accounts = append(accounts, &codex.Account{
					ID:           pCfg.ID,
					Pool:         "pool1",
					RefreshToken: pCfg.RefreshToken,
				})
				pool.GlobalPool.RegisterAccount("pool1", pCfg.ID, pCfg.ID, 1.0)
			}
			prov := codex.NewProvider(pCfg.ID, accounts)
			s.router.RegisterProvider(prov)
		case "copilot":
			var accounts []*copilot.Account
			for _, a := range pCfg.Accounts {
				token := a.APIKey
				if token == "" {
					token = a.AccessToken
				}
				poolID := a.Pool
				if poolID == "" {
					poolID = "pool1"
				}
				accounts = append(accounts, &copilot.Account{
					ID:          a.ID,
					Pool:        poolID,
					GitHubToken: token,
					User:        a.Email,
				})
				weight := a.Weight
				if weight <= 0 {
					weight = 1.0
				}
				pool.GlobalPool.RegisterAccount(poolID, pCfg.ID, a.ID, weight)
			}
			if len(accounts) == 0 && pCfg.APIKey != "" {
				accounts = append(accounts, &copilot.Account{
					ID:          pCfg.ID,
					Pool:        "pool1",
					GitHubToken: pCfg.APIKey,
				})
				pool.GlobalPool.RegisterAccount("pool1", pCfg.ID, pCfg.ID, 1.0)
			}
			prov := copilot.NewProvider(pCfg.ID, accounts)
			s.router.RegisterProvider(prov)
		case "cursor":
			var accounts []*cursor.Account
			for _, a := range pCfg.Accounts {
				poolID := a.Pool
				if poolID == "" {
					poolID = "pool1"
				}
				accounts = append(accounts, &cursor.Account{
					ID:           a.ID,
					Pool:         poolID,
					Email:        a.Email,
					AccessToken:  a.AccessToken,
					RefreshToken: a.RefreshToken,
					ExpiresAt:    a.ExpiresAt,
				})
				weight := a.Weight
				if weight <= 0 {
					weight = 1.0
				}
				pool.GlobalPool.RegisterAccount(poolID, pCfg.ID, a.ID, weight)
			}
			if len(accounts) == 0 && (pCfg.APIKey != "" || pCfg.RefreshToken != "") {
				accounts = append(accounts, &cursor.Account{
					ID:           pCfg.ID,
					Pool:         "pool1",
					AccessToken:  pCfg.APIKey,
					RefreshToken: pCfg.RefreshToken,
				})
				pool.GlobalPool.RegisterAccount("pool1", pCfg.ID, pCfg.ID, 1.0)
			}
			prov := cursor.NewProvider(pCfg.ID, accounts)
			s.router.RegisterProvider(prov)
		}
	}

	for _, combo := range cfg.Combos {
		var targets []router.Target
		for _, t := range combo.Targets {
			targets = append(targets, router.Target{
				ProviderID:   t.ProviderID,
				Model:        t.Model,
				QuotaPercent: t.QuotaPercent,
			})
		}
		s.router.RegisterCombo(combo.Name, combo.Strategy, targets)
	}

	log.Printf("[GoRoute] Configuration reloaded successfully (%d providers, %d combos)", len(cfg.Providers), len(cfg.Combos))
	return nil
}

func (s *Server) handleUsageQuota(w http.ResponseWriter, r *http.Request) {
	providerFilter := r.URL.Query().Get("provider")
	connectionIdFilter := r.URL.Query().Get("connectionId")

	if s.cfgPath == "" {
		home, _ := os.UserHomeDir()
		s.cfgPath = filepath.Join(home, ".goroute", "config.yaml")
	}
	cfg, _ := config.Load(s.cfgPath)
	if cfg == nil {
		cfg = &config.Config{}
	}

	type QuotaDetails struct {
		Used             int     `json:"used"`
		Total            int     `json:"total"`
		Remaining        int     `json:"remaining"`
		PercentRemaining float64 `json:"percentRemaining"`
		ResetAt          *string `json:"resetAt"`
		Unlimited        bool    `json:"unlimited"`
	}

	type QuotaProviderEntry struct {
		Provider     string       `json:"provider"`
		ConnectionID string       `json:"connectionId"`
		Name         string       `json:"name"`
		Status       string       `json:"status"`
		TokenStatus  string       `json:"tokenStatus"`
		Quota        QuotaDetails `json:"quota"`
	}

	var results []QuotaProviderEntry
	for _, p := range cfg.Providers {
		if providerFilter != "" && p.ID != providerFilter {
			continue
		}
		if len(p.Accounts) > 0 {
			for _, a := range p.Accounts {
				if connectionIdFilter != "" && a.ID != connectionIdFilter {
					continue
				}
				name := a.Email
				if name == "" {
					name = a.ID
				}
				tokenStatus := "valid"
				if a.Status != "" && a.Status != "active" {
					tokenStatus = a.Status
				}
				results = append(results, QuotaProviderEntry{
					Provider:     p.ID,
					ConnectionID: a.ID,
					Name:         name,
					Status:       "active",
					TokenStatus:  tokenStatus,
					Quota: QuotaDetails{
						Used:             0,
						Total:            100,
						Remaining:        100,
						PercentRemaining: 100.0,
						Unlimited:        p.Type == "openai",
					},
				})
			}
		} else {
			if connectionIdFilter != "" && p.ID != connectionIdFilter {
				continue
			}
			results = append(results, QuotaProviderEntry{
				Provider:     p.ID,
				ConnectionID: p.ID,
				Name:         p.ID,
				Status:       "active",
				TokenStatus:  "valid",
				Quota: QuotaDetails{
					Used:             0,
					Total:            100,
					Remaining:        100,
					PercentRemaining: 100.0,
					Unlimited:        true,
				},
			})
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"providers": results,
		"meta": map[string]interface{}{
			"generatedAt": time.Now().UTC().Format(time.RFC3339),
			"filters": map[string]interface{}{
				"provider":     providerFilter,
				"connectionId": connectionIdFilter,
			},
		},
	})
}

func (s *Server) handleUsageAnalytics(w http.ResponseWriter, r *http.Request) {
	logs := GetRecentLogs()
	totalReqs := len(logs)
	totalTokens := 0
	totalDuration := int64(0)
	errorsCount := 0
	byModel := make(map[string]int)
	byProvider := make(map[string]int)
	errorBreakdown := make(map[string]int)

	for _, l := range logs {
		tok := l.Tokens
		if tok == 0 {
			tok = 60
		}
		totalTokens += tok
		totalDuration += l.DurationMs
		if l.StatusCode >= 400 || l.Error != "" {
			errorsCount++
			errKey := fmt.Sprintf("%d", l.StatusCode)
			if l.Error != "" {
				errKey = l.Error
			}
			errorBreakdown[errKey]++
		}
		if l.Model != "" {
			byModel[l.Model]++
		}
		if l.Provider != "" {
			byProvider[l.Provider]++
		}
	}

	avgLatency := float64(0)
	if totalReqs > 0 {
		avgLatency = float64(totalDuration) / float64(totalReqs)
	}

	summary := map[string]interface{}{
		"totalRequests":   totalReqs,
		"totalTokens":      totalTokens,
		"promptTokens":     int(float64(totalTokens) * 0.7),
		"completionTokens": int(float64(totalTokens) * 0.3),
		"totalCost":        0.0,
		"cacheHitRate":     0.0,
		"avgLatency":       avgLatency,
		"errorRate":        float64(errorsCount) / float64(max(totalReqs, 1)),
	}

	type NamedCount struct {
		Name  string `json:"name"`
		Count int    `json:"count"`
	}
	var byModelList []NamedCount
	for k, v := range byModel {
		byModelList = append(byModelList, NamedCount{Name: k, Count: v})
	}
	var byProvList []NamedCount
	for k, v := range byProvider {
		byProvList = append(byProvList, NamedCount{Name: k, Count: v})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"summary":        summary,
		"byModel":        byModelList,
		"byProvider":     byProvList,
		"errorBreakdown": errorBreakdown,
		"generatedAt":    time.Now().UTC().Format(time.RFC3339),
	})
}

func (s *Server) handleImportCursor(w http.ResponseWriter, r *http.Request) {
	acc, err := cursor.AutoImportCursor()
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "error",
			"error":  err.Error(),
		})
		return
	}

	cfgPath := s.cfgPath
	if cfgPath == "" {
		home, _ := os.UserHomeDir()
		cfgPath = filepath.Join(home, ".goroute", "config.yaml")
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		cfg = config.DefaultConfig()
	}

	found := false
	for i, p := range cfg.Providers {
		if p.ID == "cursor" {
			found = true
			accFound := false
			for j, a := range p.Accounts {
				if a.ID == acc.ID || a.Email == acc.Email {
					cfg.Providers[i].Accounts[j].AccessToken = acc.AccessToken
					cfg.Providers[i].Accounts[j].RefreshToken = acc.RefreshToken
					cfg.Providers[i].Accounts[j].ExpiresAt = acc.ExpiresAt
					accFound = true
					break
				}
			}
			if !accFound {
				cfg.Providers[i].Accounts = append(cfg.Providers[i].Accounts, config.AccountConfig{
					ID:           acc.ID,
					Email:        acc.Email,
					AccessToken:  acc.AccessToken,
					RefreshToken: acc.RefreshToken,
					ExpiresAt:    acc.ExpiresAt,
				})
			}
			break
		}
	}

	if !found {
		cfg.Providers = append(cfg.Providers, config.ProviderConfig{
			ID:   "cursor",
			Type: "cursor",
			Accounts: []config.AccountConfig{
				{
					ID:           acc.ID,
					Email:        acc.Email,
					AccessToken:  acc.AccessToken,
					RefreshToken: acc.RefreshToken,
					ExpiresAt:    acc.ExpiresAt,
				},
			},
		})
	}

	if err := cfg.Save(cfgPath); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	_ = s.Reload()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "ok",
		"message": "Successfully imported Cursor credentials from ~/.config/cursor/auth.json",
	})
}

func (s *Server) handleListKeys(w http.ResponseWriter, r *http.Request) {
	if s.keyMgr == nil {
		http.Error(w, "Key manager uninitialized", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "ok",
		"keys":   s.keyMgr.ListKeys(),
	})
}

func (s *Server) handleCreateKey(w http.ResponseWriter, r *http.Request) {
	if s.keyMgr == nil {
		http.Error(w, "Key manager uninitialized", http.StatusInternalServerError)
		return
	}
	var req struct {
		Name          string   `json:"name"`
		AllowedModels []string `json:"allowed_models"`
		MaxRPM        int      `json:"max_rpm"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if req.Name == "" {
		req.Name = "Default Key"
	}
	key, err := s.keyMgr.CreateKey(req.Name, req.AllowedModels, req.MaxRPM)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "ok",
		"key":    key,
	})
}

func (s *Server) handleRevokeKey(w http.ResponseWriter, r *http.Request) {
	if s.keyMgr == nil {
		http.Error(w, "Key manager uninitialized", http.StatusInternalServerError)
		return
	}
	id := chi.URLParam(r, "id")
	if err := s.keyMgr.RevokeKey(id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "ok",
		"message": "Key revoked successfully",
	})
}

func (s *Server) handleCompressPrompt(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Text      string `json:"text"`
		Input     string `json:"input"`
		Intensity string `json:"intensity"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	text := req.Text
	if text == "" {
		text = req.Input
	}
	intensity := req.Intensity
	if intensity == "" {
		intensity = "full"
	}
	compressed, stats := compression.CompressWithOptions(text, intensity)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":     "ok",
		"compressed": compressed,
		"stats":      stats,
	})
}

func (s *Server) handleRouterStats(w http.ResponseWriter, r *http.Request) {
	metrics := s.router.GetAllMetrics()
	combos := s.router.GetCombos()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"metrics": metrics,
		"combos":  combos,
	})
}

func (s *Server) handleSetRouterQuota(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		ProviderID   string  `json:"provider_id"`
		Model        string  `json:"model"`
		QuotaPercent float64 `json:"quota_percent"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if payload.ProviderID == "" || payload.Model == "" {
		http.Error(w, "provider_id and model are required", http.StatusBadRequest)
		return
	}
	s.router.SetTargetQuota(payload.ProviderID, payload.Model, payload.QuotaPercent)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":        "ok",
		"provider_id":   payload.ProviderID,
		"model":         payload.Model,
		"quota_percent": payload.QuotaPercent,
	})
}

func consolidateSSEStream(body io.Reader, model string) ([]byte, error) {
	scanner := bufio.NewScanner(body)
	var fullContent strings.Builder
	var toolCalls []map[string]interface{}
	finishReason := "stop"
	respID := fmt.Sprintf("chatcmpl-goroute-%d", time.Now().UnixNano())

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || line == "data: [DONE]" {
			continue
		}
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		payload := strings.TrimPrefix(line, "data: ")
		var chunk struct {
			ID      string `json:"id"`
			Model   string `json:"model"`
			Choices []struct {
				Delta struct {
					Content   string        `json:"content"`
					ToolCalls []interface{} `json:"tool_calls"`
				} `json:"delta"`
				FinishReason *string `json:"finish_reason"`
			} `json:"choices"`
		}
		if err := json.Unmarshal([]byte(payload), &chunk); err == nil {
			if chunk.ID != "" {
				respID = chunk.ID
			}
			if len(chunk.Choices) > 0 {
				c := chunk.Choices[0]
				if c.Delta.Content != "" {
					fullContent.WriteString(c.Delta.Content)
				}
				for _, tc := range c.Delta.ToolCalls {
					if tcMap, ok := tc.(map[string]interface{}); ok {
						toolCalls = append(toolCalls, tcMap)
					}
				}
				if c.FinishReason != nil && *c.FinishReason != "" {
					finishReason = *c.FinishReason
				}
			}
		}
	}

	contentStr := fullContent.String()
	choiceMsg := map[string]interface{}{
		"role":    "assistant",
		"content": contentStr,
	}
	if len(toolCalls) > 0 {
		choiceMsg["tool_calls"] = toolCalls
		finishReason = "tool_calls"
	}

	res := map[string]interface{}{
		"id":      respID,
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   model,
		"choices": []map[string]interface{}{
			{
				"index":         0,
				"message":       choiceMsg,
				"finish_reason": finishReason,
			},
		},
		"usage": map[string]interface{}{
			"prompt_tokens":     10,
			"completion_tokens": len(contentStr) / 4,
			"total_tokens":      10 + (len(contentStr) / 4),
		},
	}
	return json.Marshal(res)
}

func (s *Server) handleListCliAgents(w http.ResponseWriter, r *http.Request) {
	tools := clitools.ListTools()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "ok",
		"count":  len(tools),
		"tools":  tools,
	})
}

func (s *Server) handleConfigureCliAgent(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		http.Error(w, "agent id is required", http.StatusBadRequest)
		return
	}
	msg, err := clitools.ConfigureTool(id)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "error",
			"error":  err.Error(),
		})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "ok",
		"message": msg,
	})
}

func (s *Server) handleTopology(w http.ResponseWriter, r *http.Request) {
	home, _ := os.UserHomeDir()
	cfgPath := s.cfgPath
	if cfgPath == "" {
		cfgPath = filepath.Join(home, ".goroute", "config.yaml")
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		cfg = config.DefaultConfig()
	}

	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	memMB := fmt.Sprintf("%.1f MB", float64(m.Alloc)/(1024*1024))

	type Node struct {
		ID       string                 `json:"id"`
		Label    string                 `json:"label"`
		Type     string                 `json:"type"`   // "gateway", "provider", "combo"
		Status   string                 `json:"status"` // "active", "cooldown", "warning"
		Details  map[string]interface{} `json:"details,omitempty"`
	}
	type Link struct {
		Source string `json:"source"`
		Target string `json:"target"`
		Status string `json:"status"`
	}

	nodes := []Node{
		{
			ID:     "goroute",
			Label:  "GoRoute Gateway",
			Type:   "gateway",
			Status: "active",
			Details: map[string]interface{}{
				"port":   8128,
				"memory": memMB,
				"uptime": time.Since(s.startTime).Round(time.Second).String(),
			},
		},
	}
	var links []Link

	cooldowns := resilience.GlobalCooldown.List()
	cooldownMap := make(map[string]bool)
	for _, cd := range cooldowns {
		cooldownMap[cd.ProviderID] = true
	}

	for _, p := range cfg.Providers {
		status := "active"
		if cooldownMap[p.ID] {
			status = "cooldown"
		}
		nodes = append(nodes, Node{
			ID:     p.ID,
			Label:  p.ID,
			Type:   "provider",
			Status: status,
			Details: map[string]interface{}{
				"type":     p.Type,
				"accounts": len(p.Accounts),
			},
		})
		links = append(links, Link{
			Source: "goroute",
			Target: p.ID,
			Status: status,
		})
	}

	for _, c := range cfg.Combos {
		nodes = append(nodes, Node{
			ID:     "combo-" + c.Name,
			Label:  c.Name,
			Type:   "combo",
			Status: "active",
			Details: map[string]interface{}{
				"strategy": c.Strategy,
				"targets":  len(c.Targets),
			},
		})
		links = append(links, Link{
			Source: "goroute",
			Target: "combo-" + c.Name,
			Status: "active",
		})
		for _, t := range c.Targets {
			links = append(links, Link{
				Source: "combo-" + c.Name,
				Target: t.ProviderID,
				Status: "active",
			})
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "ok",
		"nodes":  nodes,
		"links":  links,
	})
}

func (s *Server) handleListCombos(w http.ResponseWriter, r *http.Request) {
	home, _ := os.UserHomeDir()
	cfgPath := s.cfgPath
	if cfgPath == "" {
		cfgPath = filepath.Join(home, ".goroute", "config.yaml")
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		cfg = config.DefaultConfig()
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "ok",
		"combos": cfg.Combos,
	})
}

func (s *Server) handleSaveCombo(w http.ResponseWriter, r *http.Request) {
	var combo config.ComboConfig
	if err := json.NewDecoder(r.Body).Decode(&combo); err != nil {
		http.Error(w, "invalid combo payload: "+err.Error(), http.StatusBadRequest)
		return
	}
	if combo.Name == "" || len(combo.Targets) == 0 {
		http.Error(w, "combo name and at least one target required", http.StatusBadRequest)
		return
	}
	if combo.Strategy == "" {
		combo.Strategy = "failover"
	}

	home, _ := os.UserHomeDir()
	cfgPath := s.cfgPath
	if cfgPath == "" {
		cfgPath = filepath.Join(home, ".goroute", "config.yaml")
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		cfg = config.DefaultConfig()
	}

	found := false
	for i, c := range cfg.Combos {
		if c.Name == combo.Name {
			cfg.Combos[i] = combo
			found = true
			break
		}
	}
	if !found {
		cfg.Combos = append(cfg.Combos, combo)
	}

	if err := cfg.Save(cfgPath); err != nil {
		http.Error(w, "save config failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	s.Reload()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "ok",
		"combo":  combo,
	})
}

func (s *Server) handleDeleteCombo(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if name == "" {
		http.Error(w, "combo name required", http.StatusBadRequest)
		return
	}

	home, _ := os.UserHomeDir()
	cfgPath := s.cfgPath
	if cfgPath == "" {
		cfgPath = filepath.Join(home, ".goroute", "config.yaml")
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		cfg = config.DefaultConfig()
	}

	var newCombos []config.ComboConfig
	for _, c := range cfg.Combos {
		if c.Name != name {
			newCombos = append(newCombos, c)
		}
	}
	cfg.Combos = newCombos

	if err := cfg.Save(cfgPath); err != nil {
		http.Error(w, "save config failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	s.Reload()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "ok",
		"deleted": name,
	})
}

func (s *Server) handleListRTKFilters(w http.ResponseWriter, r *http.Request) {
	filters := rtk.GlobalEngine.ListFilters()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "ok",
		"count":   len(filters),
		"filters": filters,
	})
}

func (s *Server) handleTestRTKFilter(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		Command string `json:"command"`
		Output  string `json:"output"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	res := rtk.GlobalEngine.FilterCommandOutput(payload.Command, payload.Output)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "ok",
		"result": res,
	})
}

func (s *Server) handleListAffinitySessions(w http.ResponseWriter, r *http.Request) {
	sessions := affinity.GlobalManager.ListSessions()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":   "ok",
		"count":    len(sessions),
		"sessions": sessions,
	})
}

func (s *Server) handleListAcpSessions(w http.ResponseWriter, r *http.Request) {
	sessions := acp.GlobalManager.ListSessions()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":   "ok",
		"count":    len(sessions),
		"sessions": sessions,
	})
}

func (s *Server) handleSpawnAcpSession(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		AgentID string            `json:"agent_id"`
		Binary  string            `json:"binary"`
		Args    []string          `json:"args"`
		Env     map[string]string `json:"env"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if payload.AgentID == "" || payload.Binary == "" {
		http.Error(w, "agent_id and binary are required", http.StatusBadRequest)
		return
	}

	session, err := acp.GlobalManager.Spawn(payload.AgentID, payload.Binary, payload.Args, payload.Env)
	if err != nil {
		http.Error(w, "spawn failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "ok",
		"session": session,
	})
}

func (s *Server) handleKillAcpSession(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		http.Error(w, "session id required", http.StatusBadRequest)
		return
	}
	if err := acp.GlobalManager.KillSession(id); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "ok",
		"killed": id,
	})
}

func (s *Server) handleListTokensHealth(w http.ResponseWriter, r *http.Request) {
	healthList := tokens.GlobalScheduler.ListHealth()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "ok",
		"count":  len(healthList),
		"health": healthList,
	})
}

func (s *Server) handleListPoolUsage(w http.ResponseWriter, r *http.Request) {
	pools := pool.GlobalPool.ListAllPools()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "ok",
		"pools":  pools,
	})
}







