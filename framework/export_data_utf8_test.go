package framework

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/datexport"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// ExportData's contract is a faithful raw dump: a
// value it cannot write as JSON fails the export (a NaN or Inf cell names
// its table, row and column) and is "never silently coerced". A SQLite
// TEXT cell whose bytes are not valid UTF-8 has no JSON string spelling
// either, but the export handed it to encoding/json, which replaces each
// bad byte with U+FFFD: the archive checksummed, the import succeeded,
// and the restored row held different bytes than the source did. The
// export now refuses the cell the way it refuses a non-finite float.
func exportNotesApp(t *testing.T) (*App, *sql.DB) {
	t.Helper()
	db := openSQLiteMem(t)
	app := NewApp(WithDB(db))
	app.Registry.Register(entity.Define("notes", entity.EntityConfig{
		Table:  "notes",
		Fields: []schema.Field{{Name: "body", Type: schema.Text}},
	}.WithTimestamps(false)))
	if err := AutoMigrate(db, app.Registry); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}
	return app, db
}

func TestExportRefusesNonUTF8Text(t *testing.T) {
	datexport.Reset(t)
	ctx := context.Background()
	src, srcDB := exportNotesApp(t)
	if _, err := srcDB.ExecContext(ctx, `INSERT INTO notes (id, body) VALUES ('n0', $1)`, "café au lait"); err != nil {
		t.Fatal(err)
	}
	if err := src.ExportData(ctx, t.TempDir()); err != nil {
		t.Fatalf("export of valid UTF-8 text: %v", err)
	}
	// Latin-1 "café": 0xE9 is not valid UTF-8 on its own.
	if _, err := srcDB.ExecContext(ctx, `INSERT INTO notes (id, body) VALUES ('n1', $1)`, "caf\xe9 au lait"); err != nil {
		t.Fatal(err)
	}
	err := src.ExportData(ctx, t.TempDir())
	if err == nil {
		t.Fatal("export wrote a text cell that is not valid UTF-8; encoding/json replaces its bad bytes with U+FFFD")
	}
	for _, want := range []string{"notes", "row 2", `column "body"`, "not valid UTF-8"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("export error = %q, want it to name %q", err, want)
		}
	}
}
