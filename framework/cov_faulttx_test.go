package framework

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"testing"

	sqlite3 "github.com/mattn/go-sqlite3"
)

// A minimal commit-faulting sqlite wrapper, used to exercise App.InTx's
// tx.Commit() error branch (tx.go) — a healthy DB never fails Commit.

var errCovCommit = errors.New("cov: injected commit fault")

type covTxDriver struct{ inner driver.Driver }

func (d covTxDriver) Open(dsn string) (driver.Conn, error) {
	c, err := d.inner.Open(dsn)
	if err != nil {
		return nil, err
	}
	return covTxConn{inner: c}, nil
}

type covTxConn struct{ inner driver.Conn }

func (c covTxConn) Prepare(q string) (driver.Stmt, error) { return c.inner.Prepare(q) }
func (c covTxConn) Close() error                          { return c.inner.Close() }
func (c covTxConn) Begin() (driver.Tx, error) {
	tx, err := c.inner.Begin()
	if err != nil {
		return nil, err
	}
	return covCommitTx{inner: tx}, nil
}
func (c covTxConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	bt, ok := c.inner.(driver.ConnBeginTx)
	if !ok {
		return c.Begin()
	}
	tx, err := bt.BeginTx(ctx, opts)
	if err != nil {
		return nil, err
	}
	return covCommitTx{inner: tx}, nil
}

type covCommitTx struct{ inner driver.Tx }

func (t covCommitTx) Commit() error   { return errCovCommit }
func (t covCommitTx) Rollback() error { return t.inner.Rollback() }

func init() {
	sql.Register("covfault_fw_commit", covTxDriver{inner: &sqlite3.SQLiteDriver{}})
}

// InTx returns the Commit error when the transaction fails to commit.
func TestCovInTxCommitError(t *testing.T) {
	db, err := sql.Open("covfault_fw_commit", ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })

	app := NewApp(WithDB(db), WithoutDefaultMiddleware())
	// fn succeeds, so InTx proceeds to Commit — which the driver fails.
	err = app.InTx(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
		return nil
	})
	if !errors.Is(err, errCovCommit) {
		t.Fatalf("expected injected commit error, got %v", err)
	}
}
