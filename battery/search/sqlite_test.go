package search

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

// SQLite FTS5 integration tests for SQLiteFTS. These tests open an in-memory
// SQLite database and probe FTS5 availability at runtime — if the
// mattn/go-sqlite3 driver was compiled without the sqlite_fts5 build tag,
// every test skips individually so the default `go test ./...` stays green.
//
// Run with FTS5: go test -tags sqlite_fts5 ./battery/search/

// openSQLiteDB returns a fresh in-memory SQLite database with a single-conn
// pool (so :memory: data persists across statements on the same conn).
func openSQLiteDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	return db
}

// sqliteFTS5Available probes whether the SQLite driver supports FTS5 by
// trying to create a throwaway virtual table.
func sqliteFTS5Available(db *sql.DB) error {
	_, err := db.Exec(`CREATE VIRTUAL TABLE IF NOT EXISTS fts5_probe USING fts5(x)`)
	if err != nil {
		return err
	}
	_, _ = db.Exec(`DROP TABLE IF EXISTS fts5_probe`)
	return nil
}

// newSQLiteFTS builds a SQLiteFTS on a fresh in-memory database and ensures
// the schema exists. Skips the test when FTS5 is unavailable.
func newSQLiteFTS(t *testing.T) *SQLiteFTS {
	t.Helper()
	db := openSQLiteDB(t)
	if err := sqliteFTS5Available(db); err != nil {
		t.Skipf("FTS5 not available in this SQLite build: %v "+
			"(rebuild with -tags sqlite_fts5)", err)
	}
	idx, err := NewSQLiteFTS(db, SQLiteFTSConfig{})
	if err != nil {
		t.Fatalf("NewSQLiteFTS: %v", err)
	}
	if err := idx.EnsureSchema(context.Background()); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}
	return idx
}

func TestSQLiteEnsureSchemaIdempotent(t *testing.T) {
	idx := newSQLiteFTS(t)
	// Calling again on the already-created virtual table must be a no-op.
	if err := idx.EnsureSchema(context.Background()); err != nil {
		t.Fatalf("second EnsureSchema: %v", err)
	}
}

