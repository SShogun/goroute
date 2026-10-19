package resilience

import (
	"sync"
	"time"
)

type CooldownTarget struct {
	ProviderID   string    `json:"provider_id"`
	Model        string    `json:"model"`
	Reason       string    `json:"reason"`
	StatusCode   int       `json:"status_code"`
	CoolingUntil time.Time `json:"cooling_until"`
	Attempts     int       `json:"attempts"`
}

type CooldownManager struct {
	mu         sync.RWMutex
	cooldowns  map[string]*CooldownTarget
	defaultTTL time.Duration
}

var GlobalCooldown = NewCooldownManager(30 * time.Second)

func NewCooldownManager(defaultTTL time.Duration) *CooldownManager {
	if defaultTTL <= 0 {
		defaultTTL = 30 * time.Second
	}
	return &CooldownManager{
		cooldowns:  make(map[string]*CooldownTarget),
		defaultTTL: defaultTTL,
	}
}

func (cm *CooldownManager) MarkCooldown(providerID, model string, statusCode int, reason string, ttl time.Duration) {
	if ttl <= 0 {
		ttl = cm.defaultTTL
	}
	key := providerID + "/" + model
	cm.mu.Lock()
	defer cm.mu.Unlock()

	target, exists := cm.cooldowns[key]
	if !exists {
		target = &CooldownTarget{
			ProviderID: providerID,
			Model:      model,
		}
		cm.cooldowns[key] = target
	}
	target.Reason = reason
	target.StatusCode = statusCode
	target.CoolingUntil = time.Now().Add(ttl)
	target.Attempts++
}

func (cm *CooldownManager) IsCooling(providerID, model string) bool {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	// Check model level
	key := providerID + "/" + model
	if target, exists := cm.cooldowns[key]; exists {
		if time.Now().Before(target.CoolingUntil) {
			return true
		}
	}

	// Check provider level
	if target, exists := cm.cooldowns[providerID]; exists {
		if time.Now().Before(target.CoolingUntil) {
			return true
		}
	}

	return false
}

func (cm *CooldownManager) Reset(key string) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	if key == "" {
		cm.cooldowns = make(map[string]*CooldownTarget)
		return
	}
	delete(cm.cooldowns, key)
}

func (cm *CooldownManager) List() []*CooldownTarget {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	var res []*CooldownTarget
	now := time.Now()
	for _, target := range cm.cooldowns {
		if now.Before(target.CoolingUntil) {
			res = append(res, target)
		}
	}
	return res
}
