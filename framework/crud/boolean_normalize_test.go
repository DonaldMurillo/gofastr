package crud

import (
	"reflect"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// identityKey leaves column names untouched, so the keys in the scanned map
// are exactly the column names the query reported.
func identityKey(s string) string { return s }

// TestConvertDatabaseValueBooleanTrue pins every type branch the boolean
// normalization path (boolean == true) accepts. It is the unit-level pin for
// the asymmetry the code deliberately carries today:
//
//   - numeric inputs (int / int32 / int64 / float64) use truthiness: any
//     value != 0 is true, so 2, -3, 2.5 all become true;
//   - textual inputs ([]byte / string) accept ONLY "true" (case-insensitive,
//     surrounding whitespace trimmed) or "1", so "2", "yes", "0" are false.
//
// That means convertDatabaseValue(int64(2), true) != convertDatabaseValue("2", true).
// The asymmetry is preserved here as current behaviour, not endorsed. See the
// note in the final report. Do not "fix" it in a coverage test.
func TestConvertDatabaseValueBooleanTrue(t *testing.T) {
	cases := []struct {
		name string
		in   any
		want bool
	}{
		{"bool true", true, true},
		{"bool false", false, false},
		{"int64 zero", int64(0), false},
		{"int64 one", int64(1), true},
		{"int64 two is truthy", int64(2), true},
		{"int64 negative is truthy", int64(-3), true},
		{"int32 two is truthy", int32(2), true},
		{"int32 zero", int32(0), false},
		{"int two is truthy", int(2), true},
		{"int zero", int(0), false},
		{"float64 zero", float64(0), false},
		{"float64 fractional is truthy", float64(2.5), true},
		{"bytes true", []byte("true"), true},
		{"bytes TRUE case-insensitive", []byte("TRUE"), true},
		{"bytes one", []byte("1"), true},
		{"bytes true trimmed", []byte(" true "), true},
		{"bytes zero is false", []byte("0"), false},
		{"bytes two is false", []byte("2"), false},
		{"bytes yes is false", []byte("yes"), false},
		{"string true", "true", true},
		{"string TRUE case-insensitive", "TRUE", true},
		{"string one", "1", true},
		{"string true trimmed", " true ", true},
		{"string zero is false", "0", false},
		{"string two is false", "2", false},
		{"string yes is false", "yes", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := convertDatabaseValue(tc.in, true)
			b, ok := got.(bool)
			if !ok {
				t.Fatalf("convertDatabaseValue(%T, true) = %#v (%T), want bool", tc.in, got, got)
			}
			if b != tc.want {
				t.Fatalf("convertDatabaseValue(%T %#v, true) = %v, want %v", tc.in, tc.in, b, tc.want)
			}
		})
	}
}

// TestConvertDatabaseValueUnhandledTypeFallsThrough pins the default branch:
// a value whose type the switch does not recognize is handed to convertValue
// unchanged rather than coerced to a bool.
func TestConvertDatabaseValueUnhandledTypeFallsThrough(t *testing.T) {
	for _, in := range []any{int16(7), uint(3), nil} {
		got := convertDatabaseValue(in, true)
		if !reflect.DeepEqual(got, in) {
			t.Fatalf("convertDatabaseValue(%T %#v, true) = %#v, want %#v unchanged", in, in, got, in)
		}
	}
}

// TestConvertDatabaseValueBooleanFalseDelegates pins the non-boolean path:
// boolean == false means "this column is not a bool", so values pass straight
// to convertValue. Notably []byte becomes string; other types are returned as
// the driver gave them (a literal bool is NOT normalized away).
func TestConvertDatabaseValueBooleanFalseDelegates(t *testing.T) {
	if got := convertDatabaseValue([]byte("hello"), false); got != "hello" {
		t.Fatalf("[]byte→string: got %#v, want %q", got, "hello")
	}
	if got := convertDatabaseValue(int64(5), false); got != int64(5) {
		t.Fatalf("int64 passthrough: got %#v, want 5", got)
	}
	if got := convertDatabaseValue(true, false); got != true {
		t.Fatalf("bool passthrough (no normalization): got %#v, want true", got)
	}
}

// TestBoolColumnsNilAndMismatchedFields covers the early-out guards in
// (*CrudHandler).boolColumns: a nil receiver, a receiver with no Entity, and a
// column list whose names do not map to any declared Bool field all yield an
// all-false slice, so those columns are never misread as booleans.
func TestBoolColumnsNilAndMismatchedFields(t *testing.T) {
	var nilHandler *CrudHandler
	if got := nilHandler.boolColumns([]string{"a", "b"}); !reflect.DeepEqual(got, []bool{false, false}) {
		t.Fatalf("nil receiver: got %#v, want all-false", got)
	}
	if got := (&CrudHandler{}).boolColumns([]string{"a"}); !reflect.DeepEqual(got, []bool{false}) {
		t.Fatalf("nil Entity: got %#v, want [false]", got)
	}

	ent := entity.Define("flags", entity.EntityConfig{
		Fields: []schema.Field{{Name: "active", Type: schema.Bool}},
	}.WithTimestamps(false))
	ch := &CrudHandler{Entity: ent}

	if got := ch.boolColumns([]string{"active", "unrelated"}); !reflect.DeepEqual(got, []bool{true, false}) {
		t.Fatalf("matched+unmatched: got %#v, want [true false]", got)
	}
	if got := ch.boolColumns([]string{"nomatch"}); !reflect.DeepEqual(got, []bool{false}) {
		t.Fatalf("no field matches: got %#v, want [false]", got)
	}
}

