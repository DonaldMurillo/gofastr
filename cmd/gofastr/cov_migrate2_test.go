package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/migrate"
	_ "github.com/mattn/go-sqlite3"
)

func TestRunMigrateUpCreateDBBadDriverExits(t *testing.T) {
	dir := covT_migrationsDir(t)
	code := covT_capExit(t, func() {
		covT_capStdout(t, func() {
			runMigrateUp([]string{"--create-db", "--driver=nonexistent", "--db-url=" + filepath.Join(dir, "x.db")})
		})
	})
	if code != 1 {
		t.Fatalf("want 1 got %d", code)
	}
}

func TestLoadMigrationFilesMalformedErrors(t *testing.T) {
	dir := t.TempDir()
	md := filepath.Join(dir, "migrations")
	if err := os.Mkdir(md, 0o755); err != nil {
		t.Fatal(err)
	}
	// A .sql file with no migrate directives → RegisterFromReader errors.
	if err := os.WriteFile(filepath.Join(md, "001_bad.sql"), []byte("not a migration\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := migrate.New(nil, migrate.WithDialect(migrate.DialectSQLite))
	if err := loadMigrationFiles(m, md); err == nil {
		t.Fatal("malformed migration should error")
	}
	// Missing dir → ReadDir error.
	if err := loadMigrationFiles(m, filepath.Join(dir, "nope")); err == nil {
		t.Fatal("missing dir should error")
	}
}

func TestOpenDiffDBPostgresBranch(t *testing.T) {
	// postgres:// URL forces the postgres driver path; with no server it
	// fails at open/ping — but the postgres branch is exercised.
	if _, err := openDiffDB("postgres://user@127.0.0.1:1/db?sslmode=disable", "sqlite3"); err == nil {
		t.Fatal("expected open/ping failure for unreachable postgres")
	}
}
