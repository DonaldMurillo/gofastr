package embed

import (
	"context"
	"strings"
	"testing"
)

// Security tests for PgVectorStore. These assert the fail-closed behaviour
// called out in the brief: a hostile table name is rejected at construction,
// and filter values with SQL metacharacters are parameterized so no injection
// is possible. They mirror the SQL-injection posture of
// battery/search/injection_security_test.go.

// TestPgVectorHostileTable: a table name carrying a SQL injection payload must
// be rejected at construction by core/query.SafeIdent, before it ever reaches
// a query.
func TestPgVectorHostileTable(t *testing.T) {
	db := openPgVector(t)
	hostile := []string{
		"chunks; DROP TABLE embed_chunks; --",
		`embed"; --`,
		"bad name with spaces",
		"semicolon;table",
		"weird'name",
		"select*from",
	}
	for _, name := range hostile {
		_, err := NewPgVector(db, PgVectorConfig{Table: name, Dim: 3})
		if err == nil {
			t.Errorf("NewPgVector(table=%q) unexpectedly succeeded", name)
		}
	}
	// Sanity: a clean name is accepted.
	if _, err := NewPgVector(db, PgVectorConfig{Table: "embed_chunks", Dim: 3}); err != nil {
		t.Errorf("NewPgVector(clean name) errored: %v", err)
	}
}

// TestPgVectorFilterNoInjection: filter values containing SQL metacharacters
// are bound as parameters and matched literally — they cannot break out of the
// query, match everything, or error.
func TestPgVectorFilterNoInjection(t *testing.T) {
	s := newPgStore(t, 3)
	ctx := context.Background()
	if err := s.Add(ctx, []Chunk{
		{ID: "a", DocID: "d1", Source: "real", Text: "x", Vec: vec3(1, 0, 0)},
	}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	hostile := []string{
		"' OR '1'='1",
		"'; DROP TABLE embed_chunks; --",
		"%",
		"_",
		"x' UNION SELECT 1--",
	}
	for _, val := range hostile {
		hits, err := s.Candidates(ctx, vec3(1, 0, 0), Filter{Source: val}, 10)
		if err != nil {
			t.Errorf("Candidates with hostile Source=%q errored: %v", val, err)
			continue
		}
		// None of the payloads equal "real", so zero hits must come back —
		// proving the value was treated as a literal, not as SQL.
		if len(hits) != 0 {
			t.Errorf("hostile Source=%q returned %d hits, want 0", val, len(hits))
		}
	}
	// Sanity: the real value still matches.
	hits, err := s.Candidates(ctx, vec3(1, 0, 0), Filter{Source: "real"}, 10)
	if err != nil {
		t.Fatalf("Candidates real: %v", err)
	}
	if len(hits) != 1 {
		t.Errorf("Source=real returned %d hits, want 1", len(hits))
	}
}

// TestPgVectorMetaMatchNoInjection: a hostile MetaMatch key/value cannot break
// out of the JSONB containment predicate.
func TestPgVectorMetaMatchNoInjection(t *testing.T) {
	s := newPgStore(t, 3)
	ctx := context.Background()
	if err := s.Add(ctx, []Chunk{
		{ID: "a", DocID: "d1", Text: "x", Vec: vec3(1, 0, 0), Metadata: map[string]any{"tenant": "acme"}},
	}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	// Hostile values: each must be JSON-encoded and matched literally, never
	// matching the stored {"tenant":"acme"} row.
	hostile := []map[string]any{
		{"tenant": "' OR '1'='1"},
		{"tenant": "acme\" OR true"},
		{"tenant OR true": "x"},
		{"tenant": "acme'; --"},
	}
	for _, m := range hostile {
		hits, err := s.Candidates(ctx, vec3(1, 0, 0), Filter{MetaMatch: m}, 10)
		if err != nil {
			t.Errorf("Candidates with hostile MetaMatch=%v errored: %v", m, err)
			continue
		}
		if len(hits) != 0 {
			t.Errorf("hostile MetaMatch=%v returned %d hits, want 0", m, len(hits))
		}
	}
	// The real match still works.
	hits, err := s.Candidates(ctx, vec3(1, 0, 0), Filter{MetaMatch: map[string]any{"tenant": "acme"}}, 10)
	if err != nil {
		t.Fatalf("Candidates real MetaMatch: %v", err)
	}
	if len(hits) != 1 {
		t.Errorf("MetaMatch tenant=acme returned %d hits, want 1", len(hits))
	}
}

// TestPgVectorDimMismatch: Add rejects a vector whose length != configured Dim.
func TestPgVectorDimMismatch(t *testing.T) {
	s := newPgStore(t, 3)
	err := s.Add(context.Background(), []Chunk{
		{ID: "a", DocID: "d1", Text: "x", Vec: []float32{1, 0}}, // dim 2, want 3
	})
	if err == nil || !strings.Contains(err.Error(), "dimension") {
		t.Fatalf("expected dimension error, got %v", err)
	}
}
