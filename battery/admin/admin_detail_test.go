package admin

// Tests for the read-only record detail/show screen at /e/<table>/view/:id.

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/entity"
)

func TestEntity_DetailShowsAllFields(t *testing.T) {
	db := newDB(t)
	app := newHostedApp(t, db, map[string]entity.EntityConfig{"posts": postsConfig()})
	h := mountEntityAdmin(t, app, Config{Entities: []string{"posts"}}, testUser{"u1"})

	postForm(h, "/admin/e/posts/_create", url.Values{
		"title": {"Readme"}, "body": {"the whole body"}, "status": {"published"},
	})
	id := firstID(t, db, "posts")

	rr := get(h, "/admin/e/posts/view/"+id)
	if rr.Code != http.StatusOK {
		t.Fatalf("detail status %d body=%s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	for _, want := range []string{"Readme", "the whole body", "published"} {
		if !strings.Contains(body, want) {
			t.Fatalf("detail should show field value %q; got %q", want, body)
		}
	}
	if !strings.Contains(body, "/admin/e/posts/edit/"+id) {
		t.Fatalf("detail should link to the edit screen; got %q", body)
	}
}

func TestEntity_ListRowLinksToDetail(t *testing.T) {
	db := newDB(t)
	app := newHostedApp(t, db, map[string]entity.EntityConfig{"posts": postsConfig()})
	h := mountEntityAdmin(t, app, Config{Entities: []string{"posts"}}, testUser{"u1"})

	postForm(h, "/admin/e/posts/_create", url.Values{"title": {"Row"}, "status": {"draft"}})
	id := firstID(t, db, "posts")

	body := get(h, "/admin/e/posts").Body.String()
	if !strings.Contains(body, "/admin/e/posts/view/"+id) {
		t.Fatalf("each list row should link to its detail view; got %q", body)
	}
}

func TestEntity_DetailOwnerScoped(t *testing.T) {
	db := newDB(t)
	app := newHostedApp(t, db, map[string]entity.EntityConfig{"notes": notesConfig()})
	base := mountAdminBattery(t, app, Config{Entities: []string{"notes"}})

	asU1 := asUser(base, testUser{"u1"})
	postForm(asU1, "/admin/e/notes/_create", url.Values{"text": {"u1 only"}})
	id := firstID(t, db, "notes")

	asU2 := asUser(base, testUser{"u2"})
	rr := get(asU2, "/admin/e/notes/view/"+id)
	if strings.Contains(rr.Body.String(), "u1 only") {
		t.Fatalf("SECURITY: [admin] detail view leaked u1's owner-scoped record to u2")
	}
	if !strings.Contains(rr.Body.String(), "Record not found") {
		t.Fatalf("u2 viewing u1's record should show not-found; got %q", rr.Body.String())
	}
}
