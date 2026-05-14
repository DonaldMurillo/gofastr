package sqlite

import (
	"strings"
	"testing"
)

// ============================================================================
// Schema basics
// ============================================================================

func TestSchemaAddGetTable(t *testing.T) {
	s := NewSchema()
	info := &TableInfo{
		Name:     "users",
		RootPage: 2,
		Columns: []ColumnDef{
			{Name: "id", Type: "INTEGER", Affinity: AffinityInteger, IsPrimaryKey: true, IsRowID: true},
			{Name: "name", Type: "TEXT", Affinity: AffinityText},
		},
		PrimaryKey: 0,
	}
	s.AddTable(info)

	got, ok := s.GetTable("users")
	if !ok {
		t.Fatal("table not found")
	}
	if got.Name != "users" {
		t.Errorf("got name %q, want %q", got.Name, "users")
	}
	if got.RootPage != 2 {
		t.Errorf("got root page %d, want 2", got.RootPage)
	}
}

func TestSchemaCaseInsensitive(t *testing.T) {
	s := NewSchema()
	s.AddTable(&TableInfo{Name: "Users", RootPage: 2})

	if _, ok := s.GetTable("users"); !ok {
		t.Error("lowercase lookup failed")
	}
	if _, ok := s.GetTable("USERS"); !ok {
		t.Error("uppercase lookup failed")
	}
	if _, ok := s.GetTable("Users"); !ok {
		t.Error("exact case lookup failed")
	}
}

func TestSchemaDropTable(t *testing.T) {
	s := NewSchema()
	s.AddTable(&TableInfo{Name: "t1", RootPage: 2})
	s.AddTable(&TableInfo{Name: "t2", RootPage: 3})

	if !s.DropTable("t1") {
		t.Error("DropTable should return true for existing table")
	}
	if _, ok := s.GetTable("t1"); ok {
		t.Error("t1 should be gone after drop")
	}
	if s.DropTable("nonexistent") {
		t.Error("DropTable should return false for nonexistent table")
	}
}

func TestSchemaDropTableCascadesIndexes(t *testing.T) {
	s := NewSchema()
	s.AddTable(&TableInfo{Name: "users", RootPage: 2})
	s.AddIndex(&IndexInfo{Name: "idx_users_name", TableName: "users", RootPage: 3})

	s.DropTable("users")

	if _, ok := s.GetIndex("idx_users_name"); ok {
		t.Error("index should be dropped when table is dropped")
	}
}

// ============================================================================
// Index operations
// ============================================================================

func TestSchemaAddGetIndex(t *testing.T) {
	s := NewSchema()
	s.AddIndex(&IndexInfo{
		Name:      "idx_users_name",
		TableName: "users",
		RootPage:  3,
		Columns:   []string{"name"},
		Unique:    true,
	})

	idx, ok := s.GetIndex("idx_users_name")
	if !ok {
		t.Fatal("index not found")
	}
	if idx.TableName != "users" {
		t.Errorf("got table %q, want %q", idx.TableName, "users")
	}
	if !idx.Unique {
		t.Error("expected unique index")
	}
}

func TestSchemaIndexesForTable(t *testing.T) {
	s := NewSchema()
	s.AddIndex(&IndexInfo{Name: "idx1", TableName: "users", RootPage: 3})
	s.AddIndex(&IndexInfo{Name: "idx2", TableName: "users", RootPage: 4})
	s.AddIndex(&IndexInfo{Name: "idx3", TableName: "posts", RootPage: 5})

	indexes := s.IndexesForTable("users")
	if len(indexes) != 2 {
		t.Errorf("got %d indexes for users, want 2", len(indexes))
	}
}

func TestSchemaDropIndex(t *testing.T) {
	s := NewSchema()
	s.AddIndex(&IndexInfo{Name: "idx1", TableName: "users", RootPage: 3})

	if !s.DropIndex("idx1") {
		t.Error("DropIndex should return true")
	}
	if s.DropIndex("idx1") {
		t.Error("second DropIndex should return false")
	}
}

// ============================================================================
// ColumnInfo helpers
// ============================================================================

func TestColumnInfoIndex(t *testing.T) {
	info := &TableInfo{
		Columns: []ColumnDef{
			{Name: "id"},
			{Name: "name"},
			{Name: "email"},
		},
	}

	tests := []struct {
		name  string
		index int
	}{
		{"id", 0},
		{"name", 1},
		{"email", 2},
		{"missing", -1},
	}

	for _, tt := range tests {
		got := info.ColumnIndex(tt.name)
		if got != tt.index {
			t.Errorf("ColumnIndex(%q) = %d, want %d", tt.name, got, tt.index)
		}
	}
}

