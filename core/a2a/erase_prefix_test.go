package a2a

import (
	"database/sql"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/datexport"
)

// Two SQL stores behind two prefixes each keep their tables in the
// export and erasure registries. Registration replaces by Name, and both
// stores registered under the constant "a2a_tasks", so the first store's
// tasks were never erased (2026-10-28 review of the erasure fix).
func TestPrefixedStoresBothErase(t *testing.T) {
	datexport.Reset(t)
	t.Cleanup(func() { datexport.Reset(t) })
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, prefix := range []string{"t1_", "t2_"} {
		if _, err := NewSQLStore(db, WithTablePrefix(prefix)); err != nil {
			t.Fatal(err)
		}
	}
	erased := map[string]bool{}
	for _, e := range datexport.AllErasers() {
		erased[e.Table] = true
	}
	exported := map[string]bool{}
	for _, e := range datexport.All() {
		exported[e.Table] = true
	}
	for _, table := range []string{"t1_a2a_tasks", "t2_a2a_tasks", "t1_a2a_push_configs", "t2_a2a_push_configs"} {
		if !erased[table] || !exported[table] {
			t.Errorf("%s: erased %v, exported %v; want both", table, erased[table], exported[table])
		}
	}
}
