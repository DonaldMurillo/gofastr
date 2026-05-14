package sqlite

import (
	"bytes"
	"fmt"
	"math"
	"sort"
	"testing"
)

// newTestBTree is a helper that creates a pager, initializes it, and returns a BTree + root page.
func newTestBTree() (*BTree, int, *Pager) {
	file := NewMemFile()
	pager, _ := NewPager(file, 4096)
	pager.InitNew()
	bt := NewBTree(pager)
	rootPage, _ := bt.CreateBTree()
	return bt, rootPage, pager
}

// ============================================================================
// CreateBTree Tests
// ============================================================================

func TestBTree_Create(t *testing.T) {
	t.Parallel()
	bt, rootPage, _ := newTestBTree()
	if rootPage < 1 {
		t.Fatalf("expected rootPage >= 1, got %d", rootPage)
	}

	// The root page should be a leaf table page
	_ = bt // bt is used via rootPage
}

func TestBTree_CreateMultiple(t *testing.T) {
	t.Parallel()
	file := NewMemFile()
	pager, _ := NewPager(file, 4096)
	pager.InitNew()
	bt := NewBTree(pager)

	r1, err := bt.CreateBTree()
	if err != nil {
		t.Fatalf("CreateBTree 1: %v", err)
	}
	r2, err := bt.CreateBTree()
	if err != nil {
		t.Fatalf("CreateBTree 2: %v", err)
	}
	r3, err := bt.CreateBTree()
	if err != nil {
		t.Fatalf("CreateBTree 3: %v", err)
	}

	if r1 == r2 || r2 == r3 || r1 == r3 {
		t.Fatalf("expected distinct page numbers: %d, %d, %d", r1, r2, r3)
	}
}

// ============================================================================
// Scan Empty Tests
// ============================================================================

func TestBTree_ScanEmpty(t *testing.T) {
	t.Parallel()
	bt, rootPage, _ := newTestBTree()

	cursor, err := bt.Scan(rootPage)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	defer cursor.Close()

	if cursor.Next() {
		t.Fatal("expected Next() to return false for empty tree")
	}
}

func TestBTree_ScanEmptyGet(t *testing.T) {
	t.Parallel()
	bt, rootPage, _ := newTestBTree()

	cursor, err := bt.Scan(rootPage)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	defer cursor.Close()

	rowid, rec, err := cursor.Get()
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if rowid != 0 || rec != nil {
		t.Fatalf("expected (0, nil) for empty cursor, got (%d, %v)", rowid, rec)
	}
}

// ============================================================================
// Insert and Search Tests
// ============================================================================

func TestBTree_InsertOneSearch(t *testing.T) {
	t.Parallel()
	bt, rootPage, _ := newTestBTree()

	rec := &Record{Columns: []Value{IntegerValue(42), TextValue("hello")}}
	err := bt.Insert(rootPage, 1, rec)
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}

	got, err := bt.Search(rootPage, 1)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if got == nil {
		t.Fatal("Search returned nil")
	}
	if len(got.Columns) != 2 {
		t.Fatalf("expected 2 columns, got %d", len(got.Columns))
	}
	if got.Columns[0].IntVal != 42 {
		t.Fatalf("expected IntVal=42, got %d", got.Columns[0].IntVal)
	}
	if got.Columns[1].TextVal != "hello" {
		t.Fatalf("expected TextVal='hello', got %q", got.Columns[1].TextVal)
	}
}

func TestBTree_SearchNotFound(t *testing.T) {
	t.Parallel()
	bt, rootPage, _ := newTestBTree()

	rec := &Record{Columns: []Value{IntegerValue(1)}}
	bt.Insert(rootPage, 1, rec)

	got, err := bt.Search(rootPage, 999)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if got != nil {
		t.Fatal("expected nil for not-found search")
	}
}

func TestBTree_SearchEmptyTree(t *testing.T) {
	t.Parallel()
	bt, rootPage, _ := newTestBTree()

	got, err := bt.Search(rootPage, 1)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if got != nil {
		t.Fatal("expected nil for search on empty tree")
	}
}

// ============================================================================
// Insert Multiple and Scan Tests
// ============================================================================

func TestBTree_InsertMultipleScanOrdered(t *testing.T) {
	t.Parallel()
	bt, rootPage, _ := newTestBTree()

	for i := int64(1); i <= 10; i++ {
		rec := &Record{Columns: []Value{IntegerValue(i * 10)}}
		bt.Insert(rootPage, i, rec)
	}

	cursor, err := bt.Scan(rootPage)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	defer cursor.Close()

	var rowids []int64
	for cursor.Next() {
		rowid, _, err := cursor.Get()
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		rowids = append(rowids, rowid)
	}

	expected := []int64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	if len(rowids) != len(expected) {
		t.Fatalf("expected %d rows, got %d", len(expected), len(rowids))
	}
	for i, id := range rowids {
		if id != expected[i] {
			t.Fatalf("row %d: expected rowid %d, got %d", i, expected[i], id)
		}
	}
}

func TestBTree_ReverseOrderInsertion(t *testing.T) {
	t.Parallel()
	bt, rootPage, _ := newTestBTree()

	for i := int64(10); i >= 1; i-- {
		rec := &Record{Columns: []Value{IntegerValue(i)}}
		bt.Insert(rootPage, i, rec)
	}

	cursor, err := bt.Scan(rootPage)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	defer cursor.Close()

	var rowids []int64
	for cursor.Next() {
		rowid, _, _ := cursor.Get()
		rowids = append(rowids, rowid)
	}

	for i, id := range rowids {
		if id != int64(i+1) {
			t.Fatalf("expected rowid %d, got %d", i+1, id)
		}
	}
}

func TestBTree_ScanReturnsCorrectRecords(t *testing.T) {
	t.Parallel()
	bt, rootPage, _ := newTestBTree()

	for i := int64(1); i <= 5; i++ {
		rec := &Record{Columns: []Value{TextValue(fmt.Sprintf("row_%d", i))}}
		bt.Insert(rootPage, i, rec)
	}

	cursor, err := bt.Scan(rootPage)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	defer cursor.Close()

	for i := int64(1); i <= 5; i++ {
		if !cursor.Next() {
			t.Fatalf("expected row %d", i)
		}
		rowid, rec, err := cursor.Get()
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if rowid != i {
			t.Fatalf("expected rowid %d, got %d", i, rowid)
		}
		expected := fmt.Sprintf("row_%d", i)
		if rec.Columns[0].TextVal != expected {
			t.Fatalf("expected %q, got %q", expected, rec.Columns[0].TextVal)
		}
	}

	if cursor.Next() {
		t.Fatal("expected no more rows")
	}
}

