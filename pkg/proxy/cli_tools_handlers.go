package proxy

import (
	"encoding/json"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/ziuus/goroute/pkg/clitools"
	"github.com/ziuus/goroute/pkg/storage"
)

func (s *Server) registerCliToolsRoutes() {
	// Status & Detection
	s.mux.Get("/api/cli-tools/all-statuses", s.handleCliToolsAllStatuses)
	s.mux.Get("/api/cli-tools/status", s.handleCliToolsStatus)
	s.mux.Get("/api/cli-tools/detect", s.handleCliToolsDetect)

	// Apply & Config
	s.mux.Post("/api/cli-tools/apply", s.handleCliToolsApply)
	s.mux.Get("/api/cli-tools/config", s.handleCliToolsGetConfig)
	s.mux.Post("/api/cli-tools/config", s.handleCliToolsPostConfig)

	// Backups
	s.mux.Get("/api/cli-tools/backups", s.handleCliToolsListBackups)
	s.mux.Post("/api/cli-tools/backups", s.handleCliToolsRestoreBackup)
	s.mux.Delete("/api/cli-tools/backups", s.handleCliToolsDeleteBackup)

	// Keys & Logs
	s.mux.Get("/api/cli-tools/keys", s.handleCliToolsKeys)
	s.mux.Get("/api/cli-tools/logs", s.handleCliToolsLogs)

	// Specific tool settings
	s.mux.Get("/api/cli-tools/claude-settings", s.handleCliToolsGenericSettings("claude"))
	s.mux.Post("/api/cli-tools/claude-settings", s.handleCliToolsGenericSaveSettings("claude"))

	s.mux.Get("/api/cli-tools/codex-settings", s.handleCliToolsGenericSettings("codex"))
	s.mux.Post("/api/cli-tools/codex-settings", s.handleCliToolsGenericSaveSettings("codex"))
	s.mux.Get("/api/cli-tools/codex-profiles", s.handleCliToolsCodexProfiles)

	s.mux.Get("/api/cli-tools/cline-settings", s.handleCliToolsGenericSettings("cline"))
	s.mux.Post("/api/cli-tools/cline-settings", s.handleCliToolsGenericSaveSettings("cline"))

	s.mux.Get("/api/cli-tools/droid-settings", s.handleCliToolsGenericSettings("droid"))
	s.mux.Post("/api/cli-tools/droid-settings", s.handleCliToolsGenericSaveSettings("droid"))

	s.mux.Get("/api/cli-tools/grok-build-settings", s.handleCliToolsGenericSettings("grok-build"))
	s.mux.Post("/api/cli-tools/grok-build-settings", s.handleCliToolsGenericSaveSettings("grok-build"))

	s.mux.Get("/api/cli-tools/hermes-agent-settings", s.handleCliToolsGenericSettings("hermes"))
	s.mux.Post("/api/cli-tools/hermes-agent-settings", s.handleCliToolsGenericSaveSettings("hermes"))

	s.mux.Get("/api/cli-tools/jcode-settings", s.handleCliToolsGenericSettings("jcode"))
	s.mux.Post("/api/cli-tools/jcode-settings", s.handleCliToolsGenericSaveSettings("jcode"))

	s.mux.Get("/api/cli-tools/kilo-settings", s.handleCliToolsGenericSettings("kilo"))
	s.mux.Post("/api/cli-tools/kilo-settings", s.handleCliToolsGenericSaveSettings("kilo"))

	s.mux.Get("/api/cli-tools/letta-settings", s.handleCliToolsGenericSettings("letta"))
	s.mux.Post("/api/cli-tools/letta-settings", s.handleCliToolsGenericSaveSettings("letta"))

	s.mux.Get("/api/cli-tools/omp-settings", s.handleCliToolsGenericSettings("omp"))
	s.mux.Post("/api/cli-tools/omp-settings", s.handleCliToolsGenericSaveSettings("omp"))

	s.mux.Get("/api/cli-tools/openclaw-settings", s.handleCliToolsGenericSettings("openclaw"))
	s.mux.Post("/api/cli-tools/openclaw-settings", s.handleCliToolsGenericSaveSettings("openclaw"))
	s.mux.Get("/api/cli-tools/openclaw/auto-order", s.handleCliToolsOpenClawAutoOrder)

	s.mux.Get("/api/cli-tools/pi-settings", s.handleCliToolsGenericSettings("pi"))
	s.mux.Post("/api/cli-tools/pi-settings", s.handleCliToolsGenericSaveSettings("pi"))

	s.mux.Get("/api/cli-tools/qwen-settings", s.handleCliToolsGenericSettings("qwen"))
	s.mux.Post("/api/cli-tools/qwen-settings", s.handleCliToolsGenericSaveSettings("qwen"))

	s.mux.Get("/api/cli-tools/smelt-settings", s.handleCliToolsGenericSettings("smelt"))
	s.mux.Post("/api/cli-tools/smelt-settings", s.handleCliToolsGenericSaveSettings("smelt"))

	s.mux.Get("/api/cli-tools/codewhale-settings", s.handleCliToolsGenericSettings("codewhale"))
	s.mux.Post("/api/cli-tools/codewhale-settings", s.handleCliToolsGenericSaveSettings("codewhale"))

	s.mux.Get("/api/cli-tools/crush-settings", s.handleCliToolsGenericSettings("crush"))
	s.mux.Post("/api/cli-tools/crush-settings", s.handleCliToolsGenericSaveSettings("crush"))

	s.mux.Get("/api/cli-tools/deepseek-tui-settings", s.handleCliToolsGenericSettings("deepseek-tui"))
	s.mux.Post("/api/cli-tools/deepseek-tui-settings", s.handleCliToolsGenericSaveSettings("deepseek-tui"))

	s.mux.Get("/api/cli-tools/forge-settings", s.handleCliToolsGenericSettings("forge"))
	s.mux.Post("/api/cli-tools/forge-settings", s.handleCliToolsGenericSaveSettings("forge"))

	s.mux.Get("/api/cli-tools/guide-settings/{toolId}", s.handleCliToolsGuideSettings)
	s.mux.Get("/api/cli-tools/runtime/{toolId}", s.handleCliToolsRuntime)

	// MITM Management
	s.mux.Get("/api/cli-tools/antigravity-mitm", s.handleCliToolsMitmGet)
	s.mux.Post("/api/cli-tools/antigravity-mitm", s.handleCliToolsMitmStart)
	s.mux.Delete("/api/cli-tools/antigravity-mitm", s.handleCliToolsMitmStop)
	s.mux.Get("/api/cli-tools/antigravity-mitm/alias", s.handleCliToolsMitmAliasGet)
	s.mux.Put("/api/cli-tools/antigravity-mitm/alias", s.handleCliToolsMitmAliasPut)
}

