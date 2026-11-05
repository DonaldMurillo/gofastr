package migrate_test

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/internal/testdb"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
)

func removalViewPlans() (migrate.Plan, migrate.Plan) {
	old := migrate.Plan{Views: []migrate.View{
		{Name: "source_view", Select: "SELECT 17 AS n"},
		{Name: "consumer_view", Select: "SELECT n FROM source_view", DependsOn: []string{"source_view"}},
		{Name: "outer_view", Select: "SELECT n FROM consumer_view", DependsOn: []string{"consumer_view"}},
	}}
	next := migrate.Plan{Views: []migrate.View{
		{Name: "consumer_view", Select: "SELECT 23 AS n"},
		{Name: "outer_view", Select: "SELECT n FROM consumer_view", DependsOn: []string{"consumer_view"}},
	}}
	return old, next
}

func TestViewRemovalDropsDependentsFirst(t *testing.T) {
	old, next := removalViewPlans()
	for _, dialect := range []migrate.Dialect{migrate.DialectPostgres, migrate.DialectSQLite} {
		_, _, snapshot, err := migrate.GeneratePlan(old, migrate.SchemaSnapshot{}, dialect)
		if err != nil {
			t.Fatal(err)
		}
		up, down, _, err := migrate.GeneratePlan(next, snapshot, dialect)
		if err != nil {
			t.Fatal(err)
		}
		for _, dependent := range []string{"consumer_view", "outer_view"} {
			dropDependent := strings.Index(up, "DROP VIEW IF EXISTS "+dependent)
			dropSource := strings.Index(up, "DROP VIEW IF EXISTS source_view")
			if dropDependent < 0 || dropSource < 0 || dropDependent > dropSource {
				t.Errorf("%s drops source before %s:\n%s", dialect, dependent, up)
			}
			restoreDependent := strings.Index(down, "VIEW "+dependent+" AS")
			restoreSource := strings.Index(down, "VIEW source_view AS")
			if restoreDependent < 0 || restoreSource < 0 || restoreDependent < restoreSource {
				t.Errorf("%s restores %s before source:\n%s", dialect, dependent, down)
			}
		}
	}
}

func TestPGViewRemovalRollback(t *testing.T) {
	dialect := migrate.DialectPostgres
	db := testdb.Open(t, dialect)
	old, next := removalViewPlans()
	migrations := genChain(t, db, dialect, old, next)
	genDown(t, db, dialect, migrations)
	readsView(t, db, "outer_view")
}