// ============================================================================
// Replace Tests
// ============================================================================

func TestBTree_ReplaceRow(t *testing.T) {
	t.Parallel()
	bt, rootPage, _ := newTestBTree()

	rec1 := &Record{Columns: []Value{TextValue("original")}}
	bt.Insert(rootPage, 1, rec1)

	rec2 := &Record{Columns: []Value{TextValue("replaced")}}
	bt.Insert(rootPage, 1, rec2)

	got, err := bt.Search(rootPage, 1)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if got.Columns[0].TextVal != "replaced" {
		t.Fatalf("expected 'replaced', got %q", got.Columns[0].TextVal)
	}
}

func TestBTree_ReplacePreservesOrder(t *testing.T) {
	t.Parallel()
	bt, rootPage, _ := newTestBTree()

	for i := int64(1); i <= 5; i++ {
		bt.Insert(rootPage, i, &Record{Columns: []Value{IntegerValue(i)}})
	}

	// Replace row 3
	bt.Insert(rootPage, 3, &Record{Columns: []Value{IntegerValue(999)}})

	cursor, _ := bt.Scan(rootPage)
	defer cursor.Close()

	var rowids []int64
	for cursor.Next() {
		rowid, _, _ := cursor.Get()
		rowids = append(rowids, rowid)
	}

	expected := []int64{1, 2, 3, 4, 5}
	if len(rowids) != len(expected) {
		t.Fatalf("expected %d rows, got %d", len(expected), len(rowids))
	}
	for i, id := range rowids {
		if id != expected[i] {
			t.Fatalf("row %d: expected %d, got %d", i, expected[i], id)
		}
	}

	// Verify the replaced value
	rec, _ := bt.Search(rootPage, 3)
	if rec.Columns[0].IntVal != 999 {
		t.Fatalf("expected 999, got %d", rec.Columns[0].IntVal)
	}
}

func TestBTree_ReplaceMultipleTimes(t *testing.T) {
	t.Parallel()
	bt, rootPage, _ := newTestBTree()

	for version := 0; version < 10; version++ {
		bt.Insert(rootPage, 1, &Record{Columns: []Value{IntegerValue(int64(version))}})
	}

	got, _ := bt.Search(rootPage, 1)
	if got.Columns[0].IntVal != 9 {
		t.Fatalf("expected version 9, got %d", got.Columns[0].IntVal)
	}
}

// ============================================================================
// Delete Tests
// ============================================================================

func TestBTree_DeleteExisting(t *testing.T) {
	t.Parallel()
	bt, rootPage, _ := newTestBTree()

	bt.Insert(rootPage, 1, &Record{Columns: []Value{IntegerValue(1)}})
	bt.Insert(rootPage, 2, &Record{Columns: []Value{IntegerValue(2)}})
	bt.Insert(rootPage, 3, &Record{Columns: []Value{IntegerValue(3)}})

	err := bt.Delete(rootPage, 2)
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}

	got, _ := bt.Search(rootPage, 2)
	if got != nil {
		t.Fatal("expected nil after delete")
	}

	// Others should still be there
	got1, _ := bt.Search(rootPage, 1)
	if got1 == nil {
		t.Fatal("row 1 should still exist")
	}
	got3, _ := bt.Search(rootPage, 3)
	if got3 == nil {
		t.Fatal("row 3 should still exist")
	}
}

func TestBTree_DeleteNonExistent(t *testing.T) {
	t.Parallel()
	bt, rootPage, _ := newTestBTree()

	bt.Insert(rootPage, 1, &Record{Columns: []Value{IntegerValue(1)}})

	err := bt.Delete(rootPage, 999)
	if err != nil {
		t.Fatalf("Delete non-existent should not error: %v", err)
	}

	// Original still there
	got, _ := bt.Search(rootPage, 1)
	if got == nil {
		t.Fatal("row 1 should still exist")
	}
}

func TestBTree_DeleteFromEmptyTree(t *testing.T) {
	t.Parallel()
	bt, rootPage, _ := newTestBTree()

	err := bt.Delete(rootPage, 1)
	if err != nil {
		t.Fatalf("Delete from empty tree should not error: %v", err)
	}
}

func TestBTree_DeleteAllRows(t *testing.T) {
	t.Parallel()
	bt, rootPage, _ := newTestBTree()

	for i := int64(1); i <= 5; i++ {
		bt.Insert(rootPage, i, &Record{Columns: []Value{IntegerValue(i)}})
	}

	for i := int64(1); i <= 5; i++ {
		bt.Delete(rootPage, i)
	}

	// Scan should return nothing
	cursor, _ := bt.Scan(rootPage)
	defer cursor.Close()
	if cursor.Next() {
		t.Fatal("expected no rows after deleting all")
	}
}

func TestBTree_DeleteFirstRow(t *testing.T) {
	t.Parallel()
	bt, rootPage, _ := newTestBTree()

	bt.Insert(rootPage, 1, &Record{Columns: []Value{IntegerValue(1)}})
	bt.Insert(rootPage, 2, &Record{Columns: []Value{IntegerValue(2)}})
	bt.Insert(rootPage, 3, &Record{Columns: []Value{IntegerValue(3)}})

	bt.Delete(rootPage, 1)

	cursor, _ := bt.Scan(rootPage)
	defer cursor.Close()

	var rowids []int64
	for cursor.Next() {
		rowid, _, _ := cursor.Get()
		rowids = append(rowids, rowid)
	}

	if len(rowids) != 2 || rowids[0] != 2 || rowids[1] != 3 {
		t.Fatalf("expected [2,3], got %v", rowids)
	}
}

func TestBTree_DeleteLastRow(t *testing.T) {
	t.Parallel()
	bt, rootPage, _ := newTestBTree()

	bt.Insert(rootPage, 1, &Record{Columns: []Value{IntegerValue(1)}})
	bt.Insert(rootPage, 2, &Record{Columns: []Value{IntegerValue(2)}})
	bt.Insert(rootPage, 3, &Record{Columns: []Value{IntegerValue(3)}})

	bt.Delete(rootPage, 3)

	cursor, _ := bt.Scan(rootPage)
	defer cursor.Close()

	var rowids []int64
	for cursor.Next() {
		rowid, _, _ := cursor.Get()
		rowids = append(rowids, rowid)
	}

	if len(rowids) != 2 || rowids[0] != 1 || rowids[1] != 2 {
		t.Fatalf("expected [1,2], got %v", rowids)
	}
}

