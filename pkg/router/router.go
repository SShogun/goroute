package router

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/ziuus/goroute/pkg/providers"
)

type Target struct {
	ProviderID string
	Model      string
}

type Combo struct {
	Name     string
	Strategy string // "failover", "round_robin"
	Targets  []Target
	counter  uint64
}

type Router struct {
	mu        sync.RWMutex
	providers map[string]providers.Provider
	combos    map[string]*Combo
}

func NewRouter() *Router {
	return &Router{
		providers: make(map[string]providers.Provider),
		combos:    make(map[string]*Combo),
	}
}

func (r *Router) RegisterProvider(p providers.Provider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.providers[p.ID()] = p
}

func (r *Router) RegisterCombo(name, strategy string, targets []Target) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.combos[name] = &Combo{
		Name:     name,
		Strategy: strategy,
		Targets:  targets,
	}
}

func (r *Router) GetProvider(id string) (providers.Provider, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.providers[id]
	return p, ok
}

func (r *Router) ResolveTarget(model string) (Target, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if strings.HasPrefix(model, "gr/") {
		model = strings.TrimPrefix(model, "gr/")
	} else if strings.HasPrefix(model, "goroute/") {
		model = strings.TrimPrefix(model, "goroute/")
	}
	if strings.HasPrefix(model, "agy/") {
		model = "antigravity/" + strings.TrimPrefix(model, "agy/")
	}

	if combo, ok := r.combos[model]; ok && len(combo.Targets) > 0 {
		return combo.Targets[0], true
	}

	parts := strings.SplitN(model, "/", 2)
	if len(parts) == 2 {
		return Target{ProviderID: parts[0], Model: parts[1]}, true
	}

	return Target{}, false
}

func (r *Router) ListModels(ctx context.Context) ([]providers.ModelInfo, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var models []providers.ModelInfo
	// Add combos
	for name := range r.combos {
		models = append(models, providers.ModelInfo{
			ID:      name,
			Object:  "model",
			OwnedBy: "goroute-combo",
		})
	}

	// Add provider direct models
	for _, p := range r.providers {
		pModels, err := p.Models(ctx)
		if err == nil {
			for _, m := range pModels {
				models = append(models, providers.ModelInfo{
					ID:      fmt.Sprintf("%s/%s", p.ID(), m.ID),
					Object:  "model",
					OwnedBy: p.ID(),
				})
			}
		}
	}

	return models, nil
}

// Execute routes the request, executing automatic failover or load balancing
func (r *Router) Execute(ctx context.Context, req *providers.ChatRequest) (io.ReadCloser, bool, int, error) {
	model := req.Model
	if strings.HasPrefix(model, "gr/") {
		model = strings.TrimPrefix(model, "gr/")
	} else if strings.HasPrefix(model, "goroute/") {
		model = strings.TrimPrefix(model, "goroute/")
	}

	if strings.HasPrefix(model, "agy/") {
		model = "antigravity/" + strings.TrimPrefix(model, "agy/")
	}

	normReq := *req
	normReq.Model = model

	r.mu.RLock()
	combo, isCombo := r.combos[normReq.Model]
	r.mu.RUnlock()

	if isCombo && len(combo.Targets) > 0 {
		return r.executeCombo(ctx, combo, &normReq)
	}

	// Direct model or provider/model lookup
	return r.executeDirect(ctx, &normReq)
}

func (r *Router) executeCombo(ctx context.Context, combo *Combo, req *providers.ChatRequest) (io.ReadCloser, bool, int, error) {
	targets := combo.Targets
	if combo.Strategy == "round_robin" && len(targets) > 1 {
		offset := atomic.AddUint64(&combo.counter, 1) % uint64(len(targets))
		// Rotate targets order
		rotated := make([]Target, len(targets))
		for i := range targets {
			rotated[i] = targets[(int(offset)+i)%len(targets)]
		}
		targets = rotated
	}

	var lastErr error
	var lastStatus int

	for _, target := range targets {
		r.mu.RLock()
		prov, ok := r.providers[target.ProviderID]
		r.mu.RUnlock()

		if !ok || !prov.IsHealthy() {
			continue
		}

		// Clone request with target model
		targetReq := *req
		targetReq.Model = target.Model

		body, isStream, statusCode, err := prov.ChatCompletion(ctx, &targetReq)
		if err != nil {
			lastErr = err
			continue
		}

		// If success (2xx or 3xx), return immediately
		if statusCode >= 200 && statusCode < 400 {
			return body, isStream, statusCode, nil
		}

		// If 402 (Payment/Quota exhausted), 429 (Rate limited), 401/403 (Auth/License), 404 (Model deprecated), or 5xx (Upstream error), close body and cascade to next target!
		if statusCode == http.StatusPaymentRequired || statusCode == http.StatusTooManyRequests || statusCode == http.StatusForbidden || statusCode == http.StatusUnauthorized || statusCode == http.StatusNotFound || (statusCode >= 500 && statusCode <= 599) {
			if body != nil {
				body.Close()
			}
			lastStatus = statusCode
			continue
		}

		// Any other 4xx client error (e.g. 400 Bad Request) shouldn't cascade, return it directly
		return body, isStream, statusCode, nil
	}

	if lastErr != nil {
		return nil, false, lastStatus, lastErr
	}
	if lastStatus != 0 {
		return nil, false, lastStatus, fmt.Errorf("all combo targets failed, last status: %d", lastStatus)
	}

	return nil, false, http.StatusServiceUnavailable, fmt.Errorf("no available targets for combo %s", combo.Name)
}

func (r *Router) executeDirect(ctx context.Context, req *providers.ChatRequest) (io.ReadCloser, bool, int, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// Check if model contains provider prefix, e.g. "antigravity/claude-3-7-sonnet"
	parts := strings.SplitN(req.Model, "/", 2)
	if len(parts) == 2 {
		provID := parts[0]
		modelName := parts[1]
		if prov, ok := r.providers[provID]; ok {
			targetReq := *req
			targetReq.Model = modelName
			return prov.ChatCompletion(ctx, &targetReq)
		}
	}

	// Otherwise, send to first available provider
	for _, p := range r.providers {
		if p.IsHealthy() {
			return p.ChatCompletion(ctx, req)
		}
	}

	return nil, false, http.StatusNotFound, fmt.Errorf("no provider found for model: %s", req.Model)
}

func (r *Router) ExecuteEmbeddings(ctx context.Context, req *providers.EmbeddingRequest) (*providers.EmbeddingResponse, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// Check if model contains provider prefix, e.g. "openai/text-embedding-3-small"
	if strings.Contains(req.Model, "/") {
		parts := strings.SplitN(req.Model, "/", 2)
		provID := parts[0]
		modelName := parts[1]
		if prov, ok := r.providers[provID]; ok && prov.IsHealthy() {
			targetReq := *req
			targetReq.Model = modelName
			return prov.Embeddings(ctx, &targetReq)
		}
	}

	// Try each healthy provider
	var lastErr error
	for _, prov := range r.providers {
		if !prov.IsHealthy() {
			continue
		}
		resp, err := prov.Embeddings(ctx, req)
		if err == nil && resp != nil {
			return resp, nil
		}
		lastErr = err
	}

	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("no provider available for embeddings")
}