func TestColumnCaseInsensitive(t *testing.T) {
	info := &TableInfo{
		Columns: []ColumnDef{
			{Name: "Id"},
			{Name: "Name"},
		},
	}

	if info.ColumnIndex("id") != 0 {
		t.Error("case-insensitive lookup failed for 'id'")
	}
	if info.ColumnIndex("NAME") != 1 {
		t.Error("case-insensitive lookup failed for 'NAME'")
	}
}

func TestRowIDAlias(t *testing.T) {
	info := &TableInfo{
		Columns: []ColumnDef{
			{Name: "id", IsRowID: true, IsPrimaryKey: true},
			{Name: "name"},
		},
		PrimaryKey: 0,
	}

	if !info.HasRowIDAlias() {
		t.Error("expected HasRowIDAlias to be true")
	}
	if col := info.RowIDAliasColumn(); col != "id" {
		t.Errorf("RowIDAliasColumn() = %q, want %q", col, "id")
	}
}

func TestNoRowIDAlias(t *testing.T) {
	info := &TableInfo{
		Columns: []ColumnDef{
			{Name: "id"},
			{Name: "name"},
		},
		PrimaryKey: -1,
	}

	if info.HasRowIDAlias() {
		t.Error("expected HasRowIDAlias to be false")
	}
}

// ============================================================================
// AutoIncrement
// ============================================================================

func TestAutoIncrement(t *testing.T) {
	info := &TableInfo{AutoInc: 0}

	v1 := info.NextAutoIncrement()
	if v1 != 1 {
		t.Errorf("first autoinc = %d, want 1", v1)
	}

	v2 := info.NextAutoIncrement()
	if v2 != 2 {
		t.Errorf("second autoinc = %d, want 2", v2)
	}

	info.SetAutoIncrement(100)
	v3 := info.NextAutoIncrement()
	if v3 != 101 {
		t.Errorf("after SetAutoIncrement(100), next = %d, want 101", v3)
	}

	info.SetAutoIncrement(50) // Lower than current, should be no-op
	v4 := info.NextAutoIncrement()
	if v4 != 102 {
		t.Errorf("after SetAutoIncrement(50), next = %d, want 102", v4)
	}
}

// ============================================================================
// Affinity resolution
// ============================================================================

func TestResolveAffinityInteger(t *testing.T) {
	tests := []struct {
		typeStr  string
		affinity ColumnAffinity
	}{
		{"INTEGER", AffinityInteger},
		{"INT", AffinityInteger},
		{"BIGINT", AffinityInteger},
		{"SMALLINT", AffinityInteger},
		{"TINYINT", AffinityInteger},
		{"INT8", AffinityInteger},
		{"int", AffinityInteger},
	}
	for _, tt := range tests {
		got := ResolveColumnAffinity(tt.typeStr)
		if got != tt.affinity {
			t.Errorf("ResolveColumnAffinity(%q) = %v, want %v", tt.typeStr, got, tt.affinity)
		}
	}
}

func TestResolveAffinityText(t *testing.T) {
	tests := []struct {
		typeStr  string
		affinity ColumnAffinity
	}{
		{"TEXT", AffinityText},
		{"VARCHAR", AffinityText},
		{"VARCHAR(255)", AffinityText},
		{"CHAR", AffinityText},
		{"CLOB", AffinityText},
		{"NCHAR", AffinityText},
		{"NVARCHAR", AffinityText},
	}
	for _, tt := range tests {
		got := ResolveColumnAffinity(tt.typeStr)
		if got != tt.affinity {
			t.Errorf("ResolveColumnAffinity(%q) = %v, want %v", tt.typeStr, got, tt.affinity)
		}
	}
}

func TestResolveAffinityBlob(t *testing.T) {
	tests := []struct {
		typeStr  string
		affinity ColumnAffinity
	}{
		{"BLOB", AffinityBlob},
		{"", AffinityBlob},
	}
	for _, tt := range tests {
		got := ResolveColumnAffinity(tt.typeStr)
		if got != tt.affinity {
			t.Errorf("ResolveColumnAffinity(%q) = %v, want %v", tt.typeStr, got, tt.affinity)
		}
	}
}

