package migrate

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// TestGeneratePlan_CaseInsensitiveTableIdentity asserts that a difference in casing
// between snapshot tables and entity declarations (e.g. "posts" vs "Posts", or
// pivot table "posts_tags" vs "Posts_Tags") does NOT cause false DROP TABLE or
// duplicate CREATE TABLE statements.
func TestGeneratePlan_CaseInsensitiveTableIdentity(t *testing.T) {
	tagEnt := entity.Define("tags", entity.EntityConfig{
		Table: "Tags", // Declared with capital T
		Fields: []schema.Field{
			{Name: "id", Type: schema.String},
			{Name: "name", Type: schema.String},
		},
		Relations: []entity.Relation{
			entity.ManyToMany("posts", "posts", "Posts_Tags", "tag_id", "post_id"),
		},
	}.WithTimestamps(false))

	postEnt := entity.Define("posts", entity.EntityConfig{
		Table: "posts",
		Fields: []schema.Field{
			{Name: "id", Type: schema.String},
			{Name: "title", Type: schema.String},
		},
		Relations: []entity.Relation{
			entity.ManyToMany("tags", "tags", "Posts_Tags", "post_id", "tag_id"),
		},
	}.WithTimestamps(false))

	// Previous snapshot has lowercased "tags" and "posts_tags"
	prevSnap := SchemaSnapshot{
		Tables: map[string]map[string]string{
			"posts": {
				"id":    "TEXT",
				"title": "TEXT",
			},
			"tags": {
				"id":   "TEXT",
				"name": "TEXT",
			},
			"posts_tags": {
				"post_id": "TEXT",
				"tag_id":  "TEXT",
			},
		},
		TableDDL: map[string]string{
			"posts":      "CREATE TABLE posts (id TEXT PRIMARY KEY, title TEXT);",
			"tags":       "CREATE TABLE tags (id TEXT PRIMARY KEY, name TEXT);",
			"posts_tags": "CREATE TABLE posts_tags (post_id TEXT NOT NULL, tag_id TEXT NOT NULL, PRIMARY KEY (post_id, tag_id));",
		},
	}

	currentPlan := Plan{
		Registry: testReg{"posts": postEnt, "tags": tagEnt},
	}

	up, _, _, err := GeneratePlan(currentPlan, prevSnap, DialectPostgres)
	if err != nil {
		t.Fatalf("unexpected GeneratePlan error: %v", err)
	}

	// Since the tables and columns match except for identifier casing,
	// no DROP TABLE or duplicate CREATE TABLE should be emitted.
	if strings.Contains(up, "DROP TABLE") {
		t.Fatalf("expected no DROP TABLE for case-only differences, got UP plan:\n%s", up)
	}
	if strings.Contains(up, "CREATE TABLE") {
		t.Fatalf("expected no CREATE TABLE for existing tables with case-only differences, got UP plan:\n%s", up)
	}
}
