package migrate

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// TestSortDroppedTables_ForeignKeyDependency asserts that child/referencing
// tables (including ManyToMany pivot tables) are dropped before the referenced tables.
func TestSortDroppedTables_ForeignKeyDependency(t *testing.T) {
	tableDDL := map[string]string{
		"posts": `CREATE TABLE posts (id TEXT PRIMARY KEY, title TEXT);`,
		"tags":  `CREATE TABLE tags (id TEXT PRIMARY KEY, name TEXT);`,
		"posts_tags": `CREATE TABLE posts_tags (
			post_id TEXT NOT NULL,
			tag_id TEXT NOT NULL,
			PRIMARY KEY (post_id, tag_id),
			FOREIGN KEY (post_id) REFERENCES posts(id) ON DELETE CASCADE,
			FOREIGN KEY (tag_id) REFERENCES tags(id) ON DELETE CASCADE
		);`,
	}

	dropped := []string{"posts", "posts_tags", "tags"}
	ordered := sortDroppedTables(dropped, tableDDL)

	// posts_tags must appear before posts and tags in the drop order
	pos := make(map[string]int)
	for i, name := range ordered {
		pos[name] = i
	}

	if pos["posts_tags"] >= pos["posts"] {
		t.Fatalf("expected posts_tags (pos %d) before posts (pos %d) in drop order: %v",
			pos["posts_tags"], pos["posts"], ordered)
	}
	if pos["posts_tags"] >= pos["tags"] {
		t.Fatalf("expected posts_tags (pos %d) before tags (pos %d) in drop order: %v",
			pos["posts_tags"], pos["tags"], ordered)
	}
}

// TestGeneratePlan_DropEntityAndPivot_PostgresOrdering asserts that when an entity
// with an M2M pivot table is removed, GeneratePlan emits:
// - UP: child pivot table is dropped BEFORE the referenced parent table
// - DOWN: referenced parent table is recreated BEFORE the child pivot table
func TestGeneratePlan_DropEntityAndPivot_PostgresOrdering(t *testing.T) {
	tagEnt := entity.Define("tags", entity.EntityConfig{
		Table: "tags",
		Fields: []schema.Field{
			{Name: "id", Type: schema.String},
			{Name: "name", Type: schema.String},
		},
	}.WithTimestamps(false))

	postEnt := entity.Define("posts", entity.EntityConfig{
		Table: "posts",
		Fields: []schema.Field{
			{Name: "id", Type: schema.String},
			{Name: "title", Type: schema.String},
		},
		Relations: []entity.Relation{
			{
				Name:             "tags",
				Type:             entity.RelManyToMany,
				Entity:           "tags",
				Through:          "posts_tags",
				LocalKey:         "post_id",
				ForeignKeyTarget: "tag_id",
			},
		},
	}.WithTimestamps(false))

	initialPlan := Plan{
		Registry: testReg{"posts": postEnt, "tags": tagEnt},
	}
	prevSnap := SnapshotFromPlan(initialPlan, DialectPostgres)

	// Now remove "posts" from the plan, keeping only "tags":
	removedPlan := Plan{
		Registry: testReg{"tags": tagEnt},
	}

	up, down, _, err := GeneratePlan(removedPlan, prevSnap, DialectPostgres)
	if err != nil {
		t.Fatalf("GeneratePlan: %v", err)
	}

	// In UP, posts_tags must be dropped before posts:
	upDropPivot := strings.Index(up, "DROP TABLE IF EXISTS posts_tags;")
	upDropPost := strings.Index(up, "DROP TABLE IF EXISTS posts;")
	if upDropPivot == -1 || upDropPost == -1 {
		t.Fatalf("expected both tables dropped in UP: %s", up)
	}
	if upDropPivot >= upDropPost {
		t.Fatalf("UP order error: expected posts_tags to be dropped before posts. UP SQL:\n%s", up)
	}

	// In DOWN, posts must be recreated before posts_tags:
	downCreatePost := strings.Index(down, "CREATE TABLE IF NOT EXISTS posts (")
	downCreatePivot := strings.Index(down, "CREATE TABLE IF NOT EXISTS posts_tags (")
	if downCreatePost == -1 || downCreatePivot == -1 {
		t.Fatalf("expected both tables recreated in DOWN: %s", down)
	}
	if downCreatePost >= downCreatePivot {
		t.Fatalf("DOWN order error: expected posts to be recreated before posts_tags. DOWN SQL:\n%s", down)
	}
}
