package dsl

import (
	"strings"
	"testing"
)

func TestParseDSLEntity(t *testing.T) {
	q, err := ParseDSL("Post")
	if err != nil {
		t.Fatalf("ParseDSL: %v", err)
	}
	if q.Entity != "Post" {
		t.Fatalf("Entity = %q, want %q", q.Entity, "Post")
	}
}

func TestParseDSLWithWhere(t *testing.T) {
	q, err := ParseDSL(`Post.where(status="published")`)
	if err != nil {
		t.Fatalf("ParseDSL: %v", err)
	}
	if q.Entity != "Post" {
		t.Fatalf("Entity = %q, want %q", q.Entity, "Post")
	}
	if len(q.Filters) != 1 {
		t.Fatalf("Filters = %d, want 1", len(q.Filters))
	}
	if q.Filters[0].Field != "status" {
		t.Fatalf("Filter field = %q, want %q", q.Filters[0].Field, "status")
	}
	if q.Filters[0].Operator != "=" {
		t.Fatalf("Filter operator = %q, want %q", q.Filters[0].Operator, "=")
	}
	if q.Filters[0].Value != "published" {
		t.Fatalf("Filter value = %q, want %q", q.Filters[0].Value, "published")
	}
}

func TestParseDSLWithInclude(t *testing.T) {
	q, err := ParseDSL("Post.include(author,tags)")
	if err != nil {
		t.Fatalf("ParseDSL: %v", err)
	}
	if len(q.Includes) != 2 {
		t.Fatalf("Includes = %d, want 2", len(q.Includes))
	}
	if q.Includes[0] != "author" || q.Includes[1] != "tags" {
		t.Fatalf("Includes = %v, want [author tags]", q.Includes)
	}
}

func TestParseDSLWithOrder(t *testing.T) {
	q, err := ParseDSL("Post.order(created_at DESC)")
	if err != nil {
		t.Fatalf("ParseDSL: %v", err)
	}
	if len(q.Orders) != 1 {
		t.Fatalf("Orders = %d, want 1", len(q.Orders))
	}
	if q.Orders[0].Field != "created_at" {
		t.Fatalf("Order field = %q, want %q", q.Orders[0].Field, "created_at")
	}
	if q.Orders[0].Direction != "DESC" {
		t.Fatalf("Order direction = %q, want %q", q.Orders[0].Direction, "DESC")
	}
}

func TestParseDSLWithLimit(t *testing.T) {
	q, err := ParseDSL("Post.limit(10)")
	if err != nil {
		t.Fatalf("ParseDSL: %v", err)
	}
	if q.Limit != 10 {
		t.Fatalf("Limit = %d, want 10", q.Limit)
	}
}

func TestParseDSLComplex(t *testing.T) {
	q, err := ParseDSL(`Post.where(status="published").include(author).order(created_at DESC).limit(10)`)
	if err != nil {
		t.Fatalf("ParseDSL: %v", err)
	}
	if q.Entity != "Post" {
		t.Fatalf("Entity = %q", q.Entity)
	}
	if len(q.Filters) != 1 {
		t.Fatalf("Filters = %d", len(q.Filters))
	}
	if len(q.Includes) != 1 {
		t.Fatalf("Includes = %d", len(q.Includes))
	}
	if len(q.Orders) != 1 {
		t.Fatalf("Orders = %d", len(q.Orders))
	}
	if q.Limit != 10 {
		t.Fatalf("Limit = %d", q.Limit)
	}
}

func TestParseDSLEmpty(t *testing.T) {
	_, err := ParseDSL("")
	if err == nil {
		t.Fatal("expected error for empty input")
	}
}

func TestParseDSLUnknownCall(t *testing.T) {
	_, err := ParseDSL("Post.foo(bar)")
	if err == nil {
		t.Fatal("expected error for unknown call")
	}
	if !strings.Contains(err.Error(), "unknown call") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParseDSLCacheHit(t *testing.T) {
	// Parse the same query twice — second should be a cache hit
	input := `Post.where(status="published").limit(10)`
	q1, err := ParseDSL(input)
	if err != nil {
		t.Fatalf("first ParseDSL: %v", err)
	}
	q2, err := ParseDSL(input)
	if err != nil {
		t.Fatalf("second ParseDSL (cached): %v", err)
	}
	if q1.Entity != q2.Entity {
		t.Fatalf("cached Entity mismatch: %q != %q", q1.Entity, q2.Entity)
	}
	if q1.Limit != q2.Limit {
		t.Fatalf("cached Limit mismatch: %d != %d", q1.Limit, q2.Limit)
	}
}

func TestParseDSLCacheBounded(t *testing.T) {
	// Fill cache beyond maxParseCacheSize and verify it doesn't grow unbounded
	for i := 0; i < maxParseCacheSize+10; i++ {
		input := strings.Repeat("x", i+1) + ".limit(1)"
		_, err := ParseDSL(input)
		if err != nil {
			// Some inputs may be too short for entity parsing, skip those
			continue
		}
	}
	parseCacheMu.RLock()
	size := len(parseCache)
	parseCacheMu.RUnlock()
	if size > maxParseCacheSize {
		t.Fatalf("cache size = %d, should be <= %d", size, maxParseCacheSize)
	}
}

func TestParseDSLWhitespace(t *testing.T) {
	q, err := ParseDSL("  Post.limit(5)  ")
	if err != nil {
		t.Fatalf("ParseDSL: %v", err)
	}
	if q.Entity != "Post" {
		t.Fatalf("Entity = %q", q.Entity)
	}
	if q.Limit != 5 {
		t.Fatalf("Limit = %d, want 5", q.Limit)
	}
}
