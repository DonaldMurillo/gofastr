package dsl

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/filter"
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
	// Parse the same query twice, second should be a cache hit
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
	for i := range maxParseCacheSize + 10 {
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

func TestParseDSLRejectsHugeInput(t *testing.T) {
	huge := "Post.where(name=\"" + strings.Repeat("a", 2*1024*1024) + "\")"
	if _, err := ParseDSL(huge); err == nil {
		t.Fatal("expected ParseDSL to reject 2MB input")
	}
}

// TestContainsUsesCanonicalLikeEscape pins the `contains` operator onto
// the canonical framework/filter LIKE helpers (EscapeLikePattern +
// LikeEscapeSuffix): the caller's LIKE wildcards (%, _) and the escape
// char are escaped literally, the pattern is wrapped in %…%, and the
// fragment carries the ESCAPE clause. dsl used to re-implement the
// replacer privately; this test pins the exact emitted SQL + args so
// the dedup onto filter's exported helpers cannot drift behavior.
func TestContainsUsesCanonicalLikeEscape(t *testing.T) {
	cond, args, err := dslCondition(schema.Field{Name: "title", Type: schema.String}, "contains", `50%_off\x`)
	if err != nil {
		t.Fatalf("dslCondition: %v", err)
	}
	wantSQL := `title LIKE $1` + filter.LikeEscapeSuffix
	if cond != wantSQL {
		t.Errorf("contains SQL = %q, want %q", cond, wantSQL)
	}
	if len(args) != 1 {
		t.Fatalf("args = %v, want 1", args)
	}
	if got, ok := args[0].(string); !ok || got != `%50\%\_off\\x%` {
		t.Errorf("contains arg = %#v, want %%50\\%%\\_off\\\\x%% (wildcards + escape char escaped, wrapped)", args[0])
	}
}