func TestBTree_DeleteMiddleRow(t *testing.T) {
	t.Parallel()
	bt, rootPage, _ := newTestBTree()

	bt.Insert(rootPage, 1, &Record{Columns: []Value{IntegerValue(1)}})
	bt.Insert(rootPage, 2, &Record{Columns: []Value{IntegerValue(2)}})
	bt.Insert(rootPage, 3, &Record{Columns: []Value{IntegerValue(3)}})

	bt.Delete(rootPage, 2)

	cursor, _ := bt.Scan(rootPage)
	defer cursor.Close()

	var rowids []int64
	for cursor.Next() {
		rowid, _, _ := cursor.Get()
		rowids = append(rowids, rowid)
	}

	if len(rowids) != 2 || rowids[0] != 1 || rowids[1] != 3 {
		t.Fatalf("expected [1,3], got %v", rowids)
	}
}

// ============================================================================
// Mixed Type Tests
// ============================================================================

func TestBTree_InsertMixedTypes(t *testing.T) {
	t.Parallel()
	bt, rootPage, _ := newTestBTree()

	tests := []struct {
		rowid  int64
		record *Record
	}{
		{
			1,
			&Record{Columns: []Value{
				IntegerValue(42),
				FloatValue(3.14),
				TextValue("hello"),
				BlobValue([]byte{1, 2, 3}),
				NullValue,
			}},
		},
		{
			2,
			&Record{Columns: []Value{
				IntegerValue(0),
				FloatValue(0.0),
				TextValue(""),
				BlobValue([]byte{}),
				NullValue,
			}},
		},
		{
			3,
			&Record{Columns: []Value{
				IntegerValue(-1),
				FloatValue(-99.99),
				TextValue("world"),
				BlobValue([]byte{0xFF}),
			}},
		},
	}

	for _, tt := range tests {
		err := bt.Insert(rootPage, tt.rowid, tt.record)
		if err != nil {
			t.Fatalf("Insert rowid=%d: %v", tt.rowid, err)
		}
	}

	for _, tt := range tests {
		got, err := bt.Search(rootPage, tt.rowid)
		if err != nil {
			t.Fatalf("Search rowid=%d: %v", tt.rowid, err)
		}
		if got == nil {
			t.Fatalf("rowid=%d not found", tt.rowid)
		}
		if len(got.Columns) != len(tt.record.Columns) {
			t.Fatalf("rowid=%d: expected %d columns, got %d", tt.rowid, len(tt.record.Columns), len(got.Columns))
		}
		for j, col := range got.Columns {
			expected := tt.record.Columns[j]
			if col.Type != expected.Type {
				t.Fatalf("rowid=%d col %d: expected type %d, got %d", tt.rowid, j, expected.Type, col.Type)
			}
		}
	}
}

func TestBTree_IntegerTypes(t *testing.T) {
	t.Parallel()
	bt, rootPage, _ := newTestBTree()

	tests := []struct {
		name  string
		value int64
	}{
		{"zero", 0},
		{"one", 1},
		{"negative one", -1},
		{"large positive", 9223372036854775807},  // max int64
		{"large negative", -9223372036854775808}, // min int64
		{"small", 127},
		{"medium", 32767},
		{"large", 2147483647},
	}

	for i, tt := range tests {
		rec := &Record{Columns: []Value{IntegerValue(tt.value)}}
		err := bt.Insert(rootPage, int64(i+1), rec)
		if err != nil {
			t.Fatalf("Insert %s: %v", tt.name, err)
		}
	}

	for i, tt := range tests {
		got, err := bt.Search(rootPage, int64(i+1))
		if err != nil {
			t.Fatalf("Search %s: %v", tt.name, err)
		}
		if got.Columns[0].IntVal != tt.value {
			t.Fatalf("%s: expected %d, got %d", tt.name, tt.value, got.Columns[0].IntVal)
		}
	}
}

func TestBTree_FloatValues(t *testing.T) {
	t.Parallel()
	bt, rootPage, _ := newTestBTree()

	tests := []struct {
		name  string
		value float64
	}{
		{"zero", 0.0},
		{"positive", 3.14},
		{"negative", -2.718},
		{"very small", 0.000001},
		{"very large", 1e15},
		{"max float", math.MaxFloat64},
		{"smallest positive", math.SmallestNonzeroFloat64},
	}

	for i, tt := range tests {
		rec := &Record{Columns: []Value{FloatValue(tt.value)}}
		err := bt.Insert(rootPage, int64(i+1), rec)
		if err != nil {
			t.Fatalf("Insert %s: %v", tt.name, err)
		}
	}

	for i, tt := range tests {
		got, err := bt.Search(rootPage, int64(i+1))
		if err != nil {
			t.Fatalf("Search %s: %v", tt.name, err)
		}
		if got.Columns[0].FloatVal != tt.value {
			t.Fatalf("%s: expected %v, got %v", tt.name, tt.value, got.Columns[0].FloatVal)
		}
	}
}

func TestBTree_TextValues(t *testing.T) {
	t.Parallel()
	bt, rootPage, _ := newTestBTree()

	tests := []struct {
		name  string
		value string
	}{
		{"empty", ""},
		{"short", "hi"},
		{"medium", "hello world"},
		{"unicode", "こんにちは世界"},
		{"emoji", "😀🎉"},
		{"special chars", "foo\tbar\nbaz"},
		{"long text", string(make([]byte, 1000))}, // 1000 null bytes as text
	}

	for i, tt := range tests {
		rec := &Record{Columns: []Value{TextValue(tt.value)}}
		err := bt.Insert(rootPage, int64(i+1), rec)
		if err != nil {
			t.Fatalf("Insert %s: %v", tt.name, err)
		}
	}

	for i, tt := range tests {
		got, err := bt.Search(rootPage, int64(i+1))
		if err != nil {
			t.Fatalf("Search %s: %v", tt.name, err)
		}
		if got.Columns[0].TextVal != tt.value {
			t.Fatalf("%s: expected %q, got %q", tt.name, tt.value, got.Columns[0].TextVal)
		}
	}
}

