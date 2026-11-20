package tracing

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"

	"github.com/DonaldMurillo/gofastr/core/textsafe"
	"github.com/DonaldMurillo/gofastr/internal/sqlredact"
)

const maxQueryText = 4096

// OpenDB is sql.Open with a client span per Exec, Query, BEGIN, COMMIT and
// ROLLBACK, named by the statement's first keyword.
func OpenDB(driverName, dsn string) (*sql.DB, error) {
	probe, err := sql.Open(driverName, dsn)
	if err != nil {
		return nil, err
	}
	d := probe.Driver()
	_ = probe.Close()
	var c driver.Connector
	if dc, ok := d.(driver.DriverContext); ok {
		if c, err = dc.OpenConnector(dsn); err != nil {
			return nil, err
		}
	} else {
		c = dsnConnector{dsn: dsn, driver: d}
	}
	return sql.OpenDB(WrapConnector(c, dbSystem(driverName))), nil
}

// WrapConnector traces every connection c opens; system is the
// db.system.name attribute ("postgresql", "sqlite", "mysql").
func WrapConnector(c driver.Connector, system string) driver.Connector {
	return &connector{inner: c, system: attribute.String("db.system.name", system), dialect: sqlredact.ForSystem(system)}
}

func dbSystem(driverName string) string {
	switch driverName {
	case "sqlite3", "sqlite":
		return "sqlite"
	case "postgres", "pgx", "pgx/v5":
		return "postgresql"
	case "mysql":
		return "mysql"
	}
	return driverName
}

type dsnConnector struct {
	dsn    string
	driver driver.Driver
}

func (c dsnConnector) Connect(context.Context) (driver.Conn, error) { return c.driver.Open(c.dsn) }
func (c dsnConnector) Driver() driver.Driver                        { return c.driver }

type connector struct {
	inner   driver.Connector
	system  attribute.KeyValue
	dialect sqlredact.Dialect
}

func (c *connector) Connect(ctx context.Context) (driver.Conn, error) {
	conn, err := c.inner.Connect(ctx)
	if err != nil {
		return nil, err
	}
	return &tracedConn{inner: conn, system: c.system, dialect: c.dialect}, nil
}

func (c *connector) Driver() driver.Driver { return c.inner.Driver() }

func (c *connector) Close() error {
	if cl, ok := c.inner.(io.Closer); ok {
		return cl.Close()
	}
	return nil
}

type tracedConn struct {
	inner   driver.Conn
	system  attribute.KeyValue
	dialect sqlredact.Dialect
}

func (c *tracedConn) end(ctx context.Context, query string, start time.Time, err error) {
	span := startAt(ctx, spanName(query), start, c.system)
	defer span.End()
	if !span.IsRecording() {
		return
	}
	span.SetAttributes(attribute.String("db.query.text", queryText(query, c.dialect)))
	if err != nil {
		markError(span, err)
	}
}

func (c *tracedConn) Prepare(query string) (driver.Stmt, error) {
	return c.PrepareContext(context.Background(), query)
}

func (c *tracedConn) PrepareContext(ctx context.Context, query string) (driver.Stmt, error) {
	var st driver.Stmt
	var err error
	if p, ok := c.inner.(driver.ConnPrepareContext); ok {
		st, err = p.PrepareContext(ctx, query)
	} else {
		st, err = c.inner.Prepare(query)
	}
	if err != nil {
		return nil, err
	}
	ts := &tracedStmt{inner: st, conn: c, query: query}
	if cc, ok := st.(driver.ColumnConverter); ok {
		return &convertingStmt{ts, cc}, nil
	}
	return ts, nil
}

type convertingStmt struct {
	*tracedStmt
	cc driver.ColumnConverter
}

func (s *convertingStmt) ColumnConverter(idx int) driver.ValueConverter {
	return s.cc.ColumnConverter(idx)
}

func (c *tracedConn) Close() error { return c.inner.Close() }

func (c *tracedConn) Begin() (driver.Tx, error) {
	return c.BeginTx(context.Background(), driver.TxOptions{})
}

func (c *tracedConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	start := time.Now()
	var tx driver.Tx
	var err error
	if b, ok := c.inner.(driver.ConnBeginTx); ok {
		tx, err = b.BeginTx(ctx, opts)
	} else if opts.Isolation != driver.IsolationLevel(sql.LevelDefault) || opts.ReadOnly {
		err = errors.New("tracing: the driver does not support transaction options")
	} else {
		tx, err = c.inner.Begin()
	}
	c.end(ctx, "BEGIN", start, err)
	if err != nil {
		return nil, err
	}
	return &tracedTx{inner: tx, conn: c, ctx: ctx}, nil
}

func (c *tracedConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	switch e := c.inner.(type) {
	case driver.ExecerContext:
		return traced(c, ctx, query, args, e.ExecContext)
	case driver.Execer:
		return traced(c, ctx, query, args, withoutCtx(e.Exec))
	}
	return nil, driver.ErrSkip
}

func (c *tracedConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	switch q := c.inner.(type) {
	case driver.QueryerContext:
		return traced(c, ctx, query, args, q.QueryContext)
	case driver.Queryer:
		return traced(c, ctx, query, args, withoutCtx(q.Query))
	}
	return nil, driver.ErrSkip
}

func traced[T any](c *tracedConn, ctx context.Context, query string, args []driver.NamedValue,
	run func(context.Context, string, []driver.NamedValue) (T, error)) (T, error) {
	start := time.Now()
	out, err := run(ctx, query, args)
	if errors.Is(err, driver.ErrSkip) {
		var zero T
		return zero, err
	}
	c.end(ctx, query, start, err)
	return out, err
}

