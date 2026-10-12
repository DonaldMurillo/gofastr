package framework

import (
	"context"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
)

func sharedKeysApp(t *testing.T) *App {
	t.Helper()
	db := openSQLiteMem(t)
	app := NewApp(WithDB(db))
	app.Registry.Register(entity.Define("profiles", entity.EntityConfig{
		Table: "profiles",
		Fields: []schema.Field{
			{Name: "avatar", Type: schema.Image},
			{Name: "avatar_variants", Type: schema.JSON},
		},
	}))
	if err := AutoMigrate(db, app.Registry); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}
	return app
}

func TestUnsharedKeysSkipsExternalURLs(t *testing.T) {
	app := sharedKeysApp(t)
	got, err := app.unsharedObjectKeys(context.Background(), migrate.DialectSQLite,
		[]string{"https://cdn.example.com/a.png", "http://x.test/b.png"})
	if err != nil || got != nil {
		t.Fatalf("got %v, %v; want nil, nil", got, err)
	}
}

func TestFileRefColumnsNilRegistry(t *testing.T) {
	app := &App{}
	got, err := app.collectFileRefColumns(context.Background(), migrate.DialectSQLite)
	if err != nil || got != nil {
		t.Fatalf("got %v, %v; want nil, nil", got, err)
	}
}

func TestUnsharedKeysNamedByFileColumn(t *testing.T) {
	app := sharedKeysApp(t)
	ctx := context.Background()
	if _, err := app.DB.ExecContext(ctx, `INSERT INTO profiles (id, avatar) VALUES ('p1', 'kept/a.png')`); err != nil {
		t.Fatalf("insert: %v", err)
	}
	// Every candidate is named by the file column, so the variants
	// pass has nothing left to check.
	got, err := app.unsharedObjectKeys(ctx, migrate.DialectSQLite, []string{"kept/a.png"})
	if err != nil || len(got) != 0 {
		t.Fatalf("got %v, %v; want none", got, err)
	}
}

func TestVariantsScanAllWithoutLikeRun(t *testing.T) {
	app := sharedKeysApp(t)
	ctx := context.Background()
	if _, err := app.DB.ExecContext(ctx,
		`INSERT INTO profiles (id, avatar_variants) VALUES ('p1', '[{"storage_ref":"///"}]')`); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if run := likeRun("///"); run != "" {
		t.Fatalf("likeRun(///) = %q; want empty", run)
	}
	got, err := app.unsharedObjectKeys(ctx, migrate.DialectSQLite, []string{"///", "gone/b.png"})
	if err != nil {
		t.Fatalf("unsharedObjectKeys: %v", err)
	}
	if len(got) != 1 || got[0] != "gone/b.png" {
		t.Fatalf("got %v; want [gone/b.png]", got)
	}
}

func TestUnsharedKeysClosedDBErrors(t *testing.T) {
	app := sharedKeysApp(t)
	ctx := context.Background()
	app.DB.Close()
	if _, err := app.unsharedObjectKeys(ctx, migrate.DialectSQLite, []string{"a/b.png"}); err == nil {
		t.Fatal("want an error from a closed DB")
	}
	named := map[string]bool{}
	if err := app.markNamedInColumn(ctx, `"profiles"`, `"avatar"`, []string{"a/b.png"}, named); err == nil {
		t.Fatal("markNamedInColumn: want an error from a closed DB")
	}
	if err := app.markNamedInVariants(ctx, `"profiles"`, `"avatar_variants"`, []string{"a/b.png"}, named); err == nil {
		t.Fatal("markNamedInVariants: want an error from a closed DB")
	}
}
