package proxy

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/ziuus/goroute/pkg/agentskills"
)

func (s *Server) registerAgentSkillsRoutes() {
	s.mux.Get("/api/agent-skills", s.handleAgentSkillsList)
	s.mux.Post("/api/agent-skills", s.handleAgentSkillsCreate)
	s.mux.Get("/api/agent-skills/coverage", s.handleAgentSkillsCoverage)
	s.mux.Post("/api/agent-skills/generate", s.handleAgentSkillsGenerate)
	s.mux.Get("/api/agent-skills/{id}", s.handleAgentSkillsGet)
	s.mux.Get("/api/agent-skills/{id}/raw", s.handleAgentSkillsGetRaw)
}

func (s *Server) handleAgentSkillsList(w http.ResponseWriter, r *http.Request) {
	category := r.URL.Query().Get("category")
	area := r.URL.Query().Get("area")

	skills := agentskills.GetCatalog(category, area)
	coverage := agentskills.ComputeCoverage()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"skills":   skills,
		"count":    len(skills),
		"coverage": coverage,
	})
}

func (s *Server) handleAgentSkillsCreate(w http.ResponseWriter, r *http.Request) {
	var skill agentskills.AgentSkill
	if err := json.NewDecoder(r.Body).Decode(&skill); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if skill.ID == "" || skill.Name == "" {
		http.Error(w, "id and name are required", http.StatusBadRequest)
		return
	}

	skill.Installed = true
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(skill)
}

func (s *Server) handleAgentSkillsCoverage(w http.ResponseWriter, r *http.Request) {
	cov := agentskills.ComputeCoverage()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(cov)
}

func (s *Server) handleAgentSkillsGenerate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		DryRun  bool     `json:"dryRun"`
		Prune   bool     `json:"prune"`
		OnlyIDs []string `json:"onlyIds"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)

	report, err := agentskills.GenerateSkills(body.DryRun, body.Prune, body.OnlyIDs)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(report)
}

func (s *Server) handleAgentSkillsGet(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	for _, s := range agentskills.Catalog {
		if s.ID == id {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(s)
			return
		}
	}
	http.Error(w, "skill not found", http.StatusNotFound)
}

func (s *Server) handleAgentSkillsGetRaw(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	raw, err := agentskills.GetRawSkill(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.Write([]byte(raw))
}
