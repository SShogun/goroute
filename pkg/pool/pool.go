package pool

import (
	"math"
	"sync"
	"time"
)

type AccountUsage struct {
	AccountID     string    `json:"account_id"`
	TotalTokens   int64     `json:"total_tokens"`
	TotalRequests int64     `json:"total_requests"`
	WindowTokens  int64     `json:"window_tokens"` // Tokens consumed in current rolling window
	WindowStart   time.Time `json:"window_start"`
	Weight        float64   `json:"weight"` // Relative quota weighting, default 1.0
	Status        string    `json:"status"` // "active", "cooldown", "disabled"
	CooldownUntil time.Time `json:"cooldown_until"`
}

type AccountPool struct {
	mu            sync.RWMutex
	providerID    string
	mode          string // "balanced" (fair share), "round_robin", "priority"
	accounts      map[string]*AccountUsage
	windowSize    time.Duration
	rrCounter     uint64
}

type PoolManager struct {
	mu    sync.RWMutex
	pools map[string]*AccountPool
}

var GlobalPool = NewPoolManager()

func NewPoolManager() *PoolManager {
	return &PoolManager{
		pools: make(map[string]*AccountPool),
	}
}

func (m *PoolManager) GetOrCreatePool(providerID, mode string) *AccountPool {
	m.mu.Lock()
	defer m.mu.Unlock()

	p, exists := m.pools[providerID]
	if exists {
		if mode != "" && p.mode != mode {
			p.mode = mode
		}
		return p
	}

	if mode == "" {
		mode = "balanced"
	}

	p = &AccountPool{
		providerID: providerID,
		mode:       mode,
		accounts:   make(map[string]*AccountUsage),
		windowSize: 10 * time.Minute,
	}
	m.pools[providerID] = p
	return p
}

func (p *AccountPool) RegisterAccount(accountID string, weight float64) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if weight <= 0 {
		weight = 1.0
	}

	if acc, exists := p.accounts[accountID]; exists {
		acc.Weight = weight
		return
	}

	p.accounts[accountID] = &AccountUsage{
		AccountID:   accountID,
		Weight:      weight,
		Status:      "active",
		WindowStart: time.Now(),
	}
}

func (p *AccountPool) SetCooldown(accountID string, duration time.Duration) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if acc, ok := p.accounts[accountID]; ok {
		acc.Status = "cooldown"
		acc.CooldownUntil = time.Now().Add(duration)
	}
}

// SelectBestAccount implements Fair-Share Token Balancing.
// It selects the account with the lowest normalized token usage (WindowTokens / Weight).
func (p *AccountPool) SelectBestAccount(candidates []string) string {
	p.mu.Lock()
	defer p.mu.Unlock()

	if len(candidates) == 0 {
		return ""
	}

	now := time.Now()

	// Ensure all candidates exist in pool
	for _, cid := range candidates {
		acc, exists := p.accounts[cid]
		if !exists {
			acc = &AccountUsage{
				AccountID:   cid,
				Weight:      1.0,
				Status:      "active",
				WindowStart: now,
			}
			p.accounts[cid] = acc
		}
		// Reset rolling window if expired
		if now.Sub(acc.WindowStart) > p.windowSize {
			acc.WindowTokens = 0
			acc.WindowStart = now
		}
		// Clear cooldown if time elapsed
		if acc.Status == "cooldown" && now.After(acc.CooldownUntil) {
			acc.Status = "active"
		}
	}

	if p.mode == "round_robin" {
		idx := p.rrCounter % uint64(len(candidates))
		p.rrCounter++
		return candidates[idx]
	}

	// Mode: "balanced" (Fair-Share Token Leveling)
	var bestID string
	minNormalizedTokens := math.MaxFloat64

	// First pass: look only for active accounts
	for _, cid := range candidates {
		acc := p.accounts[cid]
		if acc.Status != "active" {
			continue
		}
		normalized := float64(acc.WindowTokens) / acc.Weight
		if normalized < minNormalizedTokens {
			minNormalizedTokens = normalized
			bestID = cid
		}
	}

	// If all are cooling down, pick the one closest to expiry
	if bestID == "" {
		var earliestCooldown time.Time
		for _, cid := range candidates {
			acc := p.accounts[cid]
			if bestID == "" || acc.CooldownUntil.Before(earliestCooldown) {
				earliestCooldown = acc.CooldownUntil
				bestID = cid
			}
		}
	}

	return bestID
}

func (p *AccountPool) RecordUsage(accountID string, tokens int64) {
	p.mu.Lock()
	defer p.mu.Unlock()

	acc, exists := p.accounts[accountID]
	if !exists {
		acc = &AccountUsage{
			AccountID:   accountID,
			Weight:      1.0,
			Status:      "active",
			WindowStart: time.Now(),
		}
		p.accounts[accountID] = acc
	}

	if tokens <= 0 {
		tokens = 1
	}

	acc.TotalTokens += tokens
	acc.WindowTokens += tokens
	acc.TotalRequests++
}

func (p *AccountPool) ListUsage() []*AccountUsage {
	p.mu.RLock()
	defer p.mu.RUnlock()

	var list []*AccountUsage
	for _, v := range p.accounts {
		list = append(list, &AccountUsage{
			AccountID:     v.AccountID,
			TotalTokens:   v.TotalTokens,
			TotalRequests: v.TotalRequests,
			WindowTokens:  v.WindowTokens,
			Weight:        v.Weight,
			Status:        v.Status,
			CooldownUntil: v.CooldownUntil,
		})
	}
	return list
}

func (p *AccountPool) RemoveAccount(accID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.accounts, accID)
}

func (m *PoolManager) RegisterAccount(provID, accID string, weight float64) {
	p := m.GetOrCreatePool(provID, "balanced")
	p.RegisterAccount(accID, weight)
}

func (m *PoolManager) RemoveAccount(provID, accID string) {
	m.mu.Lock()
	p, exists := m.pools[provID]
	m.mu.Unlock()
	if exists {
		p.RemoveAccount(accID)
	}
}

func (m *PoolManager) ListAllPools() map[string][]*AccountUsage {
	m.mu.RLock()
	defer m.mu.RUnlock()

	res := make(map[string][]*AccountUsage)
	for provID, p := range m.pools {
		res[provID] = p.ListUsage()
	}
	return res
}
