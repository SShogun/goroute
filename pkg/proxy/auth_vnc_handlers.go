package proxy

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
)

type VncSession struct {
	SessionID    string `json:"sessionId"`
	ConnectionID string `json:"connectionId"`
	ProviderID   string `json:"providerId"`
	URL          string `json:"url"`
	Status       string `json:"status"` // "running", "stopped", "harvested"
	StartedAt    string `json:"startedAt"`
	VNCPort      int    `json:"vncPort"`
}

type VncManager struct {
	mu       sync.Mutex
	sessions map[string]*VncSession
}

var globalVncMgr = &VncManager{
	sessions: make(map[string]*VncSession),
}

func (s *Server) registerAuthAndVncRoutes() {
	// Auth login/logout
	s.mux.Post("/api/auth/login", s.handleAuthLogin)
	s.mux.Post("/api/auth/logout", s.handleAuthLogout)
	s.mux.Get("/api/auth/oidc/login", s.handleAuthOidcLogin)
	s.mux.Get("/api/auth/oidc/callback", s.handleAuthOidcCallback)

	// VNC Interactive Login Sessions
	s.mux.Get("/api/vnc-session", s.handleVncList)
	s.mux.Get("/api/vnc-session/{connectionId}", s.handleVncConnectionList)
	s.mux.Post("/api/vnc-session/{connectionId}/start", s.handleVncStart)
	s.mux.Post("/api/vnc-session/{connectionId}/{sessionId}/harvest", s.handleVncHarvest)
	s.mux.Post("/api/vnc-session/{connectionId}/{sessionId}/touch", s.handleVncTouch)
	s.mux.Delete("/api/vnc-session/{connectionId}/{sessionId}", s.handleVncStop)
}

func (s *Server) handleAuthLogin(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Password string `json:"password"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)

	// Mint random session token
	tokenBytes := make([]byte, 32)
	rand.Read(tokenBytes)
	sessionToken := hex.EncodeToString(tokenBytes)

	http.SetCookie(w, &http.Cookie{
		Name:     "auth_token",
		Value:    sessionToken,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   30 * 24 * 3600, // 30 days
	})

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"token":   sessionToken,
	})
}

func (s *Server) handleAuthLogout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     "auth_token",
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		MaxAge:   -1,
	})

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
	})
}

func (s *Server) handleAuthOidcLogin(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"error": "OIDC is not configured. Use password login or configure OIDC in ~/.goroute/config.yaml.",
	})
}

func (s *Server) handleAuthOidcCallback(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
}

func (s *Server) handleVncList(w http.ResponseWriter, r *http.Request) {
	globalVncMgr.mu.Lock()
	var sessions []*VncSession
	for _, sess := range globalVncMgr.sessions {
		sessions = append(sessions, sess)
	}
	globalVncMgr.mu.Unlock()

	providers := []map[string]interface{}{
		{"id": "claude", "name": "Anthropic Claude Web", "url": "https://claude.ai/login", "kind": "interactive_login"},
		{"id": "openai", "name": "OpenAI ChatGPT", "url": "https://chatgpt.com/auth/login", "kind": "interactive_login"},
		{"id": "gemini", "name": "Google Gemini", "url": "https://gemini.google.com", "kind": "interactive_login"},
		{"id": "cursor", "name": "Cursor IDE", "url": "https://cursor.com/login", "kind": "interactive_login"},
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"sessions":  sessions,
		"providers": providers,
	})
}

func (s *Server) handleVncConnectionList(w http.ResponseWriter, r *http.Request) {
	connID := chi.URLParam(r, "connectionId")
	globalVncMgr.mu.Lock()
	var sessions []*VncSession
	for _, sess := range globalVncMgr.sessions {
		if sess.ConnectionID == connID {
			sessions = append(sessions, sess)
		}
	}
	globalVncMgr.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"connectionId": connID,
		"sessions":     sessions,
	})
}

func (s *Server) handleVncStart(w http.ResponseWriter, r *http.Request) {
	connID := chi.URLParam(r, "connectionId")
	sessID := "vnc_" + strings.ReplaceAll(connID, "-", "_") + "_" + strconvFormat(time.Now().Unix())

	sess := &VncSession{
		SessionID:    sessID,
		ConnectionID: connID,
		ProviderID:   connID,
		URL:          "http://127.0.0.1:6080/vnc.html",
		Status:       "running",
		StartedAt:    time.Now().UTC().Format(time.RFC3339),
		VNCPort:      6080,
	}

	globalVncMgr.mu.Lock()
	globalVncMgr.sessions[sessID] = sess
	globalVncMgr.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(sess)
}

func (s *Server) handleVncHarvest(w http.ResponseWriter, r *http.Request) {
	sessID := chi.URLParam(r, "sessionId")
	globalVncMgr.mu.Lock()
	sess, exists := globalVncMgr.sessions[sessID]
	if exists {
		sess.Status = "harvested"
	}
	globalVncMgr.mu.Unlock()

	if !exists {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":   true,
		"sessionId": sessID,
		"harvested": true,
	})
}

func (s *Server) handleVncTouch(w http.ResponseWriter, r *http.Request) {
	sessID := chi.URLParam(r, "sessionId")
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":   true,
		"sessionId": sessID,
	})
}

func (s *Server) handleVncStop(w http.ResponseWriter, r *http.Request) {
	sessID := chi.URLParam(r, "sessionId")
	globalVncMgr.mu.Lock()
	delete(globalVncMgr.sessions, sessID)
	globalVncMgr.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Session stopped",
	})
}

func strconvFormat(v int64) string {
	return time.Unix(v, 0).Format("150405")
}
