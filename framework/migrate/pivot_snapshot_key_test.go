package migrate

import (
	"slices"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

func TestPivotSnapshotRecordsCompositeKey(t *testing.T) {
	for _, dialect := range []Dialect{DialectSQLite, DialectPostgres} {
		t.Run(string(dialect), func(t *testing.T) {
			plan := pivotDriftPlan("post_id", "tag_id", schema.String)
			previous := SnapshotFromPlan(plan, dialect)
			if !slices.Equal(previous.PrimaryKeys["post_tags"], []string{"post_id", "tag_id"}) {
				t.Errorf("pivot key = %v, want [post_id tag_id]", previous.PrimaryKeys["post_tags"])
			}
			registry := plan.Registry.(secTypeReg)
			registry["links"] = entity.Define("links", entity.EntityConfig{
				Table: "post_tags", PrimaryKey: []string{"id"},
				Fields: []schema.Field{{Name: "id", Type: schema.String}, {Name: "post_id", Type: schema.String, Required: true}, {Name: "tag_id", Type: schema.String, Required: true}},
			}.WithTimestamps(false))
			up, _, _, err := GeneratePlan(plan, previous, dialect)
			if err != nil || !strings.Contains(up, "PRIMARY KEY (id)") && !strings.Contains(up, "id TEXT PRIMARY KEY") {
				t.Fatalf("pivot takeover must migrate its key: %s, error=%v", up, err)
			}
		})
	}
}
