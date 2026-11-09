package entityui

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// An entity that declares no fields of its own (a join or marker table
// with only its key) lists its rows by key: the table, the cards and
// the admin screen draw, linked to each record, where they used to
// panic for want of a column to title the phone layout with.
func TestListEntityWithNoFields(t *testing.T) {
	ents := map[string]entity.EntityConfig{"tags": {Table: "tags"}}
	rows := map[string][]map[string]any{"tags": {{"id": "tag-1"}, {"id": "tag-2"}}}
	x := newTestUI(t, ents, rows, withAPI(map[string]string{"tags": "/api/tags"}))
	ctx := x.userCtx("/rec/tags", "", "u1")
	for _, as := range []string{"table", "cards"} {
		body := string(x.ui.List("tags").Key(as).Base("/rec/tags").As(as).RenderCtx(ctx))
		for _, id := range []string{"tag-1", "tag-2"} {
			if !strings.Contains(body, `href="/rec/tags/`+id+`"`) {
				t.Errorf("%s: no link to %s:\n%s", as, id, body)
			}
		}
	}
}
