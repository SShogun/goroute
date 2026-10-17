package quota

import (
	"sync"
	"time"
)

type accountStats struct {
	cooldownUntil time.Time
	rpmLimit      int
	requests      []time.Time
}

type Limiter struct {
	mu       sync.RWMutex
	accounts map[string]*accountStats
}

func NewLimiter() *Limiter {
	return &Limiter{
		accounts: make(map[string]*accountStats),
	}
}

func (l *Limiter) getOrCreate(id string) *accountStats {
	l.mu.Lock()
	defer l.mu.Unlock()
	stats, ok := l.accounts[id]
	if !ok {
		stats = &accountStats{
			requests: make([]time.Time, 0, 64),
		}
		l.accounts[id] = stats
	}
	return stats
}

func (l *Limiter) SetRPM(id string, rpm int) {
	stats := l.getOrCreate(id)
	l.mu.Lock()
	defer l.mu.Unlock()
	stats.rpmLimit = rpm
}

func (l *Limiter) Record429(id string, duration time.Duration) {
	stats := l.getOrCreate(id)
	l.mu.Lock()
	defer l.mu.Unlock()
	stats.cooldownUntil = time.Now().Add(duration)
}

func (l *Limiter) Allow(id string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	stats, ok := l.accounts[id]
	if !ok {
		return true
	}

	now := time.Now()
	// Check cooldown
	if now.Before(stats.cooldownUntil) {
		return false
	}

	// Check RPM
	if stats.rpmLimit > 0 {
		cutoff := now.Add(-1 * time.Minute)
		valid := stats.requests[:0]
		for _, t := range stats.requests {
			if t.After(cutoff) {
				valid = append(valid, t)
			}
		}
		stats.requests = valid

		if len(stats.requests) >= stats.rpmLimit {
			return false
		}
	}

	stats.requests = append(stats.requests, now)
	return true
}
