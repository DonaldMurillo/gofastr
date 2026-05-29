package framework

import (
	"context"
	"database/sql"
	"testing"
)

// TestInTx_PanicReleasesConn asserts InTx closes the tx on every exit path,
// including when fn panics. Without a defer guard the *sql.Tx is never
// rolled back, the pooled connection stays checked out, and repeated panics
// exhaust MaxOpenConns -> DoS. We cap the pool at one connection: if the
// panic path leaks, the follow-up InTx can never acquire a connection.
func TestInTx_PanicReleasesConn(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		createPostsTestTable(t, db)
		app := newPostsApp(t, db)

		// One connection: a leaked tx permanently parks it.
		db.SetMaxOpenConns(1)

		// fn panics mid-transaction (e.g. nil-deref on edge data).
		func() {
			defer func() {
				if r := recover(); r == nil {
					t.Fatal("expected panic to propagate out of InTx")
				}
			}()
			_ = app.InTx(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
				if _, err := tx.ExecContext(ctx, "INSERT INTO posts(id, title, body) VALUES ($1, $2, $3)", "p1", "leak", ""); err != nil {
					return err
				}
				var p *int
				_ = *p // panic
				return nil
			})
		}()

		// The connection must be back in the pool: a fresh InTx commits.
		ctx, cancel := context.WithTimeout(context.Background(), 2_000_000_000)
		defer cancel()
		if err := app.InTx(ctx, func(ctx context.Context, tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, "INSERT INTO posts(id, title, body) VALUES ($1, $2, $3)", "p2", "ok", "")
			return err
		}); err != nil {
			t.Fatalf("follow-up InTx failed (leaked connection?): %v", err)
		}

		// The panicked tx must have rolled back: only the second insert lands.
		if got := rowCount(t, db); got != 1 {
			t.Fatalf("expected 1 row (panicked tx rolled back), got %d", got)
		}
	})
}
