package migrate_test

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/internal/testdb"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
)

func qtyEntity(f schema.Field) *entity.Entity {
	return entity.Define("qty", entity.EntityConfig{
		Table:      "qty",
		Fields:     []schema.Field{{Name: "id", Type: schema.String, Required: true}, f},
		PrimaryKey: []string{"id"},
	}.WithTimestamps(false))
}

// A Postgres text→integer retype converts rows with USING, but a text
// default on the column cannot be cast automatically, so the ALTER
// failed. The default is dropped for the retype and the declared one set
// after it; the rollback converts back.
func TestPGRetypeTextToIntWithDefault(t *testing.T) {
	db := testdb.Open(t, migrate.DialectPostgres)
	d := migrate.DialectPostgres
	migs := genChain(t, db, d,
		migrate.Plan{Registry: fkReg{qtyEntity(schema.Field{Name: "n", Type: schema.String, Default: "0"})}},
		migrate.Plan{Registry: fkReg{qtyEntity(schema.Field{Name: "n", Type: schema.Int, Default: 5})}},
	)
	fkExec(t, db, `INSERT INTO qty (id) VALUES ('r')`)
	var n int
	if err := db.QueryRow(`SELECT n FROM qty WHERE id = 'r'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 5 {
		t.Fatalf("default after retype = %d, want the declared 5", n)
	}
	genDown(t, db, d, migs)
	var typ string
	if err := db.QueryRow(`SELECT data_type FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = 'qty' AND column_name = 'n'`).Scan(&typ); err != nil {
		t.Fatal(err)
	}
	if typ != "text" {
		t.Fatalf("type after rollback = %s, want text", typ)
	}
}