func (c *tracedConn) Ping(ctx context.Context) error {
	if p, ok := c.inner.(driver.Pinger); ok {
		return p.Ping(ctx)
	}
	return nil
}

func (c *tracedConn) ResetSession(ctx context.Context) error {
	if r, ok := c.inner.(driver.SessionResetter); ok {
		return r.ResetSession(ctx)
	}
	return nil
}

func (c *tracedConn) IsValid() bool {
	if v, ok := c.inner.(driver.Validator); ok {
		return v.IsValid()
	}
	return true
}

func (c *tracedConn) CheckNamedValue(nv *driver.NamedValue) error {
	if n, ok := c.inner.(driver.NamedValueChecker); ok {
		return n.CheckNamedValue(nv)
	}
	return driver.ErrSkip
}

type tracedTx struct {
	inner driver.Tx
	conn  *tracedConn
	ctx   context.Context
}

func (t *tracedTx) Commit() error {
	start := time.Now()
	err := t.inner.Commit()
	t.conn.end(t.ctx, "COMMIT", start, err)
	return err
}

func (t *tracedTx) Rollback() error {
	start := time.Now()
	err := t.inner.Rollback()
	t.conn.end(t.ctx, "ROLLBACK", start, err)
	return err
}

type tracedStmt struct {
	inner driver.Stmt
	conn  *tracedConn
	query string
}

func (s *tracedStmt) Close() error  { return s.inner.Close() }
func (s *tracedStmt) NumInput() int { return s.inner.NumInput() }

func (s *tracedStmt) Exec(args []driver.Value) (driver.Result, error) {
	return s.ExecContext(context.Background(), valuesToNamed(args))
}

func (s *tracedStmt) Query(args []driver.Value) (driver.Rows, error) {
	return s.QueryContext(context.Background(), valuesToNamed(args))
}

func (s *tracedStmt) ExecContext(ctx context.Context, args []driver.NamedValue) (driver.Result, error) {
	if e, ok := s.inner.(driver.StmtExecContext); ok {
		return traced(s.conn, ctx, s.query, args, stmtCtx(e.ExecContext))
	}
	return traced(s.conn, ctx, s.query, args, withoutCtx(stmtLegacy(s.inner.Exec)))
}

func (s *tracedStmt) QueryContext(ctx context.Context, args []driver.NamedValue) (driver.Rows, error) {
	if q, ok := s.inner.(driver.StmtQueryContext); ok {
		return traced(s.conn, ctx, s.query, args, stmtCtx(q.QueryContext))
	}
	return traced(s.conn, ctx, s.query, args, withoutCtx(stmtLegacy(s.inner.Query)))
}

func stmtCtx[T any](f func(context.Context, []driver.NamedValue) (T, error)) func(context.Context, string, []driver.NamedValue) (T, error) {
	return func(ctx context.Context, _ string, args []driver.NamedValue) (T, error) { return f(ctx, args) }
}

func stmtLegacy[T any](f func([]driver.Value) (T, error)) func(string, []driver.Value) (T, error) {
	return func(_ string, vals []driver.Value) (T, error) { return f(vals) }
}

func (s *tracedStmt) CheckNamedValue(nv *driver.NamedValue) error {
	if n, ok := s.inner.(driver.NamedValueChecker); ok {
		return n.CheckNamedValue(nv)
	}
	return s.conn.CheckNamedValue(nv)
}

func withoutCtx[T any](f func(string, []driver.Value) (T, error)) func(context.Context, string, []driver.NamedValue) (T, error) {
	return func(ctx context.Context, query string, named []driver.NamedValue) (T, error) {
		if err := ctx.Err(); err != nil {
			var zero T
			return zero, err
		}
		vals, err := namedToValues(named)
		if err != nil {
			var zero T
			return zero, err
		}
		return f(query, vals)
	}
}

func namedToValues(named []driver.NamedValue) ([]driver.Value, error) {
	out := make([]driver.Value, len(named))
	for i, nv := range named {
		if nv.Name != "" {
			return nil, errors.New("tracing: the driver does not support named parameters")
		}
		out[i] = nv.Value
	}
	return out, nil
}

func valuesToNamed(vals []driver.Value) []driver.NamedValue {
	out := make([]driver.NamedValue, len(vals))
	for i, v := range vals {
		out[i] = driver.NamedValue{Ordinal: i + 1, Value: v}
	}
	return out
}

func spanName(query string) string {
	q := skipComments(query)
	end := 0
	for end < len(q) && isASCIILetter(q[end]) {
		end++
	}
	if end == 0 {
		return "SQL"
	}
	return strings.ToUpper(q[:end])
}

func skipComments(q string) string {
	for {
		q = strings.TrimLeft(q, " \t\r\n\f\v")
		switch {
		case strings.HasPrefix(q, "--"):
			i := strings.IndexByte(q, '\n')
			if i < 0 {
				return ""
			}
			q = q[i+1:]
		case strings.HasPrefix(q, "/*"):
			i := strings.Index(q[2:], "*/")
			if i < 0 {
				return ""
			}
			q = q[i+4:]
		default:
			return q
		}
	}
}

func queryText(query string, d sqlredact.Dialect) string {
	return textsafe.Truncate(sqlredact.Text(query, d), maxQueryText)
}

func isASCIILetter(c byte) bool { return c|0x20 >= 'a' && c|0x20 <= 'z' }
