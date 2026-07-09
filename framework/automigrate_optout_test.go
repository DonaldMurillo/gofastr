package framework

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// startAndStop boots the app on an ephemeral port, waits for OnReady,
// and shuts it down — returning Start's error if it failed instead.
func startAndStop(t *testing.T, app *App) error {
	t.Helper()
	ready := make(chan struct{}, 1)
	app.OnReady(func(string) { ready <- struct{}{} })
	done := make(chan error, 1)
	go func() { done <- app.Start("127.0.0.1:0") }()
	select {
	case <-ready:
	case err := <-done:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("OnReady never fired")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = app.Shutdown(ctx)
	<-done
	return nil
}

func hasTable(t *testing.T, db *sql.DB, table string) bool {
	t.Helper()
	_, err := db.Exec("SELECT 1 FROM " + table + " LIMIT 1")
	return err == nil
}

func TestWithoutAutoMigrateSkipsDDL(t *testing.T) {
	db := openTestDB(t, "sqlite3")
	app := NewApp(WithDB(db), WithoutAutoMigrate(), WithoutDefaultMiddleware())
	app.Entity("gadgets", entity.EntityConfig{
		Table:  "gadgets",
		Fields: []schema.Field{{Name: "name", Type: schema.String}},
	}.WithTimestamps(false))

	if err := startAndStop(t, app); err != nil {
		t.Fatalf("start: %v", err)
	}
	if hasTable(t, db, "gadgets") {
		t.Fatal("WithoutAutoMigrate still created the entity table on Start")
	}
}

func TestAutoMigrateDefaultCreatesTables(t *testing.T) {
	db := openTestDB(t, "sqlite3")
	app := NewApp(WithDB(db), WithoutDefaultMiddleware())
	app.Entity("gadgets", entity.EntityConfig{
		Table:  "gadgets",
		Fields: []schema.Field{{Name: "name", Type: schema.String}},
	}.WithTimestamps(false))

	if err := startAndStop(t, app); err != nil {
		t.Fatalf("start: %v", err)
	}
	if !hasTable(t, db, "gadgets") {
		t.Fatal("default Start should auto-migrate the entity table")
	}
}