func TestResolveAffinityReal(t *testing.T) {
	tests := []struct {
		typeStr  string
		affinity ColumnAffinity
	}{
		{"REAL", AffinityReal},
		{"FLOAT", AffinityReal},
		{"DOUBLE", AffinityReal},
		{"DOUBLE PRECISION", AffinityReal},
		{"REAL_FLOAT", AffinityReal},
	}
	for _, tt := range tests {
		got := ResolveColumnAffinity(tt.typeStr)
		if got != tt.affinity {
			t.Errorf("ResolveColumnAffinity(%q) = %v, want %v", tt.typeStr, got, tt.affinity)
		}
	}
}

func TestResolveAffinityNumeric(t *testing.T) {
	tests := []struct {
		typeStr  string
		affinity ColumnAffinity
	}{
		{"NUMERIC", AffinityNumeric},
		{"DECIMAL", AffinityNumeric},
		{"BOOLEAN", AffinityNumeric},
		{"DATETIME", AffinityNumeric},
		{"DATE", AffinityNumeric},
		{"MONEY", AffinityNumeric},
	}
	for _, tt := range tests {
		got := ResolveColumnAffinity(tt.typeStr)
		if got != tt.affinity {
			t.Errorf("ResolveColumnAffinity(%q) = %v, want %v", tt.typeStr, got, tt.affinity)
		}
	}
}

// ============================================================================
// Apply affinity
// ============================================================================

func TestApplyAffinityInteger(t *testing.T) {
	// Text "42" → integer 42
	result := ApplyAffinity(TextValue("42"), AffinityInteger)
	if result.Type != DataTypeInteger || result.IntVal != 42 {
		t.Errorf("ApplyAffinity(TextValue(42), INTEGER) = %v, want integer 42", result)
	}

	// Float → integer
	result = ApplyAffinity(FloatValue(3.7), AffinityInteger)
	if result.Type != DataTypeInteger || result.IntVal != 3 {
		t.Errorf("ApplyAffinity(FloatValue(3.7), INTEGER) = %v, want integer 3", result)
	}

	// Non-numeric text stays text
	result = ApplyAffinity(TextValue("hello"), AffinityInteger)
	if result.Type != DataTypeText {
		t.Errorf("ApplyAffinity(TextValue(hello), INTEGER) = %v, want text", result)
	}

	// NULL stays NULL
	result = ApplyAffinity(NullValue, AffinityInteger)
	if !result.IsNull() {
		t.Errorf("ApplyAffinity(NULL, INTEGER) should be NULL")
	}
}

func TestApplyAffinityReal(t *testing.T) {
	// Text "3.14" → float
	result := ApplyAffinity(TextValue("3.14"), AffinityReal)
	if result.Type != DataTypeFloat {
		t.Errorf("ApplyAffinity(TextValue(3.14), REAL) = %v, want float", result)
	}

	// Integer → float
	result = ApplyAffinity(IntegerValue(42), AffinityReal)
	if result.Type != DataTypeFloat || result.FloatVal != 42.0 {
		t.Errorf("ApplyAffinity(IntegerValue(42), REAL) = %v, want float 42.0", result)
	}
}

func TestApplyAffinityText(t *testing.T) {
	// BLOB → TEXT
	blob := BlobValue([]byte("hello"))
	result := ApplyAffinity(blob, AffinityText)
	if result.Type != DataTypeText || result.TextVal != "hello" {
		t.Errorf("ApplyAffinity(BlobValue(hello), TEXT) = %v, want text 'hello'", result)
	}
}

func TestApplyAffinityBlob(t *testing.T) {
	// TEXT → BLOB
	result := ApplyAffinity(TextValue("hello"), AffinityBlob)
	if result.Type != DataTypeBlob {
		t.Errorf("ApplyAffinity(TextValue(hello), BLOB) = %v, want blob", result)
	}
}

func TestApplyAffinityNumeric(t *testing.T) {
	// Text "42" → integer (numeric tries integer first)
	result := ApplyAffinity(TextValue("42"), AffinityNumeric)
	if result.Type != DataTypeInteger || result.IntVal != 42 {
		t.Errorf("ApplyAffinity(TextValue(42), NUMERIC) = %v, want integer 42", result)
	}

	// Text "3.14" → float
	result = ApplyAffinity(TextValue("3.14"), AffinityNumeric)
	if result.Type != DataTypeFloat {
		t.Errorf("ApplyAffinity(TextValue(3.14), NUMERIC) = %v, want float", result)
	}

	// Non-numeric text stays text
	result = ApplyAffinity(TextValue("hello"), AffinityNumeric)
	if result.Type != DataTypeText {
		t.Errorf("ApplyAffinity(TextValue(hello), NUMERIC) = %v, want text", result)
	}
}

// ============================================================================
// BuildTableInfo
// ============================================================================