func (s *Server) handleCliToolsAllStatuses(w http.ResponseWriter, r *http.Request) {
	statuses, err := clitools.GetAllStatuses()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(statuses)
}

func (s *Server) handleCliToolsStatus(w http.ResponseWriter, r *http.Request) {
	statuses, err := clitools.GetAllStatuses()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	toolId := r.URL.Query().Get("toolId")
	if toolId == "" {
		toolId = r.URL.Query().Get("tool")
	}

	w.Header().Set("Content-Type", "application/json")
	if toolId != "" {
		if st, ok := statuses[toolId]; ok {
			json.NewEncoder(w).Encode(st)
			return
		}
		http.Error(w, "tool not found", http.StatusNotFound)
		return
	}
	json.NewEncoder(w).Encode(statuses)
}

func (s *Server) handleCliToolsDetect(w http.ResponseWriter, r *http.Request) {
	toolId := r.URL.Query().Get("tool")
	if toolId == "" {
		toolId = r.URL.Query().Get("toolId")
	}

	w.Header().Set("Content-Type", "application/json")
	if toolId != "" {
		st, err := clitools.DetectTool(toolId)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		json.NewEncoder(w).Encode(st)
		return
	}

	statuses, err := clitools.GetAllStatuses()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var detected []map[string]interface{}
	for id, st := range statuses {
		if st.Detection.Installed {
			detected = append(detected, map[string]interface{}{
				"id":         id,
				"name":       clitools.Registry[id].Name,
				"installed":  st.Detection.Installed,
				"runnable":   st.Detection.Runnable,
				"version":    st.Detection.Version,
				"configured": st.Config.Status == "configured",
				"endpoint":   st.Config.Endpoint,
			})
		}
	}
	json.NewEncoder(w).Encode(map[string]interface{}{"tools": detected})
}

