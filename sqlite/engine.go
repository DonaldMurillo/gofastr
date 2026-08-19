package sqlite

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// Engine is the SQL execution engine.
// It coordinates between the pager, B-tree, schema, and parser.
type Engine struct {
	pager     PagerInterface
	btree     BTreeInterface
	schema    *Schema
	txnSnap   *txnSnapshot
	stmtCache map[string]Statement
	// fkEnforced mirrors PRAGMA foreign_keys, and the pragma now reports and
	// controls it. It used to report 0 while the engine enforced
	// unconditionally and to ignore its SET form entirely — so code gating on
	// the pragma read "off" from a connection that refused every dangling
	// write, and code turning enforcement off got no error and no effect.
	//
	// The default is ON, which is a deliberate divergence from SQLite, whose
	// default is off: the driver this framework ships turns enforcement on for
	// every application DSN, so an engine defaulting off would disagree with
	// every app that uses it.
	fkEnforced bool
	// poolShared marks an engine handed to database/sql through OpenConnector,
	// where one engine backs every connection in the pool and a pragma is
	// therefore database-wide rather than per-connection.
	poolShared  bool
	stmtCacheMu sync.RWMutex
}

// PagerInterface abstracts pager operations the engine needs.
type PagerInterface interface {
	GetPageSize() int
	PageCount() int
	AllocatePage() (int, error)
	GetPageData(num int) ([]byte, error)
	GetPageDataMutable(num int) ([]byte, error)
	SetPageData(num int, data []byte) error
	Flush() error
	Close() error
	StatementSnapshot() *pagerStatementSnapshot
	StatementRestore(*pagerStatementSnapshot)
	GetSchemaPage() int
	SetSchemaPage(page int)
	IsInTxn() bool
	BeginTxn()
	CommitTxn()
	RollbackTxn() error
}

// BTreeInterface abstracts B-tree operations.
type BTreeInterface interface {
	CreateBTree() (int, error)
	Insert(rootPage int, rowid int64, record *Record) error
	Delete(rootPage int, rowid int64) error
	Search(rootPage int, rowid int64) (*Record, error)
	Scan(rootPage int) (CursorInterface, error)
}

// CursorInterface abstracts cursor operations.
type CursorInterface interface {
	Next() bool
	Get() (int64, *Record, error)
	RawRecordData() []byte
	Close() error
}

// NewEngine creates a new execution engine.
func NewEngine(pager PagerInterface, btree BTreeInterface) *Engine {
	return &Engine{
		pager:      pager,
		btree:      btree,
		schema:     NewSchema(),
		stmtCache:  make(map[string]Statement, 64),
		fkEnforced: true,
	}
}

// Schema returns the engine's schema.
func (e *Engine) Schema() *Schema {
	return e.schema
}

// Schema page storage:
//   - Schema start page is stored in header ReservedExpansion[0:4] (big-endian uint32)
//   - Schema pages are allocated right after the last data page
//   - Page 0 of schema = 4-byte total JSON length + JSON chunk
//   - Additional pages: 4-byte chunk length + JSON chunk
//   - A schema start of 0 means no schema exists

const schemaPageSignature uint32 = 0x5343484D // "SCHM" magic

// schemaStartPage returns the page number where schema begins, or 0 if none.
func (e *Engine) schemaStartPage() int {
	if e.pager == nil {
		return 0
	}
	return e.pager.GetSchemaPage()
}

// setSchemaStartPage writes the schema start page into the header.
func (e *Engine) setSchemaStartPage(page int) {
	if e.pager == nil {
		return
	}
	e.pager.SetSchemaPage(page)
}

// LoadSchema loads the schema from the database.
func (e *Engine) LoadSchema() error {
	if e.pager == nil {
		return nil
	}

	startPage := e.schemaStartPage()
	if startPage == 0 || startPage > e.pager.PageCount() {
		return nil
	}

	var data []byte
	for pn := startPage; pn <= e.pager.PageCount(); pn++ {
		page, err := e.pager.GetPageData(pn)
		if err != nil {
			break
		}
		length := int(page[0])<<24 | int(page[1])<<16 | int(page[2])<<8 | int(page[3])
		if length <= 0 || length > len(page)-4 {
			break
		}
		data = append(data, page[4:4+length]...)
		if length < len(page)-4 {
			break
		}
	}

	if len(data) == 0 {
		return nil
	}

	var sd schemaData
	if err := json.Unmarshal(data, &sd); err != nil {
		// Non-empty schema region that won't parse is corruption, not an
		// empty database — surface it so newDiskEngine can refuse the open
		// instead of handing back an engine that silently looks fresh.
		return fmt.Errorf("sqlite: corrupt schema page: %w", err)
	}

	for _, td := range sd.Tables {
		ti := &TableInfo{
			Name:              td.Name,
			RootPage:          td.RootPage,
			SQL:               td.SQL,
			AutoInc:           td.AutoInc,
			PrimaryKey:        td.PrimaryKey,
			UniqueConstraints: td.UniqueConstraints,
		}
		for _, cd := range td.Columns {
			col := ColumnDef{
				Name:         cd.Name,
				Type:         cd.Type,
				Affinity:     ColumnAffinity(cd.Affinity),
				NotNull:      cd.NotNull,
				IsPrimaryKey: cd.IsPK,
				IsRowID:      cd.IsRowID,
			}
			loadColumnDefault(&col, cd)
			ti.Columns = append(ti.Columns, col)
		}
		for _, fd := range td.ForeignKeys {
			ti.ForeignKeys = append(ti.ForeignKeys, ForeignKeyInfo{
				FromCol: fd.FromCol,
				ToTable: fd.ToTable,
				ToCols:  fd.ToCols,
			})
		}
		e.schema.tables[strings.ToLower(ti.Name)] = ti
	}

	for _, idx := range sd.Indexes {
		ii := &IndexInfo{
			Name:      idx.Name,
			TableName: idx.Table,
			RootPage:  idx.RootPage,
			Unique:    idx.Unique,
			SQL:       idx.SQL,
			Where:     idx.Where,
		}
		ii.Columns = append(ii.Columns, idx.Columns...)
		if idx.Where != "" {
			if expr, err := ParseExpression(idx.Where); err == nil && expr != nil {
				ii.WhereExpr = expr
			}
		}
		e.schema.indexes[strings.ToLower(ii.Name)] = ii
	}

	return nil
}

// loadColumnDefault restores a column's DEFAULT from its serialized form.
// New-format files always carry the DEFAULT expression source in
// default_expr (SaveSchema renders the AST via FormatExpr); re-parsing it
// restores both constant fast-path values and non-constant defaults
// (CURRENT_TIMESTAMP et al.). Legacy files stored only the raw TextVal of
// the constant: numbers as their digits, text UNQUOTED (so `pending` must
// not be fed to ParseExpression as the source of truth — it reads as an
// identifier), and non-text non-numeric values as "". For legacy data a
// literal parse is accepted only for non-text types (the numeric
// round-trip); everything else stays the raw text — including the empty
// string, which is a real default (lane TEXT NOT NULL DEFAULT ”) in
// legacy files.
func loadColumnDefault(col *ColumnDef, cd colData) {
	switch {
	case cd.HasDefault && cd.DefaultExpr != "":
		if expr, err := ParseExpression(cd.DefaultExpr); err == nil && expr != nil {
			col.DefaultExpr = expr
			if lit, ok := expr.(LiteralExpr); ok {
				if val, err := (&ExprEval{}).Eval(lit); err == nil {
					col.Default = &val
				}
			}
		}
	case cd.HasDefault:
		if expr, err := ParseExpression(cd.Default); err == nil {
			if lit, ok := expr.(LiteralExpr); ok && lit.Type != DataTypeText {
				if val, err := (&ExprEval{}).Eval(lit); err == nil {
					col.Default = &val
					col.DefaultExpr = expr
				}
			}
		}
		if col.Default == nil {
			v := TextValue(cd.Default)
			col.Default = &v
		}
	}
}

// SaveSchema persists the current schema to the database.
func (e *Engine) SaveSchema() error {
	if e.pager == nil {
		return nil
	}

	sd := schemaData{}
	for _, ti := range e.schema.tables {
		d := tableData{Name: ti.Name, RootPage: ti.RootPage, SQL: ti.SQL, AutoInc: ti.AutoInc, PrimaryKey: ti.PrimaryKey}
		d.UniqueConstraints = ti.UniqueConstraints
		for _, c := range ti.Columns {
			cd := colData{Name: c.Name, Type: c.Type, Affinity: int(c.Affinity), NotNull: c.NotNull, IsPK: c.IsPrimaryKey, IsRowID: c.IsRowID}
			if c.Default != nil {
				cd.HasDefault = true
				cd.Default = c.Default.TextVal
			}
			// Persist non-constant defaults (CURRENT_TIMESTAMP,
			// datetime('now'), parenthesized expressions, ...) by
			// rendering their AST back to source text that LoadSchema
			// can re-parse. Without this, file-backed databases lose
			// dynamic defaults on reopen and NOT NULL inserts fail.
			if c.DefaultExpr != nil {
				src := FormatExpr(c.DefaultExpr)
				if src == "" {
					// Dropping it here would reopen the database with a
					// column that has no default at all. Statements written
					// against the declared schema would then behave
					// differently after a restart than before it, which is
					// worse than refusing to save.
					return &engineError{"cannot persist default for " + ti.Name + "." + c.Name}
				}
				cd.DefaultExpr = src
				cd.HasDefault = true
			}
			d.Columns = append(d.Columns, cd)
		}
		for _, fk := range ti.ForeignKeys {
			d.ForeignKeys = append(d.ForeignKeys, fkDataSer{FromCol: fk.FromCol, ToTable: fk.ToTable, ToCols: fk.ToCols})
		}
		sd.Tables = append(sd.Tables, d)
	}
	for _, ii := range e.schema.indexes {
		id := indexData{Name: ii.Name, Table: ii.TableName, RootPage: ii.RootPage, Unique: ii.Unique, SQL: ii.SQL, Columns: ii.Columns}
		if ii.WhereExpr != nil {
			src := FormatExpr(ii.WhereExpr)
			if src == "" {
				// See executeCreateIndex: a dropped predicate reads back as a
				// full index, not as an absent one.
				return &engineError{"cannot persist partial index predicate for " + ii.Name}
			}
			id.Where = src
		} else if ii.Where != "" {
			// Legacy/unparsed source retained verbatim.
			id.Where = ii.Where
		}
		sd.Indexes = append(sd.Indexes, id)
	}

	data, err := json.Marshal(sd)
	if err != nil {
		return err
	}

	pageSize := e.pager.GetPageSize()
	chunkSize := pageSize - 4

	// Calculate how many schema pages we need
	needed := (len(data) + chunkSize - 1) / chunkSize
	if needed == 0 {
		needed = 1
	}

	// Allocate schema pages right after current last page
	startPage := e.pager.PageCount() + 1
	for i := 0; i < needed; i++ {
		e.pager.AllocatePage()
	}

	// Store schema start page in header
	e.setSchemaStartPage(startPage)

	pn := startPage
	for len(data) > 0 {
		var chunk []byte
		if len(data) > chunkSize {
			chunk = data[:chunkSize]
			data = data[chunkSize:]
		} else {
			chunk = data
			data = nil
		}
		page := make([]byte, pageSize)
		page[0] = byte(len(chunk) >> 24)
		page[1] = byte(len(chunk) >> 16)
		page[2] = byte(len(chunk) >> 8)
		page[3] = byte(len(chunk))
		copy(page[4:], chunk)
		if err := e.pager.SetPageData(pn, page); err != nil {
			return fmt.Errorf("save schema page %d: %w", pn, err)
		}
		pn++
	}

	// Defer the flush when inside a transaction: flushed pages cannot
	// be un-flushed by RollbackTxn's cache restore, so writing now
	// would let a rolled-back DDL (CREATE TABLE / CREATE INDEX inside
	// BEGIN ... ROLLBACK) leak to disk and resurface after reopen.
	// CommitTxn performs the deferred flush at transaction end.
	if e.pager.IsInTxn() {
		return nil
	}
	return e.pager.Flush()
}

// Close flushes and closes the underlying pager.
func (e *Engine) Close() error {
	if e.pager != nil {
		return e.pager.Close()
	}
	return nil
}

// Execute parses and executes a SQL statement.
func (e *Engine) Execute(sql string, params ...Value) (*Result, error) {
	return e.ExecuteWithCache(sql, params...)
}

// ExecuteWithCache uses the statement cache to avoid re-parsing.
func (e *Engine) ExecuteWithCache(sql string, params ...Value) (*Result, error) {
	// Try prepared statement cache (read lock)
	e.stmtCacheMu.RLock()
	stmt, cached := e.stmtCache[sql]
	e.stmtCacheMu.RUnlock()

	if !cached {
		parser := NewParser(sql)
		var err error
		stmt, err = parser.Parse()
		if err != nil {
			return nil, err
		}
		// Cache the parsed statement (write lock)
		e.stmtCacheMu.Lock()
		if len(e.stmtCache) < 256 {
			e.stmtCache[sql] = stmt
		}
		e.stmtCacheMu.Unlock()
	}
	return e.ExecuteStatement(stmt, params...)
}

// ExecuteAll parses and executes multiple SQL statements separated by semicolons.
func (e *Engine) ExecuteAll(sql string, params ...Value) ([]*Result, error) {
	parser := NewParser(sql)
	stmts, err := parser.ParseAll()
	if err != nil {
		return nil, err
	}

	var results []*Result
	for _, stmt := range stmts {
		res, err := e.ExecuteStatement(stmt, params...)
		if err != nil {
			return results, err
		}
		results = append(results, res)
	}
	return results, nil
}

// ExecuteStatement executes a parsed statement.
func (e *Engine) ExecuteStatement(stmt Statement, params ...Value) (*Result, error) {
	switch s := stmt.(type) {
	case *SelectStmt:
		return e.executeSelect(s, params)
	case *CompoundSelect:
		return e.executeCompoundSelect(s, params)
	case *BeginStmt:
		return e.executeBegin()
	case *CommitStmt:
		return e.executeCommit()
	case *RollbackStmt:
		return e.executeRollback()
	default:
		// All other statements are mutations — ensure COW is active
		return e.executeMutationAtomic(stmt, s, params)
	}
}

// invalidateStmtCache clears the prepared statement cache.
// Called when DDL changes the schema.
func (e *Engine) invalidateStmtCache() {
	e.stmtCacheMu.Lock()
	for k := range e.stmtCache {
		delete(e.stmtCache, k)
	}
	e.stmtCacheMu.Unlock()
}

// isReadStmt returns true if the statement is a SELECT (read-only).

