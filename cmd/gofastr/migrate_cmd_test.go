package main

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/gofastr/gofastr/core/migrate"
	_ "github.com/mattn/go-sqlite3"
)

func TestLoadMigrationFilesRegistersInOrder(t *testing.T) {
	dir := t.TempDir()
	migrationsDir := filepath.Join(dir, "migrations")
	if err := os.Mkdir(migrationsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeMigration(t, migrationsDir, "002_second.sql", 2, "second")
	writeMigration(t, migrationsDir, "001_first.sql", 1, "first")

	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	m := migrate.New(db, migrate.WithDialect(migrate.DialectSQLite))
	if err := loadMigrationFiles(m, migrationsDir); err != nil {
		t.Fatalf("loadMigrationFiles: %v", err)
	}
	status, err := m.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(status.Pending) != 2 || status.Pending[0].Version != 1 || status.Pending[1].Version != 2 {
		t.Fatalf("pending order = %#v", status.Pending)
	}
}

func TestMigratorFromArgsRunsSQLiteMigration(t *testing.T) {
	dir := t.TempDir()
	migrationsDir := filepath.Join(dir, "migrations")
	if err := os.Mkdir(migrationsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeMigration(t, migrationsDir, "001_create_posts.sql", 1, "create_posts")

	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(oldWD)
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	dbPath := filepath.Join(dir, "test.db")
	m, closeDB, err := migratorFromArgs([]string{"--db-url=" + dbPath})
	if err != nil {
		t.Fatalf("migratorFromArgs: %v", err)
	}
	defer closeDB()
	if err := m.Up(context.Background()); err != nil {
		t.Fatalf("Up: %v", err)
	}
	status, err := m.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(status.Applied) != 1 || len(status.Pending) != 0 {
		t.Fatalf("status = %#v", status)
	}
}

func writeMigration(t *testing.T, dir, name string, version int, migrationName string) {
	t.Helper()
	content := `-- +migrate Version ` + strconv.Itoa(version) + `
-- +migrate Name ` + migrationName + `
-- +migrate Up
CREATE TABLE IF NOT EXISTS posts (id TEXT PRIMARY KEY, title TEXT);
-- +migrate Down
DROP TABLE IF EXISTS posts;
`
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
