package sqlite

import "testing"

// Legacy files (pre-default_expr) serialized only the raw TextVal of a
// constant default; new files carry the expression source. Both must load.
func TestLoadColumnDefaultBothFormats(t *testing.T) {
	load := func(cd colData) ColumnDef {
		col := ColumnDef{Name: cd.Name}
		loadColumnDefault(&col, cd)
		return col
	}

	// Legacy: empty-string default is a real default, not "no default".
	col := load(colData{Name: "lane", HasDefault: true, Default: ""})
	if col.Default == nil || col.Default.Type != DataTypeText || col.Default.TextVal != "" {
		t.Fatalf("legacy DEFAULT '' = %+v, want empty text value", col.Default)
	}

	// Legacy: unquoted text must stay text, never parse as an identifier.
	col = load(colData{Name: "status", HasDefault: true, Default: "pending"})
	if col.Default == nil || col.Default.Type != DataTypeText || col.Default.TextVal != "pending" {
		t.Fatalf("legacy DEFAULT pending = %+v, want text \"pending\"", col.Default)
	}

	// Legacy: numbers round-trip as numbers.
	col = load(colData{Name: "n", HasDefault: true, Default: "42"})
	if col.Default == nil || col.Default.Type != DataTypeInteger || col.Default.IntVal != 42 {
		t.Fatalf("legacy DEFAULT 42 = %+v, want integer 42", col.Default)
	}
	col = load(colData{Name: "x", HasDefault: true, Default: "1.5"})
	if col.Default == nil || col.Default.Type != DataTypeFloat || col.Default.FloatVal != 1.5 {
		t.Fatalf("legacy DEFAULT 1.5 = %+v, want float 1.5", col.Default)
	}

	// New format: quoted text literal.
	col = load(colData{Name: "status", HasDefault: true, DefaultExpr: "'pending'"})
	if col.Default == nil || col.Default.Type != DataTypeText || col.Default.TextVal != "pending" {
		t.Fatalf("default_expr 'pending' = %+v, want text \"pending\"", col.Default)
	}

	// New format: dynamic default has an expression but no constant.
	col = load(colData{Name: "created_at", HasDefault: true, DefaultExpr: "CURRENT_TIMESTAMP"})
	if col.DefaultExpr == nil {
		t.Fatal("default_expr CURRENT_TIMESTAMP did not restore an expression")
	}
	if col.Default != nil {
		t.Fatalf("dynamic default cached a constant %+v, want nil", col.Default)
	}

	// No default at all.
	col = load(colData{Name: "plain"})
	if col.Default != nil || col.DefaultExpr != nil {
		t.Fatalf("no-default column loaded %+v / %+v", col.Default, col.DefaultExpr)
	}
}
