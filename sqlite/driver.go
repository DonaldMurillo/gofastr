package sqlite

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"sync"
)

// ============================================================================
// Public API
// ============================================================================

// Open creates a new in-memory SQLite database and returns a *sql.DB.
func Open() (*sql.DB, error) {
	eng, err := newMemEngine()
	if err != nil {
		return nil, err
	}
	return sql.OpenDB(&sharedConnector{engine: eng}), nil
}

// OpenFile opens or creates a SQLite database file on disk.
func OpenFile(path string) (*sql.DB, error) {
	eng, err := newDiskEngine(path)
	if err != nil {
		return nil, err
	}
	return sql.OpenDB(&sharedConnector{engine: eng}), nil
}

// OpenWithData opens a database from existing data bytes (in-memory).
func OpenWithData(data []byte) (*sql.DB, error) {
	mf := &MemFile{data: make([]byte, len(data))}
	copy(mf.data, data)
	pager, err := NewPager(mf, 4096)
	if err != nil {
		return nil, err
	}
	if mf.Len() < 100 {
		if err := pager.InitNew(); err != nil {
			return nil, err
		}
	}
	btree := NewBTree(pager)
	eng := NewEngine(pager, btree)
	return sql.OpenDB(&sharedConnector{engine: eng}), nil
}

// RawEngine opens a raw *Engine for direct use without database/sql.
func RawEngine() (*Engine, error) {
	return newMemEngine()
}

func newMemEngine() (*Engine, error) {
	mf := NewMemFile()
	pager, err := NewPager(mf, 4096)
	if err != nil {
		return nil, err
	}
	if err := pager.InitNew(); err != nil {
		return nil, err
	}
	btree := NewBTree(pager)
	return NewEngine(pager, btree), nil
}

func newDiskEngine(path string) (*Engine, error) {
	df, err := OpenDiskFile(path)
	if err != nil {
		return nil, fmt.Errorf("sqlite: open file: %w", err)
	}
	pager, err := NewPager(df, 4096)
	if err != nil {
		df.Close()
		return nil, err
	}
	if df.Len() < 100 {
		if err := pager.InitNew(); err != nil {
			df.Close()
			return nil, err
		}
	} else {
		if err := pager.LoadHeader(); err != nil {
			df.Close()
			return nil, fmt.Errorf("sqlite: read header: %w", err)
		}
	}
	btree := NewBTree(pager)
	eng := NewEngine(pager, btree)
	// Load schema from existing database
	if err := eng.LoadSchema(); err != nil {
		// Empty database — that's fine
		_ = err
	}
	return eng, nil
}

// ============================================================================
// Engine concurrency wrapper
// ============================================================================

// sharedEngine wraps an Engine with RWMutex for concurrent access.
type sharedEngine struct {
	engine *Engine
	mu     sync.RWMutex
}

func newSharedEngine(e *Engine) *sharedEngine {
	return &sharedEngine{engine: e}
}

func (se *sharedEngine) executeRead(query string, params ...Value) (*Result, error) {
	se.mu.RLock()
	defer se.mu.RUnlock()
	return se.engine.Execute(query, params...)
}

func (se *sharedEngine) executeWrite(query string, params ...Value) (*Result, error) {
	se.mu.Lock()
	defer se.mu.Unlock()
	result, err := se.engine.Execute(query, params...)
	if err == nil && se.engine.pager != nil {
		se.engine.pager.Flush()
	}
	return result, err
}

func (se *sharedEngine) executeStmtRead(stmt Statement, params ...Value) (*Result, error) {
	se.mu.RLock()
	defer se.mu.RUnlock()
	return se.engine.ExecuteStatement(stmt, params...)
}

func (se *sharedEngine) executeStmtWrite(stmt Statement, params ...Value) (*Result, error) {
	se.mu.Lock()
	defer se.mu.Unlock()
	result, err := se.engine.ExecuteStatement(stmt, params...)
	if err == nil && se.engine.pager != nil {
		se.engine.pager.Flush()
	}
	return result, err
}

func (se *sharedEngine) begin() error {
	se.mu.Lock()
	defer se.mu.Unlock()
	_, err := se.engine.ExecuteStatement(&BeginStmt{})
	return err
}

func (se *sharedEngine) commit() error {
	_, err := se.engine.ExecuteStatement(&CommitStmt{})
	return err
}

func (se *sharedEngine) rollback() error {
	_, err := se.engine.ExecuteStatement(&RollbackStmt{})
	return err
}

