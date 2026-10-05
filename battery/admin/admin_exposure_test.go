package admin

import (
	"net/http"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// admin.New(admin.Config{}) must expose NO entity screens: an admin
// battery dropped into an app with a zero-value config silently turning
// every CRUD table into an editable back-office is the exposure default
// inverted. The whole-back-office behavior is the explicit
// AllEntities: true opt-in.
func TestEmptyEntitiesExposesNothing(t *testing.T) {
	db := newDB(t)
	app := newHostedApp(t, db, map[string]entity.EntityConfig{"posts": postsConfig()})
	h := mountEntityAdmin(t, app, Config{}, testUser{"u1"})

	rr := get(h, "/admin/e/posts")
	if rr.Code == http.StatusOK {
		t.Fatalf("zero-value Config exposed /admin/e/posts (200) — exposure must be opt-in")
	}
	rr = get(h, "/admin/e/posts/_rows")
	if rr.Code == http.StatusOK {
		t.Fatalf("zero-value Config exposed the rows endpoint — exposure must be opt-in")
	}
}

func TestAllEntitiesExposesCRUDEnabled(t *testing.T) {
	db := newDB(t)
	app := newHostedApp(t, db, map[string]entity.EntityConfig{"posts": postsConfig()})
	h := mountEntityAdmin(t, app, Config{AllEntities: true}, testUser{"u1"})

	rr := get(h, "/admin/e/posts")
	if rr.Code != http.StatusOK {
		t.Fatalf("AllEntities should expose posts, got %d body=%s", rr.Code, rr.Body.String())
	}
}

// The ops pages' nav must list the same entities the entity pages serve:
// AllEntities (what the generator emits) leaves Config.Entities empty.
func TestAllEntitiesNavListsEntities(t *testing.T) {
	db := newDB(t)
	app := newHostedApp(t, db, map[string]entity.EntityConfig{"posts": postsConfig()})
	h := mountEntityAdmin(t, app, Config{AllEntities: true}, testUser{"u1"})

	for _, path := range []string{"/admin", "/admin/audit"} {
		rr := get(h, path)
		if rr.Code != http.StatusOK {
			t.Fatalf("GET %s = %d", path, rr.Code)
		}
		if !strings.Contains(rr.Body.String(), `href="/admin/e/posts"`) {
			t.Errorf("GET %s nav has no link to /admin/e/posts", path)
		}
		// Same label the entity pages' nav gives it (navConfig).
		if !strings.Contains(rr.Body.String(), `>Posts</a>`) {
			t.Errorf("GET %s nav labels posts differently from the entity pages' nav (want Posts)", path)
		}
	}
}

// The entity shell's nav must link back to the ops screens, or an entity
// page is a dead end for reaching Overview and the audit log.
func TestEntityNavLinksOpsScreens(t *testing.T) {
	db := newDB(t)
	app := newHostedApp(t, db, map[string]entity.EntityConfig{"posts": postsConfig()})
	h := mountEntityAdmin(t, app, Config{AllEntities: true}, testUser{"u1"})

	rr := get(h, "/admin/e/posts")
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /admin/e/posts = %d", rr.Code)
	}
	for _, href := range []string{`href="/admin"`, `href="/admin/audit"`} {
		if !strings.Contains(rr.Body.String(), href) {
			t.Errorf("entity nav has no %s link", href)
		}
	}
}