func TestBTree_BlobValues(t *testing.T) {
	t.Parallel()
	bt, rootPage, _ := newTestBTree()

	tests := []struct {
		name  string
		value []byte
	}{
		{"nil blob", nil},
		{"empty blob", []byte{}},
		{"one byte", []byte{0x42}},
		{"zeros", make([]byte, 100)},
		{"all FF", bytes.Repeat([]byte{0xFF}, 50)},
		{"sequential", func() []byte {
			b := make([]byte, 256)
			for i := range b {
				b[i] = byte(i)
			}
			return b
		}()},
	}

	for i, tt := range tests {
		rec := &Record{Columns: []Value{BlobValue(tt.value)}}
		err := bt.Insert(rootPage, int64(i+1), rec)
		if err != nil {
			t.Fatalf("Insert %s: %v", tt.name, err)
		}
	}

	for i, tt := range tests {
		got, err := bt.Search(rootPage, int64(i+1))
		if err != nil {
			t.Fatalf("Search %s: %v", tt.name, err)
		}
		if !bytes.Equal(got.Columns[0].BlobVal, tt.value) {
			t.Fatalf("%s: blob mismatch", tt.name)
		}
	}
}

func TestBTree_NullValues(t *testing.T) {
	t.Parallel()
	bt, rootPage, _ := newTestBTree()

	rec := &Record{Columns: []Value{NullValue, IntegerValue(1), NullValue, TextValue("x")}}
	bt.Insert(rootPage, 1, rec)

	got, _ := bt.Search(rootPage, 1)
	if len(got.Columns) != 4 {
		t.Fatalf("expected 4 columns, got %d", len(got.Columns))
	}
	if !got.Columns[0].IsNull() {
		t.Fatal("column 0 should be null")
	}
	if got.Columns[1].IntVal != 1 {
		t.Fatal("column 1 should be 1")
	}
	if !got.Columns[2].IsNull() {
		t.Fatal("column 2 should be null")
	}
	if got.Columns[3].TextVal != "x" {
		t.Fatal("column 3 should be 'x'")
	}
}

// ============================================================================
// Empty Record Tests
// ============================================================================

func TestBTree_EmptyRecord(t *testing.T) {
	t.Parallel()
	bt, rootPage, _ := newTestBTree()

	rec := &Record{Columns: []Value{}}
	err := bt.Insert(rootPage, 1, rec)
	if err != nil {
		t.Fatalf("Insert empty record: %v", err)
	}

	got, err := bt.Search(rootPage, 1)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil record")
	}
	if len(got.Columns) != 0 {
		t.Fatalf("expected 0 columns, got %d", len(got.Columns))
	}
}

// ============================================================================
// Negative Rowid Tests
// ============================================================================

func TestBTree_NegativeRowids(t *testing.T) {
	t.Parallel()
	bt, rootPage, _ := newTestBTree()

	rowids := []int64{-10, -5, -1, 0, 1, 5, 10}
	for _, id := range rowids {
		rec := &Record{Columns: []Value{IntegerValue(id)}}
		err := bt.Insert(rootPage, id, rec)
		if err != nil {
			t.Fatalf("Insert rowid=%d: %v", id, err)
		}
	}

	for _, id := range rowids {
		got, err := bt.Search(rootPage, id)
		if err != nil {
			t.Fatalf("Search rowid=%d: %v", id, err)
		}
		if got == nil {
			t.Fatalf("rowid=%d not found", id)
		}
		if got.Columns[0].IntVal != id {
			t.Fatalf("expected %d, got %d", id, got.Columns[0].IntVal)
		}
	}
}

func TestBTree_NegativeRowidSearchNotFound(t *testing.T) {
	t.Parallel()
	bt, rootPage, _ := newTestBTree()

	bt.Insert(rootPage, -5, &Record{Columns: []Value{IntegerValue(-5)}})

	got, _ := bt.Search(rootPage, -999)
	if got != nil {
		t.Fatal("expected nil for non-existent negative rowid")
	}
}

// ============================================================================
// Large Number of Rows (Page Split) Tests
// ============================================================================

func TestBTree_Insert100Rows(t *testing.T) {
	t.Parallel()
	bt, rootPage, _ := newTestBTree()

	for i := int64(1); i <= 100; i++ {
		rec := &Record{Columns: []Value{TextValue(fmt.Sprintf("row_%d", i))}}
		err := bt.Insert(rootPage, i, rec)
		if err != nil {
			t.Fatalf("Insert %d: %v", i, err)
		}
	}

	// Verify all searchable
	for i := int64(1); i <= 100; i++ {
		got, err := bt.Search(rootPage, i)
		if err != nil {
			t.Fatalf("Search %d: %v", i, err)
		}
		if got == nil {
			t.Fatalf("row %d not found", i)
		}
		expected := fmt.Sprintf("row_%d", i)
		if got.Columns[0].TextVal != expected {
			t.Fatalf("row %d: expected %q, got %q", i, expected, got.Columns[0].TextVal)
		}
	}

	// Scan should return all in order
	cursor, _ := bt.Scan(rootPage)
	defer cursor.Close()

	count := 0
	var lastRowid int64 = -1
	for cursor.Next() {
		rowid, _, _ := cursor.Get()
		if rowid <= lastRowid {
			t.Fatalf("rowids not ascending: %d after %d", rowid, lastRowid)
		}
		lastRowid = rowid
		count++
	}
	if count != 100 {
		t.Fatalf("expected 100 rows, got %d", count)
	}
}

func TestBTree_Insert200Rows(t *testing.T) {
	t.Parallel()
	bt, rootPage, _ := newTestBTree()

	for i := int64(1); i <= 200; i++ {
		rec := &Record{Columns: []Value{IntegerValue(i)}}
		err := bt.Insert(rootPage, i, rec)
		if err != nil {
			t.Fatalf("Insert %d: %v", i, err)
		}
	}

	cursor, _ := bt.Scan(rootPage)
	defer cursor.Close()

	count := 0
	for cursor.Next() {
		count++
	}
	if count != 200 {
		t.Fatalf("expected 200 rows, got %d", count)
	}
}

func TestBTree_Insert500Rows(t *testing.T) {
	t.Parallel()
	bt, rootPage, _ := newTestBTree()

	for i := int64(1); i <= 500; i++ {
		rec := &Record{Columns: []Value{IntegerValue(i)}}
		err := bt.Insert(rootPage, i, rec)
		if err != nil {
			t.Fatalf("Insert %d: %v", i, err)
		}
	}

	// Spot check
	for _, id := range []int64{1, 50, 100, 250, 499, 500} {
		got, err := bt.Search(rootPage, id)
		if err != nil {
			t.Fatalf("Search %d: %v", id, err)
		}
		if got == nil || got.Columns[0].IntVal != id {
			t.Fatalf("row %d mismatch", id)
		}
	}
}