func TestSQLiteIndexSearchRoundTrip(t *testing.T) {
	ctx := context.Background()
	idx := newSQLiteFTS(t)
	if err := idx.Index(ctx, Document{
		ID: "1", Type: "posts", Text: "GoFastr framework release notes",
	}); err != nil {
		t.Fatal(err)
	}
	if err := idx.Index(ctx, Document{
		ID: "2", Type: "posts", Text: "Unrelated draft",
	}); err != nil {
		t.Fatal(err)
	}

	res, err := idx.Search(ctx, Query{Text: "gofastr", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 1 || res[0].Document.ID != "1" {
		t.Fatalf("got %#v, want only doc 1", res)
	}

	// Re-index the same id: replace (DELETE + INSERT), no duplicate row.
	if err := idx.Index(ctx, Document{
		ID: "1", Type: "posts", Text: "GoFastr framework release notes v2",
	}); err != nil {
		t.Fatal(err)
	}
	res, err = idx.Search(ctx, Query{Text: "gofastr", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 1 {
		t.Fatalf("after re-index got %d results, want 1 (no dup)", len(res))
	}
	// Updated text is now searchable.
	res, err = idx.Search(ctx, Query{Text: "v2"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 1 || res[0].Document.ID != "1" {
		t.Fatalf("updated text not found: %#v", res)
	}
}

func TestSQLiteDelete(t *testing.T) {
	ctx := context.Background()
	idx := newSQLiteFTS(t)
	_ = idx.Index(ctx, Document{ID: "1", Text: "hello world"})
	if err := idx.Delete(ctx, "1"); err != nil {
		t.Fatal(err)
	}
	res, err := idx.Search(ctx, Query{Text: "hello"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 0 {
		t.Fatalf("after delete got %d results, want 0", len(res))
	}
}

// TestSQLiteRankOrder: a document where the query term appears more
// frequently must rank higher (more negative bm25 → higher score).
func TestSQLiteRankOrder(t *testing.T) {
	ctx := context.Background()
	idx := newSQLiteFTS(t)
	_ = idx.Index(ctx, Document{ID: "frequent", Text: "pagination pagination pagination details overview"})
	_ = idx.Index(ctx, Document{ID: "sparse", Text: "pagination overview and summary notes"})

	res, err := idx.Search(ctx, Query{Text: "pagination"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 2 {
		t.Fatalf("got %d results, want 2", len(res))
	}
	if res[0].Document.ID != "frequent" {
		t.Fatalf("frequent doc should rank first: got order %s, %s (scores %v, %v)",
			res[0].Document.ID, res[1].Document.ID, res[0].Score, res[1].Score)
	}
	if res[0].Score <= res[1].Score {
		t.Fatalf("frequent score %v must exceed sparse score %v", res[0].Score, res[1].Score)
	}
}

func TestSQLiteTypeFilter(t *testing.T) {
	ctx := context.Background()
	idx := newSQLiteFTS(t)
	_ = idx.Index(ctx, Document{ID: "1", Type: "posts", Text: "gofastr release"})
	_ = idx.Index(ctx, Document{ID: "2", Type: "users", Text: "gofastr maintainer"})

	res, err := idx.Search(ctx, Query{Text: "gofastr", Type: "posts"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 1 || res[0].Document.ID != "1" {
		t.Fatalf("type filter: got %#v, want only doc 1", res)
	}
}

// TestSQLiteFieldEqualsTenantScope: two docs same text, different tenant —
// the parity case shared with the Memory and Postgres backends.
func TestSQLiteFieldEqualsTenantScope(t *testing.T) {
	ctx := context.Background()
	idx := newSQLiteFTS(t)
	_ = idx.Index(ctx, Document{
		ID: "a", Type: "posts", Text: "GoFastr release notes",
		Fields: map[string]any{"tenant": "acme"},
	})
	_ = idx.Index(ctx, Document{
		ID: "b", Type: "posts", Text: "GoFastr release notes",
		Fields: map[string]any{"tenant": "globex"},
	})

	res, err := idx.Search(ctx, Query{Text: "gofastr", FieldEquals: map[string]string{"tenant": "acme"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 1 || res[0].Document.ID != "a" {
		t.Fatalf("tenant=acme: got %#v, want only doc a", res)
	}
	// Reconstructed Fields round-trip the tenant value.
	if res[0].Document.Fields["tenant"] != "acme" {
		t.Fatalf("fields did not round-trip: %#v", res[0].Document.Fields)
	}
}

// TestSQLiteFieldEqualsStringOnly is the shared matching rule: a field whose
// value is not a string never satisfies a FieldEquals pair, even if its
// fmt.Sprint form would match. The SQLite backend encodes this as
// json_extract vs a TEXT parameter — values of different JSON storage
// classes are never equal in SQLite.
func TestSQLiteFieldEqualsStringOnly(t *testing.T) {
	ctx := context.Background()
	idx := newSQLiteFTS(t)
	_ = idx.Index(ctx, Document{
		ID: "1", Text: "hello world",
		Fields: map[string]any{"count": 42}, // numeric
	})
	// A numeric field value never satisfies a string FieldEquals pair.
	res, err := idx.Search(ctx, Query{Text: "hello", FieldEquals: map[string]string{"count": "42"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 0 {
		t.Fatalf("numeric field should not match string filter: got %#v", res)
	}
	// A string field with the same textual value does match.
	_ = idx.Index(ctx, Document{
		ID: "2", Text: "hello world",
		Fields: map[string]any{"count": "42"}, // string
	})
	res, err = idx.Search(ctx, Query{Text: "hello", FieldEquals: map[string]string{"count": "42"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 1 || res[0].Document.ID != "2" {
		t.Fatalf("string field should match: got %#v", res)
	}
}

func TestSQLitePrefixMatch(t *testing.T) {
	ctx := context.Background()
	idx := newSQLiteFTS(t)
	_ = idx.Index(ctx, Document{ID: "1", Text: "pagination helpers and utilities"})
	_ = idx.Index(ctx, Document{ID: "2", Text: "panel discussion"})

	res, err := idx.Search(ctx, Query{Text: "pagin"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 1 || res[0].Document.ID != "1" {
		t.Fatalf("prefix pagin: got %#v, want only doc 1", res)
	}
}

// TestSQLiteAdversarialQuery: hostile query strings must neither error nor
// match every document. The FTS5 query builder strips every operator and
// SQL metacharacter before the text reaches MATCH.
func TestSQLiteAdversarialQuery(t *testing.T) {
	ctx := context.Background()
	idx := newSQLiteFTS(t)
	for i := 0; i < 3; i++ {
		_ = idx.Index(ctx, Document{
			ID:   fmt.Sprintf("safe-%d", i),
			Text: fmt.Sprintf("benign document number %d about gofastr", i),
		})
	}

	hostile := []string{
		"'; DROP TABLE x; --",
		"a AND b OR c NOT d NEAR e",
		"); DROP SCHEMA public;--",
		"$$ || $$",
		"%' OR '1'='1",
		"col:value AND (secret)",
	}
	for _, h := range hostile {
		res, err := idx.Search(ctx, Query{Text: h})
		if err != nil {
			t.Fatalf("adversarial query %q errored: %v", h, err)
		}
		// None of the safe docs contain drop/table/x/and/or/etc., so 0 hits.
		// The property: it did NOT return all 3 (match-everything = sanitisation
		// failure).
		if len(res) > 0 {
			t.Fatalf("adversarial query %q matched %d docs (must not match all): %#v", h, len(res), res)
		}
	}

	// The table must still exist and be queryable (no DROP TABLE leaked).
	res, err := idx.Search(ctx, Query{Text: "gofastr"})
	if err != nil {
		t.Fatalf("index unusable after adversarial queries: %v", err)
	}
	if len(res) != 3 {
		t.Fatalf("expected 3 recoverable hits, got %d", len(res))
	}
}

func TestSQLiteEmptyQuery(t *testing.T) {
	// Parity with Memory/Postgres: empty / whitespace-only text matches ALL
	// documents (score 0), while terms that sanitize away entirely match
	// nothing.
	ctx := context.Background()
	idx := newSQLiteFTS(t)
	_ = idx.Index(ctx, Document{ID: "1", Text: "hello world", Fields: map[string]any{"tenant": "a"}})
	_ = idx.Index(ctx, Document{ID: "2", Text: "goodbye moon", Fields: map[string]any{"tenant": "b"}})

	for _, text := range []string{"", "   "} {
		res, err := idx.Search(ctx, Query{Text: text})
		if err != nil {
			t.Fatal(err)
		}
		if len(res) != 2 || res[0].Document.ID != "1" || res[0].Score != 0 {
			t.Fatalf("query %q: got %#v, want both docs at score 0", text, res)
		}
	}
	// Filters still apply on the match-all path.
	res, err := idx.Search(ctx, Query{Text: "", FieldEquals: map[string]string{"tenant": "b"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 1 || res[0].Document.ID != "2" {
		t.Fatalf("scoped match-all: %#v", res)
	}
	// Pure punctuation is NOT empty — its terms sanitize away and match nothing.
	res, err = idx.Search(ctx, Query{Text: "!!! ???"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 0 {
		t.Fatalf("punctuation query: got %d results, want 0", len(res))
	}
}

func TestSQLiteLimitOffset(t *testing.T) {
	ctx := context.Background()
	idx := newSQLiteFTS(t)
	// Five docs that all match "gofastr", with stable tiebreak by id asc.
	for _, id := range []string{"1", "2", "3", "4", "5"} {
		_ = idx.Index(ctx, Document{ID: id, Text: "gofastr release"})
	}

	page1, err := idx.Search(ctx, Query{Text: "gofastr", Limit: 2, Offset: 0})
	if err != nil {
		t.Fatal(err)
	}
	if len(page1) != 2 || page1[0].Document.ID != "1" || page1[1].Document.ID != "2" {
		t.Fatalf("page1: %#v", page1)
	}
	page2, err := idx.Search(ctx, Query{Text: "gofastr", Limit: 2, Offset: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(page2) != 2 || page2[0].Document.ID != "3" || page2[1].Document.ID != "4" {
		t.Fatalf("page2: %#v", page2)
	}
	// Last page.
	page3, err := idx.Search(ctx, Query{Text: "gofastr", Limit: 2, Offset: 4})
	if err != nil {
		t.Fatal(err)
	}
	if len(page3) != 1 || page3[0].Document.ID != "5" {
		t.Fatalf("page3: %#v", page3)
	}
	// Offset past the end → empty.
	empty, err := idx.Search(ctx, Query{Text: "gofastr", Limit: 2, Offset: 100})
	if err != nil {
		t.Fatal(err)
	}
	if len(empty) != 0 {
		t.Fatalf("past-end offset: got %d, want 0", len(empty))
	}
}

// TestSQLitePaginationBoundsClamped mirrors the Memory/Postgres property:
// pagination bounds must be clamped before querying.
func TestSQLitePaginationBoundsClamped(t *testing.T) {
	ctx := context.Background()
	idx := newSQLiteFTS(t)
	for _, id := range []string{"1", "2", "3"} {
		_ = idx.Index(ctx, Document{ID: id, Text: "alpha"})
	}

	cases := []struct {
		name  string
		query Query
		want  int
	}{
		{"happy path", Query{Text: "alpha", Limit: 2}, 2},
		{"negative offset", Query{Text: "alpha", Offset: -1}, 3},
		{"overflow limit", Query{Text: "alpha", Offset: 1, Limit: math.MaxInt}, 2},
		{"negative limit returns all", Query{Text: "alpha", Limit: -5}, 3},
		{"offset past end", Query{Text: "alpha", Offset: 100}, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := idx.Search(ctx, tc.query)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(res) != tc.want {
				t.Fatalf("got %d results, want %d", len(res), tc.want)
			}
		})
	}
}

// TestSQLiteStemming: the porter+unicode61 tokenizer should stem "correndo"
// and "correre" to a common root in Italian-ish text. We verify that
// querying a morphological variant still matches (porter stemmer applied).
func TestSQLiteStemming(t *testing.T) {
	ctx := context.Background()
	idx := newSQLiteFTS(t)
	_ = idx.Index(ctx, Document{ID: "1", Type: "posts", Text: "the cats are running fast"})
	// "cat" should match "cats" (porter stemmer strips plural).
	got, err := idx.Search(ctx, Query{Text: "cat"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Document.ID != "1" {
		t.Fatalf("stemmed query missed: %#v", got)
	}
}

func TestSQLiteHyphenatedQueryTerm(t *testing.T) {
	ctx := context.Background()
	idx := newSQLiteFTS(t)
	if err := idx.Index(ctx, Document{ID: "1", Type: "posts", Text: "the load-balancer restarted"}); err != nil {
		t.Fatal(err)
	}
	got, err := idx.Search(ctx, Query{Text: "load-balancer"})
	if err != nil {
		t.Fatalf("hyphenated query errored: %v", err)
	}
	if len(got) != 1 || got[0].Document.ID != "1" {
		t.Fatalf("hyphenated query missed: %#v", got)
	}
}

// TestSQLiteFieldEqualsMultiPair verifies AND semantics: every key/value pair
// must match.
func TestSQLiteFieldEqualsMultiPair(t *testing.T) {
	ctx := context.Background()
	idx := newSQLiteFTS(t)
	_ = idx.Index(ctx, Document{
		ID: "1", Text: "gofastr doc",
		Fields: map[string]any{"tenant": "acme", "status": "published"},
	})
	_ = idx.Index(ctx, Document{
		ID: "2", Text: "gofastr doc",
		Fields: map[string]any{"tenant": "acme", "status": "draft"},
	})

	res, err := idx.Search(ctx, Query{
		Text: "gofastr",
		FieldEquals: map[string]string{"tenant": "acme", "status": "published"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 1 || res[0].Document.ID != "1" {
		t.Fatalf("multi-pair AND: got %#v, want only doc 1", res)
	}
}
