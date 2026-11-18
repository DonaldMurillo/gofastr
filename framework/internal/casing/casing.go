// Package casing converts column names between snake_case and camelCase JSON keys, with bounded caches.
package casing

import (
	"strings"
	"sync"
	"unicode"
)

const maxCacheSize = 4096

var (
	camelCache   = make(map[string]string)
	camelCacheMu sync.RWMutex
)

var (
	snakeCache   = make(map[string]string)
	snakeCacheMu sync.RWMutex
)

func evictOne(m map[string]string) {
	for k := range m {
		delete(m, k)
		return
	}
}

// ToCamel converts snake_case to camelCase ("author_id" to "authorId").
func ToCamel(s string) string {
	if s == "" {
		return s
	}

	camelCacheMu.RLock()
	if v, ok := camelCache[s]; ok {
		camelCacheMu.RUnlock()
		return v
	}
	camelCacheMu.RUnlock()

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

// ToSnake converts camelCase to snake_case ("authorId" to "author_id").
func ToSnake(s string) string {
	if s == "" {
		return s
	}

	snakeCacheMu.RLock()
	if v, ok := snakeCache[s]; ok {
		snakeCacheMu.RUnlock()
		return v
	}
	snakeCacheMu.RUnlock()

	var b strings.Builder
	b.Grow(len(s) + 4)
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

// MapToCamel returns m with every key through ToCamel.
func MapToCamel(m map[string]any) map[string]any {
	result := make(map[string]any, len(m))
	for k, v := range m {
		result[ToCamel(k)] = v
	}
	return result
}

// MapToSnake returns m with every key through ToSnake.
func MapToSnake(m map[string]any) map[string]any {
	result := make(map[string]any, len(m))
	for k, v := range m {
		result[ToSnake(k)] = v
	}
	return result
}

// PrecomputeMapping returns each column's camelCase key, for ApplyMapping.
func PrecomputeMapping(columns []string) map[string]string {
	m := make(map[string]string, len(columns))
	for _, col := range columns {
		m[col] = ToCamel(col)
	}
	return m
}

// ApplyMapping returns row with mapped keys renamed and other keys kept as they are.
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
