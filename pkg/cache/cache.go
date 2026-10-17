package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sync"
	"time"

	"github.com/ziuus/goroute/pkg/providers"
)

type cacheItem struct {
	data      []byte
	expiresAt time.Time
}

type Cache struct {
	mu         sync.RWMutex
	items      map[string]cacheItem
	ttl        time.Duration
	maxEntries int
}

func NewCache(ttl time.Duration, maxEntries int) *Cache {
	if ttl <= 0 {
		ttl = 15 * time.Minute
	}
	if maxEntries <= 0 {
		maxEntries = 1000
	}
	c := &Cache{
		items:      make(map[string]cacheItem),
		ttl:        ttl,
		maxEntries: maxEntries,
	}

	// Background eviction every 5 minutes
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		for range ticker.C {
			c.evictExpired()
		}
	}()

	return c
}

func (c *Cache) GenerateKey(req *providers.ChatRequest) string {
	hasher := sha256.New()
	hasher.Write([]byte(req.Model))
	msgBytes, _ := json.Marshal(req.Messages)
	hasher.Write(msgBytes)
	if req.Temperature != nil {
		tempBytes, _ := json.Marshal(*req.Temperature)
		hasher.Write(tempBytes)
	}
	return hex.EncodeToString(hasher.Sum(nil))
}

func (c *Cache) Get(key string) ([]byte, bool) {
	c.mu.RLock()
	item, found := c.items[key]
	c.mu.RUnlock()

	if !found {
		return nil, false
	}

	if time.Now().After(item.expiresAt) {
		c.mu.Lock()
		delete(c.items, key)
		c.mu.Unlock()
		return nil, false
	}

	return item.data, true
}

func (c *Cache) Set(key string, data []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// If over capacity, drop expired or random entry
	if len(c.items) >= c.maxEntries {
		for k, v := range c.items {
			if time.Now().After(v.expiresAt) {
				delete(c.items, k)
				break
			}
		}
		// If still over capacity, delete one item
		if len(c.items) >= c.maxEntries {
			for k := range c.items {
				delete(c.items, k)
				break
			}
		}
	}

	c.items[key] = cacheItem{
		data:      data,
		expiresAt: time.Now().Add(c.ttl),
	}
}

func (c *Cache) evictExpired() {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	for k, v := range c.items {
		if now.After(v.expiresAt) {
			delete(c.items, k)
		}
	}
}

func (c *Cache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items = make(map[string]cacheItem)
}

func (c *Cache) Size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.items)
}
