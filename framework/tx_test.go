package framework

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	dbpkg "github.com/DonaldMurillo/gofastr/framework/db"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/hook"
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
	app.Entity("posts", entity.EntityConfig{
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

		app.HookRegistry("posts").RegisterHook(hook.AfterCreate, func(ctx context.Context, data any) error {
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
		app.HookRegistry("posts").RegisterHook(hook.AfterUpdate, func(ctx context.Context, data any) error {
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
		app.HookRegistry("posts").RegisterHook(hook.AfterDelete, func(ctx context.Context, data any) error {
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
		app.HookRegistry("posts").RegisterHook(hook.AfterCreate, func(ctx context.Context, data any) error {
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

		app.HookRegistry("posts").RegisterHook(hook.BeforeCreate, func(ctx context.Context, data any) error {
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
// Test: app.InTx runs fn inside a tx, commits on success
// ============================================================================

func TestApp_InTx_Commits(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		createPostsTestTable(t, db)
		app := newPostsApp(t, db)

		err := app.InTx(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, "INSERT INTO posts(id, title, body) VALUES ($1, $2, $3)", "p1", "in-tx", "")
			return err
		})
		if err != nil {
			t.Fatalf("InTx: %v", err)
		}
		if got := rowCount(t, db); got != 1 {
			t.Fatalf("expected 1 row after commit, got %d", got)
		}
	})
}

// ============================================================================
// Test: app.InTx rolls back when fn returns error
// ============================================================================

func TestApp_InTx_RollsBackOnError(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		createPostsTestTable(t, db)
		app := newPostsApp(t, db)

		err := app.InTx(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
			if _, err := tx.ExecContext(ctx, "INSERT INTO posts(id, title, body) VALUES ($1, $2, $3)", "p1", "tentative", ""); err != nil {
				return err
			}
			return errors.New("changed my mind")
		})
		if err == nil || err.Error() != "changed my mind" {
			t.Fatalf("expected rollback error, got %v", err)
		}
		if got := rowCount(t, db); got != 0 {
			t.Fatalf("expected 0 rows after rollback, got %d", got)
		}
	})
}

// ============================================================================
// Test: TxFromContext is populated inside InTx fn
// ============================================================================

func TestApp_InTx_TxInContext(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		createPostsTestTable(t, db)
		app := newPostsApp(t, db)

		var sawTx bool
		err := app.InTx(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
			ctxTx, ok := TxFromContext(ctx)
			if !ok {
				return errors.New("no tx in ctx")
			}
			if ctxTx != tx {
				return errors.New("ctx tx differs from arg")
			}
			sawTx = true
			return nil
		})
		if err != nil {
			t.Fatalf("InTx: %v", err)
		}
		if !sawTx {
			t.Fatal("expected fn to find tx in ctx")
		}
	})
}

// ============================================================================
// Test: app.InTx joins an ambient tx already in context instead of opening
// a second independent transaction.
// ============================================================================

func TestApp_InTx_JoinsAmbientTx(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		createPostsTestTable(t, db)
		app := newPostsApp(t, db)

		outer, err := db.BeginTx(context.Background(), nil)
		if err != nil {
			t.Fatalf("begin outer: %v", err)
		}
		committed := false
		defer func() {
			if !committed {
				_ = outer.Rollback()
			}
		}()
		ctx := dbpkg.WithTx(context.Background(), outer)

		var inner *sql.Tx
		if err := app.InTx(ctx, func(ctx context.Context, tx *sql.Tx) error {
			inner = tx
			_, e := tx.ExecContext(ctx, "INSERT INTO posts(id, title, body) VALUES ($1, $2, $3)", "p1", "ambient", "")
			return e
		}); err != nil {
			t.Fatalf("InTx: %v", err)
		}
		// The closure must have received the ambient tx, not a freshly opened
		// one. If InTx began its own tx, inner != outer.
		if inner != outer {
			t.Fatal("InTx opened a new tx instead of reusing the ambient one")
		}
		// The write lives in the ambient tx and InTx must NOT have committed it.
		// Read through the ambient tx (the only handle that can see pending
		// state) to prove the row is present but still uncommitted.
		var n int
		if err := outer.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM posts").Scan(&n); err != nil {
			t.Fatalf("count via ambient tx: %v", err)
		}
		if n != 1 {
			t.Fatalf("expected the write to be pending in the ambient tx, saw %d rows", n)
		}
		// The outer owner controls the lifecycle: commit makes it durable.
		if err := outer.Commit(); err != nil {
			t.Fatalf("commit outer: %v", err)
		}
		committed = true
		if got := rowCount(t, db); got != 1 {
			t.Fatalf("expected 1 row after outer commit, got %d", got)
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
