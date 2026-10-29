package migrate_test

import (
	"context"
	"testing"

	coremig "github.com/DonaldMurillo/gofastr/core/migrate"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/internal/testdb"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
)

// A SQLite foreign key names its parent without a schema, and the
// generator matched that bare name against the parent's qualified Table:
// main.ap's child main.zc read as referencing nothing, so dropping both
// dropped main.ap first (it sorts first), and SQLite's implicit DELETE of
// a parent whose child still holds rows failed the migration.
func TestSQLiteQualifiedDropOrder(t *testing.T) {
	db := testdb.Open(t, migrate.DialectSQLite)
	d := migrate.DialectSQLite
	child := entity.Define("zc", entity.EntityConfig{
		Table:      "main.zc",
		Fields:     []schema.Field{{Name: "id", Type: schema.String, Required: true}, {Name: "ap_id", Type: schema.String}},
		PrimaryKey: []string{"id"},
		Relations:  []entity.Relation{entity.BelongsTo("ap", "parents", "ap_id")},
	}.WithTimestamps(false))
	up1, down1, snap, err := migrate.GeneratePlan(migrate.Plan{Registry: fkReg{fkParents("main.ap"), child}}, migrate.SchemaSnapshot{}, d)
	if err != nil {
		t.Fatal(err)
	}
	up2, down2, _, err := migrate.GeneratePlan(migrate.Plan{Registry: fkReg{}}, snap, d)
	if err != nil {
		t.Fatal(err)
	}
	migs := []coremig.Migration{{Version: 1, Name: "m1", Up: up1, Down: down1}, {Version: 2, Name: "m2", Up: up2, Down: down2}}
	apply := func(n int) error {
		m := coremig.New(db, coremig.WithDialect(d))
		for _, mg := range migs[:n] {
			if err := m.Register(mg); err != nil {
				t.Fatal(err)
			}
		}
		return m.Up(context.Background())
	}
	if err := apply(1); err != nil {
		t.Fatalf("create:\n%s\n: %v", up1, err)
	}
	fkExec(t, db, `INSERT INTO ap (id) VALUES ('p')`, `INSERT INTO zc (id, ap_id) VALUES ('c', 'p')`)
	if err := apply(2); err != nil {
		t.Fatalf("drop:\n%s\n: %v", up2, err)
	}
}
