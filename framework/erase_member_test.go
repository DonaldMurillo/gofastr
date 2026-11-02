package framework

import (
	"context"
	"database/sql"
	"encoding/json"
	"slices"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/datexport"
	"github.com/DonaldMurillo/gofastr/core/featureflag"
	"github.com/DonaldMurillo/gofastr/core/handler"
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

// An erasure that carries a tenant (WithEraseTenant, or the tenant the
// request's context resolved) removes only that tenant's entry for the
// user and the bare forms. User ids are per tenant, so erasing acme's u1
// used to take "beta:u1", another tenant's user, off the list too. A
// tenant id holding the separator or '%' is matched in featureflag's
// escaped spelling. WithEraseTenant("") overrides a context tenant and
// erases every tenant's entry, the behaviour with no tenant at all.
func TestEraseRemoveMemberTenantScoped(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		ctx := context.Background()
		odd := featureflag.UserEntry("a:b%c", "u1")
		seed := `["acme:u1","beta:u1","u1",":u1","acme:u2","` + odd + `","x:acme:u1"]`
		if _, err := db.ExecContext(ctx, `CREATE TABLE tcohorts (key TEXT PRIMARY KEY, users TEXT NOT NULL)`); err != nil {
			t.Fatal(err)
		}
		datexport.RegisterEraser(datexport.DataEraser{
			Name: "tcohorts", Source: "test", Table: "tcohorts", Column: "users",
			Mode: datexport.EraseRemoveMember, MemberSeparator: ":",
		})
		t.Cleanup(func() { datexport.UnregisterEraser("tcohorts") })
		app := NewApp(WithDB(db))
		for _, tc := range []struct {
			name string
			ctx  context.Context
			opts []EraseOption
			want []string
		}{
			{"option", ctx, []EraseOption{WithEraseTenant("acme")},
				[]string{"beta:u1", "acme:u2", odd, "x:acme:u1"}},
			{"request tenant", handler.SetTenantID(ctx, "acme"), nil,
				[]string{"beta:u1", "acme:u2", odd, "x:acme:u1"}},
			{"escaped tenant", ctx, []EraseOption{WithEraseTenant("a:b%c")},
				[]string{"acme:u1", "beta:u1", "acme:u2", "x:acme:u1"}},
			{"explicit no tenant wins over the request's", handler.SetTenantID(ctx, "acme"), []EraseOption{WithEraseTenant("")},
				[]string{"acme:u2", "x:acme:u1"}},
			{"no tenant: every tenant", ctx, nil,
				[]string{"acme:u2", "x:acme:u1"}},
		} {
			resetMembers(t, db, "tcohorts", seed)
			if _, err := app.EraseUserData(tc.ctx, "u1", tc.opts...); err != nil {
				t.Fatalf("%s: EraseUserData: %v", tc.name, err)
			}
			if got := readMembers(t, db, "tcohorts"); !slices.Equal(got, tc.want) {
				t.Errorf("%s: users = %v, want %v", tc.name, got, tc.want)
			}
		}
	})
}

// A list written before tenant-qualified entries held bare user ids, so a
// user id with a ':' ("google:42") sits there whole. Erasing that user
// removes the entry, whatever tenant the erasure carries: it used to
// compare only the part after the first ':' ("42") and left the erased
// user's id behind. Erasing user 42 of tenant acme leaves it: under
// either reading it is not acme's user 42.
func TestEraseRemoveMemberLegacyColonEntry(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		ctx := context.Background()
		if _, err := db.ExecContext(ctx, `CREATE TABLE lcohorts (key TEXT PRIMARY KEY, users TEXT NOT NULL)`); err != nil {
			t.Fatal(err)
		}
		datexport.RegisterEraser(datexport.DataEraser{
			Name: "lcohorts", Source: "test", Table: "lcohorts", Column: "users",
			Mode: datexport.EraseRemoveMember, MemberSeparator: ":",
		})
		t.Cleanup(func() { datexport.UnregisterEraser("lcohorts") })
		app := NewApp(WithDB(db))
		for _, tc := range []struct {
			name, user string
			opts       []EraseOption
			want       []string
		}{
			{"legacy user, no tenant", "google:42", nil, []string{"42"}},
			{"legacy user, tenant acme", "google:42", []EraseOption{WithEraseTenant("acme")}, []string{"42"}},
			{"acme's user 42", "42", []EraseOption{WithEraseTenant("acme")}, []string{"google:42", "acme:google:42"}},
		} {
			resetMembers(t, db, "lcohorts", `["google:42","42","acme:google:42"]`)
			if _, err := app.EraseUserData(ctx, tc.user, tc.opts...); err != nil {
				t.Fatalf("%s: EraseUserData: %v", tc.name, err)
			}
			if got := readMembers(t, db, "lcohorts"); !slices.Equal(got, tc.want) {
				t.Errorf("%s: users = %v, want %v", tc.name, got, tc.want)
			}
		}
	})
}

// resetMembers replaces table's rows with one row 'a' holding members.
func resetMembers(t *testing.T, db *sql.DB, table, members string) {
	t.Helper()
	if _, err := db.Exec(`DELETE FROM ` + table); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO `+table+` VALUES ('a', $1)`, members); err != nil {
		t.Fatal(err)
	}
}

// readMembers decodes row 'a”s users array.
func readMembers(t *testing.T, db *sql.DB, table string) []string {
	t.Helper()
	var raw string
	if err := db.QueryRow(`SELECT users FROM ` + table + ` WHERE key = 'a'`).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var got []string
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("users holds %q: %v", raw, err)
	}
	return got
}
