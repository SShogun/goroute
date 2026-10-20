package affinity

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/ziuus/goroute/pkg/providers"
)

type SessionAffinity struct {
	AccountID string
	CreatedAt time.Time
	LastSeen  time.Time
}

type Manager struct {
	mu       sync.RWMutex
	sessions map[string]*SessionAffinity
	ttl      time.Duration
}

var GlobalManager = NewManager(45 * time.Minute)

func NewManager(ttl time.Duration) *Manager {
	m := &Manager{
		sessions: make(map[string]*SessionAffinity),
		ttl:      ttl,
	}
	go m.cleanupLoop()
	return m
}

func (m *Manager) cleanupLoop() {
	ticker := time.NewTicker(5 * time.Minute)
	for range ticker.C {
		m.mu.Lock()
		now := time.Now()
		for k, v := range m.sessions {
			if now.Sub(v.LastSeen) > m.ttl {
				delete(m.sessions, k)
			}
		}
		m.mu.Unlock()
	}
}

// DeriveSessionKey generates a stable hash for a conversation turn
func DeriveSessionKey(r *http.Request, req *providers.ChatRequest) string {
	// 1. Check explicit session headers
	if r != nil {
		if h := r.Header.Get("X-Session-ID"); h != "" {
			return h
		}
		if h := r.Header.Get("X-Conversation-ID"); h != "" {
			return h
		}
		if h := r.Header.Get("Claude-Session-ID"); h != "" {
			return h
		}
		if h := r.Header.Get("OpenCode-Session-ID"); h != "" {
			return h
		}
	}

	// 2. Derive stable session key from the first 2 messages in conversation history
	if req != nil && len(req.Messages) > 0 {
		var b strings.Builder
		count := 0
		for _, msg := range req.Messages {
			if msg.Role == "system" || msg.Role == "user" {
				if s, ok := msg.Content.(string); ok && len(s) > 0 {
					// Use prefix of first 200 chars of initial prompt
					prefix := s
					if len(prefix) > 200 {
						prefix = prefix[:200]
					}
					b.WriteString(msg.Role)
					b.WriteString(":")
					b.WriteString(prefix)
					b.WriteString(";")
					count++
					if count >= 2 {
						break
					}
				}
			}
		}
		if b.Len() > 0 {
			h := sha256.Sum256([]byte(b.String()))
			return hex.EncodeToString(h[:16])
		}
	}

	return ""
}

// GetAffinity returns pinned account ID if still valid
func (m *Manager) GetAffinity(sessionKey, providerID string) string {
	if sessionKey == "" || providerID == "" {
		return ""
	}
	key := providerID + ":" + sessionKey

	m.mu.RLock()
	record, exists := m.sessions[key]
	m.mu.RUnlock()

	if !exists {
		return ""
	}

	if time.Since(record.LastSeen) > m.ttl {
		m.mu.Lock()
		delete(m.sessions, key)
		m.mu.Unlock()
		return ""
	}

	m.mu.Lock()
	record.LastSeen = time.Now()
	m.mu.Unlock()

	return record.AccountID
}

// SetAffinity pins a session to a specific account
func (m *Manager) SetAffinity(sessionKey, providerID, accountID string) {
	if sessionKey == "" || providerID == "" || accountID == "" {
		return
	}
	key := providerID + ":" + sessionKey

	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	if record, exists := m.sessions[key]; exists {
		record.AccountID = accountID
		record.LastSeen = now
	} else {
		m.sessions[key] = &SessionAffinity{
			AccountID: accountID,
			CreatedAt: now,
			LastSeen:  now,
		}
	}
}

// InvalidateAffinity breaks affinity when account returns 429/401/403
func (m *Manager) InvalidateAffinity(sessionKey, providerID string) {
	if sessionKey == "" || providerID == "" {
		return
	}
	key := providerID + ":" + sessionKey

	m.mu.Lock()
	delete(m.sessions, key)
	m.mu.Unlock()
}

type SessionRecord struct {
	Key       string    `json:"key"`
	AccountID string    `json:"account_id"`
	CreatedAt time.Time `json:"created_at"`
	LastSeen  time.Time `json:"last_seen"`
	AgeSec    int64     `json:"age_sec"`
}

func (m *Manager) ListSessions() []SessionRecord {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var list []SessionRecord
	now := time.Now()
	for k, v := range m.sessions {
		list = append(list, SessionRecord{
			Key:       k,
			AccountID: v.AccountID,
			CreatedAt: v.CreatedAt,
			LastSeen:  v.LastSeen,
			AgeSec:    int64(now.Sub(v.CreatedAt).Seconds()),
		})
	}
	return list
}
