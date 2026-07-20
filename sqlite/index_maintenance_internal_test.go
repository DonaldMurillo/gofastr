package sqlite

import (
	"strings"
	"testing"
)

// scanIndexRowids opens the B-tree backing the named index and returns
// the (key, rowid) pairs it currently holds, in B-tree order. Internal
// helper for tests that need to prove the index B-tree's actual contents
// — not just that a fetch-miss is tolerated.
func scanIndexRecords(t *testing.T, e *Engine, indexName string) [][2]Value {
	t.Helper()
	idx, ok := e.schema.GetIndex(indexName)
	if !ok {
		t.Fatalf("no such index: %s", indexName)
	}
	if idx.RootPage == 0 {
		return nil
	}
	cur, err := e.btree.Scan(idx.RootPage)
	if err != nil {
		t.Fatalf("scan index %s: %v", indexName, err)
	}
	defer cur.Close()
	var out [][2]Value
	for cur.Next() {
		_, rec, err := cur.Get()
		if err != nil {
			t.Fatalf("get from index %s: %v", indexName, err)
		}
		cols := rec.Columns
		if len(cols) < 2 {
			continue
		}
		out = append(out, [2]Value{cols[0], cols[len(cols)-1]})
	}
	return out
}

func indexContains(t *testing.T, e *Engine, indexName string, key string, rowid int64) bool {
	t.Helper()
	for _, pair := range scanIndexRecords(t, e, indexName) {
		if asString(pair[0]) == key {
			if rv, ok := pair[1].AsInt64(); ok && rv == rowid {
				return true
			}
		}
	}
	return false
}

func asString(v Value) string {
	switch v.Type {
	case DataTypeText:
		return v.TextVal
	case DataTypeBlob:
		return string(v.BlobVal)
	}
	return v.String()
}

// TestInternalDeleteMaintainsIndexBTree proves the DELETE WHERE path
// evicts the row's index entry from the index B-tree itself — not just
// that a later lookup happens to miss. A recycled rowid at the old key
// must not resurface under the deleted value.
func TestInternalDeleteMaintainsIndexBTree(t *testing.T) {
	e := newTestEngine(t)
	exec(t, e, "CREATE TABLE t (id INTEGER PRIMARY KEY, v TEXT)")
	exec(t, e, "CREATE INDEX t_v ON t(v)")
	exec(t, e, "INSERT INTO t VALUES (1, 'old')")

	if !indexContains(t, e, "t_v", "old", 1) {
		t.Fatal("index missing entry for (old, rowid=1) after insert")
	}

	exec(t, e, "DELETE FROM t WHERE id = 1")

	if indexContains(t, e, "t_v", "old", 1) {
		t.Fatal("DELETE WHERE left a stale index entry for the deleted rowid")
	}
	if got := len(scanIndexRecords(t, e, "t_v")); got != 0 {
		t.Fatalf("index B-tree has %d entries after DELETE, want 0", got)
	}
}

// TestInternalUnqualifiedDeleteMaintainsIndexBTree proves the no-WHERE
// DELETE path also clears the index B-tree — previously it rebuilt only
// the table B-tree and left every index entry pointing at dead rowids.
func TestInternalUnqualifiedDeleteMaintainsIndexBTree(t *testing.T) {
	e := newTestEngine(t)
	exec(t, e, "CREATE TABLE t (id INTEGER PRIMARY KEY, v TEXT)")
	exec(t, e, "CREATE INDEX t_v ON t(v)")
	exec(t, e, "INSERT INTO t VALUES (1, 'a'), (2, 'b'), (3, 'c')")

	exec(t, e, "DELETE FROM t")

	if got := len(scanIndexRecords(t, e, "t_v")); got != 0 {
		t.Fatalf("index B-tree has %d entries after unqualified DELETE, want 0", got)
	}
}

