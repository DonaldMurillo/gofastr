package cache

import (
	"context"
	"encoding/json"
	"sync"
	"time"
)

// memoryEntry holds a cached item with its expiration time.
type memoryEntry struct {
	data      []byte // JSON-encoded value
	expiresAt time.Time
	hasExpiry bool
}

func (e *memoryEntry) isExpired() bool {
	return e.hasExpiry && time.Now().After(e.expiresAt)
}

// MemoryCache is an in-memory Cache implementation backed by a map with
// read-write mutex for thread safety. It supports TTL-based expiration
// with lazy eviction on access and an optional background cleanup goroutine.
type MemoryCache struct {
	mu       sync.RWMutex
	items    map[string]*memoryEntry
	cfg      config
	stopCh   chan struct{}
	stopOnce sync.Once
}

// NewMemoryCache creates a new in-memory cache.
// The background cleanup goroutine starts automatically when a cleanup
// interval is configured (default: 1 minute). Call Close to stop it.
func NewMemoryCache(opts ...Option) *MemoryCache {
	cfg := applyOptions(opts...)
	mc := &MemoryCache{
		items:  make(map[string]*memoryEntry),
		cfg:    cfg,
		stopCh: make(chan struct{}),
	}
	if cfg.cleanupInterval > 0 {
		go mc.cleanupLoop()
	}
	return mc
}

// Close stops the background cleanup goroutine.
func (mc *MemoryCache) Close() {
	mc.stopOnce.Do(func() {
		close(mc.stopCh)
	})
}

func (mc *MemoryCache) cleanupLoop() {
	ticker := time.NewTicker(mc.cfg.cleanupInterval)
	defer ticker.Stop()
	for {
		select {
		case <-mc.stopCh:
			return
		case <-ticker.C:
			mc.evictExpired()
		}
	}
}

func (mc *MemoryCache) evictExpired() {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	now := time.Now()
	for k, v := range mc.items {
		if v.hasExpiry && now.After(v.expiresAt) {
			delete(mc.items, k)
		}
	}
}

// Get retrieves a value from the cache and deserializes it into dest.
func (mc *MemoryCache) Get(_ context.Context, key string, dest any) error {
	k := mc.prefixedKey(key)
	mc.mu.RLock()
	entry, ok := mc.items[k]
	mc.mu.RUnlock()

	if !ok || entry.isExpired() {
		if ok && entry.isExpired() {
			// Lazily remove the expired entry.
			mc.mu.Lock()
			// Re-check under write lock.
			if e, still := mc.items[k]; still && e.isExpired() {
				delete(mc.items, k)
			}
			mc.mu.Unlock()
		}
		return ErrCacheMiss
	}
	return json.Unmarshal(entry.data, dest)
}

// Set stores a value in the cache with the given TTL.
func (mc *MemoryCache) Set(_ context.Context, key string, value any, ttl time.Duration) error {
	k := mc.prefixedKey(key)
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}

	effectiveTTL := ttl
	if effectiveTTL == 0 {
		effectiveTTL = mc.cfg.defaultTTL
	}

	entry := &memoryEntry{
		data:      data,
		hasExpiry: effectiveTTL > 0,
	}
	if entry.hasExpiry {
		entry.expiresAt = time.Now().Add(effectiveTTL)
	}

	mc.mu.Lock()
	mc.items[k] = entry
	mc.mu.Unlock()
	return nil
}

// Delete removes a key from the cache.
func (mc *MemoryCache) Delete(_ context.Context, key string) error {
	k := mc.prefixedKey(key)
	mc.mu.Lock()
	delete(mc.items, k)
	mc.mu.Unlock()
	return nil
}

// Exists checks whether a key exists and has not expired.
func (mc *MemoryCache) Exists(_ context.Context, key string) (bool, error) {
	k := mc.prefixedKey(key)
	mc.mu.RLock()
	entry, ok := mc.items[k]
	mc.mu.RUnlock()

	if !ok {
		return false, nil
	}
	if entry.isExpired() {
		// Lazily remove.
		mc.mu.Lock()
		if e, still := mc.items[k]; still && e.isExpired() {
			delete(mc.items, k)
		}
		mc.mu.Unlock()
		return false, nil
	}
	return true, nil
}

// Clear removes all entries from the cache.
func (mc *MemoryCache) Clear(_ context.Context) error {
	mc.mu.Lock()
	mc.items = make(map[string]*memoryEntry)
	mc.mu.Unlock()
	return nil
}

func (mc *MemoryCache) prefixedKey(key string) string {
	if mc.cfg.prefix == "" {
		return key
	}
	return mc.cfg.prefix + ":" + key
}