// TestEntityBoolColumnsNilEntity is the entity-side analogue: a nil entity
// cannot describe any column, so the result is all-false regardless of input.
func TestEntityBoolColumnsNilEntity(t *testing.T) {
	got := entityBoolColumns(nil, []string{"a", "b", "c"})
	if !reflect.DeepEqual(got, []bool{false, false, false}) {
		t.Fatalf("nil entity: got %#v, want all-false", got)
	}
}

// TestDatabaseBoolColumnsMatchesBoolDbType drives the database-side detector
// with a live result set so the match logic runs against real DatabaseTypeName
// values. A column declared BOOLEAN matches ("BOOL" substring, case-insensitive);
// plain INTEGER and REAL do not, ordinary integer columns holding 0/1 must NOT
// be turned into booleans. n larger than the column count leaves the tail false.
func TestDatabaseBoolColumnsMatchesBoolDbType(t *testing.T) {
	db := openBooleanReadDB(t)
	if _, err := db.Exec(`CREATE TABLE t (id TEXT, b BOOLEAN, i INTEGER, f REAL)`); err != nil {
		t.Fatal(err)
	}
	rows, err := db.Query(`SELECT b, i, f FROM t`)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = rows.Close() })

	got := databaseBoolColumnsForEntity(rows, 3, nil, []string{"b", "i", "f"})
	if want := []bool{true, false, false}; !reflect.DeepEqual(got, want) {
		t.Fatalf("BOOLEAN/INTEGER/REAL: got %#v, want %#v", got, want)
	}

	// n past the end of the type list must not panic and must stay false.
	got2 := databaseBoolColumnsForEntity(rows, 5, nil, []string{"b", "i", "f", "x", "y"})
	if want := []bool{true, false, false, false, false}; !reflect.DeepEqual(got2, want) {
		t.Fatalf("n>cols: got %#v, want %#v", got2, want)
	}
}

// TestDatabaseBoolColumnsClosedRowsAllFalse pins the fail-closed behaviour:
// once the rows are closed, ColumnTypes() errors, and databaseBoolColumns must
// return an all-false slice rather than panicking or guessing, no column gets
// falsely normalized as a boolean when its type information is unavailable.
func TestDatabaseBoolColumnsClosedRowsAllFalse(t *testing.T) {
	db := openBooleanReadDB(t)
	if _, err := db.Exec(`CREATE TABLE t (b BOOLEAN)`); err != nil {
		t.Fatal(err)
	}
	rows, err := db.Query(`SELECT b FROM t`)
	if err != nil {
		t.Fatal(err)
	}
	_ = rows.Close() // deliberately closed: ColumnTypes() will now error

	got := databaseBoolColumns(rows, 1)
	if want := []bool{false}; !reflect.DeepEqual(got, want) {
		t.Fatalf("closed rows: got %#v, want all-false (ColumnTypes unavailable)", got)
	}
}

// TestScanRowSingleRowScansValues exercises the package-level single-row
// scanner (the non-method entry point used outside a CrudHandler). It passes a
// nil bool-column set, so it performs NO boolean normalization: an INTEGER
// column holding 1 stays int64(1), not bool(true). That distinguishes it from
// the entity-aware (*CrudHandler).scanRow path.
func TestScanRowSingleRowScansValues(t *testing.T) {
	db := openBooleanReadDB(t)
	if _, err := db.Exec(`CREATE TABLE t (id TEXT, flag INTEGER)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO t (id, flag) VALUES ('a', 1)`); err != nil {
		t.Fatal(err)
	}
	row := db.QueryRow(`SELECT id, flag FROM t WHERE id = ?`, "a")
	got, err := scanRow(row, []string{"id", "flag"}, identityKey)
	if err != nil {
		t.Fatalf("scanRow: %v", err)
	}
	if got["id"] != "a" {
		t.Fatalf("id = %#v, want \"a\"", got["id"])
	}
	// No normalization at the package level: the raw int64 survives.
	if v, ok := got["flag"].(int64); !ok || v != 1 {
		t.Fatalf("flag = %#v (%T), want int64(1) (no normalization)", got["flag"], got["flag"])
	}
}