func TestBTree_LargeReverseInsertion(t *testing.T) {
	t.Parallel()
	bt, rootPage, _ := newTestBTree()

	for i := int64(100); i >= 1; i-- {
		rec := &Record{Columns: []Value{IntegerValue(i)}}
		bt.Insert(rootPage, i, rec)
	}

	// Scan should be ordered
	cursor, _ := bt.Scan(rootPage)
	defer cursor.Close()

	lastRowid := int64(0)
	count := 0
	for cursor.Next() {
		rowid, _, _ := cursor.Get()
		if rowid <= lastRowid {
			t.Fatalf("not ordered: %d after %d", rowid, lastRowid)
		}
		lastRowid = rowid
		count++
	}
	if count != 100 {
		t.Fatalf("expected 100 rows, got %d", count)
	}
}

func TestBTree_RandomOrderInsertion(t *testing.T) {
	t.Parallel()
	bt, rootPage, _ := newTestBTree()

	// Deterministic "random" order
	rowids := make([]int64, 50)
	for i := range rowids {
		rowids[i] = int64(i + 1)
	}
	// Simple shuffle using known seed pattern
	for i := len(rowids) - 1; i > 0; i-- {
		j := (i*7 + 3) % (i + 1)
		rowids[i], rowids[j] = rowids[j], rowids[i]
	}

	for _, id := range rowids {
		bt.Insert(rootPage, id, &Record{Columns: []Value{IntegerValue(id)}})
	}

	// Verify all present
	for i := int64(1); i <= 50; i++ {
		got, _ := bt.Search(rootPage, i)
		if got == nil || got.Columns[0].IntVal != i {
			t.Fatalf("row %d not found or wrong value", i)
		}
	}

	// Scan should be ordered
	cursor, _ := bt.Scan(rootPage)
	defer cursor.Close()

	lastRowid := int64(0)
	for cursor.Next() {
		rowid, _, _ := cursor.Get()
		if rowid <= lastRowid {
			t.Fatalf("not ordered: %d after %d", rowid, lastRowid)
		}
		lastRowid = rowid
	}
}

// ============================================================================
// Cursor Tests
// ============================================================================

func TestBTree_CursorNextReturnsFalseAtEnd(t *testing.T) {
	t.Parallel()
	bt, rootPage, _ := newTestBTree()

	bt.Insert(rootPage, 1, &Record{Columns: []Value{IntegerValue(1)}})

	cursor, _ := bt.Scan(rootPage)
	defer cursor.Close()

	if !cursor.Next() {
		t.Fatal("expected first Next to return true")
	}
	if cursor.Next() {
		t.Fatal("expected second Next to return false")
	}
	if cursor.Next() {
		t.Fatal("expected third Next to return false")
	}
}

func TestBTree_CursorClose(t *testing.T) {
	t.Parallel()
	bt, rootPage, _ := newTestBTree()

	bt.Insert(rootPage, 1, &Record{Columns: []Value{IntegerValue(1)}})

	cursor, _ := bt.Scan(rootPage)
	err := cursor.Close()
	if err != nil {
		t.Fatalf("Close: %v", err)
	}

	// After close, Next should return false
	if cursor.Next() {
		t.Fatal("expected false after close")
	}
}

func TestBTree_CursorMultipleScans(t *testing.T) {
	t.Parallel()
	bt, rootPage, _ := newTestBTree()

	for i := int64(1); i <= 5; i++ {
		bt.Insert(rootPage, i, &Record{Columns: []Value{IntegerValue(i)}})
	}

	// First scan
	cursor1, _ := bt.Scan(rootPage)
	var ids1 []int64
	for cursor1.Next() {
		rowid, _, _ := cursor1.Get()
		ids1 = append(ids1, rowid)
	}
	cursor1.Close()

	// Second scan
	cursor2, _ := bt.Scan(rootPage)
	var ids2 []int64
	for cursor2.Next() {
		rowid, _, _ := cursor2.Get()
		ids2 = append(ids2, rowid)
	}
	cursor2.Close()

	if len(ids1) != len(ids2) {
		t.Fatalf("scan lengths differ: %d vs %d", len(ids1), len(ids2))
	}
	for i := range ids1 {
		if ids1[i] != ids2[i] {
			t.Fatalf("scan mismatch at %d: %d vs %d", i, ids1[i], ids2[i])
		}
	}
}

func TestBTree_CursorGetBeforeNext(t *testing.T) {
	t.Parallel()
	bt, rootPage, _ := newTestBTree()

	bt.Insert(rootPage, 1, &Record{Columns: []Value{IntegerValue(42)}})

	cursor, _ := bt.Scan(rootPage)
	defer cursor.Close()

	// Get without calling Next first
	rowid, rec, err := cursor.Get()
	if err != nil {
		t.Fatalf("Get before Next: %v", err)
	}
	if rowid != 0 || rec != nil {
		t.Fatalf("expected (0, nil) before Next, got (%d, %v)", rowid, rec)
	}
}

// ============================================================================
// Multiple BTree Instances on Same Pager
// ============================================================================

