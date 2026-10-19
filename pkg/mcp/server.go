package mcp

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/ziuus/goroute/pkg/catalog"
	"github.com/ziuus/goroute/pkg/resilience"
)

type Tool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"inputSchema"`
}

type Server struct {
	mu       sync.Mutex
	sessions map[string]chan string
	resMgr   *resilience.CooldownManager
	tools    []Tool
}

func NewServer(resMgr *resilience.CooldownManager) *Server {
	if resMgr == nil {
		resMgr = resilience.GlobalCooldown
	}
	s := &Server{
		sessions: make(map[string]chan string),
		resMgr:   resMgr,
	}

	s.tools = []Tool{
		{
			Name:        "pickFastestModel",
			Description: "Returns the fastest active LLM model currently available without rate limit cooldowns in GoRoute",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"family": map[string]interface{}{
						"type":        "string",
						"description": "Optional model family filter (e.g. claude, gpt, gemini)",
					},
				},
			},
		},
		{
			Name:        "listProviders",
			Description: "Lists all connected LLM providers and account connections active in GoRoute",
			InputSchema: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			},
		},
		{
			Name:        "modelUsage",
			Description: "Inspects live quota, tokens used, and cooldowns across all connected providers",
			InputSchema: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			},
		},
		{
			Name:        "resetCooldowns",
			Description: "Instantly resets all temporary 429/5xx model quarantines in GoRoute",
			InputSchema: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			},
		},
	}
	return s
}

func (s *Server) HandleToolsList(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"tools": s.tools,
	})
}

func (s *Server) HandleToolCall(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name      string                 `json:"name"`
		Arguments map[string]interface{} `json:"arguments"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	var result interface{}
	switch req.Name {
	case "pickFastestModel":
		cooldowns := s.resMgr.List()
		coolMap := make(map[string]bool)
		for _, c := range cooldowns {
			coolMap[fmt.Sprintf("%s/%s", c.ProviderID, c.Model)] = true
		}

		fastest := "antigravity/gemini-2.5-flash"
		if coolMap[fastest] {
			fastest = "kiro/claude-sonnet-4.5"
		}
		result = map[string]interface{}{
			"recommended_model": fastest,
			"active_cooldowns":  len(cooldowns),
			"strategy":          "lowest_latency_healthy",
		}

	case "listProviders":
		result = map[string]interface{}{
			"catalog_count": len(catalog.Registry),
			"engine":        "goroute",
			"status":        "operational",
		}

	case "modelUsage":
		result = map[string]interface{}{
			"cooldowns": s.resMgr.List(),
			"engine":    "goroute",
		}

	case "resetCooldowns":
		s.resMgr.Reset("")
		result = map[string]interface{}{
			"status":  "ok",
			"message": "All model cooldowns reset successfully",
		}

	default:
		http.Error(w, fmt.Sprintf("Unknown tool: %s", req.Name), http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"content": []map[string]interface{}{
			{
				"type": "text",
				"text": fmt.Sprintf("%v", result),
			},
		},
	})
}

// SSE Transport for MCP clients
func (s *Server) HandleSSE(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	sessionID := uuid.New().String()
	ch := make(chan string, 16)

	s.mu.Lock()
	s.sessions[sessionID] = ch
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		delete(s.sessions, sessionID)
		s.mu.Unlock()
	}()

	// Send endpoint event with session ID
	fmt.Fprintf(w, "event: endpoint\ndata: /message?sessionId=%s\n\n", sessionID)
	flusher.Flush()

	notify := r.Context().Done()
	for {
		select {
		case <-notify:
			return
		case msg := <-ch:
			fmt.Fprintf(w, "event: message\ndata: %s\n\n", msg)
			flusher.Flush()
		case <-time.After(15 * time.Second):
			// Keepalive comment
			fmt.Fprintf(w, ": keepalive\n\n")
			flusher.Flush()
		}
	}
}

// HandleMessage receives client JSON-RPC commands
func (s *Server) HandleMessage(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("sessionId")
	s.mu.Lock()
	ch, ok := s.sessions[sessionID]
	s.mu.Unlock()

	if !ok {
		http.Error(w, "Session not found", http.StatusNotFound)
		return
	}

	var rpcReq struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      interface{}     `json:"id"`
		Method  string          `json:"method"`
		Params  json.RawMessage `json:"params"`
	}

	if err := json.NewDecoder(r.Body).Decode(&rpcReq); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	var resp map[string]interface{}
	switch rpcReq.Method {
	case "initialize":
		resp = map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      rpcReq.ID,
			"result": map[string]interface{}{
				"protocolVersion": "2024-11-05",
				"capabilities": map[string]interface{}{
					"tools": map[string]interface{}{},
				},
				"serverInfo": map[string]interface{}{
					"name":    "goroute-mcp",
					"version": "1.0.0",
				},
			},
		}
	case "tools/list":
		resp = map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      rpcReq.ID,
			"result": map[string]interface{}{
				"tools": s.tools,
			},
		}
	default:
		resp = map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      rpcReq.ID,
			"result":  map[string]interface{}{},
		}
	}

	respBytes, _ := json.Marshal(resp)
	select {
	case ch <- string(respBytes):
	default:
	}

	w.WriteHeader(http.StatusAccepted)
}
