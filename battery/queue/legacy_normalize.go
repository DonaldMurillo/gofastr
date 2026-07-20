package queue

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
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
// parseQueueTime to the canonical layout is skipped. Runs from NewDBQueue
// (after schema ensure/migrations) and NewDurableScheduler.ensureTables
// (after the scheduler tables are created) so the very first claim query of
// a freshly-opened upgraded DB sees canonical values. queueTime in scanJob,
// loadDue and nextWakeDelay remains as the post-scan safety net for any
// value written after construction by a non-pure driver sharing the file.
func (q *DBQueue) normalizeLegacyTimestamps(ctx context.Context) error {
	if q.dialect != dialectSQLite {
		return nil
	}
	if err := q.normalizeQueueJobsTimes(ctx); err != nil {
		return err
	}
	if err := q.normalizeSchedulesTimes(ctx); err != nil {
		return err
	}
	if err := q.normalizeOccurrencesTimes(ctx); err != nil {
		return err
	}
	return q.normalizeLeaseTimes(ctx)
}

// probeBindLayout detects the text layout the connected driver produces when a
// time.Time is bound as a parameter — the format the queue's own predicates
// compare against, and therefore the canonical target for normalization. The
// pure driver binds RFC3339Nano; mattn/go-sqlite3 binds a space-separated
// form. Rows already in the probed layout are canonical FOR THIS HOST and
// skipped, which keeps the pass idempotent on either driver instead of
// rewriting every row on every queue open when the host runs mattn. An
// unrecognized probe result falls back to RFC3339Nano (the rewrite still
// self-corrects, because the rewritten value is bound as time.Time and the
// driver formats it). Ported from framework/outbox/legacy_normalize.go.
func (q *DBQueue) probeBindLayout(ctx context.Context) string {
	ref := time.Date(2001, 2, 3, 4, 5, 6, 789012345, time.UTC)
	var got string
	if err := q.db.QueryRowContext(ctx, `SELECT CAST($1 AS TEXT)`, ref).Scan(&got); err != nil {
		return time.RFC3339Nano
	}
	for _, layout := range []string{
		time.RFC3339Nano,
		"2006-01-02 15:04:05.999999999-07:00", // mattn/go-sqlite3
	} {
		if ref.Format(layout) == got {
			return layout
		}
	}
	return time.RFC3339Nano
}

// normalizeQueueJobsTimes canonicalizes created_at / scheduled_at /
// claimed_at in queue_jobs, keyed by id. claimed_at is nullable, so NULL is
// preserved (no SET fragment). Reads are fully drained before any UPDATE:
// the queue is typically opened with SetMaxOpenConns(1) and an UPDATE issued
// while the SELECT's rows cursor still holds the one connection deadlocks.
func (q *DBQueue) normalizeQueueJobsTimes(ctx context.Context) error {
	if !q.queueTableExists(ctx, q.qt()) {
		return nil
	}
	rows, err := q.db.QueryContext(ctx, fmt.Sprintf(
		`SELECT id, created_at, scheduled_at, claimed_at FROM %s`, q.qt()))
	if err != nil {
		return err
	}
	type row struct {
		id                          string
		created, scheduled, claimed any
	}
	var collected []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.created, &r.scheduled, &r.claimed); err != nil {
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
		sets, args, err := queueTimeSets([]queueTimeCol{
			{"created_at", r.created},
			{"scheduled_at", r.scheduled},
			{"claimed_at", r.claimed},
		})
		if err != nil {
			return err
		}
		if len(sets) == 0 {
			continue
		}
		args = append(args, r.id)
		stmt := fmt.Sprintf(`UPDATE %s SET %s WHERE id=$%d`,
			q.qt(), strings.Join(sets, ", "), len(args))
		if _, err := q.db.ExecContext(ctx, stmt, args...); err != nil {
			return err
		}
	}
	return nil
}

