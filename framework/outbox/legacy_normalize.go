package outbox

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/DonaldMurillo/gofastr/core/query"
)

// normalizeLegacyTimestamps rewrites space-separated (legacy mattn/go-sqlite3)
// timestamp strings in the time columns of the parent and delivery tables to
// the canonical RFC3339Nano text the pure driver binds for time.Time. SQLite
// stores these columns as TEXT, and the relay's lease/grace SQL predicates
// (claimed_until <= $1, created_at <= $2) compare them lexicographically. A
// same-day legacy value like '2026-07-20 23:59:59+00:00' sorts BEFORE the
// canonical same-day value '2026-07-20T23:59:59…Z' because space (0x20)
// precedes 'T' (0x54), so an un-expired FUTURE lease compares as expired and
// is reclaimed (double delivery), and a not-yet-old parent compares as old
// (wrong completion/abandonment).
//
// Postgres stores real TIMESTAMPTZ values, so this is a sqlite-only no-op
// there. Idempotent: a row whose stored value already round-trips to the same
// canonical text is skipped. Runs at relay start (see relayLoop) so the
// claim/grace queries see canonical values from the first pump; parsing via
// parseOutboxTime in scanOutboxRow/listDeliveries remains as the post-scan
// safety net for any value written after construction by a non-pure driver
// sharing the file.
func (o *Outbox) normalizeLegacyTimestamps(ctx context.Context) error {
	if o.dialect != dialectSQLite {
		return nil
	}
	if err := o.normalizeParentTimes(ctx); err != nil {
		return err
	}
	return o.normalizeDeliveryTimes(ctx)
}

// normalizeParentTimes canonicalizes the four time columns of event_outbox,
// keyed by id.
func (o *Outbox) normalizeParentTimes(ctx context.Context) error {
	if !o.tableExists(ctx, o.table) {
		return nil
	}
	// Read every row first and close the iterator before any UPDATE: the
	// outbox is typically opened with SetMaxOpenConns(1) (SQLite serialises
	// writers on a single in-memory page), and an UPDATE issued while the
	// SELECT's rows cursor still holds the one connection deadlocks.
	rows, err := o.db.QueryContext(ctx, fmt.Sprintf(
		`SELECT id, created_at, dispatched_at, next_attempt_at, claimed_until FROM %s`,
		o.qt()))
	if err != nil {
		return err
	}
	type parentRow struct {
		id                                 string
		created, dispatched, next, claimed any
	}
	var collected []parentRow
	for rows.Next() {
		var r parentRow
		if err := rows.Scan(&r.id, &r.created, &r.dispatched, &r.next, &r.claimed); err != nil {
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
		sets, args, err := legacyTimeSets([]timeCol{
			{"created_at", r.created},
			{"dispatched_at", r.dispatched},
			{"next_attempt_at", r.next},
			{"claimed_until", r.claimed},
		})
		if err != nil {
			return err
		}
		if len(sets) == 0 {
			continue
		}
		args = append(args, r.id)
		q := fmt.Sprintf(`UPDATE %s SET %s WHERE id=$%d`,
			o.qt(), strings.Join(sets, ", "), len(args))
		if _, err := o.db.ExecContext(ctx, q, args...); err != nil {
			return err
		}
	}
	return nil
}

// normalizeDeliveryTimes canonicalizes the four time columns of
// event_outbox_delivery, keyed by (row_id, consumer). Reads are fully
// drained before any UPDATE for the same MaxOpenConns(1) reason as the
// parent path.
func (o *Outbox) normalizeDeliveryTimes(ctx context.Context) error {
	if !o.tableExists(ctx, o.deliveryTable) {
		return nil
	}
	rows, err := o.db.QueryContext(ctx, fmt.Sprintf(
		`SELECT row_id, consumer, created_at, next_attempt_at, claimed_until, dispatched_at FROM %s`,
		o.qd()))
	if err != nil {
		return err
	}
	type deliveryRow struct {
		rowID, consumer                    string
		created, next, claimed, dispatched any
	}
	var collected []deliveryRow
	for rows.Next() {
		var r deliveryRow
		if err := rows.Scan(&r.rowID, &r.consumer, &r.created, &r.next, &r.claimed, &r.dispatched); err != nil {
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
		sets, args, err := legacyTimeSets([]timeCol{
			{"created_at", r.created},
			{"next_attempt_at", r.next},
			{"claimed_until", r.claimed},
			{"dispatched_at", r.dispatched},
		})
		if err != nil {
			return err
		}
		if len(sets) == 0 {
			continue
		}
		args = append(args, r.rowID, r.consumer)
		q := fmt.Sprintf(`UPDATE %s SET %s WHERE row_id=$%d AND consumer=$%d`,
			o.qd(), strings.Join(sets, ", "), len(args)-1, len(args))
		if _, err := o.db.ExecContext(ctx, q, args...); err != nil {
			return err
		}
	}
	return nil
}

type timeCol struct {
	col string
	raw any
}

// legacyTimeSets builds SET-clause fragments and bind args for the time
// columns whose stored value isn't already in canonical RFC3339Nano form.
// NULL columns produce no fragment (left untouched). Canonical values parse
// and reformat to the same string → skipped, which makes the whole pass
// idempotent. Unparseable values return an error: a value parseOutboxTime
// can't handle is data corruption, and silently keeping it would leave the
// bug in place. The bound value is normalized to UTC so the rewrite matches
// what the relay itself writes (now().UTC()), keeping 'Z' suffixes uniform
// across rows for stable lexicographic comparison.
func legacyTimeSets(cols []timeCol) ([]string, []any, error) {
	var sets []string
	var args []any
	for _, c := range cols {
		if c.raw == nil {
			continue
		}
		parsed, err := outboxTime(c.raw)
		if err != nil {
			return nil, nil, fmt.Errorf("outbox: decode legacy %s: %w", c.col, err)
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

// tableExists reports whether the bare-named table is queryable. The probe is
// a SELECT … LIMIT 1 so it works on both sqlite drivers (the pure driver
// implements PRAGMA but does not expose sqlite_master, and we want one code
// path). Errors are treated as "absent": ensureTable has already run by the
// time normalization is reached (when not WithoutEnsureTable), so a real
// error here most likely means the host set WithoutEnsureTable and the
// migration hasn't created the table yet — nothing to normalize, and the
// relay's own queries will surface any genuine DB fault.
func (o *Outbox) tableExists(ctx context.Context, bareName string) bool {
	var n int
	q := fmt.Sprintf("SELECT 1 FROM %s LIMIT 1", query.QuoteIdent(bareName))
	err := o.db.QueryRowContext(ctx, q).Scan(&n)
	if err == nil || err == sql.ErrNoRows {
		return true
	}
	return false
}
