package sqlite

import "strings"

// Schema represents the database schema (metadata tables).
type Schema struct {
	tables      map[string]*TableInfo
	indexes     map[string]*IndexInfo
	views       map[string]*ViewInfo
	nextRowID   int64 // For sqlite_master
}

// ViewInfo stores metadata about a view.
type ViewInfo struct {
	Name string
	As   Statement
	SQL  string
}

// TableInfo stores metadata about a table.
type TableInfo struct {
	Name        string
	RootPage    int
	Columns     []ColumnDef
	PrimaryKey  int    // Column index of INTEGER PRIMARY KEY, -1 if none
	SQL         string // Original CREATE TABLE statement
	AutoInc     int64  // Next autoincrement value, 0 if not autoincrement
	ForeignKeys []ForeignKeyInfo
}

// ForeignKeyInfo stores metadata about a foreign key relationship.
type ForeignKeyInfo struct {
	FromCol int      // Column index in this table
	ToTable string   // Referenced table name
	ToCols  []string // Referenced column names
}

// IndexInfo stores metadata about an index.
type IndexInfo struct {
	Name      string
	TableName string
	RootPage  int
	Columns   []string
	Unique    bool
	Where     string // Partial index WHERE clause
	SQL       string // Original CREATE INDEX statement
}

// NewSchema creates a new empty schema.
func NewSchema() *Schema {
	return &Schema{
		tables:    make(map[string]*TableInfo),
		indexes:   make(map[string]*IndexInfo),
		views:     make(map[string]*ViewInfo),
		nextRowID: 1,
	}
}

// AddTable adds a table to the schema.
func (s *Schema) AddTable(info *TableInfo) {
	s.tables[strings.ToLower(info.Name)] = info
}

// GetTable returns table info by name (case-insensitive).
func (s *Schema) GetTable(name string) (*TableInfo, bool) {
	t, ok := s.tables[strings.ToLower(name)]
	return t, ok
}

// DropTable removes a table from the schema.
func (s *Schema) DropTable(name string) bool {
	key := strings.ToLower(name)
	_, ok := s.tables[key]
	if ok {
		delete(s.tables, key)
		// Also drop associated indexes
		for idxName, idx := range s.indexes {
			if strings.ToLower(idx.TableName) == key {
				delete(s.indexes, idxName)
			}
		}
	}
	return ok
}

func (s *Schema) RenameTable(oldName, newName string) bool {
	oldKey := strings.ToLower(oldName)
	newKey := strings.ToLower(newName)
	ti, ok := s.tables[oldKey]
	if !ok {
		return false
	}
	delete(s.tables, oldKey)
	ti.Name = newName
	s.tables[newKey] = ti
	// Update indexes that reference the old table name
	for _, idx := range s.indexes {
		if strings.ToLower(idx.TableName) == oldKey {
			idx.TableName = newName
		}
	}
	return true
}

// AddIndex adds an index to the schema.
func (s *Schema) AddIndex(info *IndexInfo) {
	s.indexes[strings.ToLower(info.Name)] = info
}

// GetIndex returns index info by name (case-insensitive).
func (s *Schema) GetIndex(name string) (*IndexInfo, bool) {
	idx, ok := s.indexes[strings.ToLower(name)]
	return idx, ok
}

// DropIndex removes an index from the schema.
func (s *Schema) DropIndex(name string) bool {
	key := strings.ToLower(name)
	_, ok := s.indexes[key]
	if ok {
		delete(s.indexes, key)
	}
	return ok
}

// TableNames returns all table names.
func (s *Schema) TableNames() []string {
	names := make([]string, 0, len(s.tables))
	for _, t := range s.tables {
		names = append(names, t.Name)
	}
	return names
}

// IndexNames returns all index names.
func (s *Schema) IndexNames() []string {
	names := make([]string, 0, len(s.indexes))
	for _, idx := range s.indexes {
		names = append(names, idx.Name)
	}
	return names
}

// IndexesForTable returns all indexes for a given table.
func (s *Schema) IndexesForTable(tableName string) []*IndexInfo {
	key := strings.ToLower(tableName)
	var result []*IndexInfo
	for _, idx := range s.indexes {
		if strings.ToLower(idx.TableName) == key {
			result = append(result, idx)
		}
	}
	return result
}

// Copy returns a deep copy of the schema.
func (s *Schema) Copy() *Schema {
	c := &Schema{
		tables:    make(map[string]*TableInfo, len(s.tables)),
		indexes:   make(map[string]*IndexInfo, len(s.indexes)),
		nextRowID: s.nextRowID,
	}
	for k, v := range s.tables {
		cp := *v
		cp.Columns = make([]ColumnDef, len(v.Columns))
		copy(cp.Columns, v.Columns)
		c.tables[k] = &cp
	}
	for k, v := range s.indexes {
		cp := *v
		c.indexes[k] = &cp
	}
	return c
}

// ColumnIndex returns the index of a column in a table, or -1.
func (t *TableInfo) ColumnIndex(name string) int {
	for i, col := range t.Columns {
		if strings.EqualFold(col.Name, name) {
			return i
		}
	}
	return -1
}

// ColumnByName returns the ColumnDef for a named column.
func (t *TableInfo) ColumnByName(name string) (ColumnDef, bool) {
	idx := t.ColumnIndex(name)
	if idx < 0 {
		return ColumnDef{}, false
	}
	return t.Columns[idx], true
}

// HasRowIDAlias returns true if this table has an INTEGER PRIMARY KEY that
// aliases to the rowid.
func (t *TableInfo) HasRowIDAlias() bool {
	return t.PrimaryKey >= 0
}