func (s *Server) handleCliToolsApply(w http.ResponseWriter, r *http.Request) {
	var req clitools.ApplyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON request", http.StatusBadRequest)
		return
	}
	if req.ToolID == "" {
		http.Error(w, "toolId is required", http.StatusBadRequest)
		return
	}

	res, err := clitools.ApplyTool(req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(res)
}

func (s *Server) handleCliToolsGetConfig(w http.ResponseWriter, r *http.Request) {
	toolId := r.URL.Query().Get("toolId")
	if toolId == "" {
		toolId = r.URL.Query().Get("tool")
	}

	w.Header().Set("Content-Type", "application/json")
	if toolId != "" {
		cfg, err := clitools.ReadSettingsFile(toolId)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		json.NewEncoder(w).Encode(cfg)
		return
	}

	// Batch preview
	statuses, _ := clitools.GetAllStatuses()
	configs := make(map[string]interface{})
	for id := range statuses {
		if cfg, err := clitools.ReadSettingsFile(id); err == nil {
			configs[id] = cfg
		}
	}
	json.NewEncoder(w).Encode(map[string]interface{}{"configs": configs})
}

func (s *Server) handleCliToolsPostConfig(w http.ResponseWriter, r *http.Request) {
	var req clitools.ApplyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	req.DryRun = true
	res, err := clitools.ApplyTool(req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(res)
}

func (s *Server) handleCliToolsListBackups(w http.ResponseWriter, r *http.Request) {
	tool := r.URL.Query().Get("tool")
	if tool == "" {
		tool = r.URL.Query().Get("toolId")
	}

	backups, err := clitools.ListBackups(tool)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if tool != "" {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"tool":    tool,
			"backups": backups,
		})
		return
	}

	grouped := make(map[string][]clitools.BackupEntry)
	for _, b := range backups {
		grouped[b.Tool] = append(grouped[b.Tool], b)
	}
	json.NewEncoder(w).Encode(map[string]interface{}{
		"backups": grouped,
	})
}

func (s *Server) handleCliToolsRestoreBackup(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Tool     string `json:"tool"`
		ToolID   string `json:"toolId"`
		BackupID string `json:"backupId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	tool := body.Tool
	if tool == "" {
		tool = body.ToolID
	}
	if tool == "" || body.BackupID == "" {
		http.Error(w, "tool and backupId are required", http.StatusBadRequest)
		return
	}

	if err := clitools.RestoreBackup(tool, body.BackupID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Backup restored for " + tool,
	})
}

func (s *Server) handleCliToolsDeleteBackup(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Tool     string `json:"tool"`
		ToolID   string `json:"toolId"`
		BackupID string `json:"backupId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	tool := body.Tool
	if tool == "" {
		tool = body.ToolID
	}
	if tool == "" || body.BackupID == "" {
		http.Error(w, "tool and backupId are required", http.StatusBadRequest)
		return
	}

	if err := clitools.DeleteBackup(tool, body.BackupID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Backup deleted for " + tool,
	})
}

func (s *Server) handleCliToolsKeys(w http.ResponseWriter, r *http.Request) {
	keys := []map[string]interface{}{
		{
			"id":        "key_goroute_local",
			"name":      "GoRoute Local Master Key",
			"key":       "goroute",
			"createdAt": time.Now().UTC().Format(time.RFC3339),
			"active":    true,
		},
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"keys":  keys,
		"total": len(keys),
	})
}