func TestBTree_MultipleTreesOnSamePager(t *testing.T) {
	t.Parallel()
	file := NewMemFile()
	pager, _ := NewPager(file, 4096)
	pager.InitNew()

	bt := NewBTree(pager)

	root1, _ := bt.CreateBTree()
	root2, _ := bt.CreateBTree()

	bt.Insert(root1, 1, &Record{Columns: []Value{TextValue("tree1_row1")}})
	bt.Insert(root1, 2, &Record{Columns: []Value{TextValue("tree1_row2")}})

	bt.Insert(root2, 10, &Record{Columns: []Value{TextValue("tree2_row10")}})
	bt.Insert(root2, 20, &Record{Columns: []Value{TextValue("tree2_row20")}})

	// Search in tree 1
	got1, _ := bt.Search(root1, 1)
	if got1 == nil || got1.Columns[0].TextVal != "tree1_row1" {
		t.Fatal("tree 1 search failed")
	}

	got2, _ := bt.Search(root1, 2)
	if got2 == nil || got2.Columns[0].TextVal != "tree1_row2" {
		t.Fatal("tree 1 row 2 search failed")
	}

	// Search in tree 2
	got10, _ := bt.Search(root2, 10)
	if got10 == nil || got10.Columns[0].TextVal != "tree2_row10" {
		t.Fatal("tree 2 search failed")
	}

	got20, _ := bt.Search(root2, 20)
	if got20 == nil || got20.Columns[0].TextVal != "tree2_row20" {
		t.Fatal("tree 2 row 20 search failed")
	}

	// Cross-tree search should return nil
	gotCross, _ := bt.Search(root1, 10)
	if gotCross != nil {
		t.Fatal("cross-tree search should return nil")
	}

	gotCross2, _ := bt.Search(root2, 1)
	if gotCross2 != nil {
		t.Fatal("cross-tree search should return nil")
	}

	// Scan tree 1
	cursor1, _ := bt.Scan(root1)
	defer cursor1.Close()
	var tree1Rows []int64
	for cursor1.Next() {
		rowid, _, _ := cursor1.Get()
		tree1Rows = append(tree1Rows, rowid)
	}
	if len(tree1Rows) != 2 || tree1Rows[0] != 1 || tree1Rows[1] != 2 {
		t.Fatalf("tree 1 scan: expected [1,2], got %v", tree1Rows)
	}

	// Scan tree 2
	cursor2, _ := bt.Scan(root2)
	defer cursor2.Close()
	var tree2Rows []int64
	for cursor2.Next() {
		rowid, _, _ := cursor2.Get()
		tree2Rows = append(tree2Rows, rowid)
	}
	if len(tree2Rows) != 2 || tree2Rows[0] != 10 || tree2Rows[1] != 20 {
		t.Fatalf("tree 2 scan: expected [10,20], got %v", tree2Rows)
	}
}

func TestBTree_ThreeTreesIndependent(t *testing.T) {
	t.Parallel()
	file := NewMemFile()
	pager, _ := NewPager(file, 4096)
	pager.InitNew()

	bt := NewBTree(pager)

	roots := make([]int, 3)
	for i := range roots {
		roots[i], _ = bt.CreateBTree()
	}

	// Insert different data in each tree
	for i, root := range roots {
		for j := int64(1); j <= 10; j++ {
			bt.Insert(root, j, &Record{Columns: []Value{
				IntegerValue(int64(i) * 100),
				TextValue(fmt.Sprintf("tree%d_row%d", i, j)),
			}})
		}
	}

	// Verify each tree independently
	for i, root := range roots {
		for j := int64(1); j <= 10; j++ {
			got, err := bt.Search(root, j)
			if err != nil {
				t.Fatalf("tree %d row %d: %v", i, j, err)
			}
			if got == nil {
				t.Fatalf("tree %d row %d not found", i, j)
			}
			if got.Columns[0].IntVal != int64(i*100) {
				t.Fatalf("tree %d row %d: expected %d, got %d", i, j, i*100, got.Columns[0].IntVal)
			}
		}
	}

	// Delete from one tree shouldn't affect others
	bt.Delete(roots[0], 5)
	if got, _ := bt.Search(roots[0], 5); got != nil {
		t.Fatal("delete from tree 0 should remove row 5")
	}
	if got, _ := bt.Search(roots[1], 5); got == nil {
		t.Fatal("tree 1 row 5 should still exist")
	}
	if got, _ := bt.Search(roots[2], 5); got == nil {
		t.Fatal("tree 2 row 5 should still exist")
	}
}

// ============================================================================
// Delete and Re-Insert Tests
// ============================================================================

func TestBTree_DeleteAndReInsert(t *testing.T) {
	t.Parallel()
	bt, rootPage, _ := newTestBTree()

	bt.Insert(rootPage, 1, &Record{Columns: []Value{TextValue("first")}})
	bt.Delete(rootPage, 1)

	got, _ := bt.Search(rootPage, 1)
	if got != nil {
		t.Fatal("expected nil after delete")
	}

	// Re-insert with same rowid
	bt.Insert(rootPage, 1, &Record{Columns: []Value{TextValue("second")}})
	got2, _ := bt.Search(rootPage, 1)
	if got2 == nil {
		t.Fatal("expected to find re-inserted row")
	}
	if got2.Columns[0].TextVal != "second" {
		t.Fatalf("expected 'second', got %q", got2.Columns[0].TextVal)
	}
}

func TestBTree_InsertDeleteInsert(t *testing.T) {
	t.Parallel()
	bt, rootPage, _ := newTestBTree()

	for round := 0; round < 5; round++ {
		for i := int64(1); i <= 10; i++ {
			bt.Insert(rootPage, i, &Record{Columns: []Value{IntegerValue(int64(round)*10 + i)}})
		}

		for i := int64(1); i <= 10; i++ {
			got, _ := bt.Search(rootPage, i)
			if got == nil {
				t.Fatalf("round %d row %d not found", round, i)
			}
			expected := int64(round)*10 + i
			if got.Columns[0].IntVal != expected {
				t.Fatalf("round %d row %d: expected %d, got %d", round, i, expected, got.Columns[0].IntVal)
			}
		}

		// Delete all
		for i := int64(1); i <= 10; i++ {
			bt.Delete(rootPage, i)
		}
	}
}

// ============================================================================
// Edge Cases: Single Column Types
// ============================================================================

func TestBTree_OnlyNullColumn(t *testing.T) {
	t.Parallel()
	bt, rootPage, _ := newTestBTree()

	rec := &Record{Columns: []Value{NullValue}}
	bt.Insert(rootPage, 1, rec)

	got, _ := bt.Search(rootPage, 1)
	if len(got.Columns) != 1 {
		t.Fatalf("expected 1 column, got %d", len(got.Columns))
	}
	if !got.Columns[0].IsNull() {
		t.Fatal("expected null value")
	}
}

func TestBTree_LargeTextValue(t *testing.T) {
	t.Parallel()
	bt, rootPage, _ := newTestBTree()

	longText := string(make([]byte, 2000)) // 2000 bytes of text
	for i := range longText {
		longText = longText[:i] + "A" + longText[i+1:]
	}
	// Actually create a long string
	var buf bytes.Buffer
	for i := 0; i < 2000; i++ {
		buf.WriteByte('A')
	}
	longText = buf.String()

	bt.Insert(rootPage, 1, &Record{Columns: []Value{TextValue(longText)}})

	got, _ := bt.Search(rootPage, 1)
	if got.Columns[0].TextVal != longText {
		t.Fatalf("long text mismatch: expected %d chars, got %d", len(longText), len(got.Columns[0].TextVal))
	}
}

