package migrate

import (
	"context"
	"strconv"
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

func reciprocalPivotDriftPlan(local, foreign string) Plan {
	return Plan{Registry: secTypeReg{
		"posts": entity.Define("posts", entity.EntityConfig{
			Fields:    []schema.Field{{Name: "id", Type: schema.String}},
			Relations: []entity.Relation{entity.ManyToMany("tags", "tags", "post_tags", "post_id", "tag_id")},
		}.WithTimestamps(false)),
		"tags": entity.Define("tags", entity.EntityConfig{
			Fields:    []schema.Field{{Name: "id", Type: schema.Int}},
			Relations: []entity.Relation{entity.ManyToMany("posts", "posts", "post_tags", local, foreign)},
		}.WithTimestamps(false)),
	}}
}

func TestReciprocalPivotDriftRefused(t *testing.T) {
	base := reciprocalPivotDriftPlan("tag_id", "post_id")
	for _, test := range []struct {
		name, local, foreign string
	}{
		{name: "local column", local: "label_id", foreign: "post_id"},
		{name: "target column", local: "tag_id", foreign: "article_id"},
		{name: "endpoint types", local: "post_id", foreign: "tag_id"},
	} {
		t.Run(test.name, func(t *testing.T) {
			changed := reciprocalPivotDriftPlan(test.local, test.foreign)
			for _, dialect := range []Dialect{DialectSQLite, DialectPostgres} {
				for _, initial := range []bool{false, true} {
					t.Run(string(dialect)+"/initial="+strconv.FormatBool(initial), func(t *testing.T) {
						previous := SchemaSnapshot{}
						if !initial {
							previous = SnapshotFromPlan(base, dialect)
						}
						if up, down, _, err := GeneratePlan(changed, previous, dialect); err == nil || !strings.Contains(err.Error(), "post_tags") || up != "" || down != "" {
							t.Errorf("GeneratePlan = (%q, %q, %v), want named shared-pivot refusal without SQL", up, down, err)
						}
					})
				}
			}
			for _, initial := range []bool{false, true} {
				db := openMigrateSQLite(t)
				if !initial {
					if err := AutoMigrate(db, base.Registry); err != nil {
						t.Fatal(err)
					}
				}
				if changes, err := DiffSchema(context.Background(), db, changed.Registry); err == nil || !strings.Contains(err.Error(), "post_tags") || len(changes) != 0 {
					t.Errorf("DiffSchema initial=%v = (%v, %v), want named shared-pivot refusal", initial, changes, err)
				}
				if err := AutoMigrate(db, changed.Registry); err == nil || !strings.Contains(err.Error(), "post_tags") {
					t.Errorf("AutoMigrate initial=%v = %v, want named shared-pivot refusal", initial, err)
				}
				if initial {
					var tables int
					if err := db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name IN ('posts', 'tags', 'post_tags')").Scan(&tables); err != nil || tables != 0 {
						t.Errorf("refused initial boot created %d entity tables, error = %v", tables, err)
					}
				}
			}
		})
	}
}

func TestReciprocalPivotDeclarationsAgree(t *testing.T) {
	plan := reciprocalPivotDriftPlan("tag_id", "post_id")
	for _, dialect := range []Dialect{DialectSQLite, DialectPostgres} {
		up, _, next, err := GeneratePlan(plan, SchemaSnapshot{}, dialect)
		if err != nil || strings.Count(up, "CREATE TABLE IF NOT EXISTS post_tags (") != 1 {
			t.Fatalf("initial %s plan = (%q, %v), want one pivot creation", dialect, up, err)
		}
		if up, down, _, err := GeneratePlan(plan, next, dialect); err != nil || up != "" || down != "" {
			t.Fatalf("unchanged %s plan = (%q, %q, %v)", dialect, up, down, err)
		}
	}
	db := openMigrateSQLite(t)
	if err := AutoMigrate(db, plan.Registry); err != nil {
		t.Fatal(err)
	}
	if changes, err := DiffSchema(context.Background(), db, plan.Registry); err != nil || len(changes) != 0 {
		t.Fatalf("unchanged live diff = (%v, %v)", changes, err)
	}
	if err := AutoMigrate(db, plan.Registry); err != nil {
		t.Fatalf("unchanged boot: %v", err)
	}
	for _, statement := range []string{
		"INSERT INTO posts (id) VALUES ('post-1')",
		"INSERT INTO tags (id) VALUES (1)",
		"INSERT INTO post_tags (post_id, tag_id) VALUES ('post-1', 1)",
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM tags JOIN post_tags ON tags.id = post_tags.tag_id JOIN posts ON posts.id = post_tags.post_id").Scan(&count); err != nil || count != 1 {
		t.Fatalf("reciprocal join count = %d, error = %v", count, err)
	}
}
