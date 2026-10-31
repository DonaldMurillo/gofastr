package framework

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/datexport"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
	"github.com/DonaldMurillo/gofastr/internal/pgtest"
)

// timeTextApp is a SQLite app with an entity holding a Timestamp and a
// Date, and two battery-shaped tables. leases has a DATETIME the driver
// writes from a bound time.Time, a TIMESTAMP and a DATE holding
// hand-written spellings, and DATETIMEs holding a unix integer and a
// blob. beats has no key, so the export reads it in one statement.
func timeTextApp(t *testing.T) (*App, *sql.DB) {
	t.Helper()
	db := openSQLiteMem(t)
	t.Cleanup(func() { _ = db.Close() })
	app := NewApp(WithDB(db))
	app.Registry.Register(entity.Define("events", entity.EntityConfig{
		Table: "events",
		Fields: []schema.Field{
			{Name: "at", Type: schema.Timestamp},
			{Name: "day", Type: schema.Date},
		},
	}.WithTimestamps(false)))
	if err := AutoMigrate(db, app.Registry); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE leases (id TEXT PRIMARY KEY, expires_at DATETIME, renewed_at TIMESTAMP, due DATE, ticks DATETIME, stamp DATETIME)`); err != nil {
		t.Fatal(err)
	}
	datexport.Register(datexport.DataExporter{
		Name: "leases", Source: "test", Table: "leases",
		PrimaryKey: "id", Columns: []string{"id", "expires_at", "renewed_at", "due", "ticks", "stamp"},
	})
	t.Cleanup(func() { datexport.Unregister("leases") })
	if _, err := db.Exec(`CREATE TABLE beats (id TEXT, at DATETIME)`); err != nil {
		t.Fatal(err)
	}
	datexport.Register(datexport.DataExporter{
		Name: "beats", Source: "test", Table: "beats", Columns: []string{"id", "at"},
	})
	t.Cleanup(func() { datexport.Unregister("beats") })
	return app, db
}

// storedText is column c of every row of table as SQLite holds it, by id.
func storedText(t *testing.T, db *sql.DB, table, c string) map[string]string {
	t.Helper()
	rows, err := db.Query(`SELECT id, CAST(` + c + ` AS TEXT) FROM ` + table)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var id string
		var v sql.NullString
		if err := rows.Scan(&id, &v); err != nil {
			t.Fatal(err)
		}
		out[id] = v.String
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// A SQLite restore writes every timestamp back as it was stored. The
// export formatted what the driver parsed with RFC3339Nano, which trims
// trailing zeros, so a Timestamp crud stored as "...05.120000Z" came back
// "...05.12Z": SQLite compares the text byte by byte, so a filter bound
// in the canonical layout missed it and same-second rows reordered
// (2026-10-27 D12). A battery's DATETIME, written from a time.Time in
// the driver's own spelling, came back in another.
func TestSQLiteRestoreKeepsTimestampText(t *testing.T) {
	app, db := timeTextApp(t)
	at := []string{"2024-01-03T00:00:05.120000Z", "2024-01-03T00:00:05.000000Z", "2024-01-03T00:00:05.123456Z"}
	for i, v := range at {
		if _, err := db.Exec(`INSERT INTO events (id, at, day) VALUES ($1, $2, $3)`, string(rune('a'+i)), v, "2024-01-0"+string(rune('3'+i))); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`INSERT INTO events (id, at, day) VALUES ('n', NULL, NULL)`); err != nil {
		t.Fatal(err)
	}
	lease := time.Date(2024, 1, 3, 10, 0, 0, 500_000_000, time.UTC)
	if _, err := db.Exec(`INSERT INTO leases (id, expires_at, renewed_at, due, ticks, stamp) VALUES ('l1', $1, '2024-01-03 10:00', '2024-01-03T10:00:00Z', 1704276000, x'00ff')`, lease); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO beats (id, at) VALUES ('b1', $1)`, lease); err != nil {
		t.Fatal(err)
	}
	cols := []struct{ table, col string }{
		{"events", "at"}, {"events", "day"},
		{"leases", "expires_at"}, {"leases", "renewed_at"}, {"leases", "due"},
		{"beats", "at"},
	}
	want := make([]map[string]string, len(cols))
	for i, c := range cols {
		want[i] = storedText(t, db, c.table, c.col)
	}

	dir := t.TempDir()
	ctx := context.Background()
	if err := app.ExportData(ctx, dir); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"events", "leases", "beats"} {
		if _, err := db.Exec(`DELETE FROM ` + table); err != nil {
			t.Fatal(err)
		}
	}
	if err := app.ImportData(ctx, dir); err != nil {
		t.Fatal(err)
	}
	for i, c := range cols {
		got := storedText(t, db, c.table, c.col)
		for id, w := range want[i] {
			if got[id] != w {
				t.Errorf("%s.%s of %s = %q after restore, stored %q", c.table, c.col, id, got[id], w)
			}
		}
	}
	// A number or a blob in a time column keeps its type.
	var ticks, stamp, stampHex string
	if err := db.QueryRow(`SELECT typeof(ticks), typeof(stamp), hex(stamp) FROM leases`).Scan(&ticks, &stamp, &stampHex); err != nil {
		t.Fatal(err)
	}
	if ticks != "integer" || stamp != "blob" || stampHex != "00FF" {
		t.Errorf("restored ticks %s, stamp %s %s; want integer, blob 00FF", ticks, stamp, stampHex)
	}
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM events WHERE at = $1`, schema.FormatTimestamp(time.Date(2024, 1, 3, 0, 0, 5, 120_000_000, time.UTC))).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("a canonical equality filter matches %d restored rows, want 1", n)
	}
}

// An archive an earlier version wrote holds the trimmed spellings. Its
// declared Timestamp and Date columns restore in the layouts crud writes,
// so filters and cursors match them; a value of another shape is kept.
func TestImportCanonicalizesEntityTimes(t *testing.T) {
	app, db := timeTextApp(t)
	dir := t.TempDir()
	ctx := context.Background()
	if err := app.ExportData(ctx, dir); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "events.ndjson")
	sum, err := writeNDJSON(path, []map[string]any{
		{"id": "a", "at": "2024-01-03T00:00:05.12Z", "day": "2024-01-03T00:00:00Z"},
		{"id": "b", "at": "2024-01-03T02:00:05+02:00", "day": "2024-01-04"},
		{"id": "c", "at": "not a time", "day": "2024-01-05T10:00:00Z"},
	})
	if err != nil {
		t.Fatal(err)
	}
	m := readManifest(t, dir)
	for i := range m.Entities {
		if m.Entities[i].Name == "events" {
			m.Entities[i].SHA256, m.Entities[i].RowCount = sum, 3
		}
	}
	writeManifest(t, dir, m)
	if err := app.ImportData(ctx, dir); err != nil {
		t.Fatal(err)
	}
	gotAt, gotDay := storedText(t, db, "events", "at"), storedText(t, db, "events", "day")
	for id, want := range map[string]string{"a": "2024-01-03T00:00:05.120000Z", "b": "2024-01-03T00:00:05.000000Z", "c": "not a time"} {
		if gotAt[id] != want {
			t.Errorf("at of %s = %q, want %q", id, gotAt[id], want)
		}
	}
	// A date-time that is not a midnight is not a date the archive wrote
	// for a Date column, so it is kept as it is.
	for id, want := range map[string]string{"a": "2024-01-03", "b": "2024-01-04", "c": "2024-01-05T10:00:00Z"} {
		if gotDay[id] != want {
			t.Errorf("day of %s = %q, want %q", id, gotDay[id], want)
		}
	}
}

// Postgres hands the export a time.Time for TIMESTAMPTZ and DATE. They
// leave in the layouts crud writes on SQLite, so a cross-dialect restore
// lands filterable.
func TestPostgresExportSpellsTimesCanonically(t *testing.T) {
	db := pgtest.DB(t)
	if _, err := db.Exec(`CREATE TABLE events (id TEXT PRIMARY KEY, at TIMESTAMPTZ, day DATE)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO events (id, at, day) VALUES ('a', '2024-01-03T00:00:05.12Z', '2024-01-03')`); err != nil {
		t.Fatal(err)
	}
	rows, err := rawReadAll(context.Background(), db, migrate.DialectPostgres, "events", []string{"id", "at", "day"}, []string{"id"}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0]["at"] != "2024-01-03T00:00:05.120000Z" || rows[0]["day"] != "2024-01-03" {
		t.Fatalf("exported %v, want at 2024-01-03T00:00:05.120000Z and day 2024-01-03", rows)
	}
}
