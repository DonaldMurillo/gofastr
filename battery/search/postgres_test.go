package search

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	_ "github.com/lib/pq"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
)

// Postgres integration tests for PostgresSearch. Postgres comes from
// $TEST_POSTGRES_DSN if set; otherwise an ephemeral testcontainer. If neither
// is reachable the suite skips — it never fails for lack of a database (same
// convention as battery/auth/entity_store_pg_test.go).
//
// Each test gets its own throwaway schema on the shared instance, so they run
// in parallel without colliding. SetMaxOpenConns(1) keeps the per-connection
// search_path stable.

var (
	searchPGOnce    sync.Once
	searchPGBaseDSN string
	searchPGErr     error
	searchPGUsing   string
	searchPGLogged  atomic.Bool
	searchPGKeepRef *tcpostgres.PostgresContainer
)

func resolveSearchPG() (string, error) {
	searchPGOnce.Do(func() {
		if dsn := strings.TrimSpace(os.Getenv("TEST_POSTGRES_DSN")); dsn != "" {
			searchPGBaseDSN = dsn
			searchPGUsing = "env"
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		c, err := tcpostgres.Run(ctx, "postgres:16-alpine",
			tcpostgres.WithDatabase("search_test"),
			tcpostgres.WithUsername("test"),
			tcpostgres.WithPassword("test"),
		)
		if err != nil {
			searchPGErr = fmt.Errorf("testcontainers: %w", err)
			return
		}
		dsn, err := c.ConnectionString(ctx, "sslmode=disable")
		if err != nil {
			searchPGErr = err
			return
		}
		searchPGBaseDSN = dsn
		searchPGUsing = "container"
		searchPGKeepRef = c
	})
	return searchPGBaseDSN, searchPGErr
}

// openSearchPG returns a *sql.DB bound to a fresh isolated schema. The schema
// is dropped on test cleanup. search_path is set so bare table names in
// PostgresConfig resolve into the test schema.
func openSearchPG(t *testing.T) *sql.DB {
	t.Helper()
	dsn, err := resolveSearchPG()
	if err != nil {
		t.Skipf("Postgres unavailable: %v", err)
	}
	if !searchPGLogged.Swap(true) {
		t.Logf("battery/search Postgres tests using %s", searchPGUsing)
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("open pg: %v", err)
	}
	db.SetMaxOpenConns(1)
	for i := 0; i < 25; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		if err := db.PingContext(ctx); err == nil {
			cancel()
			break
		}
		cancel()
		time.Sleep(200 * time.Millisecond)
	}
	schema := fmt.Sprintf("search_%d", time.Now().UnixNano())
	if _, err := db.Exec("CREATE SCHEMA " + schema); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	if _, err := db.Exec("SET search_path TO " + schema); err != nil {
		t.Fatalf("set search_path: %v", err)
	}
	t.Cleanup(func() {
		db.Exec("DROP SCHEMA " + schema + " CASCADE")
		db.Close()
	})
	return db
}

// newSearchIndex builds a PostgresSearch on a fresh schema and ensures the
// schema exists. Most tests use weightedFields=nil; the ranking tests pass a
// title weight.
func newSearchIndex(t *testing.T, weightedFields map[string]byte) *PostgresSearch {
	t.Helper()
	db := openSearchPG(t)
	idx, err := NewPostgres(db, PostgresConfig{WeightedFields: weightedFields})
	if err != nil {
		t.Fatalf("NewPostgres: %v", err)
	}
	if err := idx.EnsureSchema(context.Background()); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}
	return idx
}

func TestPostgresEnsureSchemaIdempotent(t *testing.T) {
	idx := newSearchIndex(t, nil)
	// Calling again on the already-created table/index must be a no-op.
	if err := idx.EnsureSchema(context.Background()); err != nil {
		t.Fatalf("second EnsureSchema: %v", err)
	}
}

