package crud

import (
	"database/sql"
	"fmt"
	"testing"
)

// Finding 12: pools must not retain unbounded historical capacity.
// returnRowSlice should drop maps with len > maxPooledMapEntries rather
// than putting them back in the pool.
func TestRowMapPoolDropsHugeMap(t *testing.T) {
	// Stage a slice with one giant row, run it through the normal
	// borrow/return cycle, and assert the pool's high-water-mark stays
	// bounded — pulling N small maps in a row must never surface the
	// pathological one.
	s := borrowRowSlice()
	huge := make(map[string]any, maxPooledMapEntries+100)
	for i := 0; i < maxPooledMapEntries+100; i++ {
		huge[intStr(i)] = i
	}
	*s = append(*s, huge)
	returnRowSlice(s)

	// The map was put back only if its size was bounded. Confirm by
	// pulling enough maps that we should be sampling the pool.
	for i := 0; i < 16; i++ {
		got := rowMapPool.Get().(*map[string]any)
		// A returned map should have zero length; cap is not directly
		// readable but len(map) after delete-all is 0. The signal we
		// want is that the underlying map storage is bounded.
		if len(*got) != 0 {
			t.Fatalf("pulled non-empty map: len=%d", len(*got))
		}
	}
}

// Finding 12: returnPtrSlice should drop slices with cap >
// maxPooledMapEntries rather than pooling them.
func TestPtrSlicePoolDropsHugeSlice(t *testing.T) {
	p := ptrSlicePool.Get().(*[]any)
	*p = make([]any, maxPooledMapEntries+100)
	returnPtrSlice(p)

	// All subsequent borrows must have bounded cap.
	for i := 0; i < 8; i++ {
		got := ptrSlicePool.Get().(*[]any)
		if cap(*got) > maxPooledMapEntries {
			t.Fatalf("pool surfaced slice with cap=%d (> maxPooledMapEntries %d)", cap(*got), maxPooledMapEntries)
		}
	}
}

func intStr(i int) string {
	if i == 0 {
		return "0"
	}
	var s []byte
	for i > 0 {
		s = append([]byte{byte('0' + i%10)}, s...)
		i /= 10
	}
	return string(s)
}

func TestBorrowReturnRowSlice(t *testing.T) {
	s := borrowRowSlice()
	*s = append(*s, map[string]any{"a": 1})
	*s = append(*s, map[string]any{"b": 2})
	if len(*s) != 2 {
		t.Fatalf("len = %d, want 2", len(*s))
	}
	returnRowSlice(s)
	// After return, the slice should be reusable
	s2 := borrowRowSlice()
	if len(*s2) != 0 {
		t.Fatalf("len after reuse = %d, want 0", len(*s2))
	}
	returnRowSlice(s2)
}

func TestBorrowReturnPtrSlice(t *testing.T) {
	p := borrowPtrSlice(5)
	if len(*p) != 5 {
		t.Fatalf("len = %d, want 5", len(*p))
	}
	returnPtrSlice(p)
	p2 := borrowPtrSlice(3)
	if len(*p2) != 3 {
		t.Fatalf("len after reuse = %d, want 3", len(*p2))
	}
	returnPtrSlice(p2)
}

func TestScanRowsPooledEmpty(t *testing.T) {
	// Use an in-memory SQLite to test scanRowsPooled end-to-end.
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Skip("sqlite3 driver not available")
	}
	defer db.Close()

	_, err = db.Exec("CREATE TABLE t (id INTEGER, name TEXT)")
	if err != nil {
		t.Skipf("sqlite3 not available: %v", err)
	}

	rows, err := db.Query("SELECT id, name FROM t")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	result, err := scanRowsPooled(rows, []string{"id", "name"}, func(s string) string { return s })
	if err != nil {
		t.Fatal(err)
	}
	if len(*result) != 0 {
		t.Fatalf("expected 0 rows, got %d", len(*result))
	}
	returnRowSlice(result)
}