// TestInternalUpsertMaintainsIndexBTree proves ON CONFLICT DO UPDATE
// replaces (not duplicates) the index entry for the upserted row —
// the prior key must be gone and the new key must point at the same rowid.
func TestInternalUpsertMaintainsIndexBTree(t *testing.T) {
	e := newTestEngine(t)
	exec(t, e, "CREATE TABLE t (id INTEGER PRIMARY KEY, v TEXT)")
	exec(t, e, "CREATE INDEX t_v ON t(v)")
	exec(t, e, "INSERT INTO t VALUES (1, 'old')")

	exec(t, e, "INSERT INTO t VALUES (1, 'new') ON CONFLICT(id) DO UPDATE SET v=excluded.v")

	if indexContains(t, e, "t_v", "old", 1) {
		t.Fatal("ON CONFLICT DO UPDATE left stale index entry for pre-upsert value 'old'")
	}
	if !indexContains(t, e, "t_v", "new", 1) {
		t.Fatal("ON CONFLICT DO UPDATE failed to add index entry for post-upsert value 'new'")
	}
	// Exactly one entry should exist after the upsert.
	if got := len(scanIndexRecords(t, e, "t_v")); got != 1 {
		t.Fatalf("index B-tree has %d entries after upsert, want 1", got)
	}
}

// TestInternalInsertOrReplaceMaintainsIndexBTree proves INSERT OR REPLACE
// removes the index entry for the replaced row before inserting the new
// one — both the table row and its index pointer must reflect the new
// value only.
func TestInternalInsertOrReplaceMaintainsIndexBTree(t *testing.T) {
	e := newTestEngine(t)
	exec(t, e, "CREATE TABLE t (id INTEGER PRIMARY KEY, v TEXT)")
	exec(t, e, "CREATE INDEX t_v ON t(v)")
	exec(t, e, "INSERT INTO t VALUES (1, 'old')")

	exec(t, e, "INSERT OR REPLACE INTO t VALUES (1, 'new')")

	if indexContains(t, e, "t_v", "old", 1) {
		t.Fatal("INSERT OR REPLACE left stale index entry for replaced value 'old'")
	}
	if !indexContains(t, e, "t_v", "new", 1) {
		t.Fatal("INSERT OR REPLACE failed to add index entry for new value 'new'")
	}
	if got := len(scanIndexRecords(t, e, "t_v")); got != 1 {
		t.Fatalf("index B-tree has %d entries after INSERT OR REPLACE, want 1", got)
	}
}

// TestInternalPartialIndexUpsertMaintainsPredicate proves ON CONFLICT
// DO UPDATE removes a row's partial-index entry when the updated row no
// longer matches the partial-index WHERE predicate (it "moved out").
func TestInternalPartialIndexUpsertMaintainsPredicate(t *testing.T) {
	e := newTestEngine(t)
	exec(t, e, "CREATE TABLE t (id INTEGER PRIMARY KEY, v TEXT, active INTEGER)")
	exec(t, e, "CREATE INDEX t_active_v ON t(v) WHERE active = 1")
	exec(t, e, "INSERT INTO t VALUES (1, 'keep', 1)")

	if !indexContains(t, e, "t_active_v", "keep", 1) {
		t.Fatal("partial index missing entry for matching row")
	}

	// Move the row out of the predicate: active 1 -> 0. The partial
	// index must no longer contain an entry for this rowid.
	exec(t, e, "INSERT INTO t VALUES (1, 'keep', 0) ON CONFLICT(id) DO UPDATE SET active=excluded.active")

	if indexContains(t, e, "t_active_v", "keep", 1) {
		t.Fatal("partial index retained entry after upsert moved row out of predicate")
	}
	if got := len(scanIndexRecords(t, e, "t_active_v")); got != 0 {
		t.Fatalf("partial index B-tree has %d entries after move-out, want 0", got)
	}
}

// Ensure the helpers don't get pruned if the test set above grows; a
// tiny smoke check that the strings import stays used.
var _ = strings.EqualFold
