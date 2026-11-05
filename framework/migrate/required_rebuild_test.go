package migrate_test

import (
	"context"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/internal/testdb"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
)

func TestRebuildDefersNewRequiredFields(t *testing.T) {
	for _, change := range []string{"retype", "unique", "default", "key"} {
		t.Run(change, func(t *testing.T) {
			dialect := migrate.DialectSQLite
			db := testdb.Open(t, dialect)
			field := schema.Field{Name: "n", Type: schema.Int, Required: true, Unique: change == "unique"}
			old := qtyEntity(field)
			if err := migrate.AutoMigrate(db, fkReg{old}); err != nil {
				t.Fatal(err)
			}
			fkExec(t, db, "INSERT INTO qty (id, n) VALUES ('kept', 31)")
			field.Unique = false
			if change == "retype" {
				field.Type = schema.String
			}
			if change == "default" {
				field.Default = 29
			}
			next := qtyEntity(field)
			if change == "key" {
				next.Config.PrimaryKey = []string{"id", "n"}
			}
			next.Config.Fields = append(next.Config.Fields,
				schema.Field{Name: "required_code", Type: schema.String, Required: true},
				schema.Field{Name: "required_count", Type: schema.Int, Required: true},
				schema.Field{Name: "fallback", Type: schema.Int, Required: true, Default: 17})
			_, _, snapshot, err := migrate.GeneratePlan(migrate.Plan{Registry: fkReg{old}}, migrate.SchemaSnapshot{}, dialect)
			if err != nil {
				t.Fatal(err)
			}
			up, _, _, err := migrate.GeneratePlan(migrate.Plan{Registry: fkReg{next}}, snapshot, dialect)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := db.ExecContext(context.Background(), up); err != nil {
				t.Fatalf("combined required add and %s rebuild: %v", change, err)
			}
			var count int
			if err := db.QueryRow("SELECT count(*) FROM qty WHERE id = 'kept' AND required_code IS NULL AND required_count IS NULL AND fallback = 17").Scan(&count); err != nil || count != 1 {
				t.Fatalf("rows retained = %d, err=%v", count, err)
			}
			for _, declaredField := range next.Config.Fields {
				if !declaredField.Required {
					t.Fatalf("generation mutated Required on %s", declaredField.Name)
				}
			}
			for _, column := range []struct {
				name    string
				notNull int
			}{{"n", 1}, {"required_code", 0}, {"required_count", 0}, {"fallback", 1}} {
				var notNull int
				if err := db.QueryRow(`SELECT "notnull" FROM pragma_table_info('qty') WHERE name = ?`, column.name).Scan(&notNull); err != nil || notNull != column.notNull {
					t.Errorf("%s NOT NULL = %d, want %d: %v", column.name, notNull, column.notNull, err)
				}
			}
		})
	}
}
