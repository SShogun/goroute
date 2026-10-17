package proxy

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/ziuus/goroute/pkg/cache"
	"github.com/ziuus/goroute/pkg/providers"
	"github.com/ziuus/goroute/pkg/quota"
	"github.com/ziuus/goroute/pkg/router"
	"github.com/ziuus/goroute/web"
)

type Server struct {
	router          *router.Router
	limiter         *quota.Limiter
	cache           *cache.Cache
	enableDashboard bool
	mux             *chi.Mux
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
	var chatReq providers.ChatRequest
	if err := json.NewDecoder(r.Body).Decode(&chatReq); err != nil {
		http.Error(w, "invalid request json: "+err.Error(), http.StatusBadRequest)
		return
	}
	log.Printf("[GoRoute] Incoming /v1/chat/completions model=%s stream=%v messages=%d", chatReq.Model, chatReq.Stream, len(chatReq.Messages))

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
			return
		}
	}

	body, isStream, statusCode, err := s.router.Execute(r.Context(), &chatReq)
	if err != nil {
		log.Printf("[GoRoute] Execute error: %v (code %d)", err, statusCode)
		if statusCode < 100 || statusCode > 599 {
			statusCode = http.StatusInternalServerError
		}
		http.Error(w, err.Error(), statusCode)
		return
	}
	defer body.Close()
	log.Printf("[GoRoute] Upstream returned status %d isStream=%v", statusCode, isStream)

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
