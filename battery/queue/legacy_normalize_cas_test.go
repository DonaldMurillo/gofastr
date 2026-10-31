package queue

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/query"
	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
)

func openLegacyQueue(t *testing.T) (*sql.DB, *DBQueue) {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(1)
	q, err := NewDBQueue(db)
	if err != nil {
		t.Fatal(err)
	}
	return db, q
}

// The normalizer read every row, then wrote each back keyed on its id
// alone. A worker that claimed the job in between had its fresh lease
// replaced with the stale one read before, so a second worker could
// claim it at once (2026-10-27 B10). Each write is guarded on the text it
// read.
func TestNormalizeKeepsLeaseClaimedMeanwhile(t *testing.T) {
	db, q := openLegacyQueue(t)
	ctx := context.Background()
	layout := query.ProbeSQLiteBindLayout(ctx, db)
	other := time.RFC3339Nano // whichever layout the driver does not bind
	if layout == other {
		other = legacyTimeLayout
	}
	stale := time.Now().Add(-time.Hour).UTC()
	if _, err := db.Exec(`INSERT INTO "queue_jobs"
		(id, occurrence_id, type, payload, priority, lane, attempts, max_attempts,
		 created_at, scheduled_at, status, claimed_at)
		VALUES ('j1','', 'work','null',0,'',1,3,?,?,'claimed',?)`,
		stale.Format(other), stale.Format(other), stale.Format(other)); err != nil {
		t.Fatal(err)
	}
	var read string
	if err := db.QueryRow(`SELECT CAST(claimed_at AS TEXT) FROM "queue_jobs" WHERE id='j1'`).Scan(&read); err != nil {
		t.Fatal(err)
	}
	fixes, err := query.SQLiteTimeFixes(layout, []query.StoredTime{{Col: "claimed_at", Raw: read}, {Col: "created_at", Raw: read}})
	if err != nil || len(fixes) != 2 {
		t.Fatalf("fixes = %v (%v), want claimed_at and created_at", fixes, err)
	}

	fresh := time.Now().UTC().Truncate(time.Microsecond)
	if _, err := db.Exec(`UPDATE "queue_jobs" SET claimed_at=$1 WHERE id='j1'`, fresh); err != nil {
		t.Fatal(err) // a worker's claim lands between the read and the write
	}
	if err := q.applyTimeFixes(ctx, q.qt(), "id", "j1", fixes); err != nil {
		t.Fatal(err)
	}

	var claimed, created any
	if err := db.QueryRow(`SELECT claimed_at, created_at FROM "queue_jobs" WHERE id='j1'`).Scan(&claimed, &created); err != nil {
		t.Fatal(err)
	}
	got, err := query.ParseDBTime(claimed)
	if err != nil || !got.Equal(fresh) {
		t.Fatalf("claimed_at = %v (%v), want the worker's fresh lease %v", got, err, fresh)
	}
	var createdText string
	if err := db.QueryRow(`SELECT CAST(created_at AS TEXT) FROM "queue_jobs" WHERE id='j1'`).Scan(&createdText); err != nil {
		t.Fatal(err)
	}
	if want := stale.Format(layout); createdText != want {
		t.Fatalf("created_at = %q, want the untouched column rewritten to %q", createdText, want)
	}
}

// A row the driver wrote is already canonical, so a boot rewrites
// nothing. The normalizer read DATETIME columns as the driver's parsed
// time.Time, which never equals the canonical text, and rewrote every
// timed row of every queue table on every boot.
func TestNormalizeSkipsCanonicalRows(t *testing.T) {
	db, q := openLegacyQueue(t)
	now := time.Now().UTC()
	if _, err := db.Exec(`INSERT INTO "queue_jobs"
		(id, occurrence_id, type, payload, priority, lane, attempts, max_attempts,
		 created_at, scheduled_at, status, claimed_at)
		VALUES ('j1','', 'work','null',0,'',1,3,?,?,'claimed',?)`, now, now, now); err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{
		`CREATE TABLE writes (n INTEGER)`,
		`CREATE TRIGGER count_writes AFTER UPDATE ON "queue_jobs" BEGIN INSERT INTO writes VALUES (1); END`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	if err := q.normalizeLegacyTimestamps(context.Background()); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM writes`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("normalizing a canonical row issued %d writes, want none", n)
	}
}