func (e *Engine) executeMutation(stmt Statement, s Statement, params []Value) (*Result, error) {
	switch s := s.(type) {
	case *InsertStmt:
		return e.executeInsert(s, params)
	case *UpdateStmt:
		return e.executeUpdate(s, params)
	case *DeleteStmt:
		return e.executeDelete(s, params)
	case *CreateTableStmt:
		return e.persistSchema(e.executeCreateTable(s))
	case *CreateIndexStmt:
		return e.persistSchema(e.executeCreateIndex(s))
	case *DropTableStmt:
		return e.persistSchema(e.executeDropTable(s))
	case *DropIndexStmt:
		return e.persistSchema(e.executeDropIndex(s))
	case *AlterAddColumnStmt:
		return e.persistSchema(e.executeAlterAddColumn(s))
	case *AlterRenameTableStmt:
		return e.persistSchema(e.executeAlterRenameTable(s))
	case *AlterRenameColumnStmt:
		return e.persistSchema(e.executeAlterRenameColumn(s))
	case *PragmaStmt:
		return e.executePragma(s)
	case *VacuumStmt:
		return &Result{}, nil
	case *ReindexStmt:
		return &Result{}, nil
	case *CreateViewStmt:
		return e.persistSchema(e.executeCreateView(s))
	case *DropViewStmt:
		return e.persistSchema(e.executeDropView(s))
	default:
		return nil, &engineError{"unsupported statement type"}
	}
}

// persistSchema finishes a DDL statement: it invalidates the prepared-statement
// cache and writes the schema out, and it RETURNS the write's error.
//
// Every one of these call sites used to discard it — `e.SaveSchema()` with no
// assignment, ten times over. A DDL statement whose schema write failed
// therefore reported success, and a file-backed database reopened without the
// table the caller had just been told it created. Reporting the failure is
// worse for nobody: the in-memory schema has the change either way, and the
// caller is the only one who can decide what to do about a database that
// could not record it.
func (e *Engine) persistSchema(res *Result, err error) (*Result, error) {
	if err != nil {
		return res, err
	}
	e.invalidateStmtCache()
	if err := e.SaveSchema(); err != nil {
		return nil, err
	}
	return res, nil
}

// Result represents the result of executing a statement.
type Result struct {
	Columns      []string
	Rows         [][]Value
	RowsAffected int64
	LastInsertID int64
}

// ============================================================================
// SELECT
// ============================================================================

// joinEntry tracks a table participating in a JOIN.
type joinEntry struct {
	info    *TableInfo
	alias   string // effective name (alias if set, otherwise table name)
	offset  int    // column offset in the combined row
	columns int    // number of columns (not including rowid)
}

// buildJoinPlan builds the list of tables involved in a SELECT.
func (e *Engine) buildJoinPlan(s *SelectStmt) ([]joinEntry, map[string]map[string]int, error) {
	var tables []joinEntry
	tableMap := make(map[string]map[string]int)
	offset := 1 // position 0 is reserved for the driving table's rowid

	// Driving table
	tableName := s.From.Table.Name
	tableInfo, ok := e.schema.GetTable(tableName)
	if !ok {
		return nil, nil, &engineError{"no such table: " + tableName}
	}
	alias := strings.ToLower(tableName)
	if s.From.Table.As != "" {
		alias = strings.ToLower(s.From.Table.As)
	}
	tables = append(tables, joinEntry{info: tableInfo, alias: alias, offset: 1, columns: len(tableInfo.Columns)})

	// Column map for driving table
	colMap := map[string]int{"rowid": 0}
	for i, col := range tableInfo.Columns {
		colMap[strings.ToLower(col.Name)] = 1 + i
	}
	tableMap[alias] = colMap
	// Also map by real table name if aliased
	if s.From.Table.As != "" {
		tableMap[strings.ToLower(tableName)] = colMap
	}

	offset = 1 + len(tableInfo.Columns)

	// Joined tables
	for _, j := range s.From.Joins {
		jTableName := j.Table.Name
		jInfo, ok := e.schema.GetTable(jTableName)
		if !ok {
			return nil, nil, &engineError{"no such table: " + jTableName}
		}
		jAlias := strings.ToLower(jTableName)
		if j.Table.As != "" {
			jAlias = strings.ToLower(j.Table.As)
		}
		tables = append(tables, joinEntry{info: jInfo, alias: jAlias, offset: offset, columns: len(jInfo.Columns)})

		jColMap := map[string]int{"rowid": offset - 1} // not really needed but for completeness
		for i, col := range jInfo.Columns {
			jColMap[strings.ToLower(col.Name)] = offset + i
		}
		tableMap[jAlias] = jColMap
		if j.Table.As != "" {
			tableMap[strings.ToLower(jTableName)] = jColMap
		}

		offset += len(jInfo.Columns)
	}

	return tables, tableMap, nil
}

// combinedRowLen returns the total length of a combined row for the given join entries.
func combinedRowLen(entries []joinEntry) int {
	if len(entries) == 0 {
		return 0
	}
	// 1 for driving rowid + columns for each table
	last := entries[len(entries)-1]
	return last.offset + last.columns
}

// outCol represents an output column during SELECT execution.
type outCol struct {
	expr Expr
	name string
}

func (e *Engine) executeSelect(s *SelectStmt, params []Value) (*Result, error) {
	if s.From == nil || s.From.Table == nil {
		return e.executeSelectNoFrom(s, params)
	}

	// Check if the FROM table is a view (before buildJoinPlan)
	if s.From != nil && s.From.Table != nil && len(s.From.Joins) == 0 {
		tableName := strings.ToLower(s.From.Table.Name)
		if view, ok := e.schema.views[tableName]; ok {
			return e.executeViewSelect(view, s, params)
		}
	}

	// Build join plan
	tables, tableMap, err := e.buildJoinPlan(s)
	if err != nil {
		return nil, err
	}
	hasJoins := len(tables) > 1
	driveInfo := tables[0].info

	// Build flat column map for unqualified lookups
	flatColumnMap := map[string]int{"rowid": 0}
	for _, t := range tables {
		for i, col := range t.info.Columns {
			name := strings.ToLower(col.Name)
			if _, exists := flatColumnMap[name]; !exists {
				flatColumnMap[name] = t.offset + i
			}
		}
	}

	// Determine output columns and which source columns to evaluate
	// Expand StarColumn into individual columns
	var outputCols []outCol

	for _, col := range s.Columns {
		if _, ok := col.Expr.(StarColumn); ok {
			if hasJoins {
				for _, t := range tables {
					for _, c := range t.info.Columns {
						outputCols = append(outputCols, outCol{
							expr: ColumnRef{Table: t.alias, Column: strings.ToLower(c.Name)},
							name: c.Name,
						})
					}
				}
			} else {
				for _, c := range driveInfo.Columns {
					outputCols = append(outputCols, outCol{
						expr: ColumnRef{Column: strings.ToLower(c.Name)},
						name: c.Name,
					})
				}
			}
		} else {
			var name string
			if col.As != "" {
				name = col.As
			} else {
				switch e := col.Expr.(type) {
				case ColumnRef:
					name = e.Column
				default:
					name = "expr_" + formatInt64(int64(len(outputCols)))
				}
			}
			outputCols = append(outputCols, outCol{expr: col.Expr, name: name})
		}
	}

	columns := make([]string, len(outputCols))
	for i, c := range outputCols {
		columns[i] = c.name
	}

	// Detect aggregates before scanning (needed for early exit decision)
	hasAgg := false
	for _, c := range outputCols {
		if fc, ok := c.expr.(FunctionCall); ok {
			if isAggregateFunc(fc.Name) {
				hasAgg = true
				break
			}
		}
	}

	// Pre-evaluate LIMIT and OFFSET for early termination
	var limitN, offsetN int
	hasLimit := false

	// Check if ORDER BY is just rowid/primary key — B-tree scan is already ordered
	orderByIsRowid := len(s.OrderBy) == 1 && !hasAgg && !s.OrderBy[0].Desc
	if orderByIsRowid {
		if colRef, ok := s.OrderBy[0].Expr.(ColumnRef); ok {
			colName := strings.ToLower(colRef.Column)
			if colName != "rowid" && (driveInfo.PrimaryKey < 0 || colName != strings.ToLower(driveInfo.Columns[driveInfo.PrimaryKey].Name)) {
				orderByIsRowid = false
			}
		} else {
			orderByIsRowid = false
		}
	}
	canEarlyExit := (len(s.OrderBy) == 0 || orderByIsRowid) && !hasAgg

	if s.Limit != nil {
		limitEval := &ExprEval{Params: params}
		if limitVal, err := limitEval.Eval(s.Limit); err == nil {
			if n, ok := limitVal.AsInt64(); ok && n >= 0 {
				limitN = int(n)
				hasLimit = true
			}
		}
	}
	if s.Offset != nil {
		offsetEval := &ExprEval{Params: params}
		if offsetVal, err := offsetEval.Eval(s.Offset); err == nil {
			if n, ok := offsetVal.AsInt64(); ok && n >= 0 {
				offsetN = int(n)
			}
		}
	}
	// Try index scan for simple single-table queries
	var indexRows [][]Value
	if !hasJoins {
		indexRows = e.tryIndexScan(driveInfo.Name, s.Where, params)
	}

	rowLen := combinedRowLen(tables)
	if rowLen == 0 {
		rowLen = 1 + len(driveInfo.Columns)
	}

	// Scan and filter
	var rows [][]Value
	var groupKeys []string // pre-computed GROUP BY keys per row
	matched := 0

	// Open driving cursor — use seek when ORDER BY rowid + OFFSET to skip rows
	var driveCursor CursorInterface
	if canEarlyExit && offsetN > 0 && orderByIsRowid {
		bc, err := e.btree.Scan(driveInfo.RootPage)
		if err != nil {
			return nil, err
		}
		if err := bc.(*BTreeCursor).SeekToRowid(int64(offsetN + 1)); err != nil {
			bc.Close()
			return nil, err
		}
		driveCursor = bc
		// We've skipped offsetN rows via seek
		matched = offsetN
	} else {
		driveCursor, err = e.btree.Scan(driveInfo.RootPage)
		if err != nil {
			return nil, err
		}
	}
	defer driveCursor.Close()

	// If index scan produced results, use those directly
	if indexRows != nil {
		idxEval := &ExprEval{
			ColumnMap: flatColumnMap,
			TableMap:  tableMap,
			Params:    params,
		}
		for _, combined := range indexRows {
			matched++
			if canEarlyExit && offsetN > 0 && matched <= offsetN {
				continue
			}
			resultRow := make([]Value, len(outputCols))
			idxEval.Row = combined
			for i, col := range outputCols {
				val, err := idxEval.Eval(col.expr)
				if err != nil {
					return nil, err
				}
				resultRow[i] = val
			}
			rows = append(rows, resultRow)
			if len(s.GroupBy) > 0 {
				groupKeys = append(groupKeys, e.evalGroupKey(combined, s.GroupBy, flatColumnMap, tableMap, params))
			}
			if canEarlyExit && hasLimit && len(rows) >= limitN {
				break
			}
		}
	} else {

		// Pre-allocate eval for reuse across rows
		emitEval := &ExprEval{
			ColumnMap: flatColumnMap,
			TableMap:  tableMap,
			Params:    params,
			Engine:    e,
		}

		emitRow := func(combined []Value) error {
			emitEval.Row = combined

			if s.Where != nil {
				val, err := emitEval.Eval(s.Where)
				if err != nil {
					return err
				}
				if val.IsNull() {
					return nil
				}
				if b, ok := val.AsInt64(); !ok || b == 0 {
					return nil
				}
			}

			matched++
			if canEarlyExit && offsetN > 0 && matched <= offsetN {
				return nil
			}

			resultRow := make([]Value, len(outputCols))
			for i, col := range outputCols {
				val, err := emitEval.Eval(col.expr)
				if err != nil {
					return err
				}
				resultRow[i] = val
			}
			rows = append(rows, resultRow)
			if len(s.GroupBy) > 0 {
				groupKeys = append(groupKeys, e.evalGroupKey(combined, s.GroupBy, flatColumnMap, tableMap, params))
			}

			if canEarlyExit && hasLimit && len(rows) >= limitN {
				return errEarlyExit
			}
			return nil
		}

		// Check if any join is RIGHT or FULL — these require scanning from the right side
		// and cannot use the standard left-driving nested-loop approach.
		needsRightDrive := false
		for _, j := range s.From.Joins {
			if j.Type == JoinRight || j.Type == JoinFull {
				needsRightDrive = true
				break
			}
		}

		if needsRightDrive {
			// Don't iterate the driving table — let the RIGHT/FULL join
			// handle both sides internally.
			combined := make([]Value, rowLen)
			if err := e.probeJoins(combined, tables, s.From.Joins, 1, emitRow); err != nil {
				if err == errEarlyExit {
					goto doneScan
				}
				return nil, err
			}
		} else {
			// Compute WHERE column indices for lazy evaluation (only worthwhile
			// when table has many columns but WHERE only needs a few)
			var whereRecordCols []int // record column indices needed by WHERE
			var whereOnlyRefs bool
			if s.Where != nil && !hasJoins && len(driveInfo.Columns) > 4 {
				whereRefs := CollectColumnRefs(s.Where)
				for _, ref := range whereRefs {
					if ref.Table != "" {
						if tmap, ok := tableMap[strings.ToLower(ref.Table)]; ok {
							if idx, ok := tmap[strings.ToLower(ref.Column)]; ok {
								recIdx := idx - tables[0].offset
								if recIdx >= 0 {
									whereRecordCols = append(whereRecordCols, recIdx)
								}
							}
						}
					} else {
						colName := strings.ToLower(ref.Column)
						if colName == "rowid" {
							continue
						}
						if idx, ok := flatColumnMap[colName]; ok {
							recIdx := idx - tables[0].offset
							if recIdx >= 0 {
								whereRecordCols = append(whereRecordCols, recIdx)
							}
						}
					}
				}
				whereOnlyRefs = len(whereRecordCols) > 0 && len(driveInfo.Columns) > len(whereRecordCols)+2
			}

			var combined []Value // reusable buffer for scan loop
			for driveCursor.Next() {
				rowid, record, err := driveCursor.Get()
				if err != nil {
					return nil, err
				}

				// Lazy WHERE eval: if WHERE only needs a subset of columns,
				// evaluate it from raw record data before decoding all columns
				if whereOnlyRefs && !hasJoins && s.Where != nil {
					// Build partial combined row with only WHERE columns
					partialCombined := make([]Value, rowLen)
					partialCombined[0] = IntegerValue(rowid)

					rawData := driveCursor.RawRecordData()
					if rawData != nil {
						for _, recIdx := range whereRecordCols {
							val, _ := DecodeRecordColumn(rawData, recIdx)
							partialCombined[tables[0].offset+recIdx] = val
						}

						// Evaluate WHERE with partial row
						emitEval.Row = partialCombined
						whereVal, wErr := emitEval.Eval(s.Where)
						if wErr == nil {
							if whereVal.IsNull() {
								continue
							}
							if b, ok := whereVal.AsInt64(); !ok || b == 0 {
								continue
							}
						}
					}
					// WHERE passed — fall through to full decode
				}

				if combined == nil {
					combined = make([]Value, rowLen)
				}
				vals := recordToValues(record, driveInfo)
				combined[0] = IntegerValue(rowid)
				for i, v := range vals {
					combined[1+i] = v
				}

				if !hasJoins {
					if err := emitRow(combined); err != nil {
						if err == errEarlyExit {
							break
						}
						return nil, err
					}
					continue
				}

				if err := e.probeJoins(combined, tables, s.From.Joins, 1, emitRow); err != nil {
					if err == errEarlyExit {
						break
					}
					return nil, err
				}
			}
		}
	} // end else (table scan)

doneScan:
	// Apply GROUP BY + aggregates
	if hasAgg || len(s.GroupBy) > 0 {
		grouped, err := e.applyGroupBy(rows, groupKeys, s, outputCols, params)
		if err != nil {
			return nil, err
		}
		rows = grouped
	}

	// Apply ORDER BY (skip if ORDER BY is rowid — already sorted by B-tree scan)
	if len(s.OrderBy) > 0 && !orderByIsRowid {
		rows = e.sortRows(rows, s.OrderBy, columns, driveInfo, params, outputCols)
	}

	// Apply DISTINCT
	if s.Distinct {
		rows = deduplicateRows(rows)
	}

	// Apply OFFSET/LIMIT post-processing (only needed when early exit was not used)
	if !canEarlyExit {
		if s.Offset != nil {
			offsetEval := &ExprEval{Params: params}
			offsetVal, err := offsetEval.Eval(s.Offset)
			if err == nil {
				if off, ok := offsetVal.AsInt64(); ok && off >= 0 && int(off) < len(rows) {
					rows = rows[off:]
				} else if ok && int(off) >= len(rows) {
					rows = nil
				}
			}
		}

		if s.Limit != nil {
			limitEval := &ExprEval{Params: params}
			limitVal, err := limitEval.Eval(s.Limit)
			if err == nil {
				if n, ok := limitVal.AsInt64(); ok && n >= 0 && int(n) < len(rows) {
					rows = rows[:n]
				}
			}
		}
	}

	return &Result{
		Columns: columns,
		Rows:    rows,
	}, nil
}

