package migrate

import (
	"slices"
	"strings"
	"testing"
)

// The snapshot's REFERENCES patterns captured one identifier, so a
// reference onto app.a read as one onto app: a qualified child lost its
// place ahead of its parent in the drop order, a CASCADE onto a qualified
// parent never reached the rebuild guard, and a key change on one never
// saw the keys onto it.
func TestQualifiedReferencesParse(t *testing.T) {
	ddl := map[string]string{
		"app.a": `CREATE TABLE app.a (id TEXT PRIMARY KEY)`,
		"app.b": `CREATE TABLE app.b (id TEXT PRIMARY KEY, a_id TEXT, FOREIGN KEY (a_id) REFERENCES app.a(id) ON DELETE CASCADE)`,
	}
	if got := sortDroppedTables([]string{"app.a", "app.b"}, ddl); strings.Join(got, ",") != "app.b,app.a" {
		t.Errorf("drop order = %v, want [app.b app.a]", got)
	}
	if got := snapshotMutatingReferrers(SchemaSnapshot{TableDDL: ddl}, "app.a"); !slices.Equal(got, []string{"app.b"}) {
		t.Errorf("mutating referrers of app.a = %v, want [app.b]", got)
	}
	if got := recordedReferencesOnto(ddl, "app.a"); len(got["app.b"]) != 1 || !slices.Equal(got["app.b"][0], []string{"id"}) {
		t.Errorf("recorded references onto app.a = %v, want app.b's (id)", got)
	}
	// The schema is part of the name: app.a is not other.a.
	if got := snapshotMutatingReferrers(SchemaSnapshot{TableDDL: ddl}, "a"); len(got) != 0 {
		t.Errorf("mutating referrers of a = %v, want none", got)
	}
}