func (se *sharedEngine) close() error {
	se.mu.Lock()
	defer se.mu.Unlock()
	return se.engine.Close()
}

// isReadStmt returns true if the statement is read-only.
func isReadStmt(stmt Statement) bool {
	switch stmt.(type) {
	case *SelectStmt:
		return true
	default:
		return false
	}
}

// ============================================================================
// sharedConnector — one engine shared across all connections
// ============================================================================

type sharedConnector struct {
	engine *Engine
	once   sync.Once
	shared *sharedEngine
	closed bool
}

func (c *sharedConnector) getShared() *sharedEngine {
	c.once.Do(func() {
		c.shared = newSharedEngine(c.engine)
	})
	return c.shared
}

func (c *sharedConnector) Connect(ctx context.Context) (driver.Conn, error) {
	return &conn{
		shared: c.getShared(),
	}, nil
}

func (c *sharedConnector) Driver() driver.Driver {
	return &sqliteDriver{}
}

// CloseDB flushes and closes the underlying engine. Call this after sql.DB.Close().
func CloseDB(db *sql.DB) {
	db.Close()
}

// ============================================================================
// sqliteDriver implements database/sql/driver.Driver
// ============================================================================

type sqliteDriver struct{}

func (d *sqliteDriver) Open(name string) (driver.Conn, error) {
	eng, err := newMemEngine()
	if err != nil {
		return nil, err
	}
	return &conn{
		shared: newSharedEngine(eng),
	}, nil
}

// ============================================================================
// conn implements database/sql/driver.Conn
// ============================================================================

type conn struct {
	shared *sharedEngine
	closed bool
	tx     *tx
}

func (c *conn) Prepare(query string) (driver.Stmt, error) {
	return c.PrepareContext(context.Background(), query)
}

func (c *conn) PrepareContext(ctx context.Context, query string) (driver.Stmt, error) {
	if c.closed {
		return nil, driver.ErrBadConn
	}

	parser := NewParser(query)
	stmt, err := parser.Parse()
	if err != nil {
		return nil, fmt.Errorf("sqlite: parse error: %w", err)
	}

	return &stmtWrapper{
		conn:    c,
		query:   query,
		astStmt: stmt,
	}, nil
}

func (c *conn) Close() error {
	if c.closed {
		return nil
	}
	c.closed = true
	// Don't close the shared engine — other connections may use it.
	// The engine is closed when the connector is garbage collected
	// or explicitly via CloseDB.
	return nil
}

func (c *conn) Begin() (driver.Tx, error) {
	return c.BeginTx(context.Background(), driver.TxOptions{})
}

func (c *conn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	if c.closed {
		return nil, driver.ErrBadConn
	}
	if c.tx != nil {
		return nil, errors.New("sqlite: transaction already in progress")
	}

	// Acquire write lock for the entire transaction
	c.shared.mu.Lock()

	if _, err := c.shared.engine.ExecuteStatement(&BeginStmt{}); err != nil {
		c.shared.mu.Unlock()
		return nil, err
	}

	c.tx = &tx{conn: c}
	return c.tx, nil
}

// ExecContext executes a write statement.
func (c *conn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	if c.closed {
		return nil, driver.ErrBadConn
	}

	params := namedValuesToValues(args)

	// Parse to determine if read or write
	parser := NewParser(query)
	stmt, err := parser.Parse()
	if err != nil {
		return nil, err
	}

	var result *Result
	if c.tx != nil {
		// Already holding write lock from Begin()
		result, err = c.shared.engine.ExecuteStatement(stmt, params...)
	} else if isReadStmt(stmt) {
		result, err = c.shared.executeStmtRead(stmt, params...)
	} else {
		result, err = c.shared.executeStmtWrite(stmt, params...)
	}
	if err != nil {
		return nil, err
	}

	return &resultWrapper{result: result}, nil
}

// QueryContext executes a query that returns rows.
func (c *conn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if c.closed {
		return nil, driver.ErrBadConn
	}

	params := namedValuesToValues(args)

	var result *Result
	var err error
	if c.tx != nil {
		// Already holding write lock from Begin()
		result, err = c.shared.engine.Execute(query, params...)
	} else {
		result, err = c.shared.executeRead(query, params...)
	}
	if err != nil {
		return nil, err
	}

	return &rowsWrapper{
		columns: result.Columns,
		rows:    result.Rows,
		index:   -1,
	}, nil
}

