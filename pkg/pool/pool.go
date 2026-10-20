package pool

import (
	"math"
	"sync"
	"time"
)

type AccountUsage struct {
	AccountID     string    `json:"account_id"`
	PoolID        string    `json:"pool_id"`
	ProviderID    string    `json:"provider_id"`
	TotalTokens   int64     `json:"total_tokens"`
	TotalRequests int64     `json:"total_requests"`
	WindowTokens  int64     `json:"window_tokens"` // Tokens consumed in current rolling window
	WindowStart   time.Time `json:"window_start"`
	Weight        float64   `json:"weight"` // Relative quota weighting, default 1.0
	Status        string    `json:"status"` // "active", "cooldown", "disabled"
	CooldownUntil time.Time `json:"cooldown_until"`
}

type AccountPool struct {
	mu         sync.RWMutex
	id         string
	mode       string // "balanced" (fair share), "round_robin", "priority"
	accounts   map[string]*AccountUsage
	windowSize time.Duration
	rrCounter  uint64
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

func (m *PoolManager) GetOrCreatePool(poolID, mode string) *AccountPool {
	m.mu.Lock()
	defer m.mu.Unlock()

	p, exists := m.pools[poolID]
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
		id:         poolID,
		mode:       mode,
		accounts:   make(map[string]*AccountUsage),
		windowSize: 10 * time.Minute,
	}
	m.pools[poolID] = p
	return p
}

func (p *AccountPool) RegisterAccount(accountID string, weight float64) {
	p.RegisterAccountWithInfo(accountID, p.id, "", weight)
}

func (p *AccountPool) RegisterAccountWithInfo(accountID, poolID, providerID string, weight float64) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if weight <= 0 {
		weight = 1.0
	}
	if poolID == "" {
		poolID = p.id
	}

	if acc, exists := p.accounts[accountID]; exists {
		acc.Weight = weight
		if poolID != "" {
			acc.PoolID = poolID
		}
		if providerID != "" {
			acc.ProviderID = providerID
		}
		return
	}

	p.accounts[accountID] = &AccountUsage{
		AccountID:   accountID,
		PoolID:      poolID,
		ProviderID:  providerID,
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

func (p *AccountPool) SetAccountDisabled(accountID string, disabled bool) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if acc, ok := p.accounts[accountID]; ok {
		if disabled {
			acc.Status = "disabled"
		} else if acc.Status == "disabled" {
			acc.Status = "active"
		}
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

	// Mode: "priority" or "failover" (OmniRoute parity: honors exact sequence order #1 -> #2 -> ...)
	if p.mode == "priority" || p.mode == "failover" {
		for _, cid := range candidates {
			acc := p.accounts[cid]
			if acc.Status == "disabled" {
				continue
			}
			if acc.Status == "active" {
				return cid
			}
		}
		// All non-disabled accounts are in cooldown; choose the one with earliest recovery
		var bestID string
		var earliestCooldown time.Time
		for _, cid := range candidates {
			acc := p.accounts[cid]
			if acc.Status == "disabled" {
				continue
			}
			if bestID == "" || acc.CooldownUntil.Before(earliestCooldown) {
				earliestCooldown = acc.CooldownUntil
				bestID = cid
			}
		}
		if bestID != "" {
			return bestID
		}
		return candidates[0]
	}

	if p.mode == "round_robin" {
		// Filter out disabled
		var activeCandidates []string
		for _, cid := range candidates {
			acc := p.accounts[cid]
			if acc.Status != "disabled" {
				activeCandidates = append(activeCandidates, cid)
			}
		}
		if len(activeCandidates) == 0 {
			activeCandidates = candidates
		}
		idx := p.rrCounter % uint64(len(activeCandidates))
		p.rrCounter++
		return activeCandidates[idx]
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
			if acc.Status == "disabled" {
				continue
			}
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
			PoolID:        v.PoolID,
			ProviderID:    v.ProviderID,
			TotalTokens:   v.TotalTokens,
			TotalRequests: v.TotalRequests,
			WindowTokens:  v.WindowTokens,
			WindowStart:   v.WindowStart,
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

func (m *PoolManager) RegisterAccount(poolID, provID, accID string, weight float64) {
	if poolID == "" {
		poolID = "pool1"
	}
	p := m.GetOrCreatePool(poolID, "balanced")
	p.RegisterAccountWithInfo(accID, poolID, provID, weight)
}

func (m *PoolManager) RemoveAccount(poolID, accID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if poolID != "" {
		if p, exists := m.pools[poolID]; exists {
			p.RemoveAccount(accID)
		}
		return
	}
	for _, p := range m.pools {
		p.RemoveAccount(accID)
	}
}

func (m *PoolManager) RecordUsage(poolID, accID string, tokens int64) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if poolID != "" {
		if p, exists := m.pools[poolID]; exists {
			p.RecordUsage(accID, tokens)
			return
		}
	}
	for _, p := range m.pools {
		p.RecordUsage(accID, tokens)
	}
}

func (m *PoolManager) SetCooldown(poolID, accID string, duration time.Duration) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if poolID != "" {
		if p, exists := m.pools[poolID]; exists {
			p.SetCooldown(accID, duration)
			return
		}
	}
	for _, p := range m.pools {
		p.SetCooldown(accID, duration)
	}
}

func (m *PoolManager) SetAccountDisabled(accID string, disabled bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, p := range m.pools {
		p.SetAccountDisabled(accID, disabled)
	}
}

func (m *PoolManager) ListAllPools() map[string][]*AccountUsage {
	m.mu.RLock()
	defer m.mu.RUnlock()

	res := make(map[string][]*AccountUsage)
	// Ensure pool1 is always visible
	res["pool1"] = []*AccountUsage{}

	for poolID, p := range m.pools {
		res[poolID] = p.ListUsage()
	}
	return res
}
