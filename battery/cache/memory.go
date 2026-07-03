package cache

import (
	"container/list"
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"
)

// memoryEntry holds a cached item with its expiration time.
type memoryEntry struct {
	data      []byte // JSON-encoded value
	expiresAt time.Time
	hasExpiry bool
	// lru points at this entry's node in the LRU recency list. It is only
	// populated (non-nil) when the cache is bounded (maxEntries > 0).
	lru *list.Element
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
	// lruList orders keys from most- (front) to least-recently-used (back).
	// It is only maintained when the cache is bounded (cfg.maxEntries > 0).
	// Each element's Value is the string key.
	lruList *list.List
}

// NewMemoryCache creates a new in-memory cache.
// The background cleanup goroutine starts automatically when a cleanup
// interval is configured (default: 1 minute). Call Close to stop it.
func NewMemoryCache(opts ...Option) *MemoryCache {
	cfg := applyOptions(opts...)
	// Unbounded AND never-expiring is the OOM shape: every distinct key
	// (especially request-derived ones) is retained forever. Warn loudly —
	// give it a default TTL (WithDefaultTTL) or a size cap (WithMaxEntries).
	if cfg.maxEntries <= 0 && cfg.defaultTTL <= 0 {
		slog.Default().Warn("cache: MemoryCache is both unbounded (no WithMaxEntries) and never-expiring (no WithDefaultTTL) — entries are retained forever, so request-derived keys will grow memory without limit",
			"fix", "set WithDefaultTTL for a finite lifetime, WithMaxEntries for an LRU bound, or both")
	}
	mc := &MemoryCache{
		items:  make(map[string]*memoryEntry),
		cfg:    cfg,
		stopCh: make(chan struct{}),
	}
	if cfg.maxEntries > 0 {
		mc.lruList = list.New()
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
			mc.removeLocked(k, v)
		}
	}
}

// Len reports the number of live entries currently held. Primarily useful for
// tests and for observing eviction on a bounded cache.
func (mc *MemoryCache) Len() int {
	mc.mu.RLock()
	defer mc.mu.RUnlock()
	return len(mc.items)
}

// removeLocked deletes a key from the map and, if the cache is bounded, from
// the LRU list. Callers must hold mc.mu for writing.
func (mc *MemoryCache) removeLocked(key string, entry *memoryEntry) {
	delete(mc.items, key)
	if mc.lruList != nil && entry != nil && entry.lru != nil {
		mc.lruList.Remove(entry.lru)
		entry.lru = nil
	}
}

// touchLocked marks an entry as most-recently-used. Callers must hold mc.mu
// for writing. No-op on an unbounded cache.
func (mc *MemoryCache) touchLocked(entry *memoryEntry) {
	if mc.lruList != nil && entry.lru != nil {
		mc.lruList.MoveToFront(entry.lru)
	}
}

// Get retrieves a value from the cache and deserializes it into dest.
func (mc *MemoryCache) Get(_ context.Context, key string, dest any) error {
	k := mc.prefixedKey(key)

	// Bounded caches must record recency on read, which mutates the LRU
	// list and therefore requires the write lock.
	if mc.lruList != nil {
		mc.mu.Lock()
		entry, ok := mc.items[k]
		if !ok || entry.isExpired() {
			if ok {
				mc.removeLocked(k, entry)
			}
			mc.mu.Unlock()
			return ErrCacheMiss
		}
		mc.touchLocked(entry)
		data := entry.data
		mc.mu.Unlock()
		return json.Unmarshal(data, dest)
	}

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
	if mc.lruList != nil {
		if existing, ok := mc.items[k]; ok {
			// Overwrite in place: reuse the recency node and promote it.
			entry.lru = existing.lru
			existing.lru = nil
			if entry.lru != nil {
				mc.lruList.MoveToFront(entry.lru)
			} else {
				entry.lru = mc.lruList.PushFront(k)
			}
			mc.items[k] = entry
		} else {
			entry.lru = mc.lruList.PushFront(k)
			mc.items[k] = entry
			// Enforce the cap by evicting least-recently-used entries.
			for len(mc.items) > mc.cfg.maxEntries {
				oldest := mc.lruList.Back()
				if oldest == nil {
					break
				}
				ok := oldest.Value.(string)
				mc.removeLocked(ok, mc.items[ok])
			}
		}
	} else {
		mc.items[k] = entry
	}
	mc.mu.Unlock()
	return nil
}

// Delete removes a key from the cache.
func (mc *MemoryCache) Delete(_ context.Context, key string) error {
	k := mc.prefixedKey(key)
	mc.mu.Lock()
	mc.removeLocked(k, mc.items[k])
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
			mc.removeLocked(k, e)
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
	if mc.lruList != nil {
		mc.lruList.Init()
	}
	mc.mu.Unlock()
	return nil
}

func (mc *MemoryCache) prefixedKey(key string) string {
	if mc.cfg.prefix == "" {
		return key
	}
	return mc.cfg.prefix + ":" + key
}
