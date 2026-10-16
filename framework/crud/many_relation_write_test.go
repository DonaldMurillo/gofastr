package crud

import (
	"context"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// A Many relation is stored in a TEXT column, so CRUD writes it as a
// string reference, the shape the entityui relation picker posts.
func TestManyRelationWritesString(t *testing.T) {
	db := setupDB(t, `CREATE TABLE posts (id TEXT PRIMARY KEY, tag_ids TEXT)`)
	ent := entity.Define("posts", entity.EntityConfig{
		Name: "posts", Table: "posts",
		Fields: []schema.Field{
			{Name: "tag_ids", Type: schema.Relation, To: "tags", Many: true},
		},
	}.WithTimestamps(false))
	ent.SetDB(db)
	ch := NewCrudHandler(ent, db)
	if _, err := ch.CreateOne(context.Background(), map[string]any{"tag_ids": "t1"}); err != nil {
		t.Fatalf("write a Many relation: %v", err)
	}
}
