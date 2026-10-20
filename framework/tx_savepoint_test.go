package framework

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"
)

func insertPost(ctx context.Context, tx *sql.Tx, id string) error {
	_, err := tx.ExecContext(ctx, "INSERT INTO posts(id, title, body) VALUES ($1, $2, $3)", id, id, "")
	return err
}

// eventBody reads record.body out of an entity.created payload.
func eventBody(data any) string {
	m, _ := data.(map[string]any)
	rec, _ := m["record"].(map[string]any)
	body, _ := rec["body"].(string)
	return body
}

func postIDs(t *testing.T, db *sql.DB) string {
	t.Helper()
	rows, err := db.Query("SELECT id FROM posts ORDER BY id")
	if err != nil {
		t.Fatalf("select ids: %v", err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan: %v", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}
	return strings.Join(ids, ",")
}

// A nested InTx that fails rolls back its own writes and leaves the outer
// transaction's writes in place, so an outer fn that handles the error can
// still commit.
func TestNestedInTxErrorKeepsOuterWrites(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		createPostsTestTable(t, db)
		app := newPostsApp(t, db)
		boom := errors.New("boom")

		err := app.InTx(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
			if err := insertPost(ctx, tx, "p1"); err != nil {
				return err
			}
			inner := app.InTx(ctx, func(ctx context.Context, tx *sql.Tx) error {
				if err := insertPost(ctx, tx, "p2"); err != nil {
					return err
				}
				return boom
			})
			if !errors.Is(inner, boom) {
				t.Fatalf("nested InTx returned %v, want boom", inner)
			}
			return insertPost(ctx, tx, "p3")
		})
		if err != nil {
			t.Fatalf("outer InTx: %v", err)
		}
		if got := postIDs(t, db); got != "p1,p3" {
			t.Fatalf("rows after commit = %q, want p1,p3", got)
		}
	})
}

// A nested InTx whose release fails reports the error with its writes
// undone, so an outer fn that handles the error and commits does not keep
// them. Cancelling the nested ctx after fn's writes makes RELEASE fail.
func TestFailedReleaseUndoesWrites(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		createPostsTestTable(t, db)
		app := newPostsApp(t, db)

		err := app.InTx(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
			if err := insertPost(ctx, tx, "p1"); err != nil {
				return err
			}
			child, cancel := context.WithCancel(ctx)
			inner := app.InTx(child, func(ctx context.Context, tx *sql.Tx) error {
				if err := insertPost(ctx, tx, "p2"); err != nil {
					return err
				}
				cancel()
				return nil
			})
			if !errors.Is(inner, context.Canceled) {
				t.Fatalf("nested InTx returned %v, want the release's cancellation", inner)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("outer InTx: %v", err)
		}
		if got := postIDs(t, db); got != "p1" {
			t.Fatalf("rows after commit = %q, want p1 alone", got)
		}
	})
}

// A failed statement inside a nested InTx must not poison the outer
// transaction. On Postgres, without a savepoint, every later statement
// fails with "current transaction is aborted".
func TestNestedInTxFailedSQLRecovers(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		createPostsTestTable(t, db)
		app := newPostsApp(t, db)

		err := app.InTx(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
			if err := insertPost(ctx, tx, "p1"); err != nil {
				return err
			}
			dup := app.InTx(ctx, func(ctx context.Context, tx *sql.Tx) error {
				return insertPost(ctx, tx, "p1") // duplicate primary key
			})
			if dup == nil {
				t.Fatal("duplicate insert succeeded")
			}
			return insertPost(ctx, tx, "p2")
		})
		if err != nil {
			t.Fatalf("outer InTx after a failed nested statement: %v", err)
		}
		if got := postIDs(t, db); got != "p1,p2" {
			t.Fatalf("rows after commit = %q, want p1,p2", got)
		}
	})
}

// A nested InTx that succeeds is released into the outer transaction and
// commits with it, or rolls back with it.
func TestNestedInTxRollsBackWithOuter(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		createPostsTestTable(t, db)
		app := newPostsApp(t, db)
		boom := errors.New("boom")

		err := app.InTx(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
			if err := app.InTx(ctx, func(ctx context.Context, tx *sql.Tx) error {
				return insertPost(ctx, tx, "p1")
			}); err != nil {
				return err
			}
			return boom
		})
		if !errors.Is(err, boom) {
			t.Fatalf("outer InTx = %v, want boom", err)
		}
		if got := postIDs(t, db); got != "" {
			t.Fatalf("rows after outer rollback = %q, want none", got)
		}
	})
}

