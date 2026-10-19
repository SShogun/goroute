package antigravity

import (
	"sync"
	"time"
)

const (
	maxThoughtSignatures = 2000
	thoughtSignatureTTL  = 30 * time.Minute
)

type thoughtEntry struct {
	signature string
	expiresAt time.Time
}

// ThoughtStore provides a thread-safe LRU/TTL cache for Gemini 3+ and Vertex thought signatures
type ThoughtStore struct {
	mu      sync.RWMutex
	entries map[string]thoughtEntry
}

var globalThoughtStore = &ThoughtStore{
	entries: make(map[string]thoughtEntry),
}

// StoreThoughtSignature records a thought signature associated with a tool call ID
func StoreThoughtSignature(toolCallID string, signature string) {
	if toolCallID == "" || signature == "" {
		return
	}
	globalThoughtStore.mu.Lock()
	defer globalThoughtStore.mu.Unlock()

	// Prune if over capacity
	if len(globalThoughtStore.entries) >= maxThoughtSignatures {
		now := time.Now()
		for k, v := range globalThoughtStore.entries {
			if v.expiresAt.Before(now) {
				delete(globalThoughtStore.entries, k)
			}
		}
		// If still over capacity, remove an arbitrary entry
		if len(globalThoughtStore.entries) >= maxThoughtSignatures {
			for k := range globalThoughtStore.entries {
				delete(globalThoughtStore.entries, k)
				break
			}
		}
	}

	globalThoughtStore.entries[toolCallID] = thoughtEntry{
		signature: signature,
		expiresAt: time.Now().Add(thoughtSignatureTTL),
	}
}

// GetThoughtSignature retrieves the stored thought signature for a tool call ID
func GetThoughtSignature(toolCallID string) string {
	if toolCallID == "" {
		return ""
	}
	globalThoughtStore.mu.RLock()
	entry, ok := globalThoughtStore.entries[toolCallID]
	globalThoughtStore.mu.RUnlock()

	if !ok {
		return ""
	}

	if time.Now().After(entry.expiresAt) {
		globalThoughtStore.mu.Lock()
		delete(globalThoughtStore.entries, toolCallID)
		globalThoughtStore.mu.Unlock()
		return ""
	}

	return entry.signature
}