// TestScanRowsAndWithKeysReturnAllRows exercises both package-level multi-row
// scanners. scanRows derives its keys from a keyFunc; scanRowsWithKeys takes
// the keys verbatim. Both must return every row with correctly keyed values.
func TestScanRowsAndWithKeysReturnAllRows(t *testing.T) {
	db := openBooleanReadDB(t)
	if _, err := db.Exec(`CREATE TABLE t (id TEXT, n INTEGER)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO t (id, n) VALUES ('a', 1), ('b', 2)`); err != nil {
		t.Fatal(err)
	}

	rows, err := db.Query(`SELECT id, n FROM t ORDER BY n`)
	if err != nil {
		t.Fatal(err)
	}
	cols, err := rows.Columns()
	if err != nil {
		t.Fatal(err)
	}
	got, err := scanRows(rows, cols, identityKey)
	_ = rows.Close()
	if err != nil {
		t.Fatalf("scanRows: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("scanRows returned %d rows, want 2", len(got))
	}
	if v, ok := got[0]["n"].(int64); !ok || v != 1 {
		t.Fatalf("scanRows row0 n = %#v, want int64(1)", got[0]["n"])
	}

	// scanRowsWithKeys keys the map from the provided slice, not a keyFunc.
	rows2, err := db.Query(`SELECT id, n FROM t ORDER BY n`)
	if err != nil {
		t.Fatal(err)
	}
	got2, err := scanRowsWithKeys(rows2, cols, []string{"id_key", "n_key"})
	_ = rows2.Close()
	if err != nil {
		t.Fatalf("scanRowsWithKeys: %v", err)
	}
	if len(got2) != 2 {
		t.Fatalf("scanRowsWithKeys returned %d rows, want 2", len(got2))
	}
	if _, ok := got2[0]["n_key"]; !ok {
		t.Fatalf("scanRowsWithKeys keyed by arg: keys = %v", mapKeys(got2[0]))
	}
}

// TestScanRowWithBoolColumnsNormalizes is the entity-aware single-row path:
// when boolCols marks a column as boolean, convertDatabaseValue coerces the
// driver's int64(0|1) into a real bool. This is the contract ListAll/EagerLoad
// rely on for drivers that surface SQLite booleans as integers.
func TestScanRowWithBoolColumnsNormalizes(t *testing.T) {
	db := openBooleanReadDB(t)
	if _, err := db.Exec(`CREATE TABLE t (id TEXT, active INTEGER)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO t (id, active) VALUES ('a', 1)`); err != nil {
		t.Fatal(err)
	}
	row := db.QueryRow(`SELECT id, active FROM t WHERE id = ?`, "a")
	got, err := scanRowWithBoolColumns(row, []string{"id", "active"}, identityKey, []bool{false, true})
	if err != nil {
		t.Fatalf("scanRowWithBoolColumns: %v", err)
	}
	if v, ok := got["active"].(bool); !ok || !v {
		t.Fatalf("active = %#v (%T), want bool true (normalized)", got["active"], got["active"])
	}
}

// TestScanRowsWithKeysForEntityPropagatesScanError pins the error-propagation
// contract documented on scanRowsWithKeysForEntity: a failure mid-scan must
// surface to the caller as (nil, err), it must never return a partial result
// set as if it succeeded. Scanning into []any cannot normally fail, so the only
// deterministic trigger is a column/destination arity mismatch (two destinations
// for a single-column query). This is a contract test of the error path, not a
// normal usage example.
func TestScanRowsWithKeysForEntityPropagatesScanError(t *testing.T) {
	db := openBooleanReadDB(t)
	if _, err := db.Exec(`CREATE TABLE t (id TEXT)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO t (id) VALUES ('a')`); err != nil {
		t.Fatal(err)
	}
	rows, err := db.Query(`SELECT id FROM t`)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = rows.Close() })

	// Two destination columns for a one-column query forces rows.Scan to error.
	got, err := scanRowsWithKeysForEntity(rows, []string{"id", "ghost"}, []string{"id", "ghost"}, nil)
	if err == nil {
		t.Fatalf("expected scan error to propagate, got result %#v with nil err", got)
	}
	if got != nil {
		t.Fatalf("on scan error result must be nil, got %#v", got)
	}
}
func mapKeys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// TestTextualBoolBranchesAgree pins that a boolean column decodes the same
// way whether the driver hands back a string or a []byte. They are the same
// logical value, and drivers differ on which they return for TEXT.
func TestTextualBoolBranchesAgree(t *testing.T) {
	for _, in := range []string{"true", "TRUE", " true ", "1", " 1 ", "1\t", "0", " 0 ", "2", "yes", ""} {
		asString := convertDatabaseValue(in, true)
		asBytes := convertDatabaseValue([]byte(in), true)
		if asString != asBytes {
			t.Errorf("convertDatabaseValue(%q) = %v as string but %v as []byte", in, asString, asBytes)
		}
	}
}
