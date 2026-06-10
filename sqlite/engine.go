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
	pager       PagerInterface
	btree       BTreeInterface
	schema      *Schema
	txnSnap     *txnSnapshot
	stmtCache   map[string]Statement
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
	Snapshot() []byte
	Restore([]byte) error
	GetSchemaPage() int
	SetSchemaPage(page int)
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
		pager:     pager,
		btree:     btree,
		schema:    NewSchema(),
		stmtCache: make(map[string]Statement, 64),
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
		return nil
	}

	for _, td := range sd.Tables {
		ti := &TableInfo{
			Name:       td.Name,
			RootPage:   td.RootPage,
			SQL:        td.SQL,
			AutoInc:    td.AutoInc,
			PrimaryKey: td.PrimaryKey,
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
			if cd.HasDefault {
				v := TextValue(cd.Default)
				col.Default = &v
			}
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
		}
		ii.Columns = append(ii.Columns, idx.Columns...)
		e.schema.indexes[strings.ToLower(ii.Name)] = ii
	}

	return nil
}

// SaveSchema persists the current schema to the database.
func (e *Engine) SaveSchema() error {
	if e.pager == nil {
		return nil
	}

	sd := schemaData{}
	for _, ti := range e.schema.tables {
		d := tableData{Name: ti.Name, RootPage: ti.RootPage, SQL: ti.SQL, AutoInc: ti.AutoInc, PrimaryKey: ti.PrimaryKey}
		for _, c := range ti.Columns {
			cd := colData{Name: c.Name, Type: c.Type, Affinity: int(c.Affinity), NotNull: c.NotNull, IsPK: c.IsPrimaryKey, IsRowID: c.IsRowID}
			if c.Default != nil {
				cd.HasDefault = true
				cd.Default = c.Default.TextVal
			}
			d.Columns = append(d.Columns, cd)
		}
		for _, fk := range ti.ForeignKeys {
			d.ForeignKeys = append(d.ForeignKeys, fkDataSer{FromCol: fk.FromCol, ToTable: fk.ToTable, ToCols: fk.ToCols})
		}
		sd.Tables = append(sd.Tables, d)
	}
	for _, ii := range e.schema.indexes {
		sd.Indexes = append(sd.Indexes, indexData{Name: ii.Name, Table: ii.TableName, RootPage: ii.RootPage, Unique: ii.Unique, SQL: ii.SQL, Columns: ii.Columns})
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
		return e.executeMutation(stmt, s, params)
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
		res, err := e.executeCreateTable(s)
		if err == nil {
			e.invalidateStmtCache()
			e.SaveSchema()
		}
		return res, err
	case *CreateIndexStmt:
		res, err := e.executeCreateIndex(s)
		if err == nil {
			e.invalidateStmtCache()
			e.SaveSchema()
		}
		return res, err
	case *DropTableStmt:
		res, err := e.executeDropTable(s)
		if err == nil {
			e.invalidateStmtCache()
			e.SaveSchema()
		}
		return res, err
	case *DropIndexStmt:
		res, err := e.executeDropIndex(s)
		if err == nil {
			e.invalidateStmtCache()
			e.SaveSchema()
		}
		return res, err
	case *AlterAddColumnStmt:
		res, err := e.executeAlterAddColumn(s)
		if err == nil {
			e.invalidateStmtCache()
			e.SaveSchema()
		}
		return res, err
	case *AlterRenameTableStmt:
		res, err := e.executeAlterRenameTable(s)
		if err == nil {
			e.invalidateStmtCache()
			e.SaveSchema()
		}
		return res, err
	case *AlterRenameColumnStmt:
		res, err := e.executeAlterRenameColumn(s)
		if err == nil {
			e.invalidateStmtCache()
			e.SaveSchema()
		}
		return res, err
	case *PragmaStmt:
		return e.executePragma(s)
	case *VacuumStmt:
		return &Result{}, nil
	case *ReindexStmt:
		return &Result{}, nil
	case *CreateViewStmt:
		res, err := e.executeCreateView(s)
		if err == nil {
			e.invalidateStmtCache()
			e.SaveSchema()
		}
		return res, err
	case *DropViewStmt:
		res, err := e.executeDropView(s)
		if err == nil {
			e.invalidateStmtCache()
			e.SaveSchema()
		}
		return res, err
	default:
		return nil, &engineError{"unsupported statement type"}
	}
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

// computeAggregate evaluates a single aggregate function over a set of rows.
func (e *Engine) computeAggregate(fc FunctionCall, rows [][]Value, colIdx int) Value {
	switch strings.ToUpper(fc.Name) {
	case "COUNT":
		if len(fc.Args) == 0 {
			return IntegerValue(int64(len(rows)))
		}
		count := 0
		for _, r := range rows {
			if colIdx < len(r) && !r[colIdx].IsNull() {
				count++
			}
		}
		return IntegerValue(int64(count))
	case "SUM":
		var sum float64
		for _, r := range rows {
			if colIdx < len(r) {
				if v, ok := r[colIdx].AsFloat64(); ok {
					sum += v
				}
			}
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
	case "MIN":
		if len(rows) == 0 {
			return NullValue
		}
		min := rows[0][colIdx]
		for _, r := range rows {
			if colIdx < len(r) && CompareValues(r[colIdx], min) < 0 {
				min = r[colIdx]
			}
		}
		return min
	case "MAX":
		if len(rows) == 0 {
			return NullValue
		}
		max := rows[0][colIdx]
		for _, r := range rows {
			if colIdx < len(r) && CompareValues(r[colIdx], max) > 0 {
				max = r[colIdx]
			}
		}
		return max
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

	var lastID int64
	var totalAffected int64

	for _, valueRow := range s.Values {
		// Evaluate expressions
		eval := &ExprEval{Params: params}
		values := make([]Value, len(valueRow))
		for i, expr := range valueRow {
			val, err := eval.Eval(expr)
			if err != nil {
				return nil, err
			}
			values[i] = val
		}

		// Map to columns
		rowValues := make([]Value, len(tableInfo.Columns))
		if len(s.Columns) > 0 {
			// Explicit column list
			for i, colName := range s.Columns {
				colIdx := tableInfo.ColumnIndex(colName)
				if colIdx < 0 {
					return nil, &engineError{"no such column: " + colName}
				}
				if i < len(values) {
					rowValues[colIdx] = ApplyAffinity(values[i], tableInfo.Columns[colIdx].Affinity)
				}
			}
		} else {
			// All columns in order
			for i := range tableInfo.Columns {
				if i < len(values) {
					rowValues[i] = ApplyAffinity(values[i], tableInfo.Columns[i].Affinity)
				} else {
					rowValues[i] = NullValue
				}
			}
		}

		// Fill defaults for missing values
		for i := range rowValues {
			if rowValues[i].Type == DataTypeNull && tableInfo.Columns[i].Default != nil {
				rowValues[i] = *tableInfo.Columns[i].Default
			}
		}

		// Determine rowid
		var rowid int64
		if tableInfo.HasRowIDAlias() {
			// Get rowid from the INTEGER PRIMARY KEY column
			pkIdx := tableInfo.PrimaryKey
			if rowValues[pkIdx].IsNull() {
				// Auto-assign rowid
				rowid = tableInfo.NextAutoIncrement()
				rowValues[pkIdx] = IntegerValue(rowid)
			} else {
				rowid = rowValues[pkIdx].IntVal
				tableInfo.SetAutoIncrement(rowid)
			}
		} else {
			// Use internal rowid counter
			// For simplicity, use a hash or auto-increment
			rowid = tableInfo.NextAutoIncrement()
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
	var toUpdate []struct {
		rowid  int64
		record *Record
	}

	for cursor.Next() {
		rowid, record, err := cursor.Get()
		if err != nil {
			return nil, err
		}

		row := recordToValues(record, tableInfo)

		// Build eval context
		eval := &ExprEval{
			Row:    append([]Value{IntegerValue(rowid)}, row...),
			Params: params,
		}
		eval.ColumnMap = make(map[string]int)
		eval.ColumnMap["rowid"] = 0
		for i, col := range tableInfo.Columns {
			eval.ColumnMap[strings.ToLower(col.Name)] = i + 1
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

		if err := e.checkForeignKeyInsert(tableInfo, newRow); err != nil {
			return nil, err
		}

		toUpdate = append(toUpdate, struct {
			rowid  int64
			record *Record
		}{rowid: rowid, record: valuesToRecord(newRow)})
		totalAffected++
	}

	// Apply updates
	for _, u := range toUpdate {
		if err := e.btree.Insert(tableInfo.RootPage, u.rowid, u.record); err != nil {
			return nil, err
		}
	}

	return &Result{RowsAffected: totalAffected}, nil
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
		// Recreate the B-tree
		newRoot, err := e.btree.CreateBTree()
		if err != nil {
			return nil, err
		}
		// Count rows first for RowsAffected
		cursor, err := e.btree.Scan(tableInfo.RootPage)
		if err != nil {
			return nil, err
		}
		var count int64
		for cursor.Next() {
			count++
		}
		cursor.Close()

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
		eval := &ExprEval{
			Row:    append([]Value{IntegerValue(rowid)}, row...),
			Params: params,
		}
		eval.ColumnMap = make(map[string]int)
		eval.ColumnMap["rowid"] = 0
		for i, col := range tableInfo.Columns {
			eval.ColumnMap[strings.ToLower(col.Name)] = i + 1
		}

		val, err := eval.Eval(s.Where)
		if err != nil {
			return nil, err
		}
		if !val.IsNull() {
			if b, ok := val.AsInt64(); ok && b != 0 {
				toDelete = append(toDelete, rowid)
				toDeleteRows = append(toDeleteRows, row)
			}
		}
	}

	for i, rowid := range toDelete {
		if err := e.checkForeignKeyDelete(tableInfo, rowid, toDeleteRows[i]); err != nil {
			return nil, err
		}
		if err := e.btree.Delete(tableInfo.RootPage, rowid); err != nil {
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

	e.schema.AddIndex(&IndexInfo{
		Name:      s.Name,
		TableName: s.Table,
		RootPage:  rootPage,
		Columns:   indexColumns(s.Columns),
		Unique:    s.Unique,
	})

	return &Result{}, nil
}

// ============================================================================
// DROP TABLE
// ============================================================================

func (e *Engine) executeDropTable(s *DropTableStmt) (*Result, error) {
	if _, ok := e.schema.GetTable(s.Name); !ok && !s.IfExists {
		return nil, &engineError{"no such table: " + s.Name}
	}
	if !e.schema.DropTable(s.Name) && !s.IfExists {
		return nil, &engineError{"no such table: " + s.Name}
	}
	return &Result{}, nil
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
	return &Result{}, nil
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
		return &Result{}, nil
	}
	return &Result{
		Columns: []string{"foreign_keys"},
		Rows:    [][]Value{{IntegerValue(0)}},
	}, nil
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
	for _, fk := range ti.ForeignKeys {
		if fk.FromCol >= len(rowValues) {
			continue
		}
		val := rowValues[fk.FromCol]
		if val.IsNull() {
			continue // NULL values pass FK checks
		}
		refTable, ok := e.schema.GetTable(fk.ToTable)
		if !ok {
			return &engineError{"foreign key mismatch: no such table: " + fk.ToTable}
		}
		found, err := e.rowExists(refTable, fk.ToCols, val)
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
	// Check all tables that reference this table
	for _, tblName := range e.schema.TableNames() {
		tbl, _ := e.schema.GetTable(tblName)
		for _, fk := range tbl.ForeignKeys {
			if !strings.EqualFold(fk.ToTable, ti.Name) {
				continue
			}
			// Check if any row in tbl references the row being deleted
			if err := e.checkNoChildReference(tbl, fk, ti, rowValues); err != nil {
				return err
			}
		}
	}
	return nil
}

func (e *Engine) checkNoChildReference(childTable *TableInfo, childFK ForeignKeyInfo, parentTable *TableInfo, parentRow []Value) error {
	// Get the parent column value being deleted
	refCol := 0
	if len(childFK.ToCols) > 0 {
		refCol = parentTable.ColumnIndex(childFK.ToCols[0])
	}
	if refCol < 0 || refCol >= len(parentRow) {
		return nil
	}
	parentVal := parentRow[refCol]

	// Scan child table for rows referencing this value
	cursor, err := e.btree.Scan(childTable.RootPage)
	if err != nil {
		return err
	}
	defer cursor.Close()

	fromCol := childFK.FromCol
	for cursor.Next() {
		_, record, err := cursor.Get()
		if err != nil {
			return err
		}
		vals := recordToValues(record, childTable)
		if fromCol < len(vals) {
			childVal := vals[fromCol]
			if !childVal.IsNull() && valueEqual(childVal, parentVal) {
				return &engineError{"foreign key constraint failed"}
			}
		}
	}
	return nil
}

// rowExists checks if a row with the given value exists in the referenced columns.
func (e *Engine) rowExists(ti *TableInfo, cols []string, val Value) (bool, error) {
	colIdx := 0
	if len(cols) > 0 {
		colIdx = ti.ColumnIndex(cols[0])
	}
	if colIdx < 0 {
		// If column not found, check rowid (INTEGER PRIMARY KEY)
		if ti.PrimaryKey >= 0 {
			colIdx = ti.PrimaryKey
		} else {
			return false, nil
		}
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
		if colIdx < len(vals) && valueEqual(vals[colIdx], val) {
			return true, nil
		}
	}
	return false, nil
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

// insertIntoIndexes adds a row to all indexes on the given table.
func (e *Engine) insertIntoIndexes(tableName string, rowid int64, rowValues []Value) error {
	indexes := e.schema.IndexesForTable(tableName)
	for _, idx := range indexes {
		if idx.RootPage == 0 {
			continue
		}
		tableInfo, ok := e.schema.GetTable(tableName)
		if !ok {
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
		// Index record: [col_value, rowid]
		recVals := recordToValues(record, tableInfo)
		if len(recVals) < 2 {
			continue
		}
		// Compare the indexed column value
		if CompareValues(recVals[0], val) == 0 {
			// Match — get the rowid (last value in index record)
			rowid := recVals[len(recVals)-1]
			if rv, ok := rowid.AsInt64(); ok {
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
	Name        string      `json:"name"`
	RootPage    int         `json:"root_page"`
	SQL         string      `json:"sql"`
	AutoInc     int64       `json:"auto_inc"`
	PrimaryKey  int         `json:"primary_key"`
	Columns     []colData   `json:"columns"`
	ForeignKeys []fkDataSer `json:"foreign_keys"`
}

type colData struct {
	Name       string `json:"name"`
	Type       string `json:"type"`
	Affinity   int    `json:"affinity"`
	NotNull    bool   `json:"not_null"`
	HasDefault bool   `json:"has_default"`
	Default    string `json:"default"`
	IsPK       bool   `json:"is_pk"`
	IsRowID    bool   `json:"is_rowid"`
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
	SQL      string   `json:"sql"`
	Columns  []string `json:"columns"`
}
