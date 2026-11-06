package migrate

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

func TestDropRefusesKeptForeignKeys(t *testing.T) {
	for _, dialect := range []Dialect{DialectSQLite, DialectPostgres} {
		for _, action := range []string{"CASCADE", "SET NULL", "NO ACTION", "RESTRICT"} {
			t.Run(string(dialect)+"/"+action, func(t *testing.T) {
				child := entity.Define("children", entity.EntityConfig{Fields: []schema.Field{{Name: "id", Type: schema.String}, {Name: "parent_id", Type: schema.String}}}.WithTimestamps(false))
				plan := Plan{Registry: secTypeReg{"children": child}}
				previous := SnapshotFromPlan(plan, dialect)
				previous.Tables["parents"] = map[string]string{"id": "TEXT"}
				previous.TableDDL["parents"] = `CREATE TABLE parents (id TEXT PRIMARY KEY)`
				previous.TableDDL["children"] = `CREATE TABLE children (id TEXT PRIMARY KEY, parent_id TEXT REFERENCES Parents(id) ON DELETE ` + action + `)`
				up, down, _, err := GeneratePlan(plan, previous, dialect)
				if err == nil || !strings.Contains(err.Error(), "children") || !strings.Contains(err.Error(), "parents") || up != "" || down != "" {
					t.Fatalf("drop with kept foreign key = (%q, %q, %v), want named refusal without SQL", up, down, err)
				}
				if _, _, _, err := GeneratePlan(Plan{}, previous, dialect); err != nil {
					t.Fatalf("dropping both sides must remain supported: %v", err)
				}
			})
		}
	}
}