// ============================================================================
// Scan After Delete Tests
// ============================================================================

func TestBTree_ScanAfterDelete(t *testing.T) {
	t.Parallel()
	bt, rootPage, _ := newTestBTree()

	for i := int64(1); i <= 20; i++ {
		bt.Insert(rootPage, i, &Record{Columns: []Value{IntegerValue(i)}})
	}

	// Delete even numbers
	for i := int64(2); i <= 20; i += 2 {
		bt.Delete(rootPage, i)
	}

	cursor, _ := bt.Scan(rootPage)
	defer cursor.Close()

	var rowids []int64
	for cursor.Next() {
		rowid, _, _ := cursor.Get()
		rowids = append(rowids, rowid)
	}

	expected := []int64{1, 3, 5, 7, 9, 11, 13, 15, 17, 19}
	if len(rowids) != len(expected) {
		t.Fatalf("expected %d rows, got %d", len(expected), len(rowids))
	}
	for i, id := range rowids {
		if id != expected[i] {
			t.Fatalf("row %d: expected %d, got %d", i, expected[i], id)
		}
	}
}

func TestBTree_ScanAfterReplace(t *testing.T) {
	t.Parallel()
	bt, rootPage, _ := newTestBTree()

	for i := int64(1); i <= 5; i++ {
		bt.Insert(rootPage, i, &Record{Columns: []Value{TextValue("original")}})
	}

	// Replace some
	bt.Insert(rootPage, 2, &Record{Columns: []Value{TextValue("replaced")}})
	bt.Insert(rootPage, 4, &Record{Columns: []Value{TextValue("replaced")}})

	cursor, _ := bt.Scan(rootPage)
	defer cursor.Close()

	count := 0
	for cursor.Next() {
		rowid, rec, _ := cursor.Get()
		count++
		if rowid == 2 || rowid == 4 {
			if rec.Columns[0].TextVal != "replaced" {
				t.Fatalf("rowid %d: expected 'replaced'", rowid)
			}
		} else {
			if rec.Columns[0].TextVal != "original" {
				t.Fatalf("rowid %d: expected 'original'", rowid)
			}
		}
	}
	if count != 5 {
		t.Fatalf("expected 5 rows, got %d", count)
	}
}

// ============================================================================
// Rowid Boundary Tests
// ============================================================================

func TestBTree_RowidZero(t *testing.T) {
	t.Parallel()
	bt, rootPage, _ := newTestBTree()

	bt.Insert(rootPage, 0, &Record{Columns: []Value{IntegerValue(0)}})

	got, _ := bt.Search(rootPage, 0)
	if got == nil {
		t.Fatal("rowid 0 not found")
	}
	if got.Columns[0].IntVal != 0 {
		t.Fatalf("expected 0, got %d", got.Columns[0].IntVal)
	}
}

func TestBTree_MaxInt64Rowid(t *testing.T) {
	t.Parallel()
	bt, rootPage, _ := newTestBTree()

	id := int64(9223372036854775807)
	bt.Insert(rootPage, id, &Record{Columns: []Value{IntegerValue(id)}})

	got, _ := bt.Search(rootPage, id)
	if got == nil {
		t.Fatal("max int64 rowid not found")
	}
	if got.Columns[0].IntVal != id {
		t.Fatalf("expected %d, got %d", id, got.Columns[0].IntVal)
	}
}

func TestBTree_MinInt64Rowid(t *testing.T) {
	t.Parallel()
	bt, rootPage, _ := newTestBTree()

	id := int64(-9223372036854775808)
	bt.Insert(rootPage, id, &Record{Columns: []Value{IntegerValue(id)}})

	got, _ := bt.Search(rootPage, id)
	if got == nil {
		t.Fatal("min int64 rowid not found")
	}
	if got.Columns[0].IntVal != id {
		t.Fatalf("expected %d, got %d", id, got.Columns[0].IntVal)
	}
}

func TestBTree_SparseRowids(t *testing.T) {
	t.Parallel()
	bt, rootPage, _ := newTestBTree()

	rowids := []int64{1, 100, 1000, 10000, 100000}
	for _, id := range rowids {
		bt.Insert(rootPage, id, &Record{Columns: []Value{IntegerValue(id)}})
	}

	cursor, _ := bt.Scan(rootPage)
	defer cursor.Close()

	var got []int64
	for cursor.Next() {
		rowid, _, _ := cursor.Get()
		got = append(got, rowid)
	}

	if len(got) != len(rowids) {
		t.Fatalf("expected %d, got %d", len(rowids), len(got))
	}
	for i, id := range got {
		if id != rowids[i] {
			t.Fatalf("position %d: expected %d, got %d", i, rowids[i], id)
		}
	}
}

// ============================================================================
// Combined Operations Tests
// ============================================================================

func TestBTree_InsertDeleteSearchSequence(t *testing.T) {
	t.Parallel()
	bt, rootPage, _ := newTestBTree()

	// Insert
	bt.Insert(rootPage, 1, &Record{Columns: []Value{TextValue("one")}})
	bt.Insert(rootPage, 2, &Record{Columns: []Value{TextValue("two")}})
	bt.Insert(rootPage, 3, &Record{Columns: []Value{TextValue("three")}})

	// Verify all
	for _, id := range []int64{1, 2, 3} {
		got, _ := bt.Search(rootPage, id)
		if got == nil {
			t.Fatalf("%d not found", id)
		}
	}

	// Delete middle
	bt.Delete(rootPage, 2)

	// Verify
	if got, _ := bt.Search(rootPage, 2); got != nil {
		t.Fatal("2 should be gone")
	}

	// Replace first
	bt.Insert(rootPage, 1, &Record{Columns: []Value{TextValue("ONE")}})

	got1, _ := bt.Search(rootPage, 1)
	if got1.Columns[0].TextVal != "ONE" {
		t.Fatal("replace failed")
	}

	// Scan
	cursor, _ := bt.Scan(rootPage)
	defer cursor.Close()

	var results []string
	for cursor.Next() {
		_, rec, _ := cursor.Get()
		results = append(results, rec.Columns[0].TextVal)
	}

	if len(results) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(results))
	}
	if results[0] != "ONE" || results[1] != "three" {
		t.Fatalf("unexpected results: %v", results)
	}
}

