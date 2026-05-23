// Package casing holds snake_case <-> camelCase helpers used internally by
// the GoFastr framework. Hook payloads are snake_cased; generated structs
// use camelCase JSON tags. These helpers translate between the two.
package casing

import (
	"strings"
	"sync"
	"unicode"
)

// maxCacheSize bounds both casing caches. User-controlled JSON keys can
// flow into the snake cache via crud.unconvertMapKeys, so an unbounded
// map would be a slow memory leak; cap at a size that comfortably covers
// real-world column-name working sets.
const maxCacheSize = 4096

// camelCache caches ToCamel results. Agents often issue the same query
// template repeatedly; cached lookups hit ~50ns / 0 allocs vs ~600ns / 3 allocs
// for a fresh conversion.
var (
	camelCache   = make(map[string]string)
	camelCacheMu sync.RWMutex
)

// snakeCache caches ToSnake results.
var (
	snakeCache   = make(map[string]string)
	snakeCacheMu sync.RWMutex
)

// evictOne removes a single (random) entry from m. Go map iteration order
// is randomized, so taking the first key approximates random eviction with
// zero bookkeeping cost. Caller must hold the write lock for m.
func evictOne(m map[string]string) {
	for k := range m {
		delete(m, k)
		return
	}
}

// ToCamel converts a snake_case string to camelCase.
// e.g. "author_id" -> "authorId", "created_at" -> "createdAt".
// Results are cached for repeat lookups.
func ToCamel(s string) string {
	if s == "" {
		return s
	}

	// Fast path: check cache
	camelCacheMu.RLock()
	if v, ok := camelCache[s]; ok {
		camelCacheMu.RUnlock()
		return v
	}
	camelCacheMu.RUnlock()

	// Slow path: convert and cache
	parts := strings.Split(s, "_")
	for i := 1; i < len(parts); i++ {
		if len(parts[i]) > 0 {
			parts[i] = string(unicode.ToUpper(rune(parts[i][0]))) + parts[i][1:]
		}
	}
	result := strings.Join(parts, "")

	camelCacheMu.Lock()
	if len(camelCache) >= maxCacheSize {
		evictOne(camelCache)
	}
	camelCache[s] = result
	camelCacheMu.Unlock()

	return result
}

// ToSnake converts a camelCase string to snake_case.
// e.g. "authorId" -> "author_id", "createdAt" -> "created_at".
// Results are cached for repeat lookups.
func ToSnake(s string) string {
	if s == "" {
		return s
	}

	// Fast path: check cache
	snakeCacheMu.RLock()
	if v, ok := snakeCache[s]; ok {
		snakeCacheMu.RUnlock()
		return v
	}
	snakeCacheMu.RUnlock()

	// Slow path: convert and cache
	var b strings.Builder
	b.Grow(len(s) + 4) // estimate
	for i, r := range s {
		if unicode.IsUpper(r) {
			if i > 0 {
				b.WriteRune('_')
			}
			b.WriteRune(unicode.ToLower(r))
		} else {
			b.WriteRune(r)
		}
	}
	result := b.String()

	snakeCacheMu.Lock()
	if len(snakeCache) >= maxCacheSize {
		evictOne(snakeCache)
	}
	snakeCache[s] = result
	snakeCacheMu.Unlock()

	return result
}

// MapToCamel converts all snake_case keys in a map to camelCase.
// Reuses the input map's capacity for the result.
func MapToCamel(m map[string]any) map[string]any {
	result := make(map[string]any, len(m))
	for k, v := range m {
		result[ToCamel(k)] = v
	}
	return result
}

// MapToSnake converts all camelCase keys in a map to snake_case.
func MapToSnake(m map[string]any) map[string]any {
	result := make(map[string]any, len(m))
	for k, v := range m {
		result[ToSnake(k)] = v
	}
	return result
}

// PrecomputeMapping builds a snake→camel mapping for a fixed set of
// column names. The returned map can be passed to ApplyMapping for
// zero-alloc key conversion on each row. Call this once at entity
// Define() time.
func PrecomputeMapping(columns []string) map[string]string {
	m := make(map[string]string, len(columns))
	for _, col := range columns {
		m[col] = ToCamel(col)
	}
	return m
}

// ApplyMapping converts keys using a precomputed mapping.
// Skips keys not in the mapping (passes them through as-is).
// Zero allocations for the mapping lookup; one allocation for the result map.
func ApplyMapping(row map[string]any, mapping map[string]string) map[string]any {
	result := make(map[string]any, len(row))
	for k, v := range row {
		if camel, ok := mapping[k]; ok {
			result[camel] = v
		} else {
			result[k] = v
		}
	}
	return result
}
