package admin

import (
	"net/http"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// The admin lists an entity that declares no fields of its own by key,
// each row linked to its record. The list used to panic: the table had
// no column to title its phone layout with.
func TestAdminListsEntityWithNoFields(t *testing.T) {
	x := setup(t, map[string]entity.EntityConfig{"tags": {Table: "tags"}}, Config{Entities: []string{"tags"}}, nil)
	for _, id := range []string{"tag-1", "tag-2"} {
		if _, err := x.db.Exec(`INSERT INTO tags (id) VALUES (?)`, id); err != nil {
			t.Fatal(err)
		}
	}
	rr := get(x.as(theAdmin), "/admin/entities/tags")
	if rr.Code != http.StatusOK {
		t.Fatalf("list = %d", rr.Code)
	}
	for _, id := range []string{"tag-1", "tag-2"} {
		if !strings.Contains(rr.Body.String(), `href="/admin/entities/tags/`+id+`"`) {
			t.Errorf("no link to %s:\n%s", id, rr.Body.String())
		}
	}
}
