package framework

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/datexport"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// The auth battery exports its user table with a name column the user
// entity fields do not declare. When the user table was also an app
// entity, the entity source won and the exporter was skipped whole, so
// the column never reached the archive and a restore brought every
// account back with no name (2026-10-27 S4).
func TestExportKeepsExporterColumnsOfEntity(t *testing.T) {
	datexport.Reset(t)
	db := openSQLiteMem(t)
	defer db.Close()
	app := NewApp(WithDB(db))
	if err := app.Registry.Register(entity.Define("users", entity.EntityConfig{
		Table:  "users",
		Fields: []schema.Field{{Name: "email", Type: schema.String}},
	})); err != nil {
		t.Fatal(err)
	}
	if err := AutoMigrate(db, app.Registry); err != nil {
		t.Fatal(err)
	}
	// The battery's EnsureSchema adds its own column to the entity table.
	if _, err := db.Exec(`ALTER TABLE users ADD COLUMN name TEXT`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO users (id, email, name, created_at, updated_at)
		VALUES ('u1', 'ada@example.test', 'Ada', '2024-01-01T00:00:00.000000Z', '2024-01-01T00:00:00.000000Z')`); err != nil {
		t.Fatal(err)
	}
	// Table and columns spelled as the unquoted DDL folds them: the same
	// table, and EMAIL and Name are email and name.
	datexport.Register(datexport.DataExporter{
		Name: "auth_users", Source: "auth", Table: "Users", PrimaryKey: "id",
		Columns: []string{"id", "EMAIL", "name", "Name"},
	})

	dir := t.TempDir()
	if err := app.ExportData(context.Background(), dir, WithExportTime(fixedExportTime())); err != nil {
		t.Fatalf("ExportData: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "auth_users.ndjson")); !os.IsNotExist(err) {
		t.Errorf("the table was exported twice, as users and auth_users (stat: %v)", err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m exportManifest
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	for _, e := range m.Entities {
		if e.Name != "users" {
			continue
		}
		seen := map[string]bool{}
		for _, c := range e.Columns {
			if seen[strings.ToLower(c)] {
				t.Errorf("users exports column %q twice: %v", c, e.Columns)
			}
			seen[strings.ToLower(c)] = true
		}
		if !seen["name"] {
			t.Errorf("users exports %v, without the exporter's name column", e.Columns)
		}
	}
	if _, err := db.Exec(`DELETE FROM users`); err != nil {
		t.Fatal(err)
	}
	if err := app.ImportData(context.Background(), dir); err != nil {
		t.Fatalf("ImportData: %v", err)
	}
	rows := readAllRows(t, db, "users", []string{"id", "email", "name"})
	if len(rows) != 1 {
		t.Fatalf("restored %d users, want 1", len(rows))
	}
	if got := rows[0]["name"]; got != "Ada" {
		t.Errorf("restored name = %v, want Ada", got)
	}
	if got := rows[0]["email"]; got != "ada@example.test" {
		t.Errorf("restored email = %v, want ada@example.test", got)
	}
}

// An entity column keeps its declared casing, so the fold applies to the
// columns already held as well as to the ones added.
func TestAppendFoldUniqueFoldsHeldColumns(t *testing.T) {
	got := appendFoldUnique([]string{"id", "Email"}, []string{"email", "name"})
	if want := []string{"id", "Email", "name"}; !slices.Equal(got, want) {
		t.Errorf("appendFoldUnique = %v, want %v", got, want)
	}
}
