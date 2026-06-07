package db_test

import (
	"testing"

	_ "github.com/mattn/go-sqlite3"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/kiln/db"
)

// TestMigrateDefersDanglingBelongsTo pins the free-order authoring contract:
// a BelongsTo whose target entity isn't registered yet must NOT fail the live
// migrate (the framework's strict AutoMigrate would reject it). Once the target
// is added, a later migrate is still clean. Before C1 the first Migrate errored
// with "BelongsTo to unknown entity", bricking the kiln rebuild.
func TestMigrateDefersDanglingBelongsTo(t *testing.T) {
	d, cleanup, err := db.EphemeralSQLite("kiln-defer")
	if err != nil {
		t.Fatalf("EphemeralSQLite: %v", err)
	}
	defer cleanup()

	app := framework.NewApp(framework.WithDB(d))
	app.Entity("posts", framework.EntityConfig{
		Fields: []schema.Field{
			{Name: "title", Type: schema.String},
			{Name: "author_id", Type: schema.String},
		},
		Relations: []framework.Relation{
			{Type: framework.RelManyToOne, Name: "author", Entity: "users", ForeignKey: "author_id"},
		},
	})

	// users is absent — the dangling BelongsTo must be deferred, not fatal.
	if err := db.Migrate(d, app.Registry); err != nil {
		t.Fatalf("Migrate with dangling BelongsTo should defer, got: %v", err)
	}

	// Add the target and migrate again — now resolvable, still clean.
	app.Entity("users", framework.EntityConfig{
		Fields: []schema.Field{{Name: "email", Type: schema.String}},
	})
	if err := db.Migrate(d, app.Registry); err != nil {
		t.Fatalf("Migrate after users added: %v", err)
	}
}