// applyGroupBy groups rows by GROUP BY keys, computes aggregates per group,
// applies HAVING filter, and returns the resulting rows.
func (e *Engine) applyGroupBy(rows [][]Value, groupKeys []string, s *SelectStmt, outputCols []outCol, params []Value) ([][]Value, error) {
	if len(s.GroupBy) == 0 {
		// No GROUP BY -- treat all rows as one group
		aggRow := e.computeAggregateRow(rows, outputCols, s, params)
		return [][]Value{aggRow}, nil
	}

	type group struct {
		key  string
		rows [][]Value
	}
	groupMap := make(map[string]*group)
	var groupOrder []string

	for i, row := range rows {
		var key string
		if i < len(groupKeys) {
			key = groupKeys[i]
		}
		if g, ok := groupMap[key]; ok {
			g.rows = append(g.rows, row)
		} else {
			g := &group{key: key, rows: [][]Value{row}}
			groupMap[key] = g
			groupOrder = append(groupOrder, key)
		}
	}

	var result [][]Value
	for _, key := range groupOrder {
		g := groupMap[key]
		aggRow := e.computeAggregateRow(g.rows, outputCols, s, params)

		// Apply HAVING filter
		if s.Having != nil {
			flatColMap := make(map[string]int)
			for i, col := range outputCols {
				if col.name != "" {
					flatColMap[strings.ToLower(col.name)] = i
				}
			}
			havingEval := &ExprEval{
				Row:       aggRow,
				ColumnMap: flatColMap,
				Params:    params,
			}
			val, err := havingEval.EvalAggregateAware(s.Having, outputCols, aggRow, g.rows)
			if err != nil {
				continue
			}
			if val.IsNull() {
				continue
			}
			if b, ok := val.AsInt64(); !ok || b == 0 {
				continue
			}
		}
		result = append(result, aggRow)
	}
	return result, nil
}

// evalGroupKey evaluates GROUP BY expressions against a source row to produce a group key.
func (e *Engine) evalGroupKey(srcRow []Value, groupBy []Expr, colMap map[string]int, tableMap map[string]map[string]int, params []Value) string {
	var buf strings.Builder
	eval := &ExprEval{Row: srcRow, ColumnMap: colMap, TableMap: tableMap, Params: params}
	for _, expr := range groupBy {
		val, err := eval.Eval(expr)
		if err != nil {
			buf.WriteByte(0)
			continue
		}
		switch val.Type {
		case DataTypeNull:
			buf.WriteByte(0)
		case DataTypeInteger:
			buf.WriteString(fmt.Sprintf("i%d", val.IntVal))
		case DataTypeFloat:
			buf.WriteString(fmt.Sprintf("f%f", val.FloatVal))
		case DataTypeText:
			buf.WriteString(fmt.Sprintf("t%s", val.TextVal))
		case DataTypeBlob:
			buf.WriteString(fmt.Sprintf("b%x", val.BlobVal))
		}
		buf.WriteByte(0)
	}
	return buf.String()
}

// computeAggregateRow computes aggregate functions for a group of rows.
func (e *Engine) computeAggregateRow(rows [][]Value, outputCols []outCol, s *SelectStmt, params []Value) []Value {
	aggRow := make([]Value, len(outputCols))
	for i, c := range outputCols {
		fc, ok := c.expr.(FunctionCall)
		if !ok || !isAggregateFunc(fc.Name) {
			// Non-aggregate column: take value from first row in group
			if len(rows) > 0 && i < len(rows[0]) {
				aggRow[i] = rows[0][i]
			} else {
				aggRow[i] = NullValue
			}
			continue
		}
		aggRow[i] = e.computeAggregate(fc, rows, i)
	}
	return aggRow
}

// aggregateKey renders a value into a comparison key for DISTINCT. The type
// prefix keeps the integer 7 and the text '7' distinct, which is what SQLite's
// storage classes do.
func aggregateKey(v Value) string {
	return formatInt64(int64(v.Type)) + ":" + v.AsText()
}

// computeAggregate evaluates a single aggregate function over a set of rows.
func (e *Engine) computeAggregate(fc FunctionCall, rows [][]Value, colIdx int) Value {
	switch strings.ToUpper(fc.Name) {
	case "COUNT":
		if fc.Star || len(fc.Args) == 0 {
			return IntegerValue(int64(len(rows)))
		}
		count := 0
		seen := map[string]bool{}
		for _, r := range rows {
			if colIdx >= len(r) || r[colIdx].IsNull() {
				continue
			}
			if fc.Distinct {
				// COUNT(DISTINCT v) counted every non-NULL row: the DISTINCT
				// keyword parsed and was then ignored, so the answer was
				// COUNT(v) under a different name.
				key := aggregateKey(r[colIdx])
				if seen[key] {
					continue
				}
				seen[key] = true
			}
			count++
		}
		return IntegerValue(int64(count))
	case "SUM":
		var sum float64
		var intSum int64
		allInt := true
		any := false
		for _, r := range rows {
			if colIdx >= len(r) || r[colIdx].IsNull() {
				continue
			}
			v, ok := r[colIdx].AsFloat64()
			if !ok {
				continue
			}
			any = true
			sum += v
			if r[colIdx].Type == DataTypeInteger {
				intSum += r[colIdx].IntVal
			} else {
				allInt = false
			}
		}
		if !any {
			// SUM over no values is NULL, not 0 — SQLite reserves 0 for
			// TOTAL(). Returning 0 makes "no rows" indistinguishable from
			// "rows that sum to zero", which is the difference a caller
			// checking for an empty result is asking about.
			return NullValue
		}
		if allInt {
			return IntegerValue(intSum)
		}
		return FloatValue(sum)
	case "AVG":
		if len(rows) == 0 {
			return NullValue
		}
		var sum float64
		count := 0
		for _, r := range rows {
			if colIdx < len(r) && !r[colIdx].IsNull() {
				if v, ok := r[colIdx].AsFloat64(); ok {
					sum += v
					count++
				}
			}
		}
		if count == 0 {
			return NullValue
		}
		return FloatValue(sum / float64(count))
	case "MIN", "MAX":
		// NULLs are skipped, not compared. Seeding the running value with the
		// first row and comparing everything meant a single NULL won every
		// MIN — NULL sorts before every value — so MIN(v) reported NULL for
		// any column that had one, which is most nullable columns.
		want := -1
		if strings.EqualFold(fc.Name, "MAX") {
			want = 1
		}
		var best Value
		found := false
		for _, r := range rows {
			if colIdx >= len(r) || r[colIdx].IsNull() {
				continue
			}
			if !found {
				best, found = r[colIdx], true
				continue
			}
			if cmp := CompareValues(r[colIdx], best); (want < 0 && cmp < 0) || (want > 0 && cmp > 0) {
				best = r[colIdx]
			}
		}
		if !found {
			return NullValue
		}
		return best
	default:
		return NullValue
	}
}

func (e *Engine) executeCompoundSelect(cs *CompoundSelect, params []Value) (*Result, error) {
	// Recursively collect rows from left side
	leftResult, err := e.executeAnySelect(cs.Left, params)
	if err != nil {
		return nil, err
	}
	rightResult, err := e.executeAnySelect(cs.Right, params)
	if err != nil {
		return nil, err
	}

	// Use left's column names
	columns := leftResult.Columns
	var rows [][]Value

	switch cs.Op {
	case SetOpUnionAll:
		rows = append(rows, leftResult.Rows...)
		rows = append(rows, rightResult.Rows...)
	case SetOpUnion:
		rows = append(rows, leftResult.Rows...)
		rows = append(rows, rightResult.Rows...)
		rows = deduplicateRows(rows)
	case SetOpIntersect:
		rows = intersectRows(leftResult.Rows, rightResult.Rows)
	case SetOpExcept:
		rows = exceptRows(leftResult.Rows, rightResult.Rows)
	}

	// ORDER BY on compound
	if len(cs.OrderBy) > 0 {
		rows = e.sortRows(rows, cs.OrderBy, columns, nil, params, nil)
	}

	// LIMIT/OFFSET on compound
	if cs.Limit != nil {
		limitVal, _ := (&ExprEval{Params: params}).Eval(cs.Limit)
		limit := int(limitVal.IntVal)
		offset := 0
		if cs.Offset != nil {
			offsetVal, _ := (&ExprEval{Params: params}).Eval(cs.Offset)
			offset = int(offsetVal.IntVal)
		}
		if offset > len(rows) {
			rows = nil
		} else {
			rows = rows[offset:]
			if limit < len(rows) {
				rows = rows[:limit]
			}
		}
	}

	return &Result{Columns: columns, Rows: rows}, nil
}

func (e *Engine) executeAnySelect(stmt Statement, params []Value) (*Result, error) {
	switch s := stmt.(type) {
	case *SelectStmt:
		return e.executeSelect(s, params)
	case *CompoundSelect:
		return e.executeCompoundSelect(s, params)
	}
	return nil, &engineError{"unexpected statement in compound select"}
}

func rowKey(row []Value) string {
	var b strings.Builder
	for i, v := range row {
		if i > 0 {
			b.WriteByte(0)
		}
		switch v.Type {
		case DataTypeInteger:
			b.WriteString("i")
			b.WriteString(strconv.FormatInt(v.IntVal, 10))
		case DataTypeFloat:
			b.WriteString("f")
			b.WriteString(strconv.FormatFloat(v.FloatVal, 'g', -1, 64))
		case DataTypeText:
			b.WriteString("t")
			b.WriteString(v.TextVal)
		case DataTypeBlob:
			b.WriteString("b")
			b.Write(v.BlobVal)
		default:
			b.WriteString("n")
		}
	}
	return b.String()
}

func deduplicateRows(rows [][]Value) [][]Value {
	seen := make(map[string]bool)
	var result [][]Value
	for _, row := range rows {
		k := rowKey(row)
		if !seen[k] {
			seen[k] = true
			result = append(result, row)
		}
	}
	return result
}

func intersectRows(left, right [][]Value) [][]Value {
	rightSet := make(map[string]bool)
	for _, r := range right {
		rightSet[rowKey(r)] = true
	}
	seen := make(map[string]bool)
	var result [][]Value
	for _, r := range left {
		k := rowKey(r)
		if rightSet[k] && !seen[k] {
			seen[k] = true
			result = append(result, r)
		}
	}
	return result
}

func exceptRows(left, right [][]Value) [][]Value {
	rightSet := make(map[string]bool)
	for _, r := range right {
		rightSet[rowKey(r)] = true
	}
	var result [][]Value
	for _, r := range left {
		if !rightSet[rowKey(r)] {
			result = append(result, r)
		}
	}
	return result
}

func (e *Engine) executeSelectNoFrom(s *SelectStmt, params []Value) (*Result, error) {
	eval := &ExprEval{Params: params}

	columns := make([]string, len(s.Columns))
	row := make([]Value, len(s.Columns))

	for i, col := range s.Columns {
		val, err := eval.Eval(col.Expr)
		if err != nil {
			return nil, err
		}
		row[i] = val
		if col.As != "" {
			columns[i] = col.As
		} else {
			columns[i] = ExprString(col.Expr)
		}
	}

	return &Result{
		Columns: columns,
		Rows:    [][]Value{row},
	}, nil
}

// ============================================================================
// INSERT
// ============================================================================

func (e *Engine) executeInsert(s *InsertStmt, params []Value) (*Result, error) {
	tableName := s.Table.Name
	tableInfo, ok := e.schema.GetTable(tableName)
	if !ok {
		return nil, &engineError{"no such table: " + tableName}
	}
	if err := validateReturningColumns(tableInfo, s.Returning); err != nil {
		return nil, err
	}
	if s.OrIgnore || s.OrReplace || s.Conflict != nil || len(s.Returning) > 0 || len(e.uniqueConstraints(tableInfo)) > 0 || s.Select != nil {
		return e.executeInsertWithConflict(s, params, tableInfo)
	}

	var lastID int64
	var totalAffected int64

	for _, valueRow := range s.Values {
		// Evaluate value expressions.
		eval := &ExprEval{Params: params}
		values := make([]Value, len(valueRow))
		for i, expr := range valueRow {
			val, err := eval.Eval(expr)
			if err != nil {
				return nil, err
			}
			values[i] = val
		}

		// buildInsertRow centralizes the column mapping, DEFAULT
		// application (omitted columns only, with affinity applied),
		// NOT NULL enforcement, and rowid handling — identical to the
		// executeInsertWithConflict path. Routing the "simple" branch
		// through it ensures a NOT NULL violation on an explicit NULL
		// still fails even when the column has a default, and that a
		// DEFAULT value inherits the column affinity.
		rowValues, rowid, err := buildInsertRow(tableInfo, s.Columns, values)
		if err != nil {
			return nil, err
		}

		// Build record
		record := valuesToRecord(rowValues)

		// Check foreign key constraints
		if err := e.checkForeignKeyInsert(tableInfo, rowValues); err != nil {
			return nil, err
		}

		// Insert into B-tree
		if err := e.btree.Insert(tableInfo.RootPage, rowid, record); err != nil {
			return nil, err
		}

		// Update indexes
		if err := e.insertIntoIndexes(tableName, rowid, rowValues); err != nil {
			return nil, err
		}

		lastID = rowid
		totalAffected++
	}

	return &Result{
		RowsAffected: totalAffected,
		LastInsertID: lastID,
	}, nil
}

