package queue

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

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
	if err := q.normalizeTimesFor(ctx, q.qt(), "id", "created_at", "scheduled_at", "claimed_at"); err != nil {
		return err
	}
	if err := q.normalizeTimesFor(ctx, q.schedulerSchedulesTable(), "id", "next_run", "updated_at"); err != nil {
		return err
	}
	if err := q.normalizeTimesFor(ctx, q.schedulerOccurrencesTable(), "occurrence_id", "scheduled_tick", "created_at"); err != nil {
		return err
	}
	return q.normalizeTimesFor(ctx, q.schedulerLeaseTable(), "name", "expires_at", "heartbeat_at")
}

// normalizeTimesFor canonicalizes the named time columns of one single-key
// table: table is the already-quoted table name, keyCol the column the
// UPDATE keys on, and timeCols the time columns to rewrite. NULL values are
// preserved (no SET fragment; claimed_at and the lease columns are
// nullable). Reads are fully drained before any UPDATE: the queue is
// typically opened with SetMaxOpenConns(1) and an UPDATE issued while the
// SELECT's rows cursor still holds the one connection deadlocks.
//
// It collapses the four former per-table copies (normalizeQueueJobsTimes,
// normalizeSchedulesTimes, normalizeOccurrencesTimes, normalizeLeaseTimes),
// which differed only in table, key, and column names.
func (q *DBQueue) normalizeTimesFor(ctx context.Context, table, keyCol string, timeCols ...string) error {
	if !q.queueTableExists(ctx, table) {
		return nil
	}
	selectCols := make([]string, 0, len(timeCols)+1)
	selectCols = append(selectCols, keyCol)
	selectCols = append(selectCols, timeCols...)
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
		cols := make([]queueTimeCol, len(timeCols))
		for i, name := range timeCols {
			cols[i] = queueTimeCol{name, r.vals[i]}
		}
		sets, args, err := queueTimeSets(cols)
		if err != nil {
			return err
		}
		if len(sets) == 0 {
			continue
		}
		args = append(args, r.key)
		stmt := fmt.Sprintf(`UPDATE %s SET %s WHERE %s=$%d`,
			table, strings.Join(sets, ", "), keyCol, len(args))
		if _, err := q.db.ExecContext(ctx, stmt, args...); err != nil {
			return err
		}
	}
	return nil
}

type queueTimeCol struct {
	col string
	raw any
}

// queueTimeSets builds SET-clause fragments and bind args for the time
// columns whose stored value isn't already in canonical RFC3339Nano form.
// NULL columns produce no fragment (left untouched). Canonical values parse
// and reformat to the same string → skipped, which makes the whole pass
// idempotent. Unparseable values return an error: a value query.ParseDBTime
// can't handle is data corruption, and silently keeping it would leave the
// bug in place. The bound value is normalized to UTC and bound as time.Time
// so the driver writes exactly what its own predicate binds compare against
// (the driver formats time.Time into the stored layout, which is what every
// claim/lease query then compares to).
func queueTimeSets(cols []queueTimeCol) ([]string, []any, error) {
	var sets []string
	var args []any
	for _, c := range cols {
		if c.raw == nil {
			continue
		}
		parsed, err := query.ParseDBTime(c.raw)
		if err != nil {
			return nil, nil, fmt.Errorf("queue: decode legacy %s: %w", c.col, err)
		}
		canonical := parsed.UTC().Format(time.RFC3339Nano)
		if s, ok := c.raw.(string); ok && s == canonical {
			continue
		}
		sets = append(sets, fmt.Sprintf("%s=$%d", c.col, len(args)+1))
		args = append(args, parsed.UTC())
	}
	return sets, args, nil
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