// normalizeSchedulesTimes canonicalizes next_run / updated_at in
// scheduler_schedules, keyed by id. Runs when the DurableScheduler has been
// constructed (the table exists); no-op otherwise.
func (q *DBQueue) normalizeSchedulesTimes(ctx context.Context) error {
	tbl := q.schedulerSchedulesTable()
	if !q.queueTableExists(ctx, tbl) {
		return nil
	}
	rows, err := q.db.QueryContext(ctx, fmt.Sprintf(
		`SELECT id, next_run, updated_at FROM %s`, tbl))
	if err != nil {
		return err
	}
	type row struct {
		id            string
		next, updated any
	}
	var collected []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.next, &r.updated); err != nil {
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
		sets, args, err := queueTimeSets([]queueTimeCol{
			{"next_run", r.next},
			{"updated_at", r.updated},
		})
		if err != nil {
			return err
		}
		if len(sets) == 0 {
			continue
		}
		args = append(args, r.id)
		stmt := fmt.Sprintf(`UPDATE %s SET %s WHERE id=$%d`,
			tbl, strings.Join(sets, ", "), len(args))
		if _, err := q.db.ExecContext(ctx, stmt, args...); err != nil {
			return err
		}
	}
	return nil
}

// normalizeOccurrencesTimes canonicalizes scheduled_tick / created_at in
// scheduler_occurrences, keyed by occurrence_id.
func (q *DBQueue) normalizeOccurrencesTimes(ctx context.Context) error {
	tbl := q.schedulerOccurrencesTable()
	if !q.queueTableExists(ctx, tbl) {
		return nil
	}
	rows, err := q.db.QueryContext(ctx, fmt.Sprintf(
		`SELECT occurrence_id, scheduled_tick, created_at FROM %s`, tbl))
	if err != nil {
		return err
	}
	type row struct {
		id            string
		tick, created any
	}
	var collected []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.tick, &r.created); err != nil {
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
		sets, args, err := queueTimeSets([]queueTimeCol{
			{"scheduled_tick", r.tick},
			{"created_at", r.created},
		})
		if err != nil {
			return err
		}
		if len(sets) == 0 {
			continue
		}
		args = append(args, r.id)
		stmt := fmt.Sprintf(`UPDATE %s SET %s WHERE occurrence_id=$%d`,
			tbl, strings.Join(sets, ", "), len(args))
		if _, err := q.db.ExecContext(ctx, stmt, args...); err != nil {
			return err
		}
	}
	return nil
}

// normalizeLeaseTimes canonicalizes expires_at / heartbeat_at in
// scheduler_leases, keyed by name. These columns back the lease fencing
// (acquireLease's "expires_at <= $1" reclaim and the heartbeat "expires_at >
// $1" re-check); a legacy space-separated value makes an active lease look
// expired so a second replica steals the fence and double-fires schedules.
func (q *DBQueue) normalizeLeaseTimes(ctx context.Context) error {
	tbl := q.schedulerLeaseTable()
	if !q.queueTableExists(ctx, tbl) {
		return nil
	}
	rows, err := q.db.QueryContext(ctx, fmt.Sprintf(
		`SELECT name, expires_at, heartbeat_at FROM %s`, tbl))
	if err != nil {
		return err
	}
	type row struct {
		name               string
		expires, heartbeat any
	}
	var collected []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.name, &r.expires, &r.heartbeat); err != nil {
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
		sets, args, err := queueTimeSets([]queueTimeCol{
			{"expires_at", r.expires},
			{"heartbeat_at", r.heartbeat},
		})
		if err != nil {
			return err
		}
		if len(sets) == 0 {
			continue
		}
		args = append(args, r.name)
		stmt := fmt.Sprintf(`UPDATE %s SET %s WHERE name=$%d`,
			tbl, strings.Join(sets, ", "), len(args))
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
// idempotent. Unparseable values return an error: a value parseQueueTime
// can't handle is data corruption, and silently keeping it would leave the
// bug in place. The bound value is normalized to UTC and bound as time.Time
// so the driver writes exactly what its own predicate binds compare against
// (the driver formats time.Time into the probed layout, which is what every
// claim/lease query then compares to).
func queueTimeSets(cols []queueTimeCol) ([]string, []any, error) {
	var sets []string
	var args []any
	for _, c := range cols {
		if c.raw == nil {
			continue
		}
		parsed, err := queueTime(c.raw)
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
// (e.g., NewDBQueue before any DurableScheduler has been constructed — the
// scheduler tables are absent), and there is nothing to normalize.
func (q *DBQueue) queueTableExists(ctx context.Context, quotedTable string) bool {
	var n int
	err := q.db.QueryRowContext(ctx, fmt.Sprintf("SELECT 1 FROM %s LIMIT 1", quotedTable)).Scan(&n)
	if err == nil || err == sql.ErrNoRows {
		return true
	}
	return false
}
