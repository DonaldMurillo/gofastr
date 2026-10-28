package outbox_test

import (
	"bufio"
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/outbox"
	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
)

func openOutboxDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// Erasing a user blanks the payload of every outbox row staged for them,
// so a replay no longer delivers their records, and keeps the rows and
// everyone else's payloads (S10). A data export carries the table.
func TestEraseBlanksOwnedOutboxPayloads(t *testing.T) {
	db := openOutboxDB(t)
	ob, err := outbox.New(db, outbox.WithTable("events"))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	ids := map[string]string{}
	for _, owner := range []string{"victim", "bystander"} {
		id, err := ob.AppendFor(ctx, db, owner, "note.created", map[string]any{"body": owner + "-secret"})
		if err != nil {
			t.Fatalf("AppendFor: %v", err)
		}
		ids[owner] = id
	}
	if _, err := ob.Append(ctx, db, "system.tick", map[string]any{"n": 1}); err != nil {
		t.Fatalf("Append: %v", err)
	}

	app := framework.NewApp(framework.WithDB(db))
	dir := t.TempDir()
	if err := app.ExportData(ctx, dir); err != nil {
		t.Fatalf("ExportData: %v", err)
	}
	if f, err := os.Open(filepath.Join(dir, "event_outbox-events.ndjson")); err != nil {
		t.Errorf("the export has no event_outbox: %v", err)
	} else {
		n := 0
		for sc := bufio.NewScanner(f); sc.Scan(); {
			n++
		}
		_ = f.Close()
		if n != 3 {
			t.Errorf("export holds %d outbox row(s), want 3", n)
		}
	}
	if _, err := app.EraseUserData(ctx, "victim"); err != nil {
		t.Fatalf("EraseUserData: %v", err)
	}
	payload := func(id string) string {
		t.Helper()
		var p sql.NullString
		if err := db.QueryRowContext(ctx, `SELECT payload FROM events WHERE id = $1`, id).Scan(&p); err != nil {
			t.Fatalf("read %s: %v", id, err)
		}
		return p.String
	}
	if got := payload(ids["victim"]); got != "null" {
		t.Errorf("the erased user's outbox payload = %q, want null", got)
	}
	if got := payload(ids["bystander"]); got != `{"body":"bystander-secret"}` {
		t.Errorf("another user's payload = %q", got)
	}
}

// An outbox table created before the owner column gains it at boot, so
// AppendFor works on an upgraded database.
func TestOutboxAddsOwnerColumn(t *testing.T) {
	db := openOutboxDB(t)
	if _, err := db.Exec(`CREATE TABLE event_outbox (
		id TEXT PRIMARY KEY, type TEXT NOT NULL, payload TEXT,
		status TEXT NOT NULL DEFAULT 'pending', attempts INTEGER NOT NULL DEFAULT 0,
		last_error TEXT, created_at DATETIME NOT NULL, dispatched_at DATETIME,
		next_attempt_at DATETIME, claimed_until DATETIME)`); err != nil {
		t.Fatal(err)
	}
	for range 2 { // a second boot finds the column already there
		ob, err := outbox.New(db)
		if err != nil {
			t.Fatalf("New on a pre-owner table: %v", err)
		}
		if _, err := ob.AppendFor(context.Background(), db, "u1", "x", nil); err != nil {
			t.Fatalf("AppendFor: %v", err)
		}
	}
}
