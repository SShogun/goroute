package proxy

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/ziuus/goroute/pkg/websearch"
)

func (s *Server) registerWebSearchRoutes() {
	// Web Fetch
	s.mux.Post("/v1/web/fetch", s.handleWebFetch)
	s.mux.Post("/api/v1/web/fetch", s.handleWebFetch)

	// Web Map
	s.mux.Post("/v1/web/map", s.handleWebMap)
	s.mux.Post("/api/v1/web/map", s.handleWebMap)

	// Search
	s.mux.Post("/v1/search", s.handleSearch)
	s.mux.Post("/api/v1/search", s.handleSearch)
	s.mux.Get("/v1/search", s.handleSearchGet)
	s.mux.Get("/api/v1/search", s.handleSearchGet)
}

func (s *Server) handleWebFetch(w http.ResponseWriter, r *http.Request) {
	var req websearch.FetchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	resp, err := websearch.Fetch(r.Context(), req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (s *Server) handleWebMap(w http.ResponseWriter, r *http.Request) {
	var req websearch.MapRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	resp, err := websearch.Map(r.Context(), req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	var req websearch.SearchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	resp, err := websearch.Search(r.Context(), req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (s *Server) handleSearchGet(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	if q == "" {
		q = r.URL.Query().Get("query")
	}
	if q == "" {
		http.Error(w, "q parameter is required", http.StatusBadRequest)
		return
	}

	maxResults := 10
	if mr := r.URL.Query().Get("max_results"); mr != "" {
		if n, err := strconv.Atoi(mr); err == nil && n > 0 {
			maxResults = n
		}
	}

	req := websearch.SearchRequest{
		Query:      q,
		Provider:   r.URL.Query().Get("provider"),
		MaxResults: maxResults,
		SearchType: r.URL.Query().Get("search_type"),
	}

	resp, err := websearch.Search(r.Context(), req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}