// ============================================================================
// UPDATE
// ============================================================================

func (e *Engine) executeUpdate(s *UpdateStmt, params []Value) (*Result, error) {
	tableName := s.Table.Name
	tableInfo, ok := e.schema.GetTable(tableName)
	if !ok {
		return nil, &engineError{"no such table: " + tableName}
	}
	if err := validateReturningColumns(tableInfo, s.Returning); err != nil {
		return nil, err
	}

	cursor, err := e.btree.Scan(tableInfo.RootPage)
	if err != nil {
		return nil, err
	}
	defer cursor.Close()

	// Build set map: column index → expression
	setMap := make(map[int]Expr)
	for _, set := range s.Sets {
		idx := tableInfo.ColumnIndex(set.Column)
		if idx < 0 {
			return nil, &engineError{"no such column: " + set.Column}
		}
		setMap[idx] = set.Expr
	}

	var totalAffected int64
	result := &Result{Columns: append([]string(nil), s.Returning...)}
	var toUpdate []struct {
		rowid  int64
		record *Record
		values []Value
	}

	for cursor.Next() {
		rowid, record, err := cursor.Get()
		if err != nil {
			return nil, err
		}

		row := recordToValues(record, tableInfo)

		// Build eval context
		columnMap := make(map[string]int, len(tableInfo.Columns)+1)
		columnMap["rowid"] = 0
		for i, col := range tableInfo.Columns {
			columnMap[strings.ToLower(col.Name)] = i + 1
		}
		tableMap := map[string]map[string]int{
			strings.ToLower(tableName): columnMap,
		}
		if s.Table.As != "" {
			tableMap[strings.ToLower(s.Table.As)] = columnMap
		}
		eval := &ExprEval{
			Row:       append([]Value{IntegerValue(rowid)}, row...),
			ColumnMap: columnMap,
			TableMap:  tableMap,
			Params:    params,
			Engine:    e,
		}

		// Check WHERE
		if s.Where != nil {
			val, err := eval.Eval(s.Where)
			if err != nil {
				return nil, err
			}
			if val.IsNull() {
				continue
			}
			if b, ok := val.AsInt64(); !ok || b == 0 {
				continue
			}
		}

		// Apply SET clauses
		newRow := make([]Value, len(row))
		copy(newRow, row)
		for idx, expr := range setMap {
			val, err := eval.Eval(expr)
			if err != nil {
				return nil, err
			}
			newRow[idx] = ApplyAffinity(val, tableInfo.Columns[idx].Affinity)
		}
		if err := validateUpdatedRow(tableInfo, row, newRow); err != nil {
			return nil, err
		}
		conflictRowID, _, duplicate, err := e.findInsertConflict(tableInfo, newRow, nil, rowid)
		if err != nil {
			return nil, err
		}
		if duplicate {
			// The on-disk B-tree scan sees rows at their PRE-statement
			// image. When the row it flagged as a conflict is itself
			// staged for update by this statement, its stale image will
			// be replaced before commit — the pairwise pending check
			// below is the authoritative conflict test for staged-vs-
			// staged, so a conflict against a staged rowid is not (yet)
			// a real violation. This matches SQLite, which evaluates
			// UNIQUE per final row image: UPDATE t SET u=u-1 over
			// (1,1),(2,2) succeeds with (1,0),(2,1).
			staged := false
			for _, prior := range toUpdate {
				if prior.rowid == conflictRowID {
					staged = true
					break
				}
			}
			if !staged {
				return nil, &engineError{"UNIQUE constraint failed"}
			}
		}
		// findInsertConflict only sees the on-disk table state, so two
		// rows updated by the SAME statement could pick the same key
		// without ever tripping the B-tree check. SQLite evaluates
		// constraints per-row and fails the statement on the first
		// collision; mirror that by also checking each pending row we
		// have already accumulated. Statement atomicity (Fix 1) then
		// guarantees no partial application leaks through.
		for _, prior := range toUpdate {
			for _, c := range e.uniqueConstraints(tableInfo) {
				if c.Predicate != nil {
					if !rowMatchesPredicate(tableInfo, prior.values, c.Predicate) {
						continue
					}
					if !rowMatchesPredicate(tableInfo, newRow, c.Predicate) {
						continue
					}
				}
				if rowsConflict(tableInfo, prior.values, newRow, c.Columns) {
					return nil, &engineError{"UNIQUE constraint failed"}
				}
			}
		}

		if err := e.checkForeignKeyInsert(tableInfo, newRow); err != nil {
			return nil, err
		}
		// checkForeignKeyInsert is the CHILD side — it asks whether this row's
		// own FK columns still point at something real. The parent side was
		// missing entirely, so moving a referenced key out from under its
		// children (UPDATE parent SET id = ...) left them dangling. Only run
		// it when a referenced key actually changed: an update to any other
		// column of a parent row is nobody's business.
		if err := e.checkForeignKeyParentUpdate(tableInfo, row, newRow); err != nil {
			return nil, err
		}

		toUpdate = append(toUpdate, struct {
			rowid  int64
			record *Record
			values []Value
		}{rowid: rowid, record: valuesToRecord(newRow), values: newRow})
		totalAffected++
		appendReturningRow(result, tableInfo, newRow)
	}

	// Apply updates. Insert at the existing rowid overwrites the table
	// row in place. For each index we delete the entry for this rowid
	// first so a row that has moved OUT of a partial-index predicate
	// leaves no stale entry behind; insertIntoIndexes then re-adds the
	// entry only if the NEW row matches the predicate.
	for _, u := range toUpdate {
		if err := e.btree.Insert(tableInfo.RootPage, u.rowid, u.record); err != nil {
			return nil, err
		}
		if err := e.deleteFromIndexes(tableInfo.Name, u.rowid); err != nil {
			return nil, err
		}
		if err := e.insertIntoIndexes(tableInfo.Name, u.rowid, u.values); err != nil {
			return nil, err
		}
	}

	result.RowsAffected = totalAffected
	return result, nil
}

// ============================================================================
// DELETE
// ============================================================================

func (e *Engine) executeDelete(s *DeleteStmt, params []Value) (*Result, error) {
	tableName := s.Table.Name
	tableInfo, ok := e.schema.GetTable(tableName)
	if !ok {
		return nil, &engineError{"no such table: " + tableName}
	}

	// If no WHERE, delete all rows efficiently
	if s.Where == nil {
		// Walk every row once to drop its secondary-index entries
		// before the table B-tree is rebuilt; otherwise each index
		// retains entries pointing into a now-empty table, and any
		// later INSERT at a recycled rowid would resurface the stale
		// value.
		cursor, err := e.btree.Scan(tableInfo.RootPage)
		if err != nil {
			return nil, err
		}
		// A bare DELETE used to skip foreign keys entirely: this path rebuilds
		// the table btree wholesale and never consulted them, so emptying a
		// referenced parent table left every child dangling.
		//
		// SQLite checks immediate foreign keys at STATEMENT end, not per row,
		// so a reference from a row this same statement removes is not a
		// violation. Every row of this table is going, so a self-reference can
		// never dangle afterwards — skipSelf tells the check to ignore
		// children living in this very table. References from OTHER tables
		// still dangle and are still refused.
		//
		// Two passes, deliberately: nothing is touched until every row has
		// cleared the check, so a statement that reports failure has changed
		// nothing.
		//
		// Honest scope: the damage a one-pass loop would do is not currently
		// observable through this engine. Index entries dropped before the
		// refusal leave no visible trace, because UNIQUE enforcement and
		// lookups both consult the table rather than relying on the index
		// alone — a one-pass mutant passes every assertion, including
		// index-shaped ones. The ordering is kept because "do not mutate on a
		// failing statement" is the right invariant, not because a test can
		// currently prove it.
		var rowids []int64
		for cursor.Next() {
			rowid, record, err := cursor.Get()
			if err != nil {
				cursor.Close()
				return nil, err
			}
			if err := e.checkForeignKeyDeleteSkipping(tableInfo, rowid, recordToValues(record, tableInfo), tableInfo.Name); err != nil {
				cursor.Close()
				return nil, err
			}
			rowids = append(rowids, rowid)
		}
		cursor.Close()
		var count int64
		for _, rowid := range rowids {
			if err := e.deleteFromIndexes(tableName, rowid); err != nil {
				return nil, err
			}
			count++
		}
		// Recreate the (now-empty) table B-tree root.
		newRoot, err := e.btree.CreateBTree()
		if err != nil {
			return nil, err
		}
		tableInfo.RootPage = newRoot
		return &Result{RowsAffected: count}, nil
	}

	cursor, err := e.btree.Scan(tableInfo.RootPage)
	if err != nil {
		return nil, err
	}
	defer cursor.Close()

	var toDelete []int64
	var toDeleteRows [][]Value

	for cursor.Next() {
		rowid, record, err := cursor.Get()
		if err != nil {
			return nil, err
		}

		row := recordToValues(record, tableInfo)
		columnMap := make(map[string]int, len(tableInfo.Columns)+1)
		columnMap["rowid"] = 0
		for i, col := range tableInfo.Columns {
			columnMap[strings.ToLower(col.Name)] = i + 1
		}
		tableMap := map[string]map[string]int{
			strings.ToLower(tableName): columnMap,
		}
		if s.Table.As != "" {
			tableMap[strings.ToLower(s.Table.As)] = columnMap
		}
		eval := &ExprEval{
			Row:       append([]Value{IntegerValue(rowid)}, row...),
			ColumnMap: columnMap,
			TableMap:  tableMap,
			Params:    params,
			Engine:    e,
		}

		val, err := eval.Eval(s.Where)
		if err != nil {
			return nil, err
		}
		if !val.IsNull() {
			if b, ok := val.AsInt64(); ok && b != 0 {
				toDelete = append(toDelete, rowid)
				// COPY, never the slice itself. The cursor reuses one Record
				// per scan and Get hands out a pointer into it, so `row`
				// aliases a buffer the next iteration overwrites. Every
				// collected row ended up holding the LAST row the scan
				// visited, and the foreign-key check below then ran against
				// that row instead of the one being deleted — invisible while
				// a test table had a single row, and a deleted-parent with
				// dangling children as soon as it had two.
				toDeleteRows = append(toDeleteRows, append([]Value(nil), row...))
			}
		}
	}

	// SQLite checks immediate foreign keys at STATEMENT end, so a reference
	// from a row this same statement removes is not a violation. The bare
	// DELETE path already applied that rule; this one did not, so
	// `DELETE FROM t` and `DELETE FROM t WHERE 1=1` — the same operation —
	// gave different answers on a self-referencing table.
	//
	// Two passes for the same reason the bare path uses them: a refusal must
	// not leave the rows before it already deleted.
	doomed := make(map[int64]bool, len(toDelete))
	for _, rowid := range toDelete {
		doomed[rowid] = true
	}
	for i, rowid := range toDelete {
		if err := e.checkForeignKeyDeleteExcept(tableInfo, rowid, toDeleteRows[i], "", doomed); err != nil {
			return nil, err
		}
	}
	for _, rowid := range toDelete {
		if err := e.btree.Delete(tableInfo.RootPage, rowid); err != nil {
			return nil, err
		}
		// Removing the table row must also remove every secondary-index
		// entry keyed by this rowid, or a subsequently recycled rowid
		// resurfaces under the dead row's indexed value.
		if err := e.deleteFromIndexes(tableInfo.Name, rowid); err != nil {
			return nil, err
		}
	}

	return &Result{RowsAffected: int64(len(toDelete))}, nil
}

// ============================================================================
// CREATE TABLE
// ============================================================================

func (e *Engine) executeCreateTable(s *CreateTableStmt) (*Result, error) {
	name := s.Name
	if _, exists := e.schema.GetTable(name); exists && !s.IfNotExists {
		return nil, &engineError{"table already exists: " + name}
	}
	if exists, _ := e.schema.GetTable(name); exists != nil && s.IfNotExists {
		return &Result{}, nil
	}

	// Create B-tree for the table
	rootPage, err := e.btree.CreateBTree()
	if err != nil {
		return nil, err
	}

	info := BuildTableInfo(s, rootPage)
	e.schema.AddTable(info)

	return &Result{}, nil
}

// ============================================================================
// CREATE INDEX
// ============================================================================

func (e *Engine) executeCreateIndex(s *CreateIndexStmt) (*Result, error) {
	if _, exists := e.schema.GetIndex(s.Name); exists && !s.IfNotExists {
		return nil, &engineError{"index already exists: " + s.Name}
	}
	if _, exists := e.schema.GetIndex(s.Name); exists && s.IfNotExists {
		return &Result{}, nil
	}

	tableInfo, ok := e.schema.GetTable(s.Table)
	if !ok {
		return nil, &engineError{"no such table: " + s.Table}
	}
	if s.Unique {
		if err := e.validateUniqueIndex(tableInfo, indexColumns(s.Columns), s.Where); err != nil {
			return nil, err
		}
	}

	// Create the index B-tree
	rootPage, err := e.btree.CreateBTree()
	if err != nil {
		return nil, err
	}

	// Populate the index with existing table rows
	cursor, err := e.btree.Scan(tableInfo.RootPage)
	if err != nil {
		return nil, err
	}

	for cursor.Next() {
		rowid, record, err := cursor.Get()
		if err != nil {
			cursor.Close()
			return nil, err
		}

		// Build index key from indexed columns
		vals := recordToValues(record, tableInfo)
		// A partial index only contains rows the predicate selects;
		// skip everything else so the index B-tree never holds entries
		// for out-of-predicate rows.
		if s.Where != nil && !rowMatchesPredicate(tableInfo, vals, s.Where) {
			continue
		}
		idxCols := indexColumns(s.Columns)
		keyVals := make([]Value, len(idxCols))
		for i, colName := range idxCols {
			idx := tableInfo.ColumnIndex(colName)
			if idx >= 0 && idx < len(vals) {
				keyVals[i] = vals[idx]
			} else {
				keyVals[i] = NullValue
			}
		}

		// Create index record: key columns + rowid
		allVals := append(keyVals, IntegerValue(rowid))
		idxRecord := valuesToRecord(allVals)

		// Insert into index B-tree
		if err := e.btree.Insert(rootPage, rowid, idxRecord); err != nil {
			cursor.Close()
			return nil, err
		}
	}
	cursor.Close()

	ii := &IndexInfo{
		Name:      s.Name,
		TableName: s.Table,
		RootPage:  rootPage,
		Columns:   indexColumns(s.Columns),
		Unique:    s.Unique,
	}
	if s.Where != nil {
		// A partial index whose predicate cannot be rendered back to source
		// cannot be persisted, and an unpersisted predicate does not come back
		// as "no index" — it comes back as a FULL index, which is a stronger
		// constraint than the one written and makes the column look like a
		// legal foreign-key target. Refusing the CREATE is the only outcome
		// that cannot silently change what the schema means.
		src := FormatExpr(s.Where)
		if src == "" {
			return nil, &engineError{"unsupported partial index predicate on " + s.Name}
		}
		ii.WhereExpr = s.Where
		ii.Where = src
	}
	e.schema.AddIndex(ii)

	return &Result{}, nil
}