func TestBuildTableInfoBasic(t *testing.T) {
	stmt := &CreateTableStmt{
		Name: "users",
		Columns: []ColumnDefAST{
			{Name: "id", Type: "INTEGER", Constraints: []ColumnConstraint{
				{Type: ConstraintPrimaryKey},
			}},
			{Name: "name", Type: "TEXT", Constraints: []ColumnConstraint{
				{Type: ConstraintNotNull},
			}},
			{Name: "age", Type: "INTEGER"},
		},
	}

	info := BuildTableInfo(stmt, 2)

	if info.Name != "users" {
		t.Errorf("name = %q, want %q", info.Name, "users")
	}
	if info.RootPage != 2 {
		t.Errorf("rootPage = %d, want 2", info.RootPage)
	}
	if len(info.Columns) != 3 {
		t.Fatalf("columns = %d, want 3", len(info.Columns))
	}

	// First column should be rowid alias
	if !info.Columns[0].IsRowID {
		t.Error("id column should be rowid alias")
	}
	if info.PrimaryKey != 0 {
		t.Errorf("PrimaryKey = %d, want 0", info.PrimaryKey)
	}

	// Second column should have NOT NULL
	if !info.Columns[1].NotNull {
		t.Error("name column should be NOT NULL")
	}

	// Third column should be plain
	if info.Columns[2].NotNull {
		t.Error("age column should allow NULL")
	}
}

func TestBuildTableInfoNoPrimaryKey(t *testing.T) {
	stmt := &CreateTableStmt{
		Name: "data",
		Columns: []ColumnDefAST{
			{Name: "key", Type: "TEXT"},
			{Name: "value", Type: "BLOB"},
		},
	}

	info := BuildTableInfo(stmt, 3)

	if info.PrimaryKey != -1 {
		t.Errorf("PrimaryKey = %d, want -1 (no INTEGER PRIMARY KEY)", info.PrimaryKey)
	}
}

func TestBuildTableInfoTextPrimaryKey(t *testing.T) {
	// TEXT PRIMARY KEY is NOT a rowid alias
	stmt := &CreateTableStmt{
		Name: "kv",
		Columns: []ColumnDefAST{
			{Name: "key", Type: "TEXT", Constraints: []ColumnConstraint{
				{Type: ConstraintPrimaryKey},
			}},
		},
	}

	info := BuildTableInfo(stmt, 2)

	if info.HasRowIDAlias() {
		t.Error("TEXT PRIMARY KEY should not be a rowid alias")
	}
}

// ============================================================================
// TableNames / IndexNames
// ============================================================================

func TestSchemaTableNames(t *testing.T) {
	s := NewSchema()
	s.AddTable(&TableInfo{Name: "alpha", RootPage: 2})
	s.AddTable(&TableInfo{Name: "beta", RootPage: 3})

	names := s.TableNames()
	if len(names) != 2 {
		t.Errorf("got %d table names, want 2", len(names))
	}
}

func TestSchemaIndexNames(t *testing.T) {
	s := NewSchema()
	s.AddIndex(&IndexInfo{Name: "idx1", TableName: "t1", RootPage: 2})
	s.AddIndex(&IndexInfo{Name: "idx2", TableName: "t1", RootPage: 3})

	names := s.IndexNames()
	if len(names) != 2 {
		t.Errorf("got %d index names, want 2", len(names))
	}
}

// ============================================================================
// looksLikeInteger
// ============================================================================

func TestLooksLikeInteger(t *testing.T) {
	tests := []struct {
		s    string
		want bool
	}{
		{"0", true},
		{"42", true},
		{"-1", true},
		{"+1", true},
		{"", false},
		{"3.14", false},
		{"hello", false},
		{"-", false},
		{"+", false},
		{"123abc", false},
	}

	for _, tt := range tests {
		got := looksLikeInteger(tt.s)
		if got != tt.want {
			t.Errorf("looksLikeInteger(%q) = %v, want %v", tt.s, got, tt.want)
		}
	}
}

// ============================================================================
// Value.String() with all types
// ============================================================================

func TestValueString(t *testing.T) {
	tests := []struct {
		v    Value
		want string
	}{
		{NullValue, "NULL"},
		{IntegerValue(0), "0"},
		{IntegerValue(42), "42"},
		{IntegerValue(-1), "-1"},
		{TextValue("hello"), "hello"},
		{FloatValue(0), "0.0"},
		{BlobValue([]byte{0xDE, 0xAD}), "X'dead'"},
		{Value{Type: DataTypeInteger, IntVal: 9223372036854775807}, "9223372036854775807"},
	}

	for _, tt := range tests {
		got := tt.v.String()
		if got != tt.want {
			t.Errorf("Value{%v}.String() = %q, want %q", tt.v.Type, got, tt.want)
		}
	}
}

