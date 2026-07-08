package search

import (
	"context"
	"testing"
)

// TestMemoryFieldEqualsScopesByTenant is the tenant-scoping parity case: two
// documents with identical text but different tenant fields must return only
// the one whose tenant matches the filter. The Postgres backend is exercised
// against the identical scenario in postgres_test.go — both must agree.
func TestMemoryFieldEqualsScopesByTenant(t *testing.T) {
	ctx := context.Background()
	idx := NewMemory()
	_ = idx.Index(ctx, Document{
		ID:     "a",
		Type:   "posts",
		Text:   "GoFastr release notes",
		Fields: map[string]any{"tenant": "acme"},
	})
	_ = idx.Index(ctx, Document{
		ID:     "b",
		Type:   "posts",
		Text:   "GoFastr release notes",
		Fields: map[string]any{"tenant": "globex"},
	})

	res, err := idx.Search(ctx, Query{Text: "gofastr", FieldEquals: map[string]string{"tenant": "acme"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 1 || res[0].Document.ID != "a" {
		t.Fatalf("FieldEquals tenant=acme: got %#v, want only doc a", res)
	}

	// No filter returns both.
	res, err = idx.Search(ctx, Query{Text: "gofastr"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 2 {
		t.Fatalf("no FieldEquals: got %d results, want 2", len(res))
	}

	// Unknown tenant value returns nothing.
	res, err = idx.Search(ctx, Query{Text: "gofastr", FieldEquals: map[string]string{"tenant": "nope"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 0 {
		t.Fatalf("unknown tenant: got %d results, want 0", len(res))
	}
}

// TestMemoryFieldEqualsRequiresMultiplePairs verifies AND semantics: every
// key/value pair must match.
func TestMemoryFieldEqualsRequiresMultiplePairs(t *testing.T) {
	ctx := context.Background()
	idx := NewMemory()
	_ = idx.Index(ctx, Document{
		ID:     "1",
		Text:   "shared body",
		Fields: map[string]any{"tenant": "acme", "status": "published"},
	})
	_ = idx.Index(ctx, Document{
		ID:     "2",
		Text:   "shared body",
		Fields: map[string]any{"tenant": "acme", "status": "draft"},
	})

	res, err := idx.Search(ctx, Query{
		Text:        "shared",
		FieldEquals: map[string]string{"tenant": "acme", "status": "published"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 1 || res[0].Document.ID != "1" {
		t.Fatalf("AND of two pairs: got %#v, want only doc 1", res)
	}
}

// TestMemoryFieldEqualsStringOnly is the shared matching rule: a field whose
// value is not a string never satisfies a FieldEquals pair, even if its
// fmt.Sprint form would match. The Postgres backend mirrors this via JSONB
// type-strict containment.
func TestMemoryFieldEqualsStringOnly(t *testing.T) {
	ctx := context.Background()
	idx := NewMemory()
	_ = idx.Index(ctx, Document{
		ID:     "1",
		Text:   "body",
		Fields: map[string]any{"count": 42}, // non-string
	})
	res, err := idx.Search(ctx, Query{
		Text:        "body",
		FieldEquals: map[string]string{"count": "42"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 0 {
		t.Fatalf("non-string field matched FieldEquals: got %#v, want 0", res)
	}
}
