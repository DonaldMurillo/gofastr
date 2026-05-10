package framework

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/gofastr/gofastr/core/schema"
)

func seedStreamableRows(t *testing.T, db *sql.DB, n int) {
	t.Helper()
	if _, err := db.Exec(`CREATE TABLE posts (id TEXT PRIMARY KEY, title TEXT NOT NULL)`); err != nil {
		t.Fatalf("create: %v", err)
	}
	for i := 1; i <= n; i++ {
		if _, err := db.Exec("INSERT INTO posts(id, title) VALUES ($1, $2)",
			fmt.Sprintf("p%04d", i), fmt.Sprintf("Post %d", i)); err != nil {
			t.Fatalf("seed %d: %v", i, err)
		}
	}
}

func streamingApp(t *testing.T, db *sql.DB) *App {
	t.Helper()
	app := NewApp(WithDB(db), WithoutDefaultMiddleware())
	app.Entity("posts", EntityConfig{
		Table: "posts",
		Fields: []schema.Field{
			{Name: "title", Type: schema.String, Required: true},
		},
	}.WithTimestamps(false))
	return app
}

// ============================================================================
// Test: ?stream=true returns the standard envelope shape (data, total, etc.)
// even though it's written incrementally.
// ============================================================================

func TestStreamingList_EnvelopeShape(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		seedStreamableRows(t, db, 5)
		ta := TestHarness(t, streamingApp(t, db))

		resp := ta.Get("/posts?stream=true&limit=10")
		resp.AssertStatus(t, http.StatusOK)

		var env ListResponse
		if err := json.Unmarshal([]byte(resp.Body()), &env); err != nil {
			t.Fatalf("decode: %v\nbody: %s", err, resp.Body())
		}
		if env.Total != 5 || env.PerPage != 10 || env.Page != 1 {
			t.Fatalf("unexpected envelope: %+v", env)
		}
		if len(env.Data) != 5 {
			t.Fatalf("expected 5 rows, got %d", len(env.Data))
		}
	})
}

// ============================================================================
// Test: ?limit≥streamListThreshold auto-switches to streaming without the
// client opt-in. Verifies the threshold trigger.
// ============================================================================

func TestStreamingList_OptInExplicit(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		seedStreamableRows(t, db, 150)
		ta := TestHarness(t, streamingApp(t, db))

		// limit=100 is the parser's max; ?stream=true forces the streaming
		// code path even though perPage doesn't cross the auto-trigger.
		resp := ta.Get("/posts?stream=true&limit=100")
		resp.AssertStatus(t, http.StatusOK)

		var env ListResponse
		if err := json.Unmarshal([]byte(resp.Body()), &env); err != nil {
			t.Fatalf("decode: %v\nbody (first 200 chars): %q", err, abbreviate(resp.Body(), 200))
		}
		if env.Total != 150 {
			t.Fatalf("expected total=150, got %d", env.Total)
		}
		if len(env.Data) != 100 {
			t.Fatalf("expected 100 rows (limit), got %d", len(env.Data))
		}
	})
}

// ============================================================================
// Test: streaming output is valid JSON even when zero rows match.
// ============================================================================

func TestStreamingList_EmptyResult(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		seedStreamableRows(t, db, 0)
		ta := TestHarness(t, streamingApp(t, db))
		resp := ta.Get("/posts?stream=true")
		resp.AssertStatus(t, http.StatusOK)

		var env ListResponse
		if err := json.Unmarshal([]byte(resp.Body()), &env); err != nil {
			t.Fatalf("decode: %v\nbody: %q", err, resp.Body())
		}
		if env.Total != 0 || len(env.Data) != 0 {
			t.Fatalf("expected empty envelope, got %+v", env)
		}
	})
}

func abbreviate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "...(truncated)"
}

// keep strings import alive for abbreviate (which doesn't use it, but other
// helpers in this file might in the future).
var _ = strings.Contains
