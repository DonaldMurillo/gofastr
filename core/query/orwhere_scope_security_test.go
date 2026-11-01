package query

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// A scope appended after a caller's OrWhere must bind to the whole
// predicate. The WHERE connectors used to be written flat, so
// Where(a).OrWhere(b).Where(tenant) rendered
//
//	(status = $1) OR (status = $2) AND (tenant_id = $3)
//
// which SQL reads as status=draft OR (status=pub AND tenant=A): every
// tenant's drafts came back. framework.ApplyTenantFilter,
// softdelete.ApplySoftDeleteFilter and CrudHandler's
// ApplyTenantScope/ApplyOwnerScope/ApplyReadScope all take a host-built
// *QueryBuilder and append exactly this Where.
func TestScopeAfterOrWhereBindsToTheWholePredicate(t *testing.T) {
	db := postsDB(t)
	qb := Select("id").From("posts").
		Where("status = $1", "draft").
		OrWhere("status = $1", "pub")
	qb.Where("tenant_id = $1", "A") // what framework.ApplyTenantFilter(qb, "A") appends
	q, args := qb.Build()
	if got, want := queryIDs(t, db, q, args), []string{"a1"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("tenant-A scope after an OrWhere returned %v, want %v (tenant B leaked)\n  sql: %s", got, want, q)
	}

	// Cursor appends its keyset predicate the same way.
	q, args = Select("id").From("posts").
		Where("status = $1", "draft").
		OrWhere("status = $1", "pub").
		Cursor("id", "b1", "forward").
		Build()
	if got, want := queryIDs(t, db, q, args), []string{"b2"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("cursor after an OrWhere returned %v, want %v\n  sql: %s", got, want, q)
	}
}

// The spelling of the fold. A chain with no OR before an AND writes no
// extra parens: framework scopes and many tests match that SQL byte for
// byte.
func TestWhereConnectorsFoldLeftToRight(t *testing.T) {
	build := func(conns ...string) string {
		qb := Select("*").From("t")
		for i, c := range conns {
			cond := fmt.Sprintf("c%d = $1", i)
			if c == "OR" {
				qb.OrWhere(cond, i)
			} else {
				qb.Where(cond, i)
			}
		}
		q, _ := qb.Build()
		return strings.TrimPrefix(q, "SELECT * FROM t WHERE ")
	}
	for _, tc := range []struct {
		conns []string
		want  string
	}{
		{[]string{"AND", "AND", "AND"}, "(c0 = $1) AND (c1 = $2) AND (c2 = $3)"},
		{[]string{"AND", "AND", "OR"}, "(c0 = $1) AND (c1 = $2) OR (c2 = $3)"},
		{[]string{"AND", "OR", "AND"}, "((c0 = $1) OR (c1 = $2)) AND (c2 = $3)"},
		{[]string{"AND", "OR", "OR", "AND", "AND"}, "((c0 = $1) OR (c1 = $2) OR (c2 = $3)) AND (c3 = $4) AND (c4 = $5)"},
		{[]string{"AND", "OR", "AND", "OR", "AND"}, "(((c0 = $1) OR (c1 = $2)) AND (c2 = $3) OR (c3 = $4)) AND (c4 = $5)"},
		// The first clause's connector is never written.
		{[]string{"OR", "AND"}, "(c0 = $1) AND (c1 = $2)"},
	} {
		if got := build(tc.conns...); got != tc.want {
			t.Errorf("%v\n  got  %s\n  want %s", tc.conns, got, tc.want)
		}
	}
}

// Every connector chain of five clauses, run on SQLite against every
// combination of five boolean columns, returns exactly the rows a left
// fold of the chain selects.
func TestWhereChainsMatchALeftFoldOnSQLite(t *testing.T) {
	db := postsDB(t)
	if _, err := db.Exec(`CREATE TABLE bits (id TEXT, b0 INT, b1 INT, b2 INT, b3 INT, b4 INT)`); err != nil {
		t.Fatal(err)
	}
	const n = 5
	for row := range 1 << n {
		vals := []any{fmt.Sprint(row)}
		for k := range n {
			vals = append(vals, row>>k&1)
		}
		if _, err := db.Exec(`INSERT INTO bits VALUES (?, ?, ?, ?, ?, ?)`, vals...); err != nil {
			t.Fatal(err)
		}
	}
	for mask := range 1 << (n - 1) {
		qb := Select("id").From("bits").Where("b0 = $1", 1)
		conns := []string{}
		for k := 1; k < n; k++ {
			cond := fmt.Sprintf("b%d = $1", k)
			if mask>>(k-1)&1 == 1 {
				qb.OrWhere(cond, 1)
				conns = append(conns, "OR")
			} else {
				qb.Where(cond, 1)
				conns = append(conns, "AND")
			}
		}
		var want []string
		for row := range 1 << n {
			acc := row&1 == 1
			for k := 1; k < n; k++ {
				bit := row>>k&1 == 1
				if conns[k-1] == "OR" {
					acc = acc || bit
				} else {
					acc = acc && bit
				}
			}
			if acc {
				want = append(want, fmt.Sprint(row))
			}
		}
		slices.Sort(want)
		q, args := qb.Build()
		if got := queryIDs(t, db, q, args); !reflect.DeepEqual(got, want) {
			t.Errorf("connectors %v\n  sql:  %s\n  got  %v\n  want %v", conns, q, got, want)
		}
	}
}