func TestBTree_InsertManyDeleteMany(t *testing.T) {
	t.Parallel()
	bt, rootPage, _ := newTestBTree()

	const N = 100
	for i := int64(1); i <= N; i++ {
		bt.Insert(rootPage, i, &Record{Columns: []Value{IntegerValue(i)}})
	}

	// Delete every other row
	for i := int64(1); i <= N; i += 2 {
		bt.Delete(rootPage, i)
	}

	// Verify even rows still exist
	for i := int64(2); i <= N; i += 2 {
		got, err := bt.Search(rootPage, i)
		if err != nil {
			t.Fatalf("Search %d: %v", i, err)
		}
		if got == nil {
			t.Fatalf("row %d should exist", i)
		}
	}

	// Verify odd rows gone
	for i := int64(1); i <= N; i += 2 {
		got, _ := bt.Search(rootPage, i)
		if got != nil {
			t.Fatalf("row %d should be deleted", i)
		}
	}

	// Scan should return 50 rows in order
	cursor, _ := bt.Scan(rootPage)
	defer cursor.Close()

	count := 0
	lastRowid := int64(0)
	for cursor.Next() {
		rowid, _, _ := cursor.Get()
		if rowid <= lastRowid {
			t.Fatalf("not ordered: %d after %d", rowid, lastRowid)
		}
		lastRowid = rowid
		count++
	}
	if count != N/2 {
		t.Fatalf("expected %d rows, got %d", N/2, count)
	}
}

// ============================================================================
// Large Record Tests
// ============================================================================

func TestBTree_LargeRecord(t *testing.T) {
	t.Parallel()
	bt, rootPage, _ := newTestBTree()

	// Create a record with many columns
	columns := make([]Value, 50)
	for i := range columns {
		columns[i] = IntegerValue(int64(i))
	}

	bt.Insert(rootPage, 1, &Record{Columns: columns})

	got, _ := bt.Search(rootPage, 1)
	if len(got.Columns) != 50 {
		t.Fatalf("expected 50 columns, got %d", len(got.Columns))
	}
	for i, col := range got.Columns {
		if col.IntVal != int64(i) {
			t.Fatalf("column %d: expected %d, got %d", i, i, col.IntVal)
		}
	}
}

// ============================================================================
// Verify scan returns rows in sorted order regardless of insertion order
// ============================================================================

func TestBTree_ScanOrderIndependence(t *testing.T) {
	t.Parallel()

	// Different insertion orders should all produce sorted scan results
	orders := [][]int64{
		{1, 2, 3, 4, 5, 6, 7, 8, 9, 10},
		{10, 9, 8, 7, 6, 5, 4, 3, 2, 1},
		{5, 3, 8, 1, 10, 2, 7, 4, 9, 6},
		{1, 10, 2, 9, 3, 8, 4, 7, 5, 6},
	}

	for orderIdx, order := range orders {
		t.Run("", func(t *testing.T) {
			bt, rootPage, _ := newTestBTree()

			for _, id := range order {
				bt.Insert(rootPage, id, &Record{Columns: []Value{IntegerValue(id)}})
			}

			cursor, _ := bt.Scan(rootPage)
			defer cursor.Close()

			var rowids []int64
			for cursor.Next() {
				rowid, _, _ := cursor.Get()
				rowids = append(rowids, rowid)
			}

			if !sort.SliceIsSorted(rowids, func(i, j int) bool { return rowids[i] < rowids[j] }) {
				t.Fatalf("order %d: rowids not sorted: %v", orderIdx, rowids)
			}
			if len(rowids) != 10 {
				t.Fatalf("order %d: expected 10 rows, got %d", orderIdx, len(rowids))
			}
		})
	}
}

// ============================================================================
// Multiple operations on same rowid
// ============================================================================

func TestBTree_ReplaceDeleteReplace(t *testing.T) {
	t.Parallel()
	bt, rootPage, _ := newTestBTree()

	bt.Insert(rootPage, 1, &Record{Columns: []Value{TextValue("v1")}})
	bt.Insert(rootPage, 1, &Record{Columns: []Value{TextValue("v2")}})
	bt.Delete(rootPage, 1)

	if got, _ := bt.Search(rootPage, 1); got != nil {
		t.Fatal("should be nil after delete")
	}

	bt.Insert(rootPage, 1, &Record{Columns: []Value{TextValue("v3")}})
	got, _ := bt.Search(rootPage, 1)
	if got == nil || got.Columns[0].TextVal != "v3" {
		t.Fatalf("expected 'v3', got %v", got)
	}
}

// ============================================================================
// BTree with different page sizes
// ============================================================================

func TestBTree_SmallPageSize(t *testing.T) {
	t.Parallel()
	file := NewMemFile()
	pager, _ := NewPager(file, 512) // minimum page size
	pager.InitNew()
	bt := NewBTree(pager)
	rootPage, _ := bt.CreateBTree()

	// With 512 byte pages, we'll hit page splits sooner
	for i := int64(1); i <= 20; i++ {
		err := bt.Insert(rootPage, i, &Record{Columns: []Value{IntegerValue(i)}})
		if err != nil {
			t.Fatalf("Insert %d: %v", i, err)
		}
	}

	for i := int64(1); i <= 20; i++ {
		got, err := bt.Search(rootPage, i)
		if err != nil {
			t.Fatalf("Search %d: %v", i, err)
		}
		if got == nil || got.Columns[0].IntVal != i {
			t.Fatalf("row %d mismatch", i)
		}
	}
}

func TestBTree_LargePageSize(t *testing.T) {
	t.Parallel()
	file := NewMemFile()
	pager, _ := NewPager(file, 32768) // 32KB page size
	pager.InitNew()
	bt := NewBTree(pager)
	rootPage, _ := bt.CreateBTree()

	for i := int64(1); i <= 200; i++ {
		bt.Insert(rootPage, i, &Record{Columns: []Value{IntegerValue(i)}})
	}

	cursor, _ := bt.Scan(rootPage)
	defer cursor.Close()

	count := 0
	lastRowid := int64(0)
	for cursor.Next() {
		rowid, _, _ := cursor.Get()
		if rowid <= lastRowid {
			t.Fatalf("not ordered: %d after %d", rowid, lastRowid)
		}
		lastRowid = rowid
		count++
	}
	if count != 200 {
		t.Fatalf("expected 200, got %d", count)
	}
}
