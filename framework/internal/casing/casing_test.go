package casing

import (
	"fmt"
	"testing"
)

func TestSnakeCacheBounded(t *testing.T) {
	// Reset cache state.
	snakeCacheMu.Lock()
	snakeCache = make(map[string]string)
	snakeCacheMu.Unlock()

	for i := 0; i < 10000; i++ {
		ToSnake(fmt.Sprintf("uniqueKey%d", i))
	}

	snakeCacheMu.RLock()
	size := len(snakeCache)
	snakeCacheMu.RUnlock()
	if size > maxCacheSize {
		t.Fatalf("snakeCache size = %d, want <= %d", size, maxCacheSize)
	}
}

func TestCamelCacheBounded(t *testing.T) {
	camelCacheMu.Lock()
	camelCache = make(map[string]string)
	camelCacheMu.Unlock()

	for i := 0; i < 10000; i++ {
		ToCamel(fmt.Sprintf("unique_key_%d", i))
	}

	camelCacheMu.RLock()
	size := len(camelCache)
	camelCacheMu.RUnlock()
	if size > maxCacheSize {
		t.Fatalf("camelCache size = %d, want <= %d", size, maxCacheSize)
	}
}

func TestSnakeStillConverts(t *testing.T) {
	if got := ToSnake("authorId"); got != "author_id" {
		t.Fatalf("ToSnake(authorId) = %q", got)
	}
	if got := ToCamel("author_id"); got != "authorId" {
		t.Fatalf("ToCamel(author_id) = %q", got)
	}
}
