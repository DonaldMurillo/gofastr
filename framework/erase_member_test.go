package framework

import (
	"context"
	"database/sql"
	"encoding/json"
	"slices"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/datexport"
)

// EraseRemoveMember takes the erased user out of every JSON array that
// lists them and leaves the rows, the other members and the rows that
// never listed them alone. A second run changes nothing.
func TestEraseRemoveMemberDropsTheUser(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		ctx := context.Background()
		for _, q := range []string{
			`CREATE TABLE cohorts (key TEXT PRIMARY KEY, kind TEXT NOT NULL, users TEXT NOT NULL)`,
			`INSERT INTO cohorts VALUES ('a','flag','["u1","u2"]'), ('b','flag','["u2"]'), ('c','flag','["u1"]'),
				('d','flag','[]'), ('e','other','["u1"]'), ('f','flag','["u10","u1x"]')`,
		} {
			if _, err := db.ExecContext(ctx, q); err != nil {
				t.Fatal(err)
			}
		}
		datexport.RegisterEraser(datexport.DataEraser{
			Name: "cohorts", Source: "test", Table: "cohorts", Column: "users",
			Mode: datexport.EraseRemoveMember, Where: []datexport.Equals{{Column: "kind", Value: "flag"}},
		})
		t.Cleanup(func() { datexport.UnregisterEraser("cohorts") })
		app := NewApp(WithDB(db))

		affected := func() int {
			t.Helper()
			rep, err := app.EraseUserData(ctx, "u1")
			if err != nil {
				t.Fatalf("EraseUserData: %v", err)
			}
			for _, r := range rep.Batteries {
				if r.Name == "cohorts" {
					if r.Mode != "remove" {
						t.Errorf("mode = %q, want remove", r.Mode)
					}
					return r.RowsAffected
				}
			}
			t.Fatal("cohorts missing from the report")
			return 0
		}
		if n := affected(); n != 2 {
			t.Errorf("first run changed %d row(s), want 2", n)
		}
		want := map[string][]string{
			"a": {"u2"}, "b": {"u2"}, "c": {}, "d": {}, "e": {"u1"}, "f": {"u10", "u1x"},
		}
		for key, members := range want {
			var raw string
			if err := db.QueryRowContext(ctx, `SELECT users FROM cohorts WHERE key = $1`, key).Scan(&raw); err != nil {
				t.Fatal(err)
			}
			var got []string
			if err := json.Unmarshal([]byte(raw), &got); err != nil {
				t.Fatalf("%s holds %q, not a JSON array: %v", key, raw, err)
			}
			if !slices.Equal(got, members) {
				t.Errorf("%s = %v, want %v", key, got, members)
			}
		}
		if n := affected(); n != 0 {
			t.Errorf("second run changed %d row(s), want 0", n)
		}
	})
}

// With a MemberSeparator, a member is matched by the part after its first
// separator: a qualified "acme:u1" goes with the bare "u1", while a member
// whose qualifier merely spells the value ("u1:other") or whose value only
// ends in it ("x:acme:u1") stays. The Where conditions still narrow the
// rows, their placeholders following the separator's.
func TestEraseRemoveMemberQualified(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		ctx := context.Background()
		for _, q := range []string{
			`CREATE TABLE qcohorts (key TEXT PRIMARY KEY, kind TEXT NOT NULL, users TEXT NOT NULL)`,
			`INSERT INTO qcohorts VALUES
				('a','flag','["acme:u1","u1",":u1","acme:u2","u1:other","x:acme:u1","b%3Ac:u1"]'),
				('b','flag','["acme:u10"]'), ('c','other','["acme:u1"]')`,
		} {
			if _, err := db.ExecContext(ctx, q); err != nil {
				t.Fatal(err)
			}
		}
		datexport.RegisterEraser(datexport.DataEraser{
			Name: "qcohorts", Source: "test", Table: "qcohorts", Column: "users",
			Mode: datexport.EraseRemoveMember, MemberSeparator: ":",
			Where: []datexport.Equals{{Column: "kind", Value: "flag"}},
		})
		t.Cleanup(func() { datexport.UnregisterEraser("qcohorts") })
		app := NewApp(WithDB(db))
		if _, err := app.EraseUserData(ctx, "u1"); err != nil {
			t.Fatalf("EraseUserData: %v", err)
		}
		want := map[string][]string{
			"a": {"acme:u2", "u1:other", "x:acme:u1"},
			"b": {"acme:u10"},
			"c": {"acme:u1"},
		}
		for key, members := range want {
			var raw string
			if err := db.QueryRowContext(ctx, `SELECT users FROM qcohorts WHERE key = $1`, key).Scan(&raw); err != nil {
				t.Fatal(err)
			}
			var got []string
			if err := json.Unmarshal([]byte(raw), &got); err != nil {
				t.Fatalf("%s holds %q, not a JSON array: %v", key, raw, err)
			}
			if !slices.Equal(got, members) {
				t.Errorf("%s = %v, want %v", key, got, members)
			}
		}
	})
}
