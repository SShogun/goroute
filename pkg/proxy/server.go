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
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/ziuus/goroute/pkg/cache"
	"github.com/ziuus/goroute/pkg/config"
	"github.com/ziuus/goroute/pkg/providers"
	"github.com/ziuus/goroute/pkg/providers/antigravity"
	"github.com/ziuus/goroute/pkg/providers/kiro"
	"github.com/ziuus/goroute/pkg/providers/openai"
	"github.com/ziuus/goroute/pkg/quota"
	"github.com/ziuus/goroute/pkg/router"
	"github.com/ziuus/goroute/pkg/store"
	"github.com/ziuus/goroute/web"
)

type Server struct {
	router          *router.Router
	limiter         *quota.Limiter
	cache           *cache.Cache
	enableDashboard bool
	mux             *chi.Mux
	cfgPath         string
	storePath       string
}

func (s *Server) SetConfigPaths(cfgPath, storePath string) {
	s.cfgPath = cfgPath
	s.storePath = storePath
}

func NewServer(r *router.Router, limiter *quota.Limiter, enableDashboard bool) *Server {
	s := &Server{
		router:          r,
		limiter:         limiter,
		cache:           cache.NewCache(15*time.Minute, 2000),
		enableDashboard: enableDashboard,
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
	s.mux.Get("/api/logs", s.handleLogs)
	s.mux.Get("/api/logs/stream", s.handleLogsStream)
	s.mux.Post("/api/reload", s.handleReload)
	s.mux.Post("/api/agents/setup", s.handleAgentSetup)
	s.mux.Post("/api/providers/add", s.handleAddProvider)
	s.mux.Get("/v1/models", s.handleModels)
	s.mux.Post("/v1/chat/completions", s.handleChatCompletions)
	s.mux.Post("/v1/messages", s.handleMessages)
	s.mux.Post("/v1/embeddings", s.handleEmbeddings)
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
		for _, a := range p.Accounts {
			ps.Accounts = append(ps.Accounts, AccountSummary{
				ID:        a.ID,
				Email:     a.Email,
				Status:    "active",
				ProjectID: a.ProjectID,
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
			cfg.Providers[i].BaseURL = req.BaseURL
			cfg.Providers[i].APIKey = req.APIKey
			cfg.Providers[i].Type = req.Type
			if req.RefreshToken != "" {
				cfg.Providers[i].RefreshToken = req.RefreshToken
			}
			if req.ProfileARN != "" {
				cfg.Providers[i].ProfileARN = req.ProfileARN
			}
			found = true
			break
		}
	}
	if !found {
		cfg.Providers = append(cfg.Providers, config.ProviderConfig{
			ID:           req.ID,
			Type:         req.Type,
			BaseURL:      req.BaseURL,
			APIKey:       req.APIKey,
			RefreshToken: req.RefreshToken,
			ProfileARN:   req.ProfileARN,
		})
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
			prov := openai.NewProvider(pCfg.ID, pCfg.BaseURL, pCfg.APIKey, pCfg.Headers)
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
			prov := kiro.NewProvider(pCfg.ID, pCfg.RefreshToken, pCfg.ProfileARN)
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