// ============================================================================
// DROP TABLE
// ============================================================================

func (e *Engine) executeDropTable(s *DropTableStmt) (*Result, error) {
	ti, exists := e.schema.GetTable(s.Name)
	if !exists && !s.IfExists {
		return nil, &engineError{"no such table: " + s.Name}
	}
	// A DROP is a delete of every row, so it is a parent-side operation and
	// needs the same check DELETE gets. SQLite performs an implicit DELETE
	// FROM under foreign-key enforcement and refuses when rows are still
	// referenced; this dropped the table and left the children pointing at a
	// table that no longer exists, with nothing to ever revisit them.
	//
	// Rows of the dropped table cannot themselves dangle afterwards, so
	// references FROM this table to itself are skipped — the same statement-end
	// reasoning the bare DELETE path uses.
	if exists {
		if err := e.checkDropTableReferences(ti); err != nil {
			return nil, err
		}
	}
	if !e.schema.DropTable(s.Name) && !s.IfExists {
		return nil, &engineError{"no such table: " + s.Name}
	}
	return &Result{}, nil
}

// checkDropTableReferences refuses a DROP whose rows other tables still
// reference. Nothing is scanned when no other table has a foreign key pointing
// here, so the common case costs a map walk.
func (e *Engine) checkDropTableReferences(ti *TableInfo) error {
	referenced := false
	for _, name := range e.schema.TableNames() {
		tbl, _ := e.schema.GetTable(name)
		if strings.EqualFold(tbl.Name, ti.Name) {
			// Self-references go with the table. Skipping here only avoids a
			// pointless scan — the per-row check below passes ti.Name as its
			// skipTable, so a self-reference could not produce an error
			// anyway. Mutation testing reports the negative direction as a
			// survivor for that reason: it is equivalent, not untested.
			continue
		}
		for _, fk := range tbl.ForeignKeys {
			if strings.EqualFold(fk.ToTable, ti.Name) {
				referenced = true
				break
			}
		}
		if referenced {
			break
		}
	}
	// Short-circuit: with no other table pointing here there is nothing a
	// scan could find. This is a performance guard, not a correctness one —
	// removing it makes the function slower and changes no verdict, which is
	// why mutation testing reports it as a survivor.
	if !referenced {
		return nil
	}
	cursor, err := e.btree.Scan(ti.RootPage)
	if err != nil {
		return err
	}
	defer cursor.Close()
	for cursor.Next() {
		rowid, record, err := cursor.Get()
		if err != nil {
			return err
		}
		if err := e.checkForeignKeyDeleteSkipping(ti, rowid, recordToValues(record, ti), ti.Name); err != nil {
			return err
		}
	}
	return nil
}

// ============================================================================
// DROP INDEX
// ============================================================================

func (e *Engine) executeDropIndex(s *DropIndexStmt) (*Result, error) {
	if _, ok := e.schema.GetIndex(s.Name); !ok && !s.IfExists {
		return nil, &engineError{"no such index: " + s.Name}
	}
	if !e.schema.DropIndex(s.Name) && !s.IfExists {
		return nil, &engineError{"no such index: " + s.Name}
	}
	return &Result{}, nil
}

func (e *Engine) executeAlterAddColumn(s *AlterAddColumnStmt) (*Result, error) {
	ti, ok := e.schema.GetTable(s.Table)
	if !ok {
		return nil, &engineError{"no such table: " + s.Table}
	}
	// Check column doesn't already exist
	for _, c := range ti.Columns {
		if strings.EqualFold(c.Name, s.Column.Name) {
			return nil, &engineError{"duplicate column name: " + s.Column.Name}
		}
	}
	// Build ColumnDef from ColumnDefAST
	affinity := ResolveColumnAffinity(s.Column.Type)
	col := ColumnDef{
		Name:     s.Column.Name,
		Type:     s.Column.Type,
		Affinity: affinity,
	}
	for _, con := range s.Column.Constraints {
		switch con.Type {
		case ConstraintNotNull:
			col.NotNull = true
		case ConstraintDefault:
			if con.Value != nil {
				ev := &ExprEval{}
				if val, err := ev.Eval(con.Value); err == nil {
					col.Default = &val
				}
			}
		}
	}
	ti.Columns = append(ti.Columns, col)
	// Carry a REFERENCES on the added column into the table's foreign keys.
	// The parser reads it; this path used to read only NOT NULL and DEFAULT and
	// drop it, so the constraint existed in the schema text and enforced
	// nothing. FromCol is the index just appended.
	//
	// SQLite only permits ADD COLUMN with a REFERENCES when the default is
	// NULL, so existing rows hold NULL and need no backfill check.
	for _, con := range s.Column.Constraints {
		if con.Type != ConstraintForeignKey {
			continue
		}
		ti.ForeignKeys = append(ti.ForeignKeys, ForeignKeyInfo{
			FromCol: len(ti.Columns) - 1,
			ToTable: con.RefTable,
			ToCols:  append([]string(nil), con.RefCols...),
		})
	}
	return &Result{}, nil
}

func (e *Engine) executeAlterRenameTable(s *AlterRenameTableStmt) (*Result, error) {
	if _, ok := e.schema.GetTable(s.OldName); !ok {
		return nil, &engineError{"no such table: " + s.OldName}
	}
	if _, ok := e.schema.GetTable(s.NewName); ok {
		return nil, &engineError{"there is already another table or index with this name: " + s.NewName}
	}
	if !e.schema.RenameTable(s.OldName, s.NewName) {
		return nil, &engineError{"no such table: " + s.OldName}
	}
	// Every child foreign key naming the old table must follow it. SQLite
	// rewrites the schema SQL of dependent objects; here the equivalent
	// in-memory metadata is rewritten. Without this the children kept pointing
	// at a table that no longer exists, so a rename SQLite treats as routine
	// made the child direction permanently unwritable — fail-closed, but
	// unrecoverable without hand-editing the schema.
	for _, name := range e.schema.TableNames() {
		tbl, _ := e.schema.GetTable(name)
		for i, fk := range tbl.ForeignKeys {
			if strings.EqualFold(fk.ToTable, s.OldName) {
				tbl.ForeignKeys[i].ToTable = s.NewName
			}
		}
	}
	return &Result{}, nil
}

func (e *Engine) executeAlterRenameColumn(s *AlterRenameColumnStmt) (*Result, error) {
	ti, ok := e.schema.GetTable(s.Table)
	if !ok {
		return nil, &engineError{"no such table: " + s.Table}
	}
	found := false
	for i, c := range ti.Columns {
		if strings.EqualFold(c.Name, s.NewName) {
			return nil, &engineError{"duplicate column name: " + s.NewName}
		}
		if strings.EqualFold(c.Name, s.OldName) {
			ti.Columns[i].Name = s.NewName
			found = true
		}
	}
	if !found {
		return nil, &engineError{"no such column: " + s.OldName}
	}
	// Propagate the rename into every place column names are cached, so
	// constraints and indexes stay attached to the renamed column.
	// SQLite's own ALTER TABLE RENAME COLUMN rewrites the schema SQL of
	// every dependent object; here we rewrite the equivalent in-memory
	// metadata directly.
	oldLower := strings.ToLower(s.OldName)
	// A renamed column may be the TARGET of another table's foreign key, so
	// every ForeignKeyInfo.ToCols entry naming it has to follow — the same
	// reasoning as the unique-constraint and index rewrites below, applied to
	// the one relationship that points in from outside this table.
	for _, name := range e.schema.TableNames() {
		tbl, _ := e.schema.GetTable(name)
		for i, fk := range tbl.ForeignKeys {
			if !strings.EqualFold(fk.ToTable, ti.Name) {
				continue
			}
			for j, col := range fk.ToCols {
				if strings.EqualFold(col, s.OldName) {
					tbl.ForeignKeys[i].ToCols[j] = s.NewName
				}
			}
		}
	}
	for i, uc := range ti.UniqueConstraints {
		for j, col := range uc {
			if strings.EqualFold(col, s.OldName) {
				ti.UniqueConstraints[i][j] = s.NewName
			}
		}
	}
	for _, idx := range e.schema.IndexesForTable(ti.Name) {
		for i, col := range idx.Columns {
			if strings.EqualFold(col, s.OldName) {
				idx.Columns[i] = s.NewName
			}
		}
		if idx.WhereExpr != nil {
			idx.WhereExpr = renameColumnRefs(idx.WhereExpr, oldLower, s.NewName)
		}
	}
	return &Result{}, nil
}

// renameColumnRefs walks an expression tree and returns a copy in which
// every ColumnRef whose name matches oldName (case-insensitive) has been
// replaced with newName. Expr node types are VALUES, so this returns
// rebuilt nodes rather than mutating in place; the caller reassigns the
// root. Used by ALTER TABLE RENAME COLUMN to keep partial-index WHERE
// predicates consistent after a column rename.
func renameColumnRefs(expr Expr, oldLower, newName string) Expr {
	if expr == nil {
		return nil
	}
	switch e := expr.(type) {
	case ColumnRef:
		if strings.ToLower(e.Column) == oldLower {
			e.Column = newName
		}
		return e
	case BinaryExpr:
		e.Left = renameColumnRefs(e.Left, oldLower, newName)
		e.Right = renameColumnRefs(e.Right, oldLower, newName)
		return e
	case UnaryExpr:
		e.Expr = renameColumnRefs(e.Expr, oldLower, newName)
		return e
	case FunctionCall:
		for i, arg := range e.Args {
			e.Args[i] = renameColumnRefs(arg, oldLower, newName)
		}
		return e
	case IsNullExpr:
		e.Expr = renameColumnRefs(e.Expr, oldLower, newName)
		return e
	case BetweenExpr:
		e.Expr = renameColumnRefs(e.Expr, oldLower, newName)
		e.Low = renameColumnRefs(e.Low, oldLower, newName)
		e.High = renameColumnRefs(e.High, oldLower, newName)
		return e
	case InExpr:
		e.Expr = renameColumnRefs(e.Expr, oldLower, newName)
		for i, v := range e.Values {
			e.Values[i] = renameColumnRefs(v, oldLower, newName)
		}
		return e
	case LikeExpr:
		e.Expr = renameColumnRefs(e.Expr, oldLower, newName)
		e.Pattern = renameColumnRefs(e.Pattern, oldLower, newName)
		if e.Escape != nil {
			e.Escape = renameColumnRefs(e.Escape, oldLower, newName)
		}
		return e
	case ParenExpr:
		e.Expr = renameColumnRefs(e.Expr, oldLower, newName)
		return e
	case CastExpr:
		e.Expr = renameColumnRefs(e.Expr, oldLower, newName)
		return e
	case CaseExpr:
		if e.Operand != nil {
			e.Operand = renameColumnRefs(e.Operand, oldLower, newName)
		}
		for _, w := range e.Whens {
			w.Condition = renameColumnRefs(w.Condition, oldLower, newName)
			w.Result = renameColumnRefs(w.Result, oldLower, newName)
		}
		if e.Else != nil {
			e.Else = renameColumnRefs(e.Else, oldLower, newName)
		}
		return e
	}
	return expr
}

func (e *Engine) executePragma(s *PragmaStmt) (*Result, error) {
	name := strings.ToLower(s.Name)

	switch name {
	case "table_info":
		return e.pragmaTableInfo(s)
	case "database_list":
		return e.pragmaDatabaseList()
	case "user_version":
		return e.pragmaUserVersion(s)
	case "journal_mode":
		return e.pragmaJournalMode(s)
	case "synchronous":
		return e.pragmaSynchronous(s)
	case "foreign_keys":
		return e.pragmaForeignKeys(s)
	case "encoding":
		return e.pragmaEncoding()
	case "page_size":
		return e.pragmaPageSize()
	case "cache_size":
		return e.pragmaCacheSize(s)
	case "schema_version":
		return e.pragmaSchemaVersion()
	case "auto_vacuum":
		return e.pragmaAutoVacuum()
	case "integrity_check":
		return e.pragmaIntegrityCheck()
	default:
		// Unknown pragmas return empty result for compatibility
		return &Result{}, nil
	}
}

func (e *Engine) pragmaTableInfo(s *PragmaStmt) (*Result, error) {
	if s.Value == nil {
		return &Result{Columns: []string{"cid", "name", "type", "notnull", "dflt_value", "pk"}}, nil
	}
	eval := &ExprEval{}
	tableName, err := eval.Eval(s.Value)
	if err != nil {
		return nil, err
	}
	ti, ok := e.schema.GetTable(tableName.TextVal)
	if !ok {
		return &Result{Columns: []string{"cid", "name", "type", "notnull", "dflt_value", "pk"}}, nil
	}
	cols := []string{"cid", "name", "type", "notnull", "dflt_value", "pk"}
	var rows [][]Value
	for i, col := range ti.Columns {
		notnull := int64(0)
		if col.NotNull {
			notnull = 1
		}
		pk := int64(0)
		if col.IsRowID || col.IsPrimaryKey {
			pk = 1
		}
		var dflt Value
		if col.Default != nil {
			dflt = *col.Default
		} else {
			dflt = Value{Type: DataTypeNull}
		}
		rows = append(rows, []Value{
			IntegerValue(int64(i)),
			TextValue(col.Name),
			TextValue(col.Type),
			IntegerValue(notnull),
			dflt,
			IntegerValue(pk),
		})
	}
	return &Result{Columns: cols, Rows: rows}, nil
}

func (e *Engine) pragmaDatabaseList() (*Result, error) {
	return &Result{
		Columns: []string{"seq", "name", "file"},
		Rows: [][]Value{{
			IntegerValue(0),
			TextValue("main"),
			TextValue(""),
		}},
	}, nil
}

