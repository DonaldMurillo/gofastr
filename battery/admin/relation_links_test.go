package admin

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// authoredPosts is posts pointing at authors, the authors public so
// their names read whether or not the admin exposes them.
func authoredPosts() map[string]entity.EntityConfig {
	return map[string]entity.EntityConfig{
		"authors": entity.EntityConfig{
			Table:    "authors",
			Fields:   []schema.Field{{Name: "name", Type: schema.String}},
			Exposure: &entity.ExposureConfig{Public: true},
		}.WithTimestamps(false),
		"posts": entity.EntityConfig{
			Table: "posts",
			Fields: []schema.Field{
				{Name: "title", Type: schema.String},
				{Name: "author_id", Type: schema.Relation, To: "authors"},
			},
		}.WithTimestamps(false),
	}
}

// A relation cell links to the related record's admin screen when the
// admin exposes that entity, and stays text when it does not.
func TestAdminRelationCellLinksExposedRecord(t *testing.T) {
	for _, tc := range []struct {
		exposed []string
		link    bool
	}{
		{[]string{"posts", "authors"}, true},
		{[]string{"posts"}, false},
	} {
		x := setup(t, authoredPosts(), Config{Entities: tc.exposed}, nil)
		x.insert("authors", map[string]any{"id": "a1", "name": "Ada Writer"})
		x.insert("posts", map[string]any{"id": "p1", "title": "Hello", "author_id": "a1"})
		body := get(withPolicy(x.as(theAdmin)), "/admin/entities/posts").Body.String()
		if !strings.Contains(body, "Ada Writer") {
			t.Fatalf("exposed %v: the relation title is missing:\n%s", tc.exposed, body)
		}
		if got := strings.Contains(body, `href="/admin/entities/authors/a1"`); got != tc.link {
			t.Errorf("exposed %v: relation link = %v, want %v:\n%s", tc.exposed, got, tc.link, body)
		}
	}
}
