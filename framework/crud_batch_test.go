package framework

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/hook"
)

// seedBatchDB creates a minimal posts table with a unique title column so we
// can engineer per-item failures via uniqueness violations.
func seedBatchDB(t *testing.T, db *sql.DB) {
	t.Helper()
	if _, err := db.Exec(`CREATE TABLE posts (
		id TEXT PRIMARY KEY,
		title TEXT NOT NULL UNIQUE,
		body TEXT DEFAULT ''
	)`); err != nil {
		t.Fatalf("create: %v", err)
	}
}

func batchApp(t *testing.T, db *sql.DB) *App {
	t.Helper()
	app := NewApp(WithDB(db), WithoutDefaultMiddleware())
	app.Entity("posts", entity.EntityConfig{
		Table: "posts",
		Fields: []schema.Field{
			{Name: "title", Type: schema.String, Required: true},
			{Name: "body", Type: schema.Text},
		},
	}.WithTimestamps(false))
	return app
}

// runBatchTest fans the body across both dialects, providing both the raw db
// (for direct assertions) and the TestApp (for HTTP calls).
func runBatchTest(t *testing.T, body func(t *testing.T, db *sql.DB, ta *TestApp)) {
	t.Helper()
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		seedBatchDB(t, db)
		app := batchApp(t, db)
		ta := TestHarness(t, app).AsUser(struct{ ID string }{ID: "u1"})
		body(t, db, ta)
	})
}

// runBatchTestWithApp is like runBatchTest but lets the test customize the
// app (e.g. registering hooks) before constructing the harness. body receives
// the open db and an `addApp` callback to register hooks; once it returns
// (nil), the harness wraps the now-mutated app and the inner body runs.
func runBatchTestWithApp(t *testing.T, configure func(*App), body func(t *testing.T, db *sql.DB, ta *TestApp)) {
	t.Helper()
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		seedBatchDB(t, db)
		app := batchApp(t, db)
		if configure != nil {
			configure(app)
		}
		ta := TestHarness(t, app).AsUser(struct{ ID string }{ID: "u1"})
		body(t, db, ta)
	})
}

func decodeBatchResponse(t *testing.T, body string) crud.BatchResponse {
	t.Helper()
	var resp crud.BatchResponse
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		t.Fatalf("decode: %v\n%s", err, body)
	}
	return resp
}