func (e *Engine) pragmaUserVersion(s *PragmaStmt) (*Result, error) {
	if s.Value != nil {
		return &Result{}, nil
	}
	return &Result{
		Columns: []string{"user_version"},
		Rows:    [][]Value{{IntegerValue(0)}},
	}, nil
}

func (e *Engine) pragmaJournalMode(s *PragmaStmt) (*Result, error) {
	if s.Value != nil {
		return &Result{}, nil
	}
	return &Result{
		Columns: []string{"journal_mode"},
		Rows:    [][]Value{{TextValue("memory")}},
	}, nil
}

func (e *Engine) pragmaSynchronous(s *PragmaStmt) (*Result, error) {
	if s.Value != nil {
		return &Result{}, nil
	}
	return &Result{
		Columns: []string{"synchronous"},
		Rows:    [][]Value{{IntegerValue(1)}},
	}, nil
}

func (e *Engine) pragmaForeignKeys(s *PragmaStmt) (*Result, error) {
	if s.Value != nil {
		// SQLite makes this pragma a no-op inside a transaction, and the
		// reason is exactly what happened here once the setter started
		// working: OFF inside a transaction took effect, survived ROLLBACK,
		// and left enforcement disabled with no error anywhere — a
		// rolled-back statement changing durable state. Enforcement is engine
		// state rather than schema state, so the transaction snapshot does not
		// carry it; refusing the change is both simpler and what SQLite does.
		if e.txnSnap != nil {
			return &Result{}, nil
		}
		// Outside a transaction the setter does take effect — and one engine serves every connection in a pool (see OpenConnector), so
		// this setting is database-wide rather than per-connection as it is in
		// SQLite. Turning enforcement ON that way is harmless — it is already
		// the default and strengthening it affects nobody adversely. Turning
		// it OFF is a side channel: one stray statement on one pooled
		// connection would silently stop checking foreign keys for every other
		// connection, with no error anywhere and nothing in the caller's own
		// code to explain why its writes started succeeding. Refusing is the
		// only answer that cannot be wrong quietly.
		if e.poolShared && !pragmaBoolValue(s.Value, e.fkEnforced) {
			return nil, &engineError{"PRAGMA foreign_keys cannot be disabled on a pooled connection: " +
				"this engine is shared across the pool, so the setting is not per-connection"}
		}
		e.fkEnforced = pragmaBoolValue(s.Value, e.fkEnforced)
		return &Result{}, nil
	}
	v := int64(0)
	if e.fkEnforced {
		v = 1
	}
	return &Result{
		Columns: []string{"foreign_keys"},
		Rows:    [][]Value{{IntegerValue(v)}},
	}, nil
}

// pragmaBoolValue reads SQLite's boolean pragma spellings: 1/0, on/off,
// yes/no, true/false. An unrecognised value leaves the setting unchanged,
// which is what SQLite does.
//
// The word forms arrive as a ColumnRef, not a literal — `PRAGMA foreign_keys =
// OFF` has no quotes, so the parser sees a bare identifier. Reading only
// LiteralExpr therefore silently ignored the most common spelling there is.
func pragmaBoolValue(expr Expr, current bool) bool {
	var word string
	switch v := expr.(type) {
	case LiteralExpr:
		switch v.Type {
		case DataTypeInteger:
			return v.IntVal != 0
		case DataTypeFloat:
			// `PRAGMA foreign_keys = 1.0` sets it ON in SQLite. Ignoring a
			// numeric literal because of its storage class is the same
			// "silently unchanged" failure this function was rewritten to
			// close, one spelling further along.
			return v.FloatVal != 0
		case DataTypeText:
			word = v.TextVal
		default:
			return current
		}
	case ColumnRef:
		word = v.Column
	default:
		return current
	}
	switch strings.ToLower(strings.TrimSpace(word)) {
	case "1", "on", "yes", "true":
		return true
	case "0", "off", "no", "false":
		return false
	}
	return current
}

func (e *Engine) pragmaEncoding() (*Result, error) {
	return &Result{
		Columns: []string{"encoding"},
		Rows:    [][]Value{{TextValue("UTF-8")}},
	}, nil
}

func (e *Engine) pragmaPageSize() (*Result, error) {
	return &Result{
		Columns: []string{"page_size"},
		Rows:    [][]Value{{IntegerValue(4096)}},
	}, nil
}

func (e *Engine) pragmaCacheSize(s *PragmaStmt) (*Result, error) {
	if s.Value != nil {
		return &Result{}, nil
	}
	return &Result{
		Columns: []string{"cache_size"},
		Rows:    [][]Value{{IntegerValue(2000)}},
	}, nil
}

func (e *Engine) pragmaSchemaVersion() (*Result, error) {
	return &Result{
		Columns: []string{"schema_version"},
		Rows:    [][]Value{{IntegerValue(0)}},
	}, nil
}

func (e *Engine) pragmaAutoVacuum() (*Result, error) {
	return &Result{
		Columns: []string{"auto_vacuum"},
		Rows:    [][]Value{{IntegerValue(0)}},
	}, nil
}

func (e *Engine) pragmaIntegrityCheck() (*Result, error) {
	return &Result{
		Columns: []string{"integrity_check"},
		Rows:    [][]Value{{TextValue("ok")}},
	}, nil
}

func (e *Engine) executeCreateView(s *CreateViewStmt) (*Result, error) {
	if _, exists := e.schema.views[strings.ToLower(s.Name)]; exists {
		return nil, &engineError{"view already exists: " + s.Name}
	}
	e.schema.views[strings.ToLower(s.Name)] = &ViewInfo{
		Name: s.Name,
		As:   s.As,
		SQL:  s.SQL,
	}
	return &Result{}, nil
}

func (e *Engine) executeDropView(s *DropViewStmt) (*Result, error) {
	key := strings.ToLower(s.Name)
	if _, exists := e.schema.views[key]; !exists {
		if s.IfExists {
			return &Result{}, nil
		}
		return nil, &engineError{"no such view: " + s.Name}
	}
	delete(e.schema.views, key)
	return &Result{}, nil
}

func (e *Engine) executeViewSelect(view *ViewInfo, outer *SelectStmt, params []Value) (*Result, error) {
	// Execute the view's inner SELECT
	innerSel, ok := view.As.(*SelectStmt)
	if !ok {
		// Could be a CompoundSelect
		return e.ExecuteStatement(view.As)
	}
	innerResult, err := e.executeSelect(innerSel, params)
	if err != nil {
		return nil, err
	}

	// If the outer SELECT is just "SELECT * FROM view", return inner result directly
	if len(outer.Columns) == 1 {
		if _, isStar := outer.Columns[0].Expr.(StarColumn); isStar && outer.Where == nil && len(outer.OrderBy) == 0 && outer.Limit == nil {
			return innerResult, nil
		}
	}

	// Apply outer SELECT's columns, WHERE, ORDER BY on the inner result
	// Treat inner result rows as a virtual table
	var rows [][]Value
	for _, innerRow := range innerResult.Rows {
		row := append([]Value(nil), innerRow...)

		// Build eval context from inner columns
		eval := &ExprEval{Row: row, Params: params}
		eval.ColumnMap = make(map[string]int)
		for i, col := range innerResult.Columns {
			eval.ColumnMap[strings.ToLower(col)] = i
		}

		// Check WHERE
		if outer.Where != nil {
			val, err := eval.Eval(outer.Where)
			if err != nil {
				return nil, err
			}
			if val.IsNull() {
				continue
			}
			if b, ok := val.AsInt64(); !ok || b == 0 {
				continue
			}
		}

		// Evaluate output columns
		var outRow []Value
		for _, col := range outer.Columns {
			if _, isStar := col.Expr.(StarColumn); isStar {
				outRow = append(outRow, row...)
			} else {
				v, err := eval.Eval(col.Expr)
				if err != nil {
					return nil, err
				}
				outRow = append(outRow, v)
			}
		}
		rows = append(rows, outRow)
	}

	// Determine output column names
	var columns []string
	for _, col := range outer.Columns {
		if _, isStar := col.Expr.(StarColumn); isStar {
			columns = append(columns, innerResult.Columns...)
		} else {
			columns = append(columns, col.As)
		}
	}

	// Apply ORDER BY
	if len(outer.OrderBy) > 0 {
		rows = e.sortRows(rows, outer.OrderBy, columns, nil, params, nil)
	}

	// Apply DISTINCT
	if outer.Distinct {
		rows = deduplicateRows(rows)
	}

	return &Result{Columns: columns, Rows: rows}, nil
}

// checkForeignKeyInsert validates foreign key constraints on INSERT/UPDATE.
func (e *Engine) checkForeignKeyInsert(ti *TableInfo, rowValues []Value) error {
	if !e.fkEnforced {
		return nil
	}
	for _, fk := range ti.ForeignKeys {
		if fk.FromCol >= len(rowValues) {
			continue
		}
		// Resolve the key BEFORE looking at the child value. A mismatch is a
		// statement that cannot be executed at all, so SQLite raises it even
		// for a NULL child value and even when the parent table holds the row
		// — the key is unusable, not unsatisfied. Skipping NULLs first meant a
		// child row could be written through a foreign key this engine had
		// already decided it could not evaluate.
		refTable, ok := e.schema.GetTable(fk.ToTable)
		if !ok {
			return &engineError{"foreign key mismatch: no such table: " + fk.ToTable}
		}
		refCol, err := e.fkParentKey(ti, refTable, fk)
		if err != nil {
			return err
		}
		val := rowValues[fk.FromCol]
		if val.IsNull() {
			continue // a NULL child key references nothing and satisfies the constraint
		}
		found, err := e.rowExistsAt(refTable, refCol, val)
		if err != nil {
			return err
		}
		if !found {
			return &engineError{"foreign key constraint failed"}
		}
	}
	return nil
}

// checkForeignKeyDelete checks if deleting a row would violate FK constraints from other tables.
func (e *Engine) checkForeignKeyDelete(ti *TableInfo, rowid int64, rowValues []Value) error {
	return e.checkForeignKeyDeleteSkipping(ti, rowid, rowValues, "")
}

// checkForeignKeyDeleteSkipping is checkForeignKeyDelete with one child table
// excluded. skipTable names a table whose every row this statement is also
// removing, so a reference from it cannot survive the statement — SQLite
// evaluates immediate foreign keys at statement end and would not report it.
// Empty skipTable checks every referencing table.
func (e *Engine) checkForeignKeyDeleteSkipping(ti *TableInfo, rowid int64, rowValues []Value, skipTable string) error {
	return e.checkForeignKeyDeleteExcept(ti, rowid, rowValues, skipTable, nil)
}

// checkForeignKeyDeleteExcept is the delete-side check with two escapes for
// rows the same statement is also removing, since SQLite evaluates immediate
// foreign keys at statement end and a reference from a doomed row cannot
// survive it.
//
// skipTable excludes a whole referencing table (used when every one of its rows
// is going, as in a bare DELETE). doomed excludes individual rows of THIS table,
// for a self-referencing table under a WHERE clause.
func (e *Engine) checkForeignKeyDeleteExcept(ti *TableInfo, rowid int64, rowValues []Value, skipTable string, doomed map[int64]bool) error {
	if !e.fkEnforced {
		return nil
	}
	// Check all tables that reference this table
	for _, tblName := range e.schema.TableNames() {
		tbl, _ := e.schema.GetTable(tblName)
		if skipTable != "" && strings.EqualFold(tbl.Name, skipTable) {
			continue
		}
		for _, fk := range tbl.ForeignKeys {
			if !strings.EqualFold(fk.ToTable, ti.Name) {
				continue
			}
			// Check if any row in tbl references the row being deleted.
			// Rows of THIS table that the same statement also deletes are
			// excluded — they cannot dangle once the statement completes.
			var skipRows map[int64]bool
			if strings.EqualFold(tbl.Name, ti.Name) {
				skipRows = doomed
			}
			if err := e.checkNoChildReference(tbl, fk, ti, rowValues, skipRows); err != nil {
				return err
			}
		}
	}
	return nil
}

// checkForeignKeyParentUpdate refuses an UPDATE that moves a referenced key
// away from the children pointing at it. It is the UPDATE counterpart of
// checkForeignKeyDelete: both ask "does anything still reference the OLD
// value", and both are about this table as a PARENT.
//
// A row whose referenced columns are unchanged is skipped, so ordinary column
// updates on a parent stay free.
func (e *Engine) checkForeignKeyParentUpdate(ti *TableInfo, oldRow, newRow []Value) error {
	if !e.fkEnforced {
		return nil
	}
	for _, tblName := range e.schema.TableNames() {
		tbl, _ := e.schema.GetTable(tblName)
		for _, fk := range tbl.ForeignKeys {
			if !strings.EqualFold(fk.ToTable, ti.Name) {
				continue
			}
			refCol, err := e.fkParentKey(tbl, ti, fk)
			if err != nil {
				return err
			}
			if refCol >= len(oldRow) || refCol >= len(newRow) {
				continue
			}
			if valueEqual(oldRow[refCol], newRow[refCol]) {
				continue // the referenced key did not move
			}
			if err := e.checkNoChildReference(tbl, fk, ti, oldRow, nil); err != nil {
				return err
			}
		}
	}
	return nil
}

func (e *Engine) checkNoChildReference(childTable *TableInfo, childFK ForeignKeyInfo, parentTable *TableInfo, parentRow []Value, skipRows map[int64]bool) error {
	// Which parent column the child points at, and whether it may point at it
	// at all. A key that cannot be processed is a mismatch on the parent side
	// too: returning nil here meant a typo'd or non-unique REFERENCES silently
	// disabled enforcement in the delete direction.
	refCol, keyErr := e.fkParentKey(childTable, parentTable, childFK)
	if keyErr != nil {
		return keyErr
	}
	if refCol >= len(parentRow) {
		return nil
	}
	parentVal := parentRow[refCol]
	parentAffinity := AffinityBlob
	if refCol < len(parentTable.Columns) {
		parentAffinity = parentTable.Columns[refCol].Affinity
	}

	// Scan child table for rows referencing this value
	cursor, err := e.btree.Scan(childTable.RootPage)
	if err != nil {
		return err
	}
	defer cursor.Close()

	fromCol := childFK.FromCol
	for cursor.Next() {
		childRowID, record, err := cursor.Get()
		if err != nil {
			return err
		}
		if skipRows[childRowID] {
			continue // this referencing row is going too
		}
		vals := recordToValues(record, childTable)
		if fromCol < len(vals) {
			childVal := vals[fromCol]
			if !childVal.IsNull() && fkValueEqual(childVal, parentVal, parentAffinity) {
				return &engineError{"foreign key constraint failed"}
			}
		}
	}
	return nil
}

