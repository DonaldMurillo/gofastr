package db

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"testing"

	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
)

func savepointTx(t *testing.T) (*sql.DB, *sql.Tx) {
	t.Helper()
	conn, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	conn.SetMaxOpenConns(1)
	t.Cleanup(func() { conn.Close() })
	if _, err := conn.Exec("CREATE TABLE t (v TEXT)"); err != nil {
		t.Fatal(err)
	}
	tx, err := conn.Begin()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback() })
	return conn, tx
}

func rowsIn(t *testing.T, tx *sql.Tx) []string {
	t.Helper()
	rows, err := tx.Query("SELECT v FROM t ORDER BY v")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			t.Fatal(err)
		}
		out = append(out, v)
	}
	return out
}

func insert(v string) func(context.Context, *sql.Tx) error {
	return func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, "INSERT INTO t VALUES (?)", v)
		return err
	}
}

func TestInSavepointUndoesOnlyItsOwnWritesOnError(t *testing.T) {
	_, tx := savepointTx(t)
	boom := errors.New("boom")
	err := InSavepoint(context.Background(), tx, func(ctx context.Context, tx *sql.Tx) error {
		if err := insert("outer")(ctx, tx); err != nil {
			return err
		}
		inner := InSavepoint(ctx, tx, func(ctx context.Context, tx *sql.Tx) error {
			_ = insert("inner")(ctx, tx)
			return boom
		})
		if !errors.Is(inner, boom) {
			t.Fatalf("inner err = %v, want boom", inner)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := rowsIn(t, tx); len(got) != 1 || got[0] != "outer" {
		t.Fatalf("rows = %v, want [outer]", got)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit after a rolled-back savepoint: %v", err)
	}
}

func TestRolledBackSavepointLeavesTheStack(t *testing.T) {
	_, tx := savepointTx(t)
	_ = InSavepoint(context.Background(), tx, func(context.Context, *sql.Tx) error { return errors.New("no") })
	name := "gofastr_sp_" + strconv.FormatUint(savepointSeq.Load(), 10)
	if _, err := tx.Exec("RELEASE SAVEPOINT " + name); err == nil {
		t.Fatalf("savepoint %s is still on the transaction after its rollback", name)
	}
}

func TestInSavepointUndoesWritesOnPanic(t *testing.T) {
	_, tx := savepointTx(t)
	func() {
		defer func() { _ = recover() }()
		_ = InSavepoint(context.Background(), tx, func(ctx context.Context, tx *sql.Tx) error {
			_ = insert("x")(ctx, tx)
			panic("hook fault")
		})
	}()
	if got := rowsIn(t, tx); len(got) != 0 {
		t.Fatalf("rows after a panicking savepoint = %v, want none", got)
	}
}

func TestInSavepointRollsBackDespiteACancelledContext(t *testing.T) {
	for _, failing := range []bool{true, false} {
		_, tx := savepointTx(t)
		ctx, cancel := context.WithCancel(context.Background())
		err := InSavepoint(ctx, tx, func(ctx context.Context, tx *sql.Tx) error {
			_ = insert("x")(ctx, tx)
			cancel()
			if failing {
				return errors.New("fn failed")
			}
			return nil
		})
		if err == nil {
			t.Fatalf("failing=%v: want an error once ctx is cancelled", failing)
		}
		if got := rowsIn(t, tx); len(got) != 0 {
			t.Fatalf("failing=%v: rows = %v, want the savepoint's writes undone", failing, got)
		}
	}
}

func TestInSavepointStagesWorkOnTheOuterQueue(t *testing.T) {
	_, tx := savepointTx(t)
	ctx, outer := WithTxQueue(context.Background(), tx)
	var ran []string
	_ = InSavepoint(ctx, tx, func(ctx context.Context, _ *sql.Tx) error {
		q, _ := CommitQueueFromContext(ctx)
		q.Add(func() { ran = append(ran, "kept") })
		return nil
	})
	_ = InSavepoint(ctx, tx, func(ctx context.Context, _ *sql.Tx) error {
		q, _ := CommitQueueFromContext(ctx)
		q.Add(func() { ran = append(ran, "dropped") })
		return errors.New("rolled back")
	})
	if len(ran) != 0 {
		t.Fatalf("staged work ran before the outer commit: %v", ran)
	}
	outer.RunAfterCommit()
	if len(ran) != 1 || ran[0] != "kept" {
		t.Fatalf("after the outer drain ran = %v, want [kept]", ran)
	}
}
