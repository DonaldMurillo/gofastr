package framework

import (
	"context"
	"strings"
	"testing"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
)

// A transiently failing dialect probe must abort the WithSeed hook path, not
// silently take the unlocked non-Postgres branch — the same fail-closed
// invariant AutoMigrate and RunSeeds hold. Two Postgres replicas whose probe
// hiccuped would otherwise run non-serialized seed hooks.
func TestSeedHooksAbortWhenDialectUnknown(t *testing.T) {
	db, m, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for range 3 {
		m.ExpectQuery("SELECT version").WillReturnError(errTimeout{})
	}

	app := NewApp(WithoutDefaultMiddleware(), WithDB(db))
	ran := false
	app.WithSeed(func(context.Context) error { ran = true; return nil })

	err = app.runSeedHooksSerialized()
	if err == nil {
		t.Fatal("expected an error from the unknown-dialect seed path, got nil")
	}
	if !strings.Contains(err.Error(), "dialect") {
		t.Errorf("error must name dialect detection, got: %v", err)
	}
	if ran {
		t.Error("seed hook ran despite the dialect being unknown")
	}
}

// errTimeout looks transient to the probe classifier (i/o timeout).
type errTimeout struct{}

func (errTimeout) Error() string { return "i/o timeout" }

// A Postgres pool pinned to one connection cannot hold the seed advisory
// lock without deadlocking the hooks' own queries, so the hooks run
// unlocked with a loud warning rather than hanging the boot.
func TestSeedHooksWarnOnSingleConnPostgres(t *testing.T) {
	db, m, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	for range 4 {
		m.ExpectQuery("SELECT version").WillReturnRows(m.NewRows([]string{"v"}).AddRow("PostgreSQL 16.2"))
	}

	app := NewApp(WithoutDefaultMiddleware(), WithDB(db))
	ran := false
	app.WithSeed(func(context.Context) error { ran = true; return nil })

	if err := app.runSeedHooksSerialized(); err != nil {
		t.Fatalf("single-conn Postgres seed path errored: %v", err)
	}
	if !ran {
		t.Error("seed hook did not run on the unlocked single-connection path")
	}
}

// No database at all: the hooks still run (single process, nothing to
// serialize against).
func TestSeedHooksRunWithoutDB(t *testing.T) {
	app := NewApp(WithoutDefaultMiddleware())
	ran := false
	app.WithSeed(func(context.Context) error { ran = true; return nil })
	if err := app.runSeedHooksSerialized(); err != nil {
		t.Fatalf("no-DB seed path errored: %v", err)
	}
	if !ran {
		t.Error("seed hook did not run without a database")
	}
}