// tablePrimaryKeyColumn returns the index of the column a bare
// `REFERENCES <table>` targets — the parent's PRIMARY KEY — or -1 when the
// table has none it can name. A composite primary key returns -1: it is
// declared as a table constraint, so no single column carries the flag, and a
// single-column foreign key cannot target it anyway.
func tablePrimaryKeyColumn(ti *TableInfo) int {
	if ti == nil {
		return -1
	}
	// INTEGER PRIMARY KEY (the rowid alias) is recorded positionally.
	if ti.PrimaryKey >= 0 && ti.PrimaryKey < len(ti.Columns) {
		return ti.PrimaryKey
	}
	// Any other single-column PRIMARY KEY — `code TEXT PRIMARY KEY` — is
	// recorded on the column instead.
	found := -1
	for i := range ti.Columns {
		if !ti.Columns[i].IsPrimaryKey {
			continue
		}
		if found >= 0 {
			return -1 // more than one: composite, not a single-column target
		}
		found = i
	}
	return found
}

// fkTargetIsUnique reports whether a parent column is a legal foreign-key
// target: SQLite requires the parent key to be the table's primary key or to
// carry its own UNIQUE constraint, because a key that can repeat cannot
// identify the row a child points at.
func (e *Engine) fkTargetIsUnique(parent *TableInfo, colIdx int) bool {
	if colIdx < 0 || colIdx >= len(parent.Columns) {
		return false
	}
	// INTEGER PRIMARY KEY is the rowid: unique by construction. BuildTableInfo
	// also records it in UniqueConstraints, so for a table created in this
	// process the check below would reach the same answer — but the serialized
	// schema writes unique_constraints with `omitempty`, so a database file
	// written before that field existed loads with a primary key and no unique
	// constraints at all. Without this line every foreign key into such a file
	// would become a mismatch on open.
	if colIdx == parent.PrimaryKey {
		return true
	}
	name := parent.Columns[colIdx].Name
	for _, uc := range parent.UniqueConstraints {
		if len(uc) == 1 && strings.EqualFold(uc[0], name) {
			return true
		}
	}
	for _, idx := range e.schema.IndexesForTable(parent.Name) {
		// A PARTIAL unique index does not qualify: it constrains only the
		// rows matching its predicate, so keys outside it can still repeat.
		if idx.Unique && idx.Where == "" && len(idx.Columns) == 1 && strings.EqualFold(idx.Columns[0], name) {
			return true
		}
	}
	return false
}

// fkParentKey resolves the parent column a foreign key targets, and refuses
// the declarations SQLite refuses.
//
// SQLite reports "foreign key mismatch" — not a constraint failure — when a
// foreign key cannot be processed at all: the parent column does not exist,
// the parent has no primary key for a bare `REFERENCES p` to target, or the
// named parent column is not unique. The error is raised when the key is
// USED, not when the table is created, because the parent may be created
// afterwards.
//
// Every foreign-key path resolves its parent column here, both the child side
// (does this row's value exist upstream) and the parent side (does anything
// still point at this row). Two paths resolving the column independently is
// how the parent side ended up enforcing against whichever column happened to
// be declared first while the child side used the primary key.
func (e *Engine) fkParentKey(childTable *TableInfo, parent *TableInfo, fk ForeignKeyInfo) (int, error) {
	mismatch := func() error {
		child := ""
		if childTable != nil {
			child = childTable.Name
		}
		return &engineError{fmt.Sprintf("foreign key mismatch - %q referencing %q", child, parent.Name)}
	}
	colIdx := -1
	if len(fk.ToCols) > 0 && fk.ToCols[0] != "" {
		colIdx = parent.ColumnIndex(fk.ToCols[0])
	} else {
		colIdx = tablePrimaryKeyColumn(parent)
	}
	// One guard, not two: fkTargetIsUnique already answers false for an
	// out-of-range column, so a separate `colIdx < 0` return was a second
	// spelling of the same verdict — mutation testing reported it as a
	// survivor for exactly that reason.
	if !e.fkTargetIsUnique(parent, colIdx) {
		return -1, mismatch()
	}
	return colIdx, nil
}

// rowExistsAt reports whether any row of the parent table holds val in the
// given column.
func (e *Engine) rowExistsAt(ti *TableInfo, colIdx int, val Value) (bool, error) {
	parentAffinity := AffinityBlob
	if colIdx >= 0 && colIdx < len(ti.Columns) {
		parentAffinity = ti.Columns[colIdx].Affinity
	}

	cursor, err := e.btree.Scan(ti.RootPage)
	if err != nil {
		return false, err
	}
	defer cursor.Close()

	for cursor.Next() {
		_, record, err := cursor.Get()
		if err != nil {
			return false, err
		}
		vals := recordToValues(record, ti)
		if colIdx < len(vals) && fkValueEqual(val, vals[colIdx], parentAffinity) {
			return true, nil
		}
	}
	return false, nil
}

// fkValueEqual compares a child key value against a parent key value the way
// SQLite does: the PARENT key's affinity is applied to the child value first
// (SQLite docs §4.2). Without that step a TEXT child column referencing an
// INTEGER parent key never matched — valueEqual is type-strict — so this
// engine refused rows real SQLite accepts, and the pure-engine suites
// validated behavior production does not have.
func fkValueEqual(childVal, parentVal Value, parentAffinity ColumnAffinity) bool {
	coerced := ApplyAffinity(childVal, parentAffinity)
	if valueEqual(coerced, parentVal) {
		return true
	}
	// ApplyAffinity's INTEGER conversion only accepts text that parses as a
	// strict int64, while SQLite's is looser: a decimal, a leading space, a
	// plus sign all convert. Rather than widen storage coercion for every
	// INSERT, compare numerically here — but ONLY when the parent key is
	// itself numeric. A TEXT parent key compares as text, so '007' and 7 stay
	// different keys; allowing a numeric fallback there would silently merge
	// them, which is a false ACCEPT and the one direction that must not move.
	switch parentVal.Type {
	case DataTypeInteger, DataTypeFloat:
	default:
		return false
	}
	cf, ok := numericFloat(coerced)
	if !ok {
		return false
	}
	pf, ok := numericFloat(parentVal)
	if !ok {
		return false
	}
	return cf == pf
}

// numericFloat interprets a value as a number for foreign-key comparison.
// Blobs are excluded: SQLite never equates a blob with a number, so coercing
// one would accept references real SQLite refuses.
func numericFloat(v Value) (float64, bool) {
	switch v.Type {
	case DataTypeInteger:
		return float64(v.IntVal), true
	case DataTypeFloat:
		return v.FloatVal, true
	case DataTypeText:
		t := strings.TrimSpace(v.TextVal)
		if t == "" {
			return 0, false
		}
		f, err := strconv.ParseFloat(t, 64)
		if err != nil {
			return 0, false
		}
		return f, true
	}
	return 0, false
}

// valuesEqual compares two Values for equality.
func valueEqual(a, b Value) bool {
	if a.Type != b.Type {
		// Allow integer/float comparison
		if (a.Type == DataTypeInteger || a.Type == DataTypeFloat) && (b.Type == DataTypeInteger || b.Type == DataTypeFloat) {
			return CompareValues(a, b) == 0
		}
		return false
	}
	return CompareValues(a, b) == 0
}

// txnSnapshot holds state for transaction rollback.
type txnSnapshot struct {
	data   []byte // unused with page-level COW
	schema *Schema
}

func (e *Engine) executeBegin() (*Result, error) {
	e.txnSnap = &txnSnapshot{
		data:   nil,
		schema: e.schema.Copy(),
	}
	e.pager.BeginTxn()
	return &Result{}, nil
}
func (e *Engine) executeCommit() (*Result, error) {
	e.pager.CommitTxn()
	e.txnSnap = nil
	return &Result{}, nil
}

func (e *Engine) executeRollback() (*Result, error) {
	if e.txnSnap != nil {
		if err := e.pager.RollbackTxn(); err != nil {
			return nil, err
		}
		e.schema = e.txnSnap.schema
		e.txnSnap = nil
	}
	return &Result{}, nil
}

// ============================================================================
// Helpers
// ============================================================================

func resolveSelectColumns(cols []SelectColumn, tableInfo *TableInfo) []string {
	if len(cols) == 0 {
		// SELECT *
		result := make([]string, len(tableInfo.Columns))
		for i, col := range tableInfo.Columns {
			result[i] = col.Name
		}
		return result
	}

	result := make([]string, len(cols))
	for i, col := range cols {
		if col.As != "" {
			result[i] = col.As
		} else {
			switch e := col.Expr.(type) {
			case ColumnRef:
				result[i] = e.Column
			case StarColumn:
				// Expand * into all column names
				// This should be handled differently but for now
				result[i] = "*"
			default:
				result[i] = "expr_" + formatInt64(int64(i))
			}
		}
	}
	return result
}

func recordToValues(record *Record, tableInfo *TableInfo) []Value {
	if record == nil {
		return make([]Value, len(tableInfo.Columns))
	}
	vals := record.Columns
	// Pad with NULLs or defaults if record has fewer columns than table (ALTER ADD COLUMN)
	for len(vals) < len(tableInfo.Columns) {
		colDef := tableInfo.Columns[len(vals)]
		if colDef.Default != nil {
			vals = append(vals, *colDef.Default)
		} else {
			vals = append(vals, Value{Type: DataTypeNull})
		}
	}
	return vals
}

func valuesToRecord(values []Value) *Record {
	return &Record{Columns: values}
}

func (e *Engine) sortRows(rows [][]Value, orderBy []OrderItem, columns []string, tableInfo *TableInfo, params []Value, outputCols []outCol) [][]Value {
	if len(orderBy) == 0 || len(rows) <= 1 {
		return rows
	}

	sort.SliceStable(rows, func(i, j int) bool {
		return e.compareRows(rows[i], rows[j], orderBy, columns, params, outputCols) < 0
	})
	return rows
}

func (e *Engine) compareRows(a, b []Value, orderBy []OrderItem, columns []string, params []Value, outputCols []outCol) int {
	for _, item := range orderBy {
		var aVal, bVal Value
		if colRef, ok := item.Expr.(ColumnRef); ok {
			idx := -1
			for i, name := range columns {
				if strings.EqualFold(name, colRef.Column) {
					idx = i
					break
				}
			}
			if idx >= 0 && idx < len(a) && idx < len(b) {
				aVal = a[idx]
				bVal = b[idx]
			}
		} else {
			// For non-ColumnRef ORDER BY (e.g., SUM(val)), find matching output column
			for i, oc := range outputCols {
				if exprsMatch(item.Expr, oc.expr) {
					if i < len(a) && i < len(b) {
						aVal = a[i]
						bVal = b[i]
					}
					break
				}
			}
		}

		cmp := CompareValues(aVal, bVal)
		if item.Desc {
			cmp = -cmp
		}
		if cmp != 0 {
			return int(cmp)
		}
	}
	return 0
}

func indexColumns(cols []IndexedColumn) []string {
	result := make([]string, len(cols))
	for i, col := range cols {
		result[i] = col.Name
	}
	return result
}

// engineError is a sentinel error type for engine errors.
type engineError struct{ msg string }

func (e *engineError) Error() string { return e.msg }

// String method for Expr (used in column name resolution)
// We need to add this to the AST types. For now, use a simple approach.
// This is defined in a separate helper to avoid modifying the AST file.

// ExprString returns a string representation of an expression.
func ExprString(expr Expr) string {
	switch e := expr.(type) {
	case LiteralExpr:
		switch e.Type {
		case DataTypeText:
			return e.TextVal
		case DataTypeInteger:
			return formatInt64(e.IntVal)
		case DataTypeFloat:
			return formatFloat64(e.FloatVal)
		case DataTypeBlob:
			return formatBlob(e.BlobVal)
		default:
			return "NULL"
		}
	case ColumnRef:
		if e.Table != "" {
			return e.Table + "." + e.Column
		}
		return e.Column
	case StarColumn:
		return "*"
	case BinaryExpr:
		return ExprString(e.Left) + " ? " + ExprString(e.Right)
	case FunctionCall:
		return e.Name + "(...)"
	case ParamExpr:
		return "?"
	case ParenExpr:
		return "(" + ExprString(e.Expr) + ")"
	default:
		return "expr"
	}
}

func isAggregateFunc(name string) bool {
	switch strings.ToUpper(name) {
	case "COUNT", "SUM", "AVG", "MIN", "MAX", "GROUP_CONCAT":
		return true
	}
	return false
}

// exprsMatch checks if two expressions are structurally equal (for ORDER BY matching).
func exprsMatch(a, b Expr) bool {
	switch av := a.(type) {
	case FunctionCall:
		bv, ok := b.(FunctionCall)
		if !ok {
			return false
		}
		if !strings.EqualFold(av.Name, bv.Name) || len(av.Args) != len(bv.Args) {
			return false
		}
		for i := range av.Args {
			if !exprsMatch(av.Args[i], bv.Args[i]) {
				return false
			}
		}
		return true
	case ColumnRef:
		bv, ok := b.(ColumnRef)
		if !ok {
			return false
		}
		return strings.EqualFold(av.Column, bv.Column) && strings.EqualFold(av.Table, bv.Table)
	case LiteralExpr:
		bv, ok := b.(LiteralExpr)
		if !ok {
			return false
		}
		return av.Type == bv.Type && av.IntVal == bv.IntVal && av.FloatVal == bv.FloatVal && av.TextVal == bv.TextVal
	}
	return false
}

// errEarlyExit signals that the scan loop should stop.
var errEarlyExit = errors.New("early exit")

// probeJoins recursively probes joined tables using nested-loop.
func (e *Engine) probeJoins(combined []Value, tables []joinEntry, joins []JoinClause, joinIdx int, emitRow func([]Value) error) error {
	if joinIdx >= len(tables) {
		return emitRow(combined)
	}

	join := joins[joinIdx-1]
	switch join.Type {
	case JoinLeft:
		return e.probeLeftJoin(combined, tables, joins, joinIdx, emitRow)
	case JoinRight:
		return e.probeRightJoin(combined, tables, joins, joinIdx, emitRow)
	case JoinFull:
		return e.probeFullJoin(combined, tables, joins, joinIdx, emitRow)
	default: // JoinInner, JoinCross
		return e.probeInnerJoin(combined, tables, joins, joinIdx, emitRow)
	}
}

// probeInnerJoin is the standard nested-loop join: only emits rows where ON matches.
func (e *Engine) probeInnerJoin(combined []Value, tables []joinEntry, joins []JoinClause, joinIdx int, emitRow func([]Value) error) error {
	jt := tables[joinIdx]

	cur, err := e.btree.Scan(jt.info.RootPage)
	if err != nil {
		return err
	}
	defer cur.Close()

	for cur.Next() {
		_, record, err := cur.Get()
		if err != nil {
			return err
		}
		vals := recordToValues(record, jt.info)
		for i, v := range vals {
			if jt.offset+i < len(combined) {
				combined[jt.offset+i] = v
			}
		}
		if !e.evalJoinOn(joins[joinIdx-1], tables, joinIdx, combined) {
			continue
		}
		if err := e.probeJoins(combined, tables, joins, joinIdx+1, emitRow); err != nil {
			return err
		}
	}
	return nil
}

