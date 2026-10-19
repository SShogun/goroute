package proxy

import (
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
	"github.com/ziuus/goroute/pkg/cache"
	"github.com/ziuus/goroute/pkg/catalog"
	"github.com/ziuus/goroute/pkg/compression"
	"github.com/ziuus/goroute/pkg/config"
	"github.com/ziuus/goroute/pkg/keys"
	"github.com/ziuus/goroute/pkg/mcp"
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
	"github.com/ziuus/goroute/pkg/store"
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
		mux:             chi.NewRouter(),
	}

	s.setupRoutes()
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

	// Built-in MCP Server
	s.mux.Get("/sse", s.mcpServer.HandleSSE)
	s.mux.Post("/message", s.mcpServer.HandleMessage)
	s.mux.Get("/mcp/tools", s.mcpServer.HandleToolsList)
	s.mux.Post("/mcp/tools", s.mcpServer.HandleToolCall)

	s.tunnelMgr.RegisterRoutes(s.mux)
	s.mux.Get("/v1/models", s.handleModels)
	s.mux.Post("/v1/chat/completions", s.handleChatCompletions)
	s.mux.Post("/v1/messages", s.handleMessages)
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

	body, isStream, statusCode, err := s.router.Execute(r.Context(), &chatReq)
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

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":    "active",
		"engine":    "goroute",
		"port":      8128,
		"memory_mb": float64(m.Alloc) / 1024 / 1024,
		"providers": provSummaries,
		"combos":    cfg.Combos,
		"models":    models,
		"agents":    agentMap,
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
				}
			}
			if pCfg.APIKey != "" {
				keys = append(keys, pCfg.APIKey)
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
				accounts = append(accounts, &antigravity.Account{
					ID:           a.ID,
					Email:        a.Email,
					RefreshToken: refToken,
					AccessToken:  a.AccessToken,
					ExpiresAt:    a.ExpiresAt,
					ProjectID:    a.ProjectID,
				})
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
				accounts = append(accounts, &kiro.Account{
					ID:           a.ID,
					RefreshToken: refToken,
					ProfileARN:   a.ProfileARN,
				})
			}
			if len(accounts) == 0 && pCfg.RefreshToken != "" {
				accounts = append(accounts, &kiro.Account{
					ID:           pCfg.ID,
					RefreshToken: pCfg.RefreshToken,
					ProfileARN:   pCfg.ProfileARN,
				})
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
				accounts = append(accounts, &codex.Account{
					ID:           a.ID,
					RefreshToken: refToken,
					AccessToken:  a.AccessToken,
					AccountID:    a.AccountID,
					ExpiresAt:    a.ExpiresAt,
				})
			}
			if len(accounts) == 0 && pCfg.RefreshToken != "" {
				accounts = append(accounts, &codex.Account{
					ID:           pCfg.ID,
					RefreshToken: pCfg.RefreshToken,
				})
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
				accounts = append(accounts, &copilot.Account{
					ID:          a.ID,
					GitHubToken: token,
					User:        a.Email,
				})
			}
			if len(accounts) == 0 && pCfg.APIKey != "" {
				accounts = append(accounts, &copilot.Account{
					ID:          pCfg.ID,
					GitHubToken: pCfg.APIKey,
				})
			}
			prov := copilot.NewProvider(pCfg.ID, accounts)
			s.router.RegisterProvider(prov)
		case "cursor":
			var accounts []*cursor.Account
			for _, a := range pCfg.Accounts {
				accounts = append(accounts, &cursor.Account{
					ID:           a.ID,
					Email:        a.Email,
					AccessToken:  a.AccessToken,
					RefreshToken: a.RefreshToken,
					ExpiresAt:    a.ExpiresAt,
				})
			}
			if len(accounts) == 0 && (pCfg.APIKey != "" || pCfg.RefreshToken != "") {
				accounts = append(accounts, &cursor.Account{
					ID:           pCfg.ID,
					AccessToken:  pCfg.APIKey,
					RefreshToken: pCfg.RefreshToken,
				})
			}
			prov := cursor.NewProvider(pCfg.ID, accounts)
			s.router.RegisterProvider(prov)
		}
	}

	for _, combo := range cfg.Combos {
		var targets []router.Target
		for _, t := range combo.Targets {
			targets = append(targets, router.Target{
				ProviderID: t.ProviderID,
				Model:      t.Model,
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
		Text string `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	compressed, stats := compression.CompressText(req.Text)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":     "ok",
		"compressed": compressed,
		"stats":      stats,
	})
}


