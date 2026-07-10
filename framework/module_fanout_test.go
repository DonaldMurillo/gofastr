package framework

import (
	"context"
	"database/sql"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/DonaldMurillo/gofastr/core/fanout"
)

func TestModuleFanoutPropagation(t *testing.T) {
	// Shared DB so both replicas' module stores see the same persisted state.
	// After M2, the fanout message is a refresh SIGNAL — the receiving
	// replica re-reads from its store, so the store must be shared.
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)

	f := fanout.NewInProcess()

	app1 := NewApp(WithDB(db), WithFanout(f))
	app1.RegisterModule(&modStub{name: "m1", manifest: ModuleManifest{}, init: noopInit})
	if err := app1.InitPlugins(); err != nil {
		t.Fatalf("app1 InitPlugins: %v", err)
	}

	app2 := NewApp(WithDB(db), WithFanout(f))
	app2.RegisterModule(&modStub{name: "m1", manifest: ModuleManifest{}, init: noopInit})
	if err := app2.InitPlugins(); err != nil {
		t.Fatalf("app2 InitPlugins: %v", err)
	}

	// Both enabled by default.
	if !app1.Modules().Enabled("m1") || !app2.Modules().Enabled("m1") {
		t.Fatal("both should be enabled")
	}

	// Disable on app1 → app2 should see the change via fanout (refresh
	// from the shared store).
	if err := app1.Modules().Disable(context.Background(), "m1"); err != nil {
		t.Fatalf("Disable: %v", err)
	}

	// Give the fanout delivery goroutine time to fire.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if !app2.Modules().Enabled("m1") {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if app2.Modules().Enabled("m1") {
		t.Fatal("app2 did not receive the disable via fanout")
	}

	// Re-enable on app1 → app2 should see it too.
	app1.Modules().Enable(context.Background(), "m1")
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if app2.Modules().Enabled("m1") {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !app2.Modules().Enabled("m1") {
		t.Fatal("app2 did not receive the re-enable via fanout")
	}
}
