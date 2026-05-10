package framework

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"testing"

	"github.com/gofastr/gofastr/core/schema"
)

// createPostsTestTable creates a portable posts table for tx tests. TEXT and
// PRIMARY KEY are dialect-agnostic; no DATETIME columns so this DDL works
// on both engines without translation.
func createPostsTestTable(t *testing.T, db *sql.DB) {
	t.Helper()
	if _, err := db.Exec(`CREATE TABLE posts (
		id TEXT PRIMARY KEY,
		title TEXT NOT NULL,
		body TEXT DEFAULT ''
	)`); err != nil {
		t.Fatalf("create table: %v", err)
	}
}

func newPostsApp(t *testing.T, db *sql.DB) *App {
	t.Helper()
	app := NewApp(WithDB(db), WithoutDefaultMiddleware())
	app.Entity("posts", EntityConfig{
		Table: "posts",
		Fields: []schema.Field{
			{Name: "title", Type: schema.String, Required: true},
			{Name: "body", Type: schema.Text},
		},
	}.WithTimestamps(false))
	return app
}

// rowCount returns the number of rows in the posts table.
func rowCount(t *testing.T, db *sql.DB) int {
	t.Helper()
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM posts").Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}

// ============================================================================
// Test: AfterCreate hook returning error rolls back the INSERT.
// ============================================================================

func TestTx_AfterCreateError_RollsBackInsert(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		createPostsTestTable(t, db)
		app := newPostsApp(t, db)

		app.HookRegistry("posts").RegisterHook(AfterCreate, func(ctx context.Context, data any) error {
			return errors.New("boom")
		})

		ta := TestHarness(t, app)
		ta.Post("/posts", map[string]any{"title": "Should Be Rolled Back"}).
			AssertStatus(t, http.StatusInternalServerError)

		if got := rowCount(t, db); got != 0 {
			t.Fatalf("expected 0 rows after AfterCreate rollback, got %d", got)
		}
	})
}

// ============================================================================
// Test: AfterUpdate hook returning error rolls back the UPDATE.
// ============================================================================

func TestTx_AfterUpdateError_RollsBackUpdate(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		createPostsTestTable(t, db)
		if _, err := db.Exec("INSERT INTO posts(id, title, body) VALUES ($1, $2, $3)", "p1", "Original", ""); err != nil {
			t.Fatalf("seed: %v", err)
		}

		app := newPostsApp(t, db)
		app.HookRegistry("posts").RegisterHook(AfterUpdate, func(ctx context.Context, data any) error {
			return errors.New("boom")
		})

		ta := TestHarness(t, app)
		ta.Put("/posts/p1", map[string]any{"title": "Changed"}).
			AssertStatus(t, http.StatusInternalServerError)

		var title string
		if err := db.QueryRow("SELECT title FROM posts WHERE id = $1", "p1").Scan(&title); err != nil {
			t.Fatalf("read back: %v", err)
		}
		if title != "Original" {
			t.Fatalf("expected title rolled back to Original, got %q", title)
		}
	})
}

// ============================================================================
// Test: AfterDelete hook returning error rolls back the DELETE.
// ============================================================================

func TestTx_AfterDeleteError_RollsBackDelete(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		createPostsTestTable(t, db)
		if _, err := db.Exec("INSERT INTO posts(id, title, body) VALUES ($1, $2, $3)", "p1", "Keep Me", ""); err != nil {
			t.Fatalf("seed: %v", err)
		}

		app := newPostsApp(t, db)
		app.HookRegistry("posts").RegisterHook(AfterDelete, func(ctx context.Context, data any) error {
			return errors.New("boom")
		})

		ta := TestHarness(t, app)
		ta.Delete("/posts/p1").AssertStatus(t, http.StatusInternalServerError)

		if got := rowCount(t, db); got != 1 {
			t.Fatalf("expected 1 row remaining after AfterDelete rollback, got %d", got)
		}
	})
}

// ============================================================================
// Test: TxFromContext exposes the active tx to hooks; query reads pending state.
// ============================================================================

func TestTx_FromContext_HookSeesPendingWrite(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		createPostsTestTable(t, db)
		app := newPostsApp(t, db)

		var sawTx bool
		var pendingTitle string
		// AfterCreate runs after INSERT but before COMMIT. A query through the tx
		// must see the new row; a query through the raw DB must not.
		app.HookRegistry("posts").RegisterHook(AfterCreate, func(ctx context.Context, data any) error {
			tx, ok := TxFromContext(ctx)
			if !ok {
				return errors.New("no tx in context")
			}
			sawTx = true
			row := tx.QueryRowContext(ctx, "SELECT title FROM posts WHERE title = $1", "tx-visible")
			if err := row.Scan(&pendingTitle); err != nil {
				return err
			}
			return nil
		})

		ta := TestHarness(t, app)
		ta.Post("/posts", map[string]any{"title": "tx-visible"}).
			AssertStatus(t, http.StatusCreated)

		if !sawTx {
			t.Fatal("expected hook to find *sql.Tx in context")
		}
		if pendingTitle != "tx-visible" {
			t.Fatalf("expected hook to read pending row through tx, got %q", pendingTitle)
		}
	})
}

// ============================================================================
// Test: BeforeCreate rejection rolls back without an INSERT being attempted.
// ============================================================================

func TestTx_BeforeCreateRejection_NoInsert(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		createPostsTestTable(t, db)
		app := newPostsApp(t, db)

		app.HookRegistry("posts").RegisterHook(BeforeCreate, func(ctx context.Context, data any) error {
			return errors.New("policy says no")
		})

		ta := TestHarness(t, app)
		ta.Post("/posts", map[string]any{"title": "blocked"}).
			AssertStatus(t, http.StatusBadRequest).
			AssertBodyContains(t, "policy says no")

		if got := rowCount(t, db); got != 0 {
			t.Fatalf("expected 0 rows after BeforeCreate rejection, got %d", got)
		}
	})
}

// ============================================================================
// Test: TxFromContext returns false when no tx is active.
// ============================================================================

func TestTx_FromContext_NoTxReturnsFalse(t *testing.T) {
	if _, ok := TxFromContext(context.Background()); ok {
		t.Fatal("expected no tx in fresh context")
	}
}
