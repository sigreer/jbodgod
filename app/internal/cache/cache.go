package cache

import (
	"sync"
	"time"
)

// TTL constants for different data types
const (
	// Static data - never changes unless hardware swapped
	TTLStatic = 24 * time.Hour

	// Slow-moving - firmware versions, enclosure config
	TTLSlow = 1 * time.Hour

	// Medium - ZFS pool membership, drive assignments
	TTLMedium = 5 * time.Minute

	// Fast - drive state (active/standby)
	TTLFast = 5 * time.Second

	// Dynamic - temperatures (fetched on demand with configurable interval)
	TTLDynamic = 30 * time.Second
)

// CacheEntry holds a cached value with expiration
type CacheEntry struct {
	Value     interface{}
	ExpiresAt time.Time
	FetchedAt time.Time
}

// IsExpired returns true if the entry has expired
func (e *CacheEntry) IsExpired() bool {
	return time.Now().After(e.ExpiresAt)
}

// Age returns how long ago the entry was fetched
func (e *CacheEntry) Age() time.Duration {
	return time.Since(e.FetchedAt)
}

// Cache provides thread-safe TTL-based caching
type Cache struct {
	mu      sync.RWMutex
	entries map[string]*CacheEntry
}

// New creates a new cache instance
func New() *Cache {
	return &Cache{
		entries: make(map[string]*CacheEntry),
	}
}

// Get retrieves a value from cache, returns nil if expired or not found
func (c *Cache) Get(key string) interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()

	entry, ok := c.entries[key]
	if !ok || entry.IsExpired() {
		return nil
	}
	return entry.Value
}

// GetEntry retrieves the full cache entry (for checking age, etc.)
func (c *Cache) GetEntry(key string) *CacheEntry {
	c.mu.RLock()
	defer c.mu.RUnlock()

	entry, ok := c.entries[key]
	if !ok {
		return nil
	}
	return entry
}

// Set stores a value with the given TTL
func (c *Cache) Set(key string, value interface{}, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.entries[key] = &CacheEntry{
		Value:     value,
		ExpiresAt: time.Now().Add(ttl),
		FetchedAt: time.Now(),
	}
}

// SetStatic stores static data (very long TTL)
func (c *Cache) SetStatic(key string, value interface{}) {
	c.Set(key, value, TTLStatic)
}

// SetSlow stores slow-moving data
func (c *Cache) SetSlow(key string, value interface{}) {
	c.Set(key, value, TTLSlow)
}

// SetMedium stores medium-refresh data
func (c *Cache) SetMedium(key string, value interface{}) {
	c.Set(key, value, TTLMedium)
}

// SetFast stores fast-refresh data
func (c *Cache) SetFast(key string, value interface{}) {
	c.Set(key, value, TTLFast)
}

// SetDynamic stores dynamic data
func (c *Cache) SetDynamic(key string, value interface{}) {
	c.Set(key, value, TTLDynamic)
}

// Delete removes an entry from cache
func (c *Cache) Delete(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.entries, key)
}

// Clear removes all entries from cache
func (c *Cache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = make(map[string]*CacheEntry)
}

// Keys returns all cache keys (for debugging)
func (c *Cache) Keys() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	keys := make([]string, 0, len(c.entries))
	for k := range c.entries {
		keys = append(keys, k)
	}
	return keys
}

// Cleanup removes expired entries
func (c *Cache) Cleanup() {
	c.mu.Lock()
	defer c.mu.Unlock()

	for k, v := range c.entries {
		if v.IsExpired() {
			delete(c.entries, k)
		}
	}
}

// Global cache instance
var global *Cache
var once sync.Once

// Global returns the global cache instance
func Global() *Cache {
	once.Do(func() {
		global = New()
	})
	return global
}
