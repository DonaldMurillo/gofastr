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
	for _, d := range []Dialect{DialectSQLite, DialectPostgres} {
		if got := sortDroppedTables(d, []string{"app.a", "app.b"}, ddl); strings.Join(got, ",") != "app.b,app.a" {
			t.Errorf("%s: drop order = %v, want [app.b app.a]", d, got)
		}
		if got := snapshotMutatingReferrers(d, SchemaSnapshot{TableDDL: ddl}, "app.a"); !slices.Equal(got, []string{"app.b"}) {
			t.Errorf("%s: mutating referrers of app.a = %v, want [app.b]", d, got)
		}
		if got := recordedReferencesOnto(d, ddl, "app.a"); len(got["app.b"]) != 1 || !slices.Equal(got["app.b"][0], []string{"id"}) {
			t.Errorf("%s: recorded references onto app.a = %v, want app.b's (id)", d, got)
		}
		// The schema is part of the name: app.a is not other.a.
		if got := snapshotMutatingReferrers(d, SchemaSnapshot{TableDDL: ddl}, "a"); len(got) != 0 {
			t.Errorf("%s: mutating referrers of a = %v, want none", d, got)
		}
	}
}

// A SQLite REFERENCES clause names its parent bare, and the parent is in
// the child's schema: aux.b's "REFERENCES a" is aux.a, and main.d's is
// main.c, the same table as c. The readers compared the bare name with
// the qualified Table and found no referrer at all.
func TestSQLiteBareReferencesResolve(t *testing.T) {
	ddl := map[string]string{
		"aux.a":  `CREATE TABLE aux.a (id TEXT PRIMARY KEY)`,
		"aux.b":  `CREATE TABLE aux.b (id TEXT PRIMARY KEY, a_id TEXT, FOREIGN KEY (a_id) REFERENCES a(id) ON DELETE CASCADE)`,
		"a":      `CREATE TABLE a (id TEXT PRIMARY KEY)`,
		"main.c": `CREATE TABLE main.c (id TEXT PRIMARY KEY)`,
		"main.d": `CREATE TABLE main.d (id TEXT PRIMARY KEY, c_id TEXT, FOREIGN KEY (c_id) REFERENCES c(id) ON DELETE SET NULL)`,
	}
	snap := SchemaSnapshot{TableDDL: ddl}
	if got := sortDroppedTables(DialectSQLite, []string{"aux.a", "aux.b"}, ddl); strings.Join(got, ",") != "aux.b,aux.a" {
		t.Errorf("drop order = %v, want [aux.b aux.a]", got)
	}
	if got := snapshotMutatingReferrers(DialectSQLite, snap, "aux.a"); !slices.Equal(got, []string{"aux.b"}) {
		t.Errorf("mutating referrers of aux.a = %v, want [aux.b]", got)
	}
	if got := recordedReferencesOnto(DialectSQLite, ddl, "aux.a"); len(got["aux.b"]) != 1 || !slices.Equal(got["aux.b"][0], []string{"id"}) {
		t.Errorf("recorded references onto aux.a = %v, want aux.b's (id)", got)
	}
	// main.a is another table: aux.b's bare reference stays in aux.
	if got := snapshotMutatingReferrers(DialectSQLite, snap, "a"); len(got) != 0 {
		t.Errorf("mutating referrers of a = %v, want none", got)
	}
	// main.c and c are one table, whichever spelling asks.
	for _, table := range []string{"main.c", "c"} {
		if got := snapshotMutatingReferrers(DialectSQLite, snap, table); !slices.Equal(got, []string{"main.d"}) {
			t.Errorf("mutating referrers of %s = %v, want [main.d]", table, got)
		}
	}
}

// A bare Postgres reference resolves through search_path, which the
// generator takes as public, and never into the child's schema.
func TestPostgresBareReferencesResolve(t *testing.T) {
	ddl := map[string]string{
		"s.kids": `CREATE TABLE s.kids (id TEXT PRIMARY KEY, parent_id TEXT, FOREIGN KEY (parent_id) REFERENCES parents (id) ON DELETE CASCADE)`,
	}
	snap := SchemaSnapshot{TableDDL: ddl}
	for _, table := range []string{"parents", "public.parents"} {
		if got := snapshotMutatingReferrers(DialectPostgres, snap, table); !slices.Equal(got, []string{"s.kids"}) {
			t.Errorf("mutating referrers of %s = %v, want [s.kids]", table, got)
		}
	}
	if got := snapshotMutatingReferrers(DialectPostgres, snap, "s.parents"); len(got) != 0 {
		t.Errorf("mutating referrers of s.parents = %v, want none", got)
	}
}
