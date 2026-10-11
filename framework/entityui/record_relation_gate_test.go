package entityui

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// postsWithGatedAuthor is a public posts entity whose author relation
// points at default-posture users an anonymous caller may not read.
func postsWithGatedAuthor(t *testing.T, locked bool) *testUI {
	t.Helper()
	posts := entity.EntityConfig{
		Fields: fields(
			schema.Field{Name: "title", Type: schema.String},
			schema.Field{Name: "author_id", Type: schema.Relation, To: "users"},
		),
		Exposure: &entity.ExposureConfig{Public: true},
	}
	if locked {
		posts.Display = &entity.DisplayConfig{Fields: map[string]entity.FieldDisplay{"author_id": {Locked: true}}}
	}
	return newTestUI(t,
		map[string]entity.EntityConfig{
			"users": {Fields: fields(schema.Field{Name: "name", Type: schema.String})},
			"posts": posts,
		},
		map[string][]map[string]any{
			"users": {{"id": "usr-9q", "name": "Jane Author"}},
			"posts": {{"id": "p1", "title": "Hello", "author_id": "usr-9q"}},
		},
		withAPI(map[string]string{"posts": "/api/posts", "users": "/api/users"}),
	)
}

// The edit form's picker and a locked relation's value name a record
// the caller may not read with the em dash, never its name and never
// the raw foreign key. The picker still carries the id as its value, so
// a save keeps the column.
func TestRecordRefusedRelationIsMuted(t *testing.T) {
	for _, locked := range []bool{false, true} {
		x := postsWithGatedAuthor(t, locked)
		html := string(x.ui.Record("posts", "p1").RenderCtx(x.ctx("/posts/p1", "")))
		if strings.Contains(html, "Jane Author") || strings.Contains(html, ">usr-9q<") {
			t.Fatalf("locked=%v: the refused relation leaked a label:\n%s", locked, html)
		}
		if !locked && !strings.Contains(html, `selected="" value="usr-9q">—</option>`) {
			t.Fatalf("the picker dropped the kept value or its muted label:\n%s", html)
		}
	}
}
