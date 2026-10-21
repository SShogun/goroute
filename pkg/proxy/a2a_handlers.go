package proxy

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/ziuus/goroute/pkg/a2a"
)

func (s *Server) registerA2ARoutes() {
	s.mux.Get("/.well-known/agent.json", s.handleA2AAgentCard)
	s.mux.Get("/api/a2a/status", s.handleA2AStatus)
	s.mux.Get("/api/a2a/tasks", s.handleA2AListTasks)
	s.mux.Post("/api/a2a/tasks", s.handleA2ACreateTask)
	s.mux.Get("/api/a2a/tasks/{id}", s.handleA2AGetTask)
	s.mux.Post("/api/a2a/tasks/{id}/cancel", s.handleA2ACancelTask)
	s.mux.Get("/api/a2a/tasks/history", s.handleA2ATasksHistory)
}

func (s *Server) handleA2AAgentCard(w http.ResponseWriter, r *http.Request) {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	baseURL := scheme + "://" + r.Host
	card := a2a.GetAgentCard(baseURL)

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	json.NewEncoder(w).Encode(card)
}

func (s *Server) handleA2AStatus(w http.ResponseWriter, r *http.Request) {
	stats := a2a.GetStats()
	card := a2a.GetAgentCard("")

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":       "ok",
		"online":       true,
		"enabled":      true,
		"tasks":        stats,
		"agent":        card,
		"capabilities": card.Capabilities,
		"skills":       card.Skills,
	})
}

func (s *Server) handleA2AListTasks(w http.ResponseWriter, r *http.Request) {
	state := r.URL.Query().Get("state")
	skill := r.URL.Query().Get("skill")
	limit := 50
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 {
			limit = n
		}
	}
	offset := 0
	if o := r.URL.Query().Get("offset"); o != "" {
		if n, err := strconv.Atoi(o); err == nil && n >= 0 {
			offset = n
		}
	}

	tasks, total, err := a2a.ListTasks(state, skill, limit, offset)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"tasks":  tasks,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})
}

func (s *Server) handleA2ACreateTask(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Skill string `json:"skill"`
		Input string `json:"input"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if body.Skill == "" {
		http.Error(w, "skill is required", http.StatusBadRequest)
		return
	}

	task, err := a2a.CreateTask(body.Skill, body.Input)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(task)
}

func (s *Server) handleA2AGetTask(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	task, err := a2a.GetTask(id)
	if err != nil || task == nil {
		http.Error(w, "task not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(task)
}

func (s *Server) handleA2ACancelTask(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := a2a.CancelTask(id); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Task cancelled",
	})
}

func (s *Server) handleA2ATasksHistory(w http.ResponseWriter, r *http.Request) {
	tasks, total, err := a2a.ListTasks("completed", "", 100, 0)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"history": tasks,
		"total":   total,
	})
}
