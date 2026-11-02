package factory_test

import (
	"context"
	"database/sql"
	"sync/atomic"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/factory"
	"github.com/DonaldMurillo/gofastr/framework/hook"
	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
)

// factories.md promises the production pipeline: "BeforeCreate hooks →
// INSERT → AfterCreate hooks → event emission". Built from the documented
// factory.New(app.Registry, …), the factory constructed a bare
// crud.NewCrudHandler, which carries none of the hooks, events or JSON
// casing the App wires onto its own handlers, so a test seeding through
// the factory skipped every hook the app registered.
func TestFactoryFromAppRegistryRunsTheAppPipeline(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })

	app := framework.NewApp(framework.WithConfig(framework.AppConfig{JSONCase: crud.CaseSnake}), framework.WithDB(db))
	app.Entity("gadgets", entity.EntityConfig{
		Table: "gadgets",
		Fields: []schema.Field{
			{Name: "display_name", Type: schema.String, Required: true},
		},
	})
	if err := framework.AutoMigrate(db, app.Registry); err != nil {
		t.Fatal(err)
	}
	var before, after atomic.Int32
	app.HookRegistry("gadgets").RegisterHook(hook.BeforeCreate, func(context.Context, any) error {
		before.Add(1)
		return nil
	})
	app.HookRegistry("gadgets").RegisterHook(hook.AfterCreate, func(context.Context, any) error {
		after.Add(1)
		return nil
	})

	f, err := factory.New(app.Registry, "gadgets", func() map[string]any {
		return map[string]any{"display_name": "Widget"}
	})
	if err != nil {
		t.Fatal(err)
	}
	row, err := f.Create(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if before.Load() != 1 || after.Load() != 1 {
		t.Errorf("factory ran BeforeCreate %d and AfterCreate %d times, want once each", before.Load(), after.Load())
	}
	if _, ok := row["display_name"]; !ok {
		t.Errorf("factory row is not keyed in the app's snake_case: %v", row)
	}
}