// Events staged inside a nested InTx that rolls back are dropped even
// when the outer transaction commits.
func TestNestedRollbackDropsEvents(t *testing.T) {
	forEachDialect(t, func(t *testing.T, dbc *sql.DB, _ Dialect) {
		app, ch, got := notesEventApp(t, dbc)
		boom := errors.New("boom")

		err := app.InTx(context.Background(), func(ctx context.Context, _ *sql.Tx) error {
			if _, e := ch.CreateOne(ctx, map[string]any{"body": "kept"}); e != nil {
				return e
			}
			_ = app.InTx(ctx, func(ctx context.Context, _ *sql.Tx) error {
				if _, e := ch.CreateOne(ctx, map[string]any{"body": "phantom"}); e != nil {
					return e
				}
				return boom
			})
			return nil
		})
		if err != nil {
			t.Fatalf("InTx: %v", err)
		}
		var bodies []string
		deadline := time.After(800 * time.Millisecond)
	collect:
		for {
			select {
			case ev := <-got:
				bodies = append(bodies, eventBody(ev.Data))
			case <-deadline:
				break collect
			}
		}
		if strings.Join(bodies, ",") != "kept" {
			t.Fatalf("published bodies = %v, want only kept", bodies)
		}
	})
}

// Events staged inside a nested InTx that succeeds publish once the outer
// transaction commits, not when the savepoint is released.
func TestNestedReleaseDefersEvents(t *testing.T) {
	forEachDialect(t, func(t *testing.T, dbc *sql.DB, _ Dialect) {
		app, ch, got := notesEventApp(t, dbc)

		err := app.InTx(context.Background(), func(ctx context.Context, _ *sql.Tx) error {
			if err := app.InTx(ctx, func(ctx context.Context, _ *sql.Tx) error {
				_, e := ch.CreateOne(ctx, map[string]any{"body": "inner"})
				return e
			}); err != nil {
				return err
			}
			select {
			case ev := <-got:
				t.Fatalf("event published before the outer commit: %v", ev.Data)
			case <-time.After(300 * time.Millisecond):
			}
			return nil
		})
		if err != nil {
			t.Fatalf("InTx: %v", err)
		}
		select {
		case <-got:
		case <-time.After(2 * time.Second):
			t.Fatal("inner event not published after the outer commit")
		}
	})
}

// TxReadOnly opens a read-only transaction: a write inside it fails.
func TestInTxReadOnlyRefusesWrites(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		createPostsTestTable(t, db)
		app := newPostsApp(t, db)

		err := app.InTx(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
			return insertPost(ctx, tx, "p1")
		}, TxReadOnly())
		if err == nil {
			t.Fatal("insert inside a read-only transaction succeeded")
		}
		if got := postIDs(t, db); got != "" {
			t.Fatalf("rows = %q, want none", got)
		}
	})
}

// A read-only transaction does not leave its connection read-only: the
// next transaction on the same pooled connection can write.
func TestReadOnlyTxLeavesPoolWritable(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		createPostsTestTable(t, db)
		db.SetMaxOpenConns(1)
		app := newPostsApp(t, db)

		if err := app.InTx(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
			return tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM posts").Scan(new(int))
		}, TxReadOnly()); err != nil {
			t.Fatalf("read-only InTx: %v", err)
		}
		if err := app.InTx(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
			return insertPost(ctx, tx, "p1")
		}); err != nil {
			t.Fatalf("write after a read-only transaction: %v", err)
		}
	})
}

// TxIsolation reaches BeginTx: Postgres reports the level it was given.
func TestInTxIsolationReachesDB(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, d Dialect) {
		createPostsTestTable(t, db)
		app := newPostsApp(t, db)
		if d != DialectPostgres {
			// SQLite has one level, so a weaker request still runs.
			err := app.InTx(context.Background(), func(context.Context, *sql.Tx) error {
				return nil
			}, TxIsolation(sql.LevelReadCommitted))
			if err != nil {
				t.Fatalf("InTx: %v", err)
			}
			return
		}

		var level string
		err := app.InTx(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
			return tx.QueryRowContext(ctx, "SHOW transaction_isolation").Scan(&level)
		}, TxIsolation(sql.LevelSerializable))
		if err != nil {
			t.Fatalf("InTx: %v", err)
		}
		if level != "serializable" {
			t.Fatalf("transaction_isolation = %q, want serializable", level)
		}
		ran := false
		err = app.InTx(context.Background(), func(context.Context, *sql.Tx) error {
			ran = true
			return nil
		}, TxIsolation(sql.LevelSnapshot))
		if err == nil || ran {
			t.Fatalf("LevelSnapshot: err = %v, ran = %v; want a refusal before fn", err, ran)
		}
	})
}

// Options cannot change a transaction that is already open: a nested InTx
// that asks for them is refused before fn runs.
func TestNestedInTxRefusesOptions(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		createPostsTestTable(t, db)
		app := newPostsApp(t, db)

		err := app.InTx(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
			ran := false
			inner := app.InTx(ctx, func(context.Context, *sql.Tx) error {
				ran = true
				return nil
			}, TxReadOnly())
			if !errors.Is(inner, ErrTxOptionsNested) {
				t.Fatalf("nested InTx with options = %v, want ErrTxOptionsNested", inner)
			}
			if ran {
				t.Fatal("fn ran despite the refused options")
			}
			return nil
		})
		if err != nil {
			t.Fatalf("outer InTx: %v", err)
		}
	})
}