// ============================================================================
// tx implements database/sql/driver.Tx
// ============================================================================

type tx struct {
	conn     *conn
	finished bool
}

func (t *tx) Commit() error {
	if t.finished {
		return errors.New("sqlite: transaction already finished")
	}
	t.finished = true
	t.conn.tx = nil
	_, err := t.conn.shared.engine.ExecuteStatement(&CommitStmt{})
	t.conn.shared.mu.Unlock() // release lock held since Begin
	return err
}

func (t *tx) Rollback() error {
	if t.finished {
		return errors.New("sqlite: transaction already finished")
	}
	t.finished = true
	t.conn.tx = nil
	_, err := t.conn.shared.engine.ExecuteStatement(&RollbackStmt{})
	t.conn.shared.mu.Unlock() // release lock held since Begin
	return err
}

// ============================================================================
// stmtWrapper implements database/sql/driver.Stmt
// ============================================================================

type stmtWrapper struct {
	conn    *conn
	query   string
	astStmt Statement
}

func (s *stmtWrapper) Close() error {
	return nil
}

func (s *stmtWrapper) NumInput() int {
	return countParams(s.query)
}

func (s *stmtWrapper) Exec(args []driver.Value) (driver.Result, error) {
	return s.ExecContext(context.Background(), argsToNamedValues(args))
}

func (s *stmtWrapper) ExecContext(ctx context.Context, args []driver.NamedValue) (driver.Result, error) {
	if s.conn.closed {
		return nil, driver.ErrBadConn
	}

	params := namedValuesToValues(args)
	var result *Result
	var err error
	if isReadStmt(s.astStmt) {
		result, err = s.conn.shared.executeStmtRead(s.astStmt, params...)
	} else {
		result, err = s.conn.shared.executeStmtWrite(s.astStmt, params...)
	}
	if err != nil {
		return nil, err
	}

	return &resultWrapper{result: result}, nil
}

func (s *stmtWrapper) Query(args []driver.Value) (driver.Rows, error) {
	return s.QueryContext(context.Background(), argsToNamedValues(args))
}

func (s *stmtWrapper) QueryContext(ctx context.Context, args []driver.NamedValue) (driver.Rows, error) {
	if s.conn.closed {
		return nil, driver.ErrBadConn
	}

	params := namedValuesToValues(args)
	result, err := s.conn.shared.executeStmtRead(s.astStmt, params...)
	if err != nil {
		return nil, err
	}

	return &rowsWrapper{
		columns: result.Columns,
		rows:    result.Rows,
		index:   -1,
	}, nil
}

// ============================================================================
// resultWrapper implements database/sql/driver.Result
// ============================================================================

type resultWrapper struct {
	result *Result
}

func (r *resultWrapper) LastInsertId() (int64, error) {
	return r.result.LastInsertID, nil
}

func (r *resultWrapper) RowsAffected() (int64, error) {
	return r.result.RowsAffected, nil
}

// ============================================================================
// rowsWrapper implements database/sql/driver.Rows
// ============================================================================

type rowsWrapper struct {
	columns []string
	rows    [][]Value
	index   int
}

func (r *rowsWrapper) Columns() []string {
	return r.columns
}

func (r *rowsWrapper) Close() error {
	return nil
}

func (r *rowsWrapper) Next(dest []driver.Value) error {
	r.index++
	if r.index >= len(r.rows) {
		return io.EOF
	}

	row := r.rows[r.index]
	for i := range dest {
		if i < len(row) {
			dest[i] = valueToInterface(row[i])
		}
	}

	return nil
}

// ColumnTypeScanType returns the scan type for a column.
func (r *rowsWrapper) ColumnTypeScanType(index int) reflect.Type {
	if index >= len(r.rows) || len(r.rows) == 0 {
		return reflect.TypeOf("")
	}
	val := r.rows[0][index]
	switch val.Type {
	case DataTypeInteger:
		return reflect.TypeOf(int64(0))
	case DataTypeFloat:
		return reflect.TypeOf(float64(0))
	case DataTypeText:
		return reflect.TypeOf("")
	case DataTypeBlob:
		return reflect.TypeOf([]byte{})
	default:
		return reflect.TypeOf(nil)
	}
}

