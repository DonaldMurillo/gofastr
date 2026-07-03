package admin

import (
	"net/http"
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
