package migrate

import (
	"context"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

func pivotDriftPlan(local, foreign string, keyType schema.FieldType) Plan {
	return Plan{Registry: secTypeReg{
		"posts": entity.Define("posts", entity.EntityConfig{
			Fields:    []schema.Field{{Name: "id", Type: schema.String}},
			Relations: []entity.Relation{entity.ManyToMany("tags", "tags", "post_tags", local, foreign)},
		}.WithTimestamps(false)),
		"tags": entity.Define("tags", entity.EntityConfig{
			Fields: []schema.Field{{Name: "id", Type: keyType}},
		}.WithTimestamps(false)),
	}}
}

func TestPivotDriftRefused(t *testing.T) {
	base := pivotDriftPlan("post_id", "tag_id", schema.String)
	for _, dialect := range []Dialect{DialectSQLite, DialectPostgres} {
		for name, changed := range map[string]Plan{
			"local":   pivotDriftPlan("article_id", "tag_id", schema.String),
			"foreign": pivotDriftPlan("post_id", "label_id", schema.String),
			"type":    pivotDriftPlan("post_id", "tag_id", schema.Int),
		} {
			t.Run(string(dialect)+"/"+name, func(t *testing.T) {
				previous := SnapshotFromPlan(base, dialect)
				if _, _, _, err := GeneratePlan(changed, previous, dialect); err == nil || !strings.Contains(err.Error(), "post_tags") {
					t.Errorf("GeneratePlan = %v, want named pivot drift refusal", err)
				}
				if dialect != DialectSQLite {
					return
				}
				db := openMigrateSQLite(t)
				if err := AutoMigrate(db, base.Registry); err != nil {
					t.Fatal(err)
				}
				if _, err := DiffSchema(context.Background(), db, changed.Registry); err == nil || !strings.Contains(err.Error(), "post_tags") {
					t.Errorf("DiffSchema = %v, want named pivot drift refusal", err)
				}
				if err := AutoMigrate(db, changed.Registry); err == nil || !strings.Contains(err.Error(), "post_tags") {
					t.Errorf("AutoMigrate = %v, want named pivot drift refusal", err)
				}
			})
		}
		if _, _, _, err := GeneratePlan(base, SnapshotFromPlan(base, dialect), dialect); err != nil {
			t.Fatalf("unchanged pivot: %v", err)
		}
	}
}