// ============================================================================
// Value conversion
// ============================================================================

func TestValueAsInt64(t *testing.T) {
	tests := []struct {
		v    Value
		want int64
		ok   bool
	}{
		{IntegerValue(42), 42, true},
		{FloatValue(3.7), 3, true},
		{TextValue("100"), 100, true},
		{TextValue("hello"), 0, false},
		{NullValue, 0, false},
		{BlobValue([]byte{1}), 0, false},
	}

	for _, tt := range tests {
		got, ok := tt.v.AsInt64()
		if ok != tt.ok {
			t.Errorf("AsInt64(%v) ok = %v, want %v", tt.v, ok, tt.ok)
		}
		if ok && got != tt.want {
			t.Errorf("AsInt64(%v) = %d, want %d", tt.v, got, tt.want)
		}
	}
}

func TestValueAsFloat64(t *testing.T) {
	tests := []struct {
		v    Value
		want float64
		ok   bool
	}{
		{FloatValue(3.14), 3.14, true},
		{IntegerValue(42), 42.0, true},
		{TextValue("2.5"), 2.5, true},
		{TextValue("hello"), 0, false},
		{NullValue, 0, false},
	}

	for _, tt := range tests {
		got, ok := tt.v.AsFloat64()
		if ok != tt.ok {
			t.Errorf("AsFloat64(%v) ok = %v, want %v", tt.v, ok, tt.ok)
		}
		if ok && got != tt.want {
			t.Errorf("AsFloat64(%v) = %f, want %f", tt.v, got, tt.want)
		}
	}
}

// ============================================================================
// Value comparison
// ============================================================================

func TestCompareValuesNull(t *testing.T) {
	// NULL = NULL
	if CompareValues(NullValue, NullValue) != CompareEqual {
		t.Error("NULL should equal NULL")
	}
	// NULL < anything
	if CompareValues(NullValue, IntegerValue(0)) != CompareLess {
		t.Error("NULL should be less than 0")
	}
	if CompareValues(IntegerValue(0), NullValue) != CompareGreater {
		t.Error("0 should be greater than NULL")
	}
}

func TestCompareValuesIntegers(t *testing.T) {
	if CompareValues(IntegerValue(1), IntegerValue(2)) != CompareLess {
		t.Error("1 < 2")
	}
	if CompareValues(IntegerValue(2), IntegerValue(1)) != CompareGreater {
		t.Error("2 > 1")
	}
	if CompareValues(IntegerValue(5), IntegerValue(5)) != CompareEqual {
		t.Error("5 == 5")
	}
}

func TestCompareValuesMixed(t *testing.T) {
	// int vs float: compare as numbers
	if CompareValues(IntegerValue(1), FloatValue(2.0)) != CompareLess {
		t.Error("1 < 2.0")
	}
	if CompareValues(IntegerValue(2), FloatValue(1.5)) != CompareGreater {
		t.Error("2 > 1.5")
	}

	// TEXT vs INTEGER: TEXT is greater by type order
	if CompareValues(TextValue("a"), IntegerValue(999)) != CompareGreater {
		t.Error("TEXT should be greater than INTEGER by type order")
	}

	// BLOB vs TEXT: BLOB is greater by type order
	if CompareValues(BlobValue([]byte{1}), TextValue("hello")) != CompareGreater {
		t.Error("BLOB should be greater than TEXT by type order")
	}
}

func TestCompareValuesStrings(t *testing.T) {
	if CompareValues(TextValue("abc"), TextValue("abd")) != CompareLess {
		t.Error("abc < abd")
	}
	if CompareValues(TextValue("b"), TextValue("a")) != CompareGreater {
		t.Error("b > a")
	}
	if CompareValues(TextValue("same"), TextValue("same")) != CompareEqual {
		t.Error("same == same")
	}
}

func TestCompareValuesBlobs(t *testing.T) {
	if CompareValues(BlobValue([]byte{1}), BlobValue([]byte{2})) != CompareLess {
		t.Error("{1} < {2}")
	}
	if CompareValues(BlobValue([]byte{1, 2}), BlobValue([]byte{1})) != CompareGreater {
		t.Error("{1,2} > {1} (longer)")
	}
	if CompareValues(BlobValue([]byte{1}), BlobValue([]byte{1})) != CompareEqual {
		t.Error("{1} == {1}")
	}
}

// Suppress unused import warning
var _ = strings.ToLower
