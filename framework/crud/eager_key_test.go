package crud

import (
	"context"
	"errors"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// EagerLoad joins on each entity's own key column, not a fixed "id".
func TestEagerLoadJoinsOnDeclaredKeys(t *testing.T) {
	ctx := context.Background()
	db := setupDB(t,
		`CREATE TABLE skus (sku TEXT PRIMARY KEY, name TEXT)`,
		`CREATE TABLE lines (code TEXT PRIMARY KEY, sku_id TEXT)`,
		`CREATE TABLE tags (slug TEXT PRIMARY KEY, label TEXT)`,
		`CREATE TABLE line_tags (line_code TEXT, tag_slug TEXT)`,
	)
	seedRows(t, db, "skus", []map[string]any{{"sku": "k1", "name": "Bolt"}})
	seedRows(t, db, "lines", []map[string]any{{"code": "l1", "sku_id": "k1"}})
	seedRows(t, db, "tags", []map[string]any{{"slug": "t1", "label": "steel"}})
	seedRows(t, db, "line_tags", []map[string]any{{"line_code": "l1", "tag_slug": "t1"}})

	skus := entity.Define("skus", entity.EntityConfig{
		Fields:     []schema.Field{{Name: "sku", Type: schema.String}, {Name: "name", Type: schema.String}},
		PrimaryKey: []string{"sku"},
	}.WithTimestamps(false))
	tags := entity.Define("tags", entity.EntityConfig{
		Fields:     []schema.Field{{Name: "slug", Type: schema.String}, {Name: "label", Type: schema.String}},
		PrimaryKey: []string{"slug"},
	}.WithTimestamps(false))
	tagged := entity.ManyToMany("tags", "tags", "line_tags", "line_code", "tag_slug")
	tagged.ForeignKey = "tag_slug" // EagerLoad validates ForeignKey for every shape
	lines := entity.Define("lines", entity.EntityConfig{
		Fields:     []schema.Field{{Name: "code", Type: schema.String}, {Name: "sku_id", Type: schema.String}},
		PrimaryKey: []string{"code"},
		Relations:  []entity.Relation{entity.BelongsTo("sku", "skus", "sku_id"), tagged},
	}.WithTimestamps(false))
	reg := stubRegistry{byName: map[string]*entity.Entity{"skus": skus, "tags": tags, "lines": lines}}

	got, err := EagerLoad(ctx, db, lines, lines.Config.Relations, []string{"l1"}, reg)
	if err != nil {
		t.Fatalf("EagerLoad: %v", err)
	}
	if sku, _ := got["l1"]["sku"].(map[string]any); sku["name"] != "Bolt" {
		t.Errorf("belongs_to through sku = %v, want the Bolt row", got["l1"]["sku"])
	}
	if tagRows, _ := got["l1"]["tags"].([]map[string]any); len(tagRows) != 1 || tagRows[0]["label"] != "steel" {
		t.Errorf("many_to_many through slug = %v, want the steel row", got["l1"]["tags"])
	}
}

// A composite-key parent has no one column for the ids to name.
func TestEagerLoadRefusesCompositeParent(t *testing.T) {
	db := setupDB(t, `CREATE TABLE enrollments (student_id TEXT, course_id TEXT, PRIMARY KEY (student_id, course_id))`)
	ent := entity.Define("enrollments", entity.EntityConfig{
		Fields: []schema.Field{
			{Name: "student_id", Type: schema.String, Required: true},
			{Name: "course_id", Type: schema.String, Required: true},
		},
		PrimaryKey: []string{"student_id", "course_id"},
		Relations:  []entity.Relation{entity.HasMany("grades", "grades", "student_id")},
	}.WithTimestamps(false))
	_, err := EagerLoad(context.Background(), db, ent, ent.Config.Relations, []string{"s1\x1fc1"})
	if !errors.Is(err, entity.ErrCompositeKey) {
		t.Fatalf("EagerLoad on a composite parent: err = %v, want ErrCompositeKey", err)
	}
}