// probeLeftJoin emits matched rows normally. If no right-row matches, emits
// the left row with NULLs for all right-table columns.
func (e *Engine) probeLeftJoin(combined []Value, tables []joinEntry, joins []JoinClause, joinIdx int, emitRow func([]Value) error) error {
	jt := tables[joinIdx]
	join := joins[joinIdx-1]

	cur, err := e.btree.Scan(jt.info.RootPage)
	if err != nil {
		return err
	}
	defer cur.Close()

	matched := false
	for cur.Next() {
		_, record, err := cur.Get()
		if err != nil {
			return err
		}
		vals := recordToValues(record, jt.info)
		for i, v := range vals {
			if jt.offset+i < len(combined) {
				combined[jt.offset+i] = v
			}
		}
		if !e.evalJoinOn(join, tables, joinIdx, combined) {
			continue
		}
		matched = true
		if err := e.probeJoins(combined, tables, joins, joinIdx+1, emitRow); err != nil {
			return err
		}
	}

	if !matched {
		// No right row matched - emit left row with NULLs for right columns
		for i := jt.offset; i < jt.offset+jt.columns; i++ {
			if i < len(combined) {
				combined[i] = NullValue
			}
		}
		if err := e.probeJoins(combined, tables, joins, joinIdx+1, emitRow); err != nil {
			return err
		}
	}
	return nil
}

// probeRightJoin scans the right table. For each right row that does not match
// any left row, it emits NULLs for all left columns + the right row.
func (e *Engine) probeRightJoin(combined []Value, tables []joinEntry, joins []JoinClause, joinIdx int, emitRow func([]Value) error) error {
	jt := tables[joinIdx]
	join := joins[joinIdx-1]

	// Collect all left rows from the driving table (tables[0])
	leftInfo := tables[0]
	leftCur, err := e.btree.Scan(leftInfo.info.RootPage)
	if err != nil {
		return err
	}
	var leftRows [][]Value
	for leftCur.Next() {
		_, rec, err := leftCur.Get()
		if err != nil {
			leftCur.Close()
			return err
		}
		row := make([]Value, leftInfo.columns)
		vals := recordToValues(rec, leftInfo.info)
		copy(row, vals)
		leftRows = append(leftRows, row)
	}
	leftCur.Close()

	// Scan the right table
	rightCur, err := e.btree.Scan(jt.info.RootPage)
	if err != nil {
		return err
	}
	defer rightCur.Close()

	for rightCur.Next() {
		_, record, err := rightCur.Get()
		if err != nil {
			return err
		}
		rightVals := recordToValues(record, jt.info)
		for i, v := range rightVals {
			if jt.offset+i < len(combined) {
				combined[jt.offset+i] = v
			}
		}

		// Try to find a matching left row
		foundMatch := false
		for _, leftRow := range leftRows {
			// Set left values in combined
			for i, v := range leftRow {
				if leftInfo.offset+i < len(combined) {
					combined[leftInfo.offset+i] = v
				}
			}
			if e.evalJoinOn(join, tables, joinIdx, combined) {
				foundMatch = true
				if err := e.probeJoins(combined, tables, joins, joinIdx+1, emitRow); err != nil {
					return err
				}
			}
		}

		if !foundMatch {
			// No left row matched this right row - emit NULLs for left
			for i := leftInfo.offset; i < leftInfo.offset+leftInfo.columns; i++ {
				if i < len(combined) {
					combined[i] = NullValue
				}
			}
			if err := e.probeJoins(combined, tables, joins, joinIdx+1, emitRow); err != nil {
				return err
			}
		}
	}
	return nil
}

// probeFullJoin combines LEFT + RIGHT: unmatched left rows get NULLs for right,
// and unmatched right rows get NULLs for left.
func (e *Engine) probeFullJoin(combined []Value, tables []joinEntry, joins []JoinClause, joinIdx int, emitRow func([]Value) error) error {
	jt := tables[joinIdx]
	join := joins[joinIdx-1]

	// Collect all left rows from the driving table (tables[0])
	leftInfo := tables[0]
	leftCur, err := e.btree.Scan(leftInfo.info.RootPage)
	if err != nil {
		return err
	}
	type leftRowEntry struct {
		rowid int64
		vals  []Value
	}
	var leftRows []leftRowEntry
	for leftCur.Next() {
		rid, rec, err := leftCur.Get()
		if err != nil {
			leftCur.Close()
			return err
		}
		vals := recordToValues(rec, leftInfo.info)
		row := make([]Value, len(vals))
		copy(row, vals)
		leftRows = append(leftRows, leftRowEntry{rowid: rid, vals: row})
	}
	leftCur.Close()

	leftMatched := make([]bool, len(leftRows))

	// Scan the right table
	rightCur, err := e.btree.Scan(jt.info.RootPage)
	if err != nil {
		return err
	}
	defer rightCur.Close()

	for rightCur.Next() {
		_, record, err := rightCur.Get()
		if err != nil {
			return err
		}
		rightVals := recordToValues(record, jt.info)
		for i, v := range rightVals {
			if jt.offset+i < len(combined) {
				combined[jt.offset+i] = v
			}
		}

		rightMatched := false
		for li, leftRow := range leftRows {
			// Set left values
			combined[0] = IntegerValue(leftRow.rowid)
			for i, v := range leftRow.vals {
				if leftInfo.offset+i < len(combined) {
					combined[leftInfo.offset+i] = v
				}
			}
			if e.evalJoinOn(join, tables, joinIdx, combined) {
				rightMatched = true
				leftMatched[li] = true
				if err := e.probeJoins(combined, tables, joins, joinIdx+1, emitRow); err != nil {
					return err
				}
			}
		}

		if !rightMatched {
			// Right row unmatched - NULLs for all left columns
			for i := 0; i < jt.offset; i++ {
				combined[i] = NullValue
			}
			if err := e.probeJoins(combined, tables, joins, joinIdx+1, emitRow); err != nil {
				return err
			}
		}
	}

	// Emit unmatched left rows with NULLs for right columns
	for li, matched := range leftMatched {
		if matched {
			continue
		}
		combined[0] = IntegerValue(leftRows[li].rowid)
		for i, v := range leftRows[li].vals {
			if leftInfo.offset+i < len(combined) {
				combined[leftInfo.offset+i] = v
			}
		}
		// NULLs for right columns
		for i := jt.offset; i < jt.offset+jt.columns; i++ {
			if i < len(combined) {
				combined[i] = NullValue
			}
		}
		if err := e.probeJoins(combined, tables, joins, joinIdx+1, emitRow); err != nil {
			return err
		}
	}
	return nil
}

// evalJoinOn evaluates the ON clause for a join at position joinIdx.
// Returns true if the ON clause passes (or is absent).
func (e *Engine) evalJoinOn(join JoinClause, tables []joinEntry, joinIdx int, combined []Value) bool {
	if join.On == nil {
		return true
	}
	tm := make(map[string]map[string]int)
	for _, t := range tables[:joinIdx+1] {
		cm := make(map[string]int)
		for i, col := range t.info.Columns {
			cm[strings.ToLower(col.Name)] = t.offset + i
		}
		tm[t.alias] = cm
	}
	flatColMap := make(map[string]int)
	for _, t := range tables[:joinIdx+1] {
		for i, col := range t.info.Columns {
			name := strings.ToLower(col.Name)
			if _, exists := flatColMap[name]; !exists {
				flatColMap[name] = t.offset + i
			}
		}
	}
	onEval := &ExprEval{
		Row:       combined,
		ColumnMap: flatColMap,
		TableMap:  tm,
	}
	val, err := onEval.Eval(join.On)
	if err != nil {
		return false
	}
	if val.IsNull() {
		return false
	}
	b, ok := val.AsInt64()
	return ok && b != 0
}

// insertIntoIndexes adds a row to every index on the given table that
// the row belongs in. For a partial index the row is added only when it
// satisfies the index's WHERE predicate; otherwise the index skips it
// (no entry is created, mirroring SQLite). The caller is responsible
// for removing any stale entry from a prior version of the row — see
// deleteFromIndexes, which UPDATE uses to handle rows that have moved
// out of the predicate.
func (e *Engine) insertIntoIndexes(tableName string, rowid int64, rowValues []Value) error {
	indexes := e.schema.IndexesForTable(tableName)
	tableInfo, hasTable := e.schema.GetTable(tableName)
	if !hasTable {
		return nil
	}
	for _, idx := range indexes {
		if idx.RootPage == 0 {
			continue
		}
		if idx.WhereExpr != nil && !rowMatchesPredicate(tableInfo, rowValues, idx.WhereExpr) {
			continue
		}
		keyVals := make([]Value, len(idx.Columns))
		for i, colName := range idx.Columns {
			ci := tableInfo.ColumnIndex(colName)
			if ci >= 0 && ci < len(rowValues) {
				keyVals[i] = rowValues[ci]
			} else {
				keyVals[i] = NullValue
			}
		}
		allVals := append(keyVals, IntegerValue(rowid))
		idxRecord := valuesToRecord(allVals)
		if err := e.btree.Insert(idx.RootPage, rowid, idxRecord); err != nil {
			return err
		}
	}
	return nil
}

// deleteFromIndexes removes a rowid from every index on the table.
// UPDATE calls this before insertIntoIndexes so that a row moving OUT
// of a partial index's predicate has its stale entry removed, and so
// that re-inserting an unconditional index replaces the prior key
// cleanly without depending on Insert-overwrites-rowid semantics.
func (e *Engine) deleteFromIndexes(tableName string, rowid int64) error {
	indexes := e.schema.IndexesForTable(tableName)
	for _, idx := range indexes {
		if idx.RootPage == 0 {
			continue
		}
		// btree.Delete returns nil for a missing rowid, which is the
		// correct outcome for a partial index that never contained
		// this row — no special-casing needed.
		if err := e.btree.Delete(idx.RootPage, rowid); err != nil {
			return err
		}
	}
	return nil
}

// tryIndexScan attempts to use an index for the WHERE clause.
// Returns the rows if an index was used, or nil to fall back to table scan.
func (e *Engine) tryIndexScan(tableName string, where Expr, params []Value) [][]Value {
	if where == nil {
		return nil
	}

	// Look for simple equality: column = value
	binExpr, ok := where.(BinaryExpr)
	if !ok || binExpr.Op != OpEq {
		return nil
	}

	// Determine which side is the column
	var colRef ColumnRef
	var valExpr Expr
	if cr, ok := binExpr.Left.(ColumnRef); ok && !strings.Contains(cr.Table, ".") {
		colRef = cr
		valExpr = binExpr.Right
	} else if cr, ok := binExpr.Right.(ColumnRef); ok && !strings.Contains(cr.Table, ".") {
		colRef = cr
		valExpr = binExpr.Left
	} else {
		return nil
	}

	// Find an index on this column
	indexes := e.schema.IndexesForTable(tableName)
	var idx *IndexInfo
	for _, i := range indexes {
		// Partial indexes only contain rows matching their predicate, so
		// using one for a generic equality scan would silently drop
		// non-matching rows from the result set. Keep them out of scan
		// planning until predicate-aware scan routing exists —
		// correctness over speed.
		if i.WhereExpr != nil {
			continue
		}
		if len(i.Columns) == 1 && strings.EqualFold(i.Columns[0], colRef.Column) && i.RootPage != 0 {
			idx = i
			break
		}
	}
	if idx == nil {
		return nil
	}

	// Evaluate the search value
	val, err := (&ExprEval{Params: params}).Eval(valExpr)
	if err != nil {
		return nil
	}

	// Scan the index B-tree and find matches
	cursor, err := e.btree.Scan(idx.RootPage)
	if err != nil {
		return nil
	}
	defer cursor.Close()

	tableInfo, ok := e.schema.GetTable(tableName)
	if !ok {
		return nil
	}

	var results [][]Value
	for cursor.Next() {
		_, record, err := cursor.Get()
		if err != nil {
			return nil
		}
		// Index records carry the indexed-column values followed by the
		// rowid ([v0, v1, ..., rowid]) — they are NOT table rows and
		// must not be padded to table width (recordToValues pads with
		// DEFAULTs after ALTER ADD COLUMN, which would mask the rowid
		// and yield the wrong table row on lookup).
		idxVals := record.Columns
		if len(idxVals) < 2 {
			continue
		}
		// Compare the indexed column value
		if CompareValues(idxVals[0], val) == 0 {
			// Match — the rowid is the last value in the index record.
			rowidVal := idxVals[len(idxVals)-1]
			if rv, ok := rowidVal.AsInt64(); ok {
				// Fetch the actual table row
				rec, err := e.btree.Search(tableInfo.RootPage, rv)
				if err != nil {
					continue
				}
				rowVals := recordToValues(rec, tableInfo)
				fullRow := make([]Value, len(rowVals)+1)
				fullRow[0] = IntegerValue(rv)
				copy(fullRow[1:], rowVals)
				results = append(results, fullRow)
			}
		}
	}

	return results
}

// Schema serialization types for persistence.
type schemaData struct {
	Tables  []tableData `json:"tables"`
	Indexes []indexData `json:"indexes"`
}

type tableData struct {
	Name              string      `json:"name"`
	RootPage          int         `json:"root_page"`
	SQL               string      `json:"sql"`
	AutoInc           int64       `json:"auto_inc"`
	PrimaryKey        int         `json:"primary_key"`
	Columns           []colData   `json:"columns"`
	ForeignKeys       []fkDataSer `json:"foreign_keys"`
	UniqueConstraints [][]string  `json:"unique_constraints,omitempty"`
}

type colData struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Affinity    int    `json:"affinity"`
	NotNull     bool   `json:"not_null"`
	HasDefault  bool   `json:"has_default"`
	Default     string `json:"default,omitempty"`
	DefaultExpr string `json:"default_expr,omitempty"`
	IsPK        bool   `json:"is_pk"`
	IsRowID     bool   `json:"is_rowid"`
}

type fkDataSer struct {
	FromCol int      `json:"from_col"`
	ToTable string   `json:"to_table"`
	ToCols  []string `json:"to_cols"`
}

type indexData struct {
	Name     string   `json:"name"`
	Table    string   `json:"table"`
	RootPage int      `json:"root_page"`
	Unique   bool     `json:"unique"`
	SQL      string   `json:"sql,omitempty"`
	Columns  []string `json:"columns"`
	Where    string   `json:"where,omitempty"`
}
