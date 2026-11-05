package migrate_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/internal/testdb"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
)

func TestRetypeDownRestoresOldDefault(t *testing.T) {
	for _, value := range []int{5, 37, -12} {
		for _, renamed := range []bool{false, true} {
			old := migrate.Plan{Registry: fkReg{qtyEntity(schema.Field{Name: "n", Type: schema.Int, Default: value})}}
			_, _, snapshot, err := migrate.GeneratePlan(old, migrate.SchemaSnapshot{}, migrate.DialectPostgres)
			if err != nil {
				t.Fatal(err)
			}
			name := "n"
			if renamed {
				name = "count"
			}
			nextEntity := qtyEntity(schema.Field{Name: name, Type: schema.String})
			if renamed {
				nextEntity.Config.Renames = map[string]string{"n": name}
			}
			_, down, _, err := migrate.GeneratePlan(migrate.Plan{Registry: fkReg{nextEntity}}, snapshot, migrate.DialectPostgres)
			if err != nil {
				t.Fatal(err)
			}
			want := fmt.Sprintf(`USING %s::BIGINT, ALTER COLUMN %s SET DEFAULT %d`, name, name, value)
			if !strings.Contains(down, want) {
				t.Errorf("rollback misses %q:\n%s", want, down)
			}
		}
	}
}

func TestPGRetypeDownKeepsDefault(t *testing.T) {
	dialect := migrate.DialectPostgres
	db := testdb.Open(t, dialect)
	migrations := genChain(t, db, dialect,
		migrate.Plan{Registry: fkReg{qtyEntity(schema.Field{Name: "n", Type: schema.Int, Default: 37})}},
		migrate.Plan{Registry: fkReg{qtyEntity(schema.Field{Name: "n", Type: schema.String})}},
	)
	genDown(t, db, dialect, migrations)
	fkExec(t, db, "INSERT INTO qty (id) VALUES ('restored')")
	var value int
	if err := db.QueryRow("SELECT n FROM qty WHERE id = 'restored'").Scan(&value); err != nil || value != 37 {
		t.Fatalf("rollback default = %d, err=%v", value, err)
	}
}
