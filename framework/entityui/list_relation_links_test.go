package entityui

import (
	"context"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/hook"
)

// recPath gives every entity record screens under /rec/<name>.
func recPath(e *entity.Entity) (string, bool) { return "/rec/" + e.GetName(), true }

// With a record path, a readable relation's title is a chip linking to
// the related record.
func TestRelationCellLinksToRecord(t *testing.T) {
	x := usersPostsWorld(t, []map[string]any{{"id": "usr-7k2", "name": "Jane Author"}})
	ui := x.ui.WithRecordPath(recPath)
	html := listHTML(t, ui.List("posts"), x.userCtx("/posts", "", "u1"))
	if !strings.Contains(html, `href="/rec/users/usr-7k2"`) || !strings.Contains(html, `fui-tag`) || !strings.Contains(html, "Jane Author") {
		t.Fatalf("a readable relation did not link to its record:\n%s", html)
	}
	// Without one, the title stays text.
	html = listHTML(t, x.ui.List("posts"), x.userCtx("/posts", "", "u1"))
	if strings.Contains(html, "/users/usr-7k2") || !strings.Contains(html, "Jane Author") {
		t.Fatalf("a UI with no record path linked a relation:\n%s", html)
	}
	// A path that answers false for the related entity leaves text.
	none := x.ui.WithRecordPath(func(*entity.Entity) (string, bool) { return "", false })
	if html = listHTML(t, none.List("posts"), x.userCtx("/posts", "", "u1")); strings.Contains(html, "/users/usr-7k2") {
		t.Fatalf("a relation linked to an entity with no record screen:\n%s", html)
	}
}

// A refused relation links nowhere: the muted cell carries no href, and
// so no foreign key.
func TestRelationLinkRefusedCarriesNoID(t *testing.T) {
	x := usersPostsWorld(t, []map[string]any{{"id": "usr-7k2", "name": "Jane Author"}})
	html := listHTML(t, x.ui.WithRecordPath(recPath).List("posts"), x.ctx("/posts", ""))
	if strings.Contains(html, "usr-7k2") || strings.Contains(html, "Jane Author") {
		t.Fatalf("SECURITY: a refused relation linked its record:\n%s", html)
	}
}

// An id the caller's read did not return prints as text and links
// nowhere: a link is drawn only for a title the read returned.
func TestRelationLinkNeedsAReadTitle(t *testing.T) {
	x := usersPostsWorld(t, []map[string]any{{"id": "usr-7k2", "name": "Jane Author"}})
	ch, err := x.host.Crud(mustEntity(t, x, "users"))
	if err != nil {
		t.Fatal(err)
	}
	ch.Hooks = hook.NewHookRegistry()
	ch.Hooks.RegisterHook(hook.BeforeList, func(_ context.Context, data any) error {
		data.(*hook.ListPayload).AddWhere("name = $1", "nobody")
		return nil
	})
	html := listHTML(t, x.ui.WithRecordPath(recPath).List("posts"), x.userCtx("/posts", "", "u1"))
	if strings.Contains(html, `href="/rec/users/`) {
		t.Fatalf("an unread relation linked its record:\n%s", html)
	}
}
