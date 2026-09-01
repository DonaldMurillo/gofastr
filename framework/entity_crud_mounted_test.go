package framework

import (
	"database/sql"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// TestEntityCRUDMountedTracksTheMount pins the exported predicate the
// route-advertising surfaces are documented to use (sdkdocs.Config's
// CRUDMounted, repolint's crud-exposure-rederived message, sdk.md). The
// #358 rework deleted the export while all three references stood, so the
// documented wiring did not compile for one release — this test is the
// symbol's existence proof as much as its behavior pin.
func TestEntityCRUDMountedTracksTheMount(t *testing.T) {
	off := false
	define := func() (*entity.Entity, *entity.Entity) {
		auto := entity.Define("posts", entity.EntityConfig{
			Table:  "posts",
			Fields: []schema.Field{{Name: "title", Type: schema.String}},
		}.WithTimestamps(false))
		disabled := entity.Define("drafts", entity.EntityConfig{
			Table:    "drafts",
			Exposure: &entity.ExposureConfig{CRUD: &off},
			Fields:   []schema.Field{{Name: "title", Type: schema.String}},
		}.WithTimestamps(false))
		return auto, disabled
	}

	// The case Exposure.CRUD alone cannot see: no DB, so nothing mounts,
	// while the entity still reads "auto".
	auto, disabled := define()
	noDB := NewApp()
	noDB.Registry.Register(auto)
	if noDB.EntityCRUDMounted(auto) {
		t.Fatal("DB-less app reported an entity's CRUD routes as mounted — nothing was registered")
	}

	forEachDialect(t, func(t *testing.T, dbc *sql.DB, _ Dialect) {
		auto, disabled = define()
		app := NewApp(WithDB(dbc))
		app.Registry.Register(auto)
		app.Registry.Register(disabled)
		if !app.EntityCRUDMounted(auto) {
			t.Fatal("auto-exposed entity on an app with a DB must report mounted")
		}
		if app.EntityCRUDMounted(disabled) {
			t.Fatal("Exposure.CRUD=false entity must not report mounted")
		}

		// A columned view registers an entity and mounts READ-ONLY
		// (App.View). Its read routes exist, and read routes are what a
		// route-advertising surface documents — so the predicate must say
		// mounted, not treat read-only as absent.
		app.Registry.Register(entity.Define("users", entity.EntityConfig{
			Table: "users",
			Fields: []schema.Field{
				{Name: "name", Type: schema.String},
				{Name: "active", Type: schema.Bool},
			},
		}.WithTimestamps(false)))
		app.View(activeUsersView())
		viewEnt, err := app.Registry.Get("active_users")
		if err != nil {
			t.Fatalf("view entity not registered: %v", err)
		}
		if !app.EntityCRUDMounted(viewEnt) {
			t.Fatal("a columned view mounts read-only routes; the predicate must report it mounted")
		}
	})
}