func TestPostgresIndexSearchRoundTrip(t *testing.T) {
	ctx := context.Background()
	idx := newSearchIndex(t, nil)
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

	// Re-index the same id: upsert, no duplicate row.
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

func TestPostgresDelete(t *testing.T) {
	ctx := context.Background()
	idx := newSearchIndex(t, nil)
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

// TestPostgresRankingWeightAOverC: a query term in the weight-A text body must
// outrank the same term in a weight-C weighted field.
func TestPostgresRankingWeightAOverC(t *testing.T) {
	ctx := context.Background()
	idx := newSearchIndex(t, map[string]byte{"title": 'C'})

	// bodyHit: "pagination" appears in the text body (weight A).
	_ = idx.Index(ctx, Document{
		ID: "body", Text: "pagination pagination pagination details",
		Fields: map[string]any{"title": "general notes"},
	})
	// titleHit: "pagination" appears only in the title (weight C).
	_ = idx.Index(ctx, Document{
		ID: "title", Text: "general overview and summary",
		Fields: map[string]any{"title": "pagination guide"},
	})

	res, err := idx.Search(ctx, Query{Text: "pagination"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 2 {
		t.Fatalf("got %d results, want 2", len(res))
	}
	if res[0].Document.ID != "body" {
		t.Fatalf("weight-A body should rank first: got order %s, %s (scores %v, %v)",
			res[0].Document.ID, res[1].Document.ID, res[0].Score, res[1].Score)
	}
	if res[0].Score <= res[1].Score {
		t.Fatalf("weight-A score %v must exceed weight-C score %v", res[0].Score, res[1].Score)
	}
}

// TestPostgresWeightedFieldMatches: a term that appears only in a configured
// weighted field (not in the text body) must still produce a hit.
func TestPostgresWeightedFieldMatches(t *testing.T) {
	ctx := context.Background()
	idx := newSearchIndex(t, map[string]byte{"title": 'B'})

	_ = idx.Index(ctx, Document{
		ID: "1", Text: "completely different body text here",
		Fields: map[string]any{"title": "advanced pagination techniques"},
	})
	res, err := idx.Search(ctx, Query{Text: "pagination"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 1 || res[0].Document.ID != "1" {
		t.Fatalf("weighted-field-only term not found: %#v", res)
	}
}

func TestPostgresTypeFilter(t *testing.T) {
	ctx := context.Background()
	idx := newSearchIndex(t, nil)
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

// TestPostgresFieldEqualsTenantScope: two docs same text, different tenant —
// the parity case shared with the Memory backend.
func TestPostgresFieldEqualsTenantScope(t *testing.T) {
	ctx := context.Background()
	idx := newSearchIndex(t, nil)
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

func TestPostgresPrefixMatch(t *testing.T) {
	ctx := context.Background()
	idx := newSearchIndex(t, nil)
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

// TestPostgresAdversarialQuery: hostile query strings must neither error nor
// match every document. The tsquery builder strips every to_tsquery operator
// and SQL metacharacter before the text reaches the database.
func TestPostgresAdversarialQuery(t *testing.T) {
	ctx := context.Background()
	idx := newSearchIndex(t, nil)
	for i := 0; i < 3; i++ {
		_ = idx.Index(ctx, Document{
			ID:   fmt.Sprintf("safe-%d", i),
			Text: fmt.Sprintf("benign document number %d about gofastr", i),
		})
	}

	hostile := []string{
		"'; DROP TABLE x; --",
		"a & b | c :* ! (",
		"); DROP SCHEMA public;--",
		"$$ || $$",
		"%' OR '1'='1",
	}
	for _, h := range hostile {
		res, err := idx.Search(ctx, Query{Text: h})
		if err != nil {
			t.Fatalf("adversarial query %q errored: %v", h, err)
		}
		// None of the safe docs contain drop/table/x/etc., so 0 hits. The
		// property that matters: it did NOT return all 3 (a "match everything"
		// would mean sanitisation failed).
		if len(res) > 0 {
			t.Fatalf("adversarial query %q matched %d docs (must not match all): %#v", h, len(res), res)
		}
	}

	// The table must still exist and be queryable (no DROP TABLE leaked through).
	res, err := idx.Search(ctx, Query{Text: "gofastr"})
	if err != nil {
		t.Fatalf("index unusable after adversarial queries: %v", err)
	}
	if len(res) != 3 {
		t.Fatalf("expected 3 recoverable hits, got %d", len(res))
	}
}

func TestPostgresEmptyQuery(t *testing.T) {
	// Parity with Memory: empty / whitespace-only text matches ALL documents
	// (score 0), while terms that sanitize away entirely match nothing.
	ctx := context.Background()
	idx := newSearchIndex(t, nil)
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

func TestPostgresLimitOffset(t *testing.T) {
	ctx := context.Background()
	idx := newSearchIndex(t, nil)
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

func TestPostgresLanguageStemming(t *testing.T) {
	// The tsquery must parse with the configured language, not the database
	// default. Spanish stems "corriendo" and "corría" to the same lexeme
	// ('corr'); an english-parsed query would keep them distinct and miss.
	ctx := context.Background()
	db := openSearchPG(t)
	idx, err := NewPostgres(db, PostgresConfig{Language: "spanish"})
	if err != nil {
		t.Fatalf("NewPostgres: %v", err)
	}
	if err := idx.EnsureSchema(ctx); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}
	if err := idx.Index(ctx, Document{ID: "1", Type: "posts", Text: "el perro estaba corriendo"}); err != nil {
		t.Fatal(err)
	}
	got, err := idx.Search(ctx, Query{Text: "corría"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Document.ID != "1" {
		t.Fatalf("spanish stem query missed: %#v", got)
	}
}

func TestPostgresHyphenatedQueryTerm(t *testing.T) {
	// Hyphens survive sanitizeTsTerm, and compound tokens interact with the
	// ":*" prefix suffix inside to_tsquery — make sure that combination never
	// errors and still matches.
	ctx := context.Background()
	idx := newSearchIndex(t, nil)
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
	// Hyphenated term in a non-final position (no ":*" suffix) too.
	got, err = idx.Search(ctx, Query{Text: "load-balancer restarted"})
	if err != nil {
		t.Fatalf("hyphenated first term errored: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("hyphenated first term missed: %#v", got)
	}
}