func postsRowCount(t *testing.T, db *sql.DB) int {
	t.Helper()
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM posts").Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}

// ============================================================================
// Test: BatchCreate happy path commits all items
// ============================================================================

func TestBatchCreate_AllSucceed(t *testing.T) {
	runBatchTest(t, func(t *testing.T, db *sql.DB, ta *TestApp) {

		resp := ta.Post("/posts/_batch", map[string]any{
			"items": []map[string]any{
				{"title": "First"},
				{"title": "Second"},
				{"title": "Third"},
			},
		})
		resp.AssertStatus(t, http.StatusOK)

		got := decodeBatchResponse(t, resp.Body())
		if !got.Committed {
			t.Fatal("expected committed=true")
		}
		if len(got.Results) != 3 {
			t.Fatalf("expected 3 results, got %d", len(got.Results))
		}
		for i, r := range got.Results {
			if r.Index != i {
				t.Fatalf("result %d has wrong index %d", i, r.Index)
			}
			if r.Error != "" || r.Skipped {
				t.Fatalf("result %d should be data: %+v", i, r)
			}
			if r.Data["title"] == nil {
				t.Fatalf("result %d missing title in data: %+v", i, r.Data)
			}
		}
		if got := postsRowCount(t, db); got != 3 {
			t.Fatalf("expected 3 rows committed, got %d", got)
		}
	})
}

// ============================================================================
// Test: BatchCreate rolls back fully on first per-item failure
// ============================================================================

func TestBatchCreate_PartialFailRollsBack(t *testing.T) {
	runBatchTest(t, func(t *testing.T, db *sql.DB, ta *TestApp) {
		if _, err := db.Exec("INSERT INTO posts(id, title) VALUES ($1, $2)", "p0", "Conflict"); err != nil {
			t.Fatalf("seed: %v", err)
		}

		resp := ta.Post("/posts/_batch", map[string]any{
			"items": []map[string]any{
				{"title": "A"},
				{"title": "Conflict"}, // unique violation
				{"title": "C"},
			},
		})
		resp.AssertStatus(t, http.StatusBadRequest)

		got := decodeBatchResponse(t, resp.Body())
		if got.Committed {
			t.Fatal("expected committed=false")
		}
		if len(got.Results) != 3 {
			t.Fatalf("expected 3 results, got %d", len(got.Results))
		}
		if got.Results[0].Error != "" {
			t.Fatalf("expected index 0 to look successful (rolled back later): %+v", got.Results[0])
		}
		if got.Results[1].Error == "" {
			t.Fatalf("expected index 1 error, got: %+v", got.Results[1])
		}
		if !got.Results[2].Skipped {
			t.Fatalf("expected index 2 skipped (loop aborted): %+v", got.Results[2])
		}

		// Only the seed row should remain — the batch must have rolled back.
		if got := postsRowCount(t, db); got != 1 {
			t.Fatalf("expected 1 row (seed only) after rollback, got %d", got)
		}
	})
}

// ============================================================================
// Test: BatchCreate validation failure short-circuits with field details
// ============================================================================

func TestBatchCreate_ValidationFailure_ExposesFields(t *testing.T) {
	runBatchTest(t, func(t *testing.T, db *sql.DB, ta *TestApp) {

		resp := ta.Post("/posts/_batch", map[string]any{
			"items": []map[string]any{
				{"title": "Valid One"},
				{"body": "missing title"}, // schema requires title
			},
		})
		resp.AssertStatus(t, http.StatusBadRequest)

		got := decodeBatchResponse(t, resp.Body())
		if got.Committed {
			t.Fatal("expected committed=false")
		}
		if got.Results[1].Error != "validation failed" {
			t.Fatalf("expected validation failed at index 1, got %q", got.Results[1].Error)
		}
		if got.Results[1].Fields == nil {
			t.Fatalf("expected fields detail at index 1, got %+v", got.Results[1])
		}
		if _, ok := got.Results[1].Fields["title"]; !ok {
			t.Fatalf("expected title field error at index 1, got %v", got.Results[1].Fields)
		}
	})
}

// ============================================================================
// Test: Empty items rejected
// ============================================================================

func TestBatchCreate_EmptyRejected(t *testing.T) {
	runBatchTest(t, func(t *testing.T, db *sql.DB, ta *TestApp) {

		resp := ta.Post("/posts/_batch", map[string]any{"items": []map[string]any{}})
		resp.AssertStatus(t, http.StatusBadRequest).
			AssertBodyContains(t, "non-empty")
	})
}

// ============================================================================
// Test: Oversize batch rejected
// ============================================================================

func TestBatchCreate_OversizeRejected(t *testing.T) {
	runBatchTest(t, func(t *testing.T, db *sql.DB, ta *TestApp) {

		items := make([]map[string]any, crud.MaxBatchSize+1)
		for i := range items {
			items[i] = map[string]any{"title": fmt.Sprintf("Item %d", i)}
		}
		resp := ta.Post("/posts/_batch", map[string]any{"items": items})
		resp.AssertStatus(t, http.StatusBadRequest).
			AssertBodyContains(t, "exceeds max")

		if got := postsRowCount(t, db); got != 0 {
			t.Fatalf("expected 0 rows (rejected before tx), got %d", got)
		}
	})
}

// ============================================================================
// Test: BatchUpdate happy path
// ============================================================================

func TestBatchUpdate_AllSucceed(t *testing.T) {
	runBatchTest(t, func(t *testing.T, db *sql.DB, ta *TestApp) {
		for i, name := range []string{"A", "B", "C"} {
			if _, err := db.Exec("INSERT INTO posts(id, title) VALUES ($1, $2)", fmt.Sprintf("p%d", i+1), name); err != nil {
				t.Fatalf("seed: %v", err)
			}
		}

		body := map[string]any{
			"items": []map[string]any{
				{"id": "p1", "title": "A2"},
				{"id": "p2", "title": "B2"},
			},
		}
		resp := ta.Request(http.MethodPatch, "/posts/_batch", nil).WithBody(body).Execute()
		resp.AssertStatus(t, http.StatusOK)

		got := decodeBatchResponse(t, resp.Body())
		if !got.Committed || len(got.Results) != 2 {
			t.Fatalf("unexpected response: %+v", got)
		}

		var t1, t2 string
		if err := db.QueryRow("SELECT title FROM posts WHERE id = $1", "p1").Scan(&t1); err != nil {
			t.Fatalf("read p1: %v", err)
		}
		if err := db.QueryRow("SELECT title FROM posts WHERE id = $1", "p2").Scan(&t2); err != nil {
			t.Fatalf("read p2: %v", err)
		}
		if t1 != "A2" || t2 != "B2" {
			t.Fatalf("expected A2/B2, got %q/%q", t1, t2)
		}
	})
}

// ============================================================================
// Test: BatchUpdate without id on an item is rejected and rolls back
// ============================================================================

func TestBatchUpdate_MissingID_RollsBack(t *testing.T) {
	runBatchTest(t, func(t *testing.T, db *sql.DB, ta *TestApp) {
		if _, err := db.Exec("INSERT INTO posts(id, title) VALUES ($1, $2)", "p1", "Original"); err != nil {
			t.Fatalf("seed: %v", err)
		}

		body := map[string]any{
			"items": []map[string]any{
				{"id": "p1", "title": "Changed"},
				{"title": "no id here"},
			},
		}
		resp := ta.Request(http.MethodPatch, "/posts/_batch", nil).WithBody(body).Execute()
		resp.AssertStatus(t, http.StatusBadRequest)

		got := decodeBatchResponse(t, resp.Body())
		if got.Committed {
			t.Fatal("expected committed=false")
		}
		if !strings.Contains(got.Results[1].Error, "missing") {
			t.Fatalf("expected missing-id error at index 1, got %+v", got.Results[1])
		}

		// p1 must be unchanged because the batch rolled back
		var title string
		if err := db.QueryRow("SELECT title FROM posts WHERE id = $1", "p1").Scan(&title); err != nil {
			t.Fatalf("read p1: %v", err)
		}
		if title != "Original" {
			t.Fatalf("expected title rolled back to Original, got %q", title)
		}
	})
}

// ============================================================================
// Test: BatchDelete happy path
// ============================================================================

func TestBatchDelete_AllSucceed(t *testing.T) {
	runBatchTest(t, func(t *testing.T, db *sql.DB, ta *TestApp) {
		for i, name := range []string{"A", "B", "C"} {
			if _, err := db.Exec("INSERT INTO posts(id, title) VALUES ($1, $2)", fmt.Sprintf("p%d", i+1), name); err != nil {
				t.Fatalf("seed: %v", err)
			}
		}

		body := map[string]any{"ids": []string{"p1", "p3"}}
		resp := ta.Request(http.MethodDelete, "/posts/_batch", nil).WithBody(body).Execute()
		resp.AssertStatus(t, http.StatusOK)

		got := decodeBatchResponse(t, resp.Body())
		if !got.Committed || len(got.Results) != 2 {
			t.Fatalf("unexpected response: %+v", got)
		}

		if got := postsRowCount(t, db); got != 1 {
			t.Fatalf("expected 1 remaining row (p2), got %d", got)
		}
	})
}

// ============================================================================
// Test: BatchDelete with one missing id rolls all back
// ============================================================================

func TestBatchDelete_MissingID_RollsBack(t *testing.T) {
	runBatchTest(t, func(t *testing.T, db *sql.DB, ta *TestApp) {
		if _, err := db.Exec("INSERT INTO posts(id, title) VALUES ($1, $2)", "p1", "Keep"); err != nil {
			t.Fatalf("seed: %v", err)
		}

		body := map[string]any{"ids": []string{"p1", "does-not-exist"}}
		resp := ta.Request(http.MethodDelete, "/posts/_batch", nil).WithBody(body).Execute()
		resp.AssertStatus(t, http.StatusBadRequest)

		got := decodeBatchResponse(t, resp.Body())
		if got.Committed {
			t.Fatal("expected committed=false")
		}
		if got.Results[1].Error == "" {
			t.Fatalf("expected error at index 1, got %+v", got.Results[1])
		}

		// p1 must still exist because the batch rolled back
		if got := postsRowCount(t, db); got != 1 {
			t.Fatalf("expected 1 row (rolled back), got %d", got)
		}
	})
}

// ============================================================================
// Test: AfterCreate hook error rolls back the entire batch
// ============================================================================

func TestBatchCreate_AfterHookError_RollsBack(t *testing.T) {
	var calls int
	configure := func(app *App) {
		app.HookRegistry("posts").RegisterHook(hook.AfterCreate, func(ctx context.Context, data any) error {
			calls++
			if calls == 2 {
				return errors.New("policy reject")
			}
			return nil
		})
	}
	runBatchTestWithApp(t, configure, func(t *testing.T, db *sql.DB, ta *TestApp) {
		calls = 0 // reset between dialect subtests so the second-item failure replays
		resp := ta.Post("/posts/_batch", map[string]any{
			"items": []map[string]any{
				{"title": "ok-1"},
				{"title": "fail-2"},
				{"title": "skipped-3"},
			},
		})
		resp.AssertStatus(t, http.StatusBadRequest)

		got := decodeBatchResponse(t, resp.Body())
		if got.Committed {
			t.Fatal("expected committed=false")
		}
		// Hook error messages are redacted to "internal error" in the
		// batch response so they can't smuggle internal state to the
		// client (see batch_error_leak_security_test.go / error_leak_security_test.go).
		// The Index field still pins down which item triggered the
		// rollback; the original message is logged server-side.
		if got.Results[1].Error == "" {
			t.Fatalf("expected non-empty error at index 1, got %+v", got.Results[1])
		}
		if !got.Results[2].Skipped {
			t.Fatalf("expected index 2 skipped, got %+v", got.Results[2])
		}

		if got := postsRowCount(t, db); got != 0 {
			t.Fatalf("expected 0 rows after rollback, got %d", got)
		}
	})
}