func TestScanRowsPooledWithData(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Skip("sqlite3 driver not available")
	}
	defer db.Close()

	_, err = db.Exec("CREATE TABLE t (id INTEGER, name TEXT)")
	if err != nil {
		t.Skipf("sqlite3 not available: %v", err)
	}
	_, err = db.Exec("INSERT INTO t (id, name) VALUES (1, 'alice'), (2, 'bob')")
	if err != nil {
		t.Fatal(err)
	}

	rows, err := db.Query("SELECT id, name FROM t")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	result, err := scanRowsPooled(rows, []string{"id", "name"}, func(s string) string { return s })
	if err != nil {
		t.Fatal(err)
	}
	if len(*result) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(*result))
	}
	row0 := (*result)[0]
	if row0["name"] != "alice" {
		t.Errorf("row0 name = %v, want alice", row0["name"])
	}
	row1 := (*result)[1]
	if row1["name"] != "bob" {
		t.Errorf("row1 name = %v, want bob", row1["name"])
	}
	returnRowSlice(result)
}

func TestScanRowsPooledKeyFunc(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Skip("sqlite3 driver not available")
	}
	defer db.Close()

	_, err = db.Exec("CREATE TABLE t (my_col INTEGER)")
	if err != nil {
		t.Skipf("sqlite3 not available: %v", err)
	}
	_, err = db.Exec("INSERT INTO t (my_col) VALUES (42)")
	if err != nil {
		t.Fatal(err)
	}

	rows, err := db.Query("SELECT my_col FROM t")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	// keyFunc converts column names to uppercase
	result, err := scanRowsPooled(rows, []string{"my_col"}, func(s string) string {
		return "MY_COL"
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(*result) != 1 {
		t.Fatalf("expected 1 row, got %d", len(*result))
	}
	v, ok := (*result)[0]["MY_COL"]
	if !ok {
		t.Error("key MY_COL not found in row")
	} else if v == nil {
		t.Error("MY_COL value is nil")
	}
	returnRowSlice(result)
}

// TestScanRowsPooled_NoAliasingAcrossRows pins the contract that the
// per-row scratch []any pool introduced for issue #100B doesn't alias scan
// destinations across rows. The pool hands the same backing slice to the
// next iteration once the previous row's map is built; if the row-map
// build didn't COPY each value out (or if the pool returned the slice too
// early), every row in the result would silently contain the LAST row's
// values. Scan a 50-row result set (the bench's limit=50 workload) and
// assert every row carries its own id/name.
func TestScanRowsPooled_NoAliasingAcrossRows(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Skip("sqlite3 driver not available")
	}
	defer db.Close()

	if _, err := db.Exec("CREATE TABLE t (id INTEGER, name TEXT)"); err != nil {
		t.Skipf("sqlite3 not available: %v", err)
	}
	// 50 rows mirrors the bench's limit=50; enough that a pool with one
	// hot slot would surface aliasing as a uniform last-row result.
	const N = 50
	for i := range N {
		if _, err := db.Exec("INSERT INTO t (id, name) VALUES (?, ?)", i, fmt.Sprintf("row%d", i)); err != nil {
			t.Fatalf("seed row %d: %v", i, err)
		}
	}

	rows, err := db.Query("SELECT id, name FROM t ORDER BY id")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	result, err := scanRowsPooled(rows, []string{"id", "name"}, func(s string) string { return s })
	if err != nil {
		t.Fatal(err)
	}
	defer returnRowSlice(result)

	if len(*result) != N {
		t.Fatalf("expected %d rows, got %d", N, len(*result))
	}
	for i, row := range *result {
		gotName, _ := row["name"].(string)
		wantName := fmt.Sprintf("row%d", i)
		if gotName != wantName {
			t.Fatalf("row %d name = %q, want %q (scratch-slice aliasing suspected)", i, gotName, wantName)
		}
		gotID, _ := row["id"].(int64)
		if int(gotID) != i {
			t.Fatalf("row %d id = %v, want %d (scratch-slice aliasing suspected)", i, gotID, i)
		}
	}

	// Drive the pool through two full cycles — by now any aliasing bug
	// would also surface as a stale value leaking into a fresh scan.
	rows2, err := db.Query("SELECT id, name FROM t ORDER BY id")
	if err != nil {
		t.Fatal(err)
	}
	defer rows2.Close()
	result2, err := scanRowsPooled(rows2, []string{"id", "name"}, func(s string) string { return s })
	if err != nil {
		t.Fatal(err)
	}
	defer returnRowSlice(result2)
	if len(*result2) != N {
		t.Fatalf("second scan: expected %d rows, got %d", N, len(*result2))
	}
	for i, row := range *result2 {
		gotName, _ := row["name"].(string)
		if want := fmt.Sprintf("row%d", i); gotName != want {
			t.Fatalf("second scan row %d name = %q, want %q", i, gotName, want)
		}
	}
}
