package migrate_test

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
)

func TestUniqueFieldNameCollisions(t *testing.T) {
	first := qtyEntity(schema.Field{Name: "code", Type: schema.String, Unique: true})
	for _, declared := range []bool{false, true} {
		other := entity.EntityConfig{Table: "qty_code", Fields: []schema.Field{{Name: "id", Type: schema.String}, {Name: "value", Type: schema.String}}, PrimaryKey: []string{"id"}}
		if declared {
			other.Indices = []entity.Index{{Name: "qty_code_key", Columns: []string{"value"}}}
		} else {
			other.Table = "qty_code"
			other.Fields[1] = schema.Field{Name: "key", Type: schema.String, Unique: true}
			first = entity.Define("first", entity.EntityConfig{Table: "qty", Fields: []schema.Field{{Name: "id", Type: schema.String}, {Name: "code_key", Type: schema.String, Unique: true}}, PrimaryKey: []string{"id"}}.WithTimestamps(false))
		}
		if declared {
			first = qtyEntity(schema.Field{Name: "code", Type: schema.String, Unique: true})
		}
		second := entity.Define("second", other.WithTimestamps(false))
		for _, dialect := range []migrate.Dialect{migrate.DialectSQLite, migrate.DialectPostgres} {
			_, _, _, err := migrate.GeneratePlan(migrate.Plan{Registry: fkReg{first, second}}, migrate.SchemaSnapshot{}, dialect)
			if err == nil || !strings.Contains(err.Error(), "index name") {
				t.Fatalf("declared=%t dialect=%s: collision error = %v", declared, dialect, err)
			}
		}
	}
}