func (s *Server) handleCliToolsLogs(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 && n <= 500 {
			limit = n
		}
	}

	db := storage.GetDB()
	var logs []storage.CallLog
	if db != nil {
		logs, _ = db.QueryRecentLogs(limit, 0)
	}

	var entries []map[string]interface{}
	for _, l := range logs {
		entries = append(entries, map[string]interface{}{
			"timestamp": l.Timestamp.Format(time.RFC3339),
			"level":     "info",
			"component": "router",
			"module":    l.Provider,
			"msg":       "Proxy call to " + l.Model + " status=" + strconv.Itoa(l.StatusCode),
			"latencyMs": l.LatencyMs,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(entries)
}

func (s *Server) handleCliToolsGenericSettings(toolId string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		status, _ := clitools.DetectTool(toolId)
		settings, _ := clitools.ReadSettingsFile(toolId)

		installed := false
		runnable := false
		command := ""
		commandPath := ""
		if status != nil {
			installed = status.Detection.Installed
			runnable = status.Detection.Runnable
			command = status.Detection.Command
			commandPath = status.Detection.CommandPath
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"installed":   installed,
			"runnable":    runnable,
			"command":     command,
			"commandPath": commandPath,
			"settings":    settings,
			"configured":  status != nil && status.Config.Status == "configured",
		})
	}
}

func (s *Server) handleCliToolsGenericSaveSettings(toolId string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req clitools.ApplyRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		req.ToolID = toolId

		res, err := clitools.ApplyTool(req)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(res)
	}
}

func (s *Server) handleCliToolsCodexProfiles(w http.ResponseWriter, r *http.Request) {
	profiles := []map[string]interface{}{
		{
			"id":       "goroute",
			"name":     "GoRoute Universal Gateway",
			"endpoint": "http://127.0.0.1:8128/v1",
			"model":    "gpt-5.1-codex",
		},
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"profiles": profiles,
	})
}

func (s *Server) handleCliToolsOpenClawAutoOrder(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"provider": map[string]interface{}{
			"order":           []string{"antigravity", "openai", "copilot", "cursor", "kiro"},
			"allow_fallbacks": true,
		},
		"generated_at": time.Now().UTC().Format(time.RFC3339),
		"source":       "goroute-auto-combo",
	})
}

func (s *Server) handleCliToolsGuideSettings(w http.ResponseWriter, r *http.Request) {
	toolId := chi.URLParam(r, "toolId")
	tool, exists := clitools.Registry[toolId]
	if !exists {
		http.Error(w, "unknown tool", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"toolId":      toolId,
		"name":        tool.Name,
		"description": tool.Description,
		"docsUrl":     tool.DocsURL,
		"settings":    tool.SettingsFile,
	})
}

func (s *Server) handleCliToolsRuntime(w http.ResponseWriter, r *http.Request) {
	toolId := chi.URLParam(r, "toolId")
	status, err := clitools.DetectTool(toolId)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(status)
}

func (s *Server) handleCliToolsMitmGet(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"running":           s.tunnelServer != nil,
		"pid":               os.Getpid(),
		"dnsConfigured":     true,
		"certExists":        true,
		"hasCachedPassword": true,
		"isWin":             false,
		"needsSudoPassword": false,
	})
}

func (s *Server) handleCliToolsMitmStart(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"running": true,
		"pid":     os.Getpid(),
	})
}

func (s *Server) handleCliToolsMitmStop(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"running": false,
	})
}

func (s *Server) handleCliToolsMitmAliasGet(w http.ResponseWriter, r *http.Request) {
	tool := r.URL.Query().Get("tool")
	aliases := map[string]interface{}{
		"gpt-4": map[string]string{"model": "claude-sonnet-4.5"},
		"o1":    map[string]string{"model": "claude-opus-5.5"},
	}
	w.Header().Set("Content-Type", "application/json")
	if tool != "" {
		json.NewEncoder(w).Encode(map[string]interface{}{"aliases": aliases})
		return
	}
	json.NewEncoder(w).Encode(map[string]interface{}{
		"antigravity": aliases,
	})
}

func (s *Server) handleCliToolsMitmAliasPut(w http.ResponseWriter, r *http.Request) {
	var body map[string]interface{}
	_ = json.NewDecoder(r.Body).Decode(&body)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"aliases": body["mappings"],
	})
}