// RowIDAliasColumn returns the column name that aliases to rowid, or "".
func (t *TableInfo) RowIDAliasColumn() string {
	if t.PrimaryKey >= 0 && t.PrimaryKey < len(t.Columns) {
		return t.Columns[t.PrimaryKey].Name
	}
	return ""
}

// NextAutoIncrement returns the next autoincrement value.
func (t *TableInfo) NextAutoIncrement() int64 {
	t.AutoInc++
	return t.AutoInc
}

// SetAutoIncrement ensures AutoInc is at least v.
func (t *TableInfo) SetAutoIncrement(v int64) {
	if v > t.AutoInc {
		t.AutoInc = v
	}
}

// ResolveColumnAffinity determines the column affinity from the type string.
// SQLite rules: https://www.sqlite.org/datatype3.html
func ResolveColumnAffinity(typeStr string) ColumnAffinity {
	upper := strings.ToUpper(typeStr)

	// Rule 1: If "INT" is anywhere in the type → INTEGER
	if strings.Contains(upper, "INT") {
		return AffinityInteger
	}

	// Rule 2: If "CHAR", "CLOB", or "TEXT" is anywhere → TEXT
	if strings.Contains(upper, "CHAR") || strings.Contains(upper, "CLOB") || strings.Contains(upper, "TEXT") {
		return AffinityText
	}

	// Rule 3: If "BLOB" is anywhere or type is empty → BLOB (NONE)
	if upper == "" || strings.Contains(upper, "BLOB") {
		return AffinityBlob
	}

	// Rule 4: If "REAL", "FLOA", or "DOUB" is anywhere → REAL
	if strings.Contains(upper, "REAL") || strings.Contains(upper, "FLOA") || strings.Contains(upper, "DOUB") {
		return AffinityReal
	}

	// Rule 5: Everything else → NUMERIC
	return AffinityNumeric
}

// ApplyAffinity applies type affinity to a value.
// Returns the value possibly coerced to the affinity type.
func ApplyAffinity(v Value, affinity ColumnAffinity) Value {
	if v.IsNull() {
		return v
	}

	switch affinity {
	case AffinityInteger:
		// Try to convert text to integer
		if v.Type == DataTypeText {
			if n, ok := v.AsInt64(); ok {
				return IntegerValue(n)
			}
		}
		if v.Type == DataTypeFloat {
			return IntegerValue(int64(v.FloatVal))
		}

	case AffinityReal:
		if v.Type == DataTypeText {
			if f, ok := v.AsFloat64(); ok {
				return FloatValue(f)
			}
		}
		if v.Type == DataTypeInteger {
			return FloatValue(float64(v.IntVal))
		}

	case AffinityText:
		if v.Type == DataTypeBlob {
			// BLOB to TEXT
			return TextValue(string(v.BlobVal))
		}

	case AffinityBlob:
		if v.Type == DataTypeText {
			// TEXT to BLOB
			return BlobValue([]byte(v.TextVal))
		}

	case AffinityNumeric:
		// Try integer first, then float
		if v.Type == DataTypeText {
			s := v.TextVal
			// Check if it looks like an integer
			if looksLikeInteger(s) {
				if n, err := parseInt64(s); err == nil {
					return IntegerValue(n)
				}
			}
			if f, ok := v.AsFloat64(); ok {
				return FloatValue(f)
			}
		}
	}

	return v
}

func looksLikeInteger(s string) bool {
	if len(s) == 0 {
		return false
	}
	start := 0
	if s[0] == '-' || s[0] == '+' {
		start = 1
	}
	if start >= len(s) {
		return false
	}
	for i := start; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// BuildTableInfo creates a TableInfo from a parsed CREATE TABLE statement.
func BuildTableInfo(stmt *CreateTableStmt, rootPage int) *TableInfo {
	info := &TableInfo{
		Name:     stmt.Name,
		RootPage: rootPage,
		SQL:      "", // Will be set by caller with original SQL
	}

	for _, colAST := range stmt.Columns {
		affinity := ResolveColumnAffinity(colAST.Type)
		col := ColumnDef{
			Name:     colAST.Name,
			Type:     colAST.Type,
			Affinity: affinity,
		}

		isPrimaryKey := false
		isAutoInc := false

		for _, con := range colAST.Constraints {
			switch con.Type {
			case ConstraintPrimaryKey:
				isPrimaryKey = true
				// Check for AUTOINCREMENT
				// (handled via AST check)
			case ConstraintNotNull:
				col.NotNull = true
			case ConstraintDefault:
				// Evaluate the default expression to a Value
				if con.Value != nil {
					eval := &ExprEval{}
					val, err := eval.Eval(con.Value)
					if err == nil {
						col.Default = &val
					}
				}
			case ConstraintForeignKey:
				fk := ForeignKeyInfo{
					FromCol: len(info.Columns),
					ToTable: con.RefTable,
					ToCols:  con.RefCols,
				}
				info.ForeignKeys = append(info.ForeignKeys, fk)
			}
		}

		// Check if this is the INTEGER PRIMARY KEY (rowid alias)
		if isPrimaryKey && affinity == AffinityInteger {
			col.IsPrimaryKey = true
			col.IsRowID = true
			info.PrimaryKey = len(info.Columns)
		} else if isPrimaryKey {
			col.IsPrimaryKey = true
		}

		_ = isAutoInc
		info.Columns = append(info.Columns, col)
	}

	// If no INTEGER PRIMARY KEY was found, set PrimaryKey to -1
	if info.PrimaryKey == 0 && len(info.Columns) > 0 && !info.Columns[0].IsRowID {
		info.PrimaryKey = -1
	}

	return info
}