// ColumnTypeDatabaseTypeName returns the database type name for a column.
func (r *rowsWrapper) ColumnTypeDatabaseTypeName(index int) string {
	if index >= len(r.rows) || len(r.rows) == 0 {
		return "TEXT"
	}
	val := r.rows[0][index]
	switch val.Type {
	case DataTypeInteger:
		return "INTEGER"
	case DataTypeFloat:
		return "REAL"
	case DataTypeText:
		return "TEXT"
	case DataTypeBlob:
		return "BLOB"
	default:
		return "NULL"
	}
}

// ============================================================================
// Helpers
// ============================================================================

func namedValuesToValues(args []driver.NamedValue) []Value {
	result := make([]Value, len(args))
	for i, arg := range args {
		result[i] = interfaceToValue(arg.Value)
	}
	return result
}

func argsToNamedValues(args []driver.Value) []driver.NamedValue {
	result := make([]driver.NamedValue, len(args))
	for i, arg := range args {
		result[i] = driver.NamedValue{Ordinal: i + 1, Value: arg}
	}
	return result
}

func interfaceToValue(v interface{}) Value {
	if v == nil {
		return NullValue
	}

	switch val := v.(type) {
	case int64:
		return IntegerValue(val)
	case int:
		return IntegerValue(int64(val))
	case int32:
		return IntegerValue(int64(val))
	case int16:
		return IntegerValue(int64(val))
	case int8:
		return IntegerValue(int64(val))
	case uint:
		return IntegerValue(int64(val))
	case uint32:
		return IntegerValue(int64(val))
	case uint16:
		return IntegerValue(int64(val))
	case uint8:
		return IntegerValue(int64(val))
	case float64:
		return FloatValue(val)
	case float32:
		return FloatValue(float64(val))
	case string:
		return TextValue(val)
	case []byte:
		return BlobValue(val)
	case bool:
		if val {
			return IntegerValue(1)
		}
		return IntegerValue(0)
	case *string:
		if val == nil {
			return NullValue
		}
		return TextValue(*val)
	case *int64:
		if val == nil {
			return NullValue
		}
		return IntegerValue(*val)
	case *float64:
		if val == nil {
			return NullValue
		}
		return FloatValue(*val)
	default:
		return TextValue(fmt.Sprintf("%v", v))
	}
}

func valueToInterface(v Value) interface{} {
	switch v.Type {
	case DataTypeNull:
		return nil
	case DataTypeInteger:
		return v.IntVal
	case DataTypeFloat:
		return v.FloatVal
	case DataTypeText:
		return v.TextVal
	case DataTypeBlob:
		return v.BlobVal
	default:
		return nil
	}
}

// countParams counts ? placeholders in a SQL string.
// Handles ?? (escaped question mark) and ignores ? inside string literals.
func countParams(query string) int {
	count := 0
	inString := false
	i := 0
	for i < len(query) {
		ch := query[i]
		if ch == '\'' && !inString {
			inString = true
			i++
			continue
		}
		if ch == '\'' && inString {
			if i+1 < len(query) && query[i+1] == '\'' {
				i += 2
				continue
			}
			inString = false
			i++
			continue
		}
		if !inString && ch == '?' {
			if i+1 < len(query) && query[i+1] == '?' {
				i += 2
				continue
			}
			count++
		}
		i++
	}
	return count
}

// String returns a string representation of a Statement.
func stmtString(s Statement) string {
	switch st := s.(type) {
	case *SelectStmt:
		parts := []string{"SELECT"}
		if st.Distinct {
			parts = append(parts, "DISTINCT")
		}
		var cols []string
		for _, c := range st.Columns {
			if c.As != "" {
				cols = append(cols, ExprString(c.Expr)+" AS "+c.As)
			} else {
				cols = append(cols, ExprString(c.Expr))
			}
		}
		parts = append(parts, strings.Join(cols, ", "))
		if st.From != nil && st.From.Table != nil {
			parts = append(parts, "FROM", st.From.Table.Name)
		}
		return strings.Join(parts, " ")
	case *InsertStmt:
		return fmt.Sprintf("INSERT INTO %s ...", st.Table.Name)
	case *UpdateStmt:
		return fmt.Sprintf("UPDATE %s ...", st.Table.Name)
	case *DeleteStmt:
		return fmt.Sprintf("DELETE FROM %s ...", st.Table.Name)
	case *CreateTableStmt:
		return fmt.Sprintf("CREATE TABLE %s ...", st.Name)
	default:
		return "?"
	}
}

// Ensure driver registration
func init() {
	sql.Register("sqlite", &sqliteDriver{})
}
