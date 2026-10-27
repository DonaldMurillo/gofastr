package migrate_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/internal/testdb"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
)

func viewPlan(views ...migrate.View) migrate.Plan {
	return migrate.Plan{Registry: fkReg{qtyEntity(schema.Field{Name: "n", Type: schema.Int})}, Views: views}
}

// readsView fails the test when a SELECT from name errors.
func readsView(t *testing.T, db *sql.DB, name string) {
	t.Helper()
	if _, err := db.Exec("SELECT * FROM " + name); err != nil {
		t.Fatalf("view %s: %v", name, err)
	}
}

// relkind reports the Postgres kind of name: v for a view, m for a
// materialized view.
func relkind(t *testing.T, db *sql.DB, name string) string {
	t.Helper()
	var k string
	if err := db.QueryRow(`SELECT relkind::text FROM pg_class WHERE relname = $1 AND relnamespace = current_schema()::regnamespace`, name).Scan(&k); err != nil {
		t.Fatalf("relkind %s: %v", name, err)
	}
	return k
}

// A changed materialized view is dropped and recreated, and Postgres
// refuses the drop while a declared view reads it. Boot drops the
// dependents first; the generated migration did not.
func TestPGChangedMatViewWithDependent(t *testing.T) {
	db := testdb.Open(t, migrate.DialectPostgres)
	dep := migrate.View{Name: "qv", Select: "SELECT id FROM qmv", DependsOn: []string{"qmv"}}
	migs := genChain(t, db, migrate.DialectPostgres,
		viewPlan(migrate.View{Name: "qmv", Select: "SELECT id, n FROM qty", DependsOn: []string{"qty"}, Materialized: true}, dep),
		viewPlan(migrate.View{Name: "qmv", Select: "SELECT id, n, n + 1 AS m FROM qty", DependsOn: []string{"qty"}, Materialized: true}, dep),
	)
	readsView(t, db, "qv")
	genDown(t, db, migrate.DialectPostgres, migs)
	readsView(t, db, "qv")
}

// Turning a plain view materialized ran DROP MATERIALIZED VIEW against
// the plain view, which Postgres refuses; the reverse ran CREATE OR
// REPLACE VIEW against the materialized one.
func TestPGViewKindChangeGenerated(t *testing.T) {
	db := testdb.Open(t, migrate.DialectPostgres)
	plain := migrate.View{Name: "qk", Select: "SELECT id FROM qty", DependsOn: []string{"qty"}}
	mat := plain
	mat.Materialized = true
	migs := genChain(t, db, migrate.DialectPostgres, viewPlan(plain), viewPlan(mat))
	if k := relkind(t, db, "qk"); k != "m" {
		t.Fatalf("after plain→materialized relkind = %s, want m", k)
	}
	genDown(t, db, migrate.DialectPostgres, migs)
	if k := relkind(t, db, "qk"); k != "v" {
		t.Fatalf("after rollback relkind = %s, want v", k)
	}

	db = testdb.Open(t, migrate.DialectPostgres)
	migs = genChain(t, db, migrate.DialectPostgres, viewPlan(mat), viewPlan(plain))
	if k := relkind(t, db, "qk"); k != "v" {
		t.Fatalf("after materialized→plain relkind = %s, want v", k)
	}
	genDown(t, db, migrate.DialectPostgres, migs)
	if k := relkind(t, db, "qk"); k != "m" {
		t.Fatalf("after rollback relkind = %s, want m", k)
	}
}

// Boot has no snapshot, so it met the same refusal on the live object.
func TestPGViewKindChangeAtBoot(t *testing.T) {
	db := testdb.Open(t, migrate.DialectPostgres)
	ctx := context.Background()
	plain := migrate.View{Name: "qb", Select: "SELECT id FROM qty", DependsOn: []string{"qty"}}
	mat := plain
	mat.Materialized = true
	dep := migrate.View{Name: "qbd", Select: "SELECT id FROM qb", DependsOn: []string{"qb"}}
	matDep := dep
	matDep.Materialized = true
	// The last boot changes both kinds: the dependent is dropped as the
	// plain view it still is, not as the materialized one it becomes.
	for i, p := range []migrate.Plan{viewPlan(plain, dep), viewPlan(mat, dep), viewPlan(plain, dep), viewPlan(mat, matDep)} {
		if err := migrate.AutoMigratePlanContext(ctx, db, p); err != nil {
			t.Fatalf("boot %d: %v", i+1, err)
		}
		want := "v"
		if i%2 == 1 {
			want = "m"
		}
		if k := relkind(t, db, "qb"); k != want {
			t.Fatalf("boot %d relkind = %s, want %s", i+1, k, want)
		}
		readsView(t, db, "qbd")
	}
}
