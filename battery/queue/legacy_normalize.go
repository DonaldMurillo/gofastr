package queue

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/query"
)

// normalizeLegacyTimestamps rewrites space-separated (legacy mattn/go-sqlite3)
// timestamp strings in the time columns of the queue and scheduler tables to
// the canonical text the pure driver binds for time.Time. SQLite stores these
// columns as TEXT, and the queue's claim/lease/scheduled_at predicates
// (claimed_at <= $1, scheduled_at <= $2, expires_at <= $3, next_run <= $4)
// compare them lexicographically. A same-day legacy value like
// '2026-07-20 23:59:59+00:00' sorts BEFORE the canonical same-day value
// '2026-07-20T23:59:59…Z' because space (0x20) precedes 'T' (0x54), so an
// un-expired FUTURE lease compares as expired and is reclaimed (double
// delivery of an in-flight job), and a not-yet-due scheduled_at compares as
// due (retry backoff voided). Same root cause the outbox fixed in 84b0e167;
// this is the local port for the queue tables.
//
// Postgres stores real TIMESTAMPTZ values, so this is a sqlite-only no-op
// there. Idempotent: a row whose stored value already round-trips through
// query.ParseDBTime to the canonical layout is skipped. Runs from NewDBQueue
// (after schema ensure/migrations) and NewDurableScheduler.ensureTables
// (after the scheduler tables are created) so the very first claim query of
// a freshly-opened upgraded DB sees canonical values. query.ParseDBTime in scanJob,
// loadDue and nextWakeDelay remains as the post-scan safety net for any
// value written after construction by a non-pure driver sharing the file.
func (q *DBQueue) normalizeLegacyTimestamps(ctx context.Context) error {
	if q.dialect != dialectSQLite {
		return nil
	}
	layout := query.ProbeSQLiteBindLayout(ctx, q.db)
	if err := q.normalizeTimesFor(ctx, layout, q.qt(), "id", "created_at", "scheduled_at", "claimed_at"); err != nil {
		return err
	}
	if err := q.normalizeTimesFor(ctx, layout, q.schedulerSchedulesTable(), "id", "next_run", "updated_at"); err != nil {
		return err
	}
	if err := q.normalizeTimesFor(ctx, layout, q.schedulerOccurrencesTable(), "occurrence_id", "scheduled_tick", "created_at"); err != nil {
		return err
	}
	return q.normalizeTimesFor(ctx, layout, q.schedulerLeaseTable(), "name", "expires_at", "heartbeat_at")
}

// normalizeTimesFor canonicalizes the named time columns of one single-key
// table: layout is the text the driver binds a time.Time as, table the
// already-quoted table name, keyCol the column the UPDATE keys on, and
// timeCols the time columns to rewrite. The columns are read as stored
// text: the driver hands a DATETIME column back as a time.Time, which
// never equals the canonical text, so every row was rewritten on every
// boot. NULL values are
// preserved (no SET fragment; claimed_at and the lease columns are
// nullable). Reads are fully drained before any UPDATE: the queue is
// typically opened with SetMaxOpenConns(1) and an UPDATE issued while the
// SELECT's rows cursor still holds the one connection deadlocks.
//
// It collapses the four former per-table copies (normalizeQueueJobsTimes,
// normalizeSchedulesTimes, normalizeOccurrencesTimes, normalizeLeaseTimes),
// which differed only in table, key, and column names.
func (q *DBQueue) normalizeTimesFor(ctx context.Context, layout, table, keyCol string, timeCols ...string) error {
	if !q.queueTableExists(ctx, table) {
		return nil
	}
	selectCols := make([]string, 0, len(timeCols)+1)
	selectCols = append(selectCols, keyCol)
	for _, col := range timeCols {
		selectCols = append(selectCols, fmt.Sprintf("CAST(%s AS TEXT)", col))
	}
	rows, err := q.db.QueryContext(ctx, fmt.Sprintf(
		`SELECT %s FROM %s`, strings.Join(selectCols, ", "), table))
	if err != nil {
		return err
	}
	type keyRow struct {
		key  string
		vals []any
	}
	var collected []keyRow
	for rows.Next() {
		var r keyRow
		r.vals = make([]any, len(timeCols))
		dest := make([]any, 0, len(timeCols)+1)
		dest = append(dest, &r.key)
		for i := range r.vals {
			dest = append(dest, &r.vals[i])
		}
		if err := rows.Scan(dest...); err != nil {
			rows.Close()
			return err
		}
		collected = append(collected, r)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, r := range collected {
		cols := make([]query.StoredTime, len(timeCols))
		for i, name := range timeCols {
			cols[i] = query.StoredTime{Col: name, Raw: r.vals[i]}
		}
		fixes, err := query.SQLiteTimeFixes(layout, cols)
		if err != nil {
			return fmt.Errorf("queue: %w", err)
		}
		if err := q.applyTimeFixes(ctx, table, keyCol, r.key, fixes); err != nil {
			return err
		}
	}
	return nil
}

// applyTimeFixes writes one row's fixes, a statement per column, each
// guarded on the text the column held when it was read. The read and the
// writes are not one transaction, and a worker that claimed the row in
// between wrote its fresh lease in the driver's own layout: an unguarded
// write put the stale lease back, and a second worker could claim the job
// at once (2026-10-27 B10). One column per statement, so a column the
// worker rewrote does not hold back the others.
func (q *DBQueue) applyTimeFixes(ctx context.Context, table, keyCol, key string, fixes []query.TimeFix) error {
	for _, f := range fixes {
		stmt := fmt.Sprintf(`UPDATE %s SET %s=$1 WHERE %s=$2 AND CAST(%s AS TEXT)=$3`,
			table, f.Col, keyCol, f.Col)
		if _, err := q.db.ExecContext(ctx, stmt, f.At, key, f.Read); err != nil {
			return err
		}
	}
	return nil
}

// queueTableExists reports whether the already-quoted table is queryable.
// The probe is a SELECT … LIMIT 1 so it works on both sqlite drivers (the
// pure driver implements PRAGMA but does not expose sqlite_master, and we
// want one code path). Errors are treated as "absent": ensureTable /
// ensureTables has already run by the time normalization is reached, so a
// real error here most likely means the table genuinely doesn't exist yet
// (e.g., NewDBQueue before any DurableScheduler has been constructed, the
// scheduler tables are absent), and there is nothing to normalize.
func (q *DBQueue) queueTableExists(ctx context.Context, quotedTable string) bool {
	var n int
	err := q.db.QueryRowContext(ctx, fmt.Sprintf("SELECT 1 FROM %s LIMIT 1", quotedTable)).Scan(&n)
	if err == nil || err == sql.ErrNoRows {
		return true
	}
	return false
}
