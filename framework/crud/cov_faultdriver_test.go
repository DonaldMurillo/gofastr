package crud

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"strings"
	"sync"
	"testing"

	sqlite3 "github.com/mattn/go-sqlite3"
)

// cov_faultdriver wraps mattn/go-sqlite3 with a SQL-substring-targeted fault
// injector so tests can deterministically exercise the DB-error-propagation
// branches (Query / rows.Err / Exec / Commit failures) that a healthy
// in-memory database never triggers. Faults are matched on the query text so a
// single handler call's *other* queries run normally — only the targeted
// statement fails.

var errCovInjected = errors.New("cov: injected db fault")

type covFaults struct {
	mu         sync.Mutex
	queryErrOn string // QueryContext returns an error when the SQL contains this
	nextErrOn  string // the matched query's rows.Next returns an error (→ rows.Err)
	execErrOn  string // ExecContext returns an error when the SQL contains this
	commitErr  bool   // tx.Commit returns an error
}

func (f *covFaults) snapshot() covFaults {
	f.mu.Lock()
	defer f.mu.Unlock()
	return covFaults{queryErrOn: f.queryErrOn, nextErrOn: f.nextErrOn, execErrOn: f.execErrOn, commitErr: f.commitErr}
}

func (f *covFaults) set(fn func(*covFaults)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fn(f)
}

func (f *covFaults) reset() {
	f.set(func(c *covFaults) {
		c.queryErrOn, c.nextErrOn, c.execErrOn, c.commitErr = "", "", "", false
	})
}

var covFault = &covFaults{}

type covDriver struct{ inner driver.Driver }

func (d covDriver) Open(dsn string) (driver.Conn, error) {
	c, err := d.inner.Open(dsn)
	if err != nil {
		return nil, err
	}
	return &covConn{inner: c}, nil
}

type covConn struct{ inner driver.Conn }

func (c *covConn) Prepare(q string) (driver.Stmt, error) { return c.inner.Prepare(q) }
func (c *covConn) Close() error                          { return c.inner.Close() }

func (c *covConn) Begin() (driver.Tx, error) {
	tx, err := c.inner.Begin()
	if err != nil {
		return nil, err
	}
	return &covTx{inner: tx}, nil
}

func (c *covConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	bt, ok := c.inner.(driver.ConnBeginTx)
	if !ok {
		return c.Begin()
	}
	tx, err := bt.BeginTx(ctx, opts)
	if err != nil {
		return nil, err
	}
	return &covTx{inner: tx}, nil
}

func (c *covConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	st := covFault.snapshot()
	if st.queryErrOn != "" && strings.Contains(query, st.queryErrOn) {
		return nil, errCovInjected
	}
	qc, ok := c.inner.(driver.QueryerContext)
	if !ok {
		return nil, driver.ErrSkip
	}
	rows, err := qc.QueryContext(ctx, query, args)
	if err != nil {
		return nil, err
	}
	failNext := st.nextErrOn != "" && strings.Contains(query, st.nextErrOn)
	return &covRows{inner: rows, failNext: failNext}, nil
}

func (c *covConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	st := covFault.snapshot()
	if st.execErrOn != "" && strings.Contains(query, st.execErrOn) {
		return nil, errCovInjected
	}
	ec, ok := c.inner.(driver.ExecerContext)
	if !ok {
		return nil, driver.ErrSkip
	}
	return ec.ExecContext(ctx, query, args)
}

type covTx struct{ inner driver.Tx }

func (t *covTx) Commit() error {
	if covFault.snapshot().commitErr {
		return errCovInjected
	}
	return t.inner.Commit()
}
func (t *covTx) Rollback() error { return t.inner.Rollback() }

type covRows struct {
	inner    driver.Rows
	failNext bool
}

func (r *covRows) Columns() []string { return r.inner.Columns() }
func (r *covRows) Close() error      { return r.inner.Close() }
func (r *covRows) Next(dest []driver.Value) error {
	if r.failNext {
		return errCovInjected
	}
	return r.inner.Next(dest)
}

func init() {
	sql.Register("covfault_sqlite", covDriver{inner: &sqlite3.SQLiteDriver{}})
}

// covSetupFaultDB opens a fault-injectable in-memory DB and runs DDL.
func covSetupFaultDB(t *testing.T, ddl ...string) *sql.DB {
	t.Helper()
	db, err := sql.Open("covfault_sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open covfault: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close(); covFault.reset() })
	for _, stmt := range ddl {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("ddl %q: %v", stmt, err)
		}
	}
	return db
}

// Sanity: the driver proxies normally when no fault is set.
func TestCovFaultDriverPassthrough(t *testing.T) {
	covFault.reset()
	db := covSetupFaultDB(t, `CREATE TABLE t (id TEXT PRIMARY KEY, n TEXT)`)
	if _, err := db.Exec(`INSERT INTO t (id, n) VALUES ('1','a')`); err != nil {
		t.Fatalf("insert: %v", err)
	}
	var n string
	if err := db.QueryRow(`SELECT n FROM t WHERE id='1'`).Scan(&n); err != nil || n != "a" {
		t.Fatalf("read: %v n=%q", err, n)
	}
	// Targeted query fault.
	covFault.set(func(c *covFaults) { c.queryErrOn = "SELECT n FROM t" })
	if err := db.QueryRow(`SELECT n FROM t WHERE id='1'`).Scan(&n); !errors.Is(err, errCovInjected) {
		t.Fatalf("expected injected query error, got %v", err)
	}
	covFault.reset()
	// Commit fault.
	covFault.set(func(c *covFaults) { c.commitErr = true })
	tx, _ := db.Begin()
	if err := tx.Commit(); !errors.Is(err, errCovInjected) {
		t.Fatalf("expected injected commit error, got %v", err)
	}
}

var _ = context.Background
