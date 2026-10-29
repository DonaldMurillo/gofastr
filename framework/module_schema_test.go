package framework

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
)

// initModule runs init as its Init.
type initModule struct{ init func(*App) error }

func (initModule) Name() string                  { return "feature" }
func (initModule) OnStart(context.Context) error { return nil }
func (initModule) OnStop(context.Context) error  { return nil }
func (initModule) Manifest() ModuleManifest      { return ModuleManifest{Version: "1.0.0"} }
func (m initModule) Init(app *App) error         { return m.init(app) }

// Start migrated and seeded before InitPlugins ran, so an entity, view or
// routine a module declared in Init (the pattern modules.md documents)
// never reached the database: Start succeeded and the module failed at
// its first query (2026-10-27 S2).
func TestModuleInitSchemaIsMigrated(t *testing.T) {
	fields := []schema.Field{{Name: "name", Type: schema.String, Required: true}}
	cases := []struct {
		name  string
		init  func(*App) error
		query string
	}{
		{"entity and seed", func(app *App) error {
			return app.TryEntity("widgets", entity.EntityConfig{
				Fields: fields,
				Seed: func(ctx context.Context, db *sql.DB) error {
					_, err := db.ExecContext(ctx, "INSERT INTO widgets (id, name) VALUES ('1', 'first')")
					return err
				},
			})
		}, "SELECT COUNT(*) FROM widgets WHERE name = 'first'"},
		{"view", func(app *App) error {
			app.View(migrate.View{Name: "gadget_names", Select: "SELECT id, name FROM gadgets", DependsOn: []string{"gadgets"}})
			return nil
		}, "SELECT 1 + COUNT(*) FROM gadget_names"},
		{"routine", func(app *App) error {
			app.Routine(migrate.Routine{Name: "marker", Up: "CREATE VIEW IF NOT EXISTS routine_marker AS SELECT 1 AS x", Down: "DROP VIEW IF EXISTS routine_marker"})
			return nil
		}, "SELECT x FROM routine_marker"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := newStoreDB(t)
			t.Cleanup(func() { _ = db.Close() })
			app := NewApp(WithDB(db), WithoutDefaultMiddleware())
			app.Entity("gadgets", entity.EntityConfig{Fields: fields})
			app.RegisterModule(initModule{init: tc.init})
			_, done := startForTest(t, app)
			t.Cleanup(func() {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				_ = app.Shutdown(ctx)
				<-done
			})
			var n int
			if err := db.QueryRow(tc.query).Scan(&n); err != nil {
				t.Fatalf("what the module declared in Init: %v", err)
			}
			if n != 1 {
				t.Errorf("%s = %d, want 1", tc.query, n)
			}
		})
	}
}
