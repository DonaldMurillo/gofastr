package tracing

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"testing"
)

// legacyConn is a driver with only the pre-context methods.
type legacyConn struct{ calls int }

func (c *legacyConn) Prepare(string) (driver.Stmt, error) { return &legacyStmt{c}, nil }
func (c *legacyConn) Close() error                        { return nil }
func (c *legacyConn) Begin() (driver.Tx, error)           { return legacyTx{}, nil }
func (c *legacyConn) Exec(string, []driver.Value) (driver.Result, error) {
	c.calls++
	return driver.RowsAffected(1), nil
}
func (c *legacyConn) Query(string, []driver.Value) (driver.Rows, error) {
	c.calls++
	return nil, nil
}

type legacyTx struct{}

func (legacyTx) Commit() error   { return nil }
func (legacyTx) Rollback() error { return nil }

type legacyStmt struct{ c *legacyConn }

func (s *legacyStmt) Close() error  { return nil }
func (s *legacyStmt) NumInput() int { return -1 }
func (s *legacyStmt) Exec([]driver.Value) (driver.Result, error) {
	s.c.calls++
	return driver.RowsAffected(1), nil
}
func (s *legacyStmt) Query([]driver.Value) (driver.Rows, error) {
	s.c.calls++
	return nil, nil
}

func TestLegacyDriverHonorsCanceledCtx(t *testing.T) {
	inner := &legacyConn{}
	c := &tracedConn{inner: inner}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := c.ExecContext(ctx, "UPDATE t SET a = 1", nil); !errors.Is(err, context.Canceled) {
		t.Errorf("conn exec: err = %v, want context.Canceled", err)
	}
	if _, err := c.QueryContext(ctx, "SELECT 1", nil); !errors.Is(err, context.Canceled) {
		t.Errorf("conn query: err = %v, want context.Canceled", err)
	}
	st, err := c.Prepare("UPDATE t SET a = 1")
	if err != nil {
		t.Fatal(err)
	}
	ts := st.(*tracedStmt)
	if _, err := ts.ExecContext(ctx, nil); !errors.Is(err, context.Canceled) {
		t.Errorf("stmt exec: err = %v, want context.Canceled", err)
	}
	if _, err := ts.QueryContext(ctx, nil); !errors.Is(err, context.Canceled) {
		t.Errorf("stmt query: err = %v, want context.Canceled", err)
	}
	if inner.calls != 0 {
		t.Errorf("the driver ran %d statements after cancellation", inner.calls)
	}
}

// convStmt is a statement that converts its own arguments.
type convStmt struct{ legacyStmt }

func (convStmt) ColumnConverter(int) driver.ValueConverter { return driver.DefaultParameterConverter }

type convConn struct{ legacyConn }

func (c *convConn) Prepare(string) (driver.Stmt, error) {
	return &convStmt{legacyStmt{&c.legacyConn}}, nil
}

func TestColumnConverterForwarded(t *testing.T) {
	plain, err := (&tracedConn{inner: &legacyConn{}}).Prepare("SELECT 1")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := plain.(driver.ColumnConverter); ok {
		t.Error("a statement without ColumnConverter gained one")
	}
	conv, err := (&tracedConn{inner: &convConn{}}).Prepare("SELECT 1")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := conv.(driver.ColumnConverter); !ok {
		t.Error("the statement's ColumnConverter was hidden")
	}
}

// A legacy driver's Begin takes no options, so BeginTx refuses the ones it
// would silently drop, and still opens a plain transaction.
func TestLegacyDriverRejectsTxOptions(t *testing.T) {
	c := &tracedConn{inner: &legacyConn{}}
	for _, opts := range []driver.TxOptions{
		{Isolation: driver.IsolationLevel(sql.LevelSerializable)},
		{ReadOnly: true},
	} {
		if _, err := c.BeginTx(context.Background(), opts); err == nil {
			t.Errorf("BeginTx(%+v) on a legacy driver succeeded", opts)
		}
	}
	if _, err := c.BeginTx(context.Background(), driver.TxOptions{}); err != nil {
		t.Errorf("BeginTx with default options: %v", err)
	}
}
