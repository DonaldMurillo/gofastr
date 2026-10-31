package outbox

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/query"
)

// normalizeLegacyTimestamps rewrites timestamp strings in the time columns
// of the parent and delivery tables to the canonical text layout the
// CONNECTED driver binds for time.Time (RFC3339Nano on the pure driver,
// space-separated on mattn/go-sqlite3; see probeBindLayout). SQLite
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
// query.ParseDBTime in scanOutboxRow/listDeliveries remains as
// the post-scan safety net for any value written after construction by a
// non-pure driver sharing the file.
func (o *Outbox) normalizeLegacyTimestamps(ctx context.Context) error {
	if o.dialect != dialectSQLite {
		return nil
	}
	layout := o.probeBindLayout(ctx)
	if err := o.normalizeParentTimes(ctx, layout); err != nil {
		return err
	}
	return o.normalizeDeliveryTimes(ctx, layout)
}

// probeBindLayout detects the text layout the connected driver produces
// when a time.Time is bound as a parameter, the format the relay's own
// predicates compare against, and therefore the canonical target for
// normalization. See query.ProbeSQLiteBindLayout, the shared probe this
// delegates to (formerly duplicated in battery/queue).
func (o *Outbox) probeBindLayout(ctx context.Context) string {
	return query.ProbeSQLiteBindLayout(ctx, o.db)
}

// timeText reads a time column as its stored text. The idempotency skip in
// legacyTimeFixes compares stored text against canonical text, so the column
// must not arrive pre-parsed: modernc.org/sqlite hands back a time.Time for
// a DATETIME column, which fails the string type assertion and makes every
// row look like it needs rewriting on every relay start. Casting keeps the
// comparison driver-neutral. Only reached on SQLite (normalizeLegacyTimestamps
// returns early on Postgres), so this never touches a real TIMESTAMPTZ.
func timeText(col string) string {
	return fmt.Sprintf("CAST(%s AS TEXT) AS %s", col, col)
}

// parentTimeSelect reads the four parent time columns as stored text.
func (o *Outbox) parentTimeSelect() string {
	return fmt.Sprintf(`SELECT id, %s, %s, %s, %s FROM %s`,
		timeText("created_at"), timeText("dispatched_at"),
		timeText("next_attempt_at"), timeText("claimed_until"), o.qt())
}

// normalizeParentTimes canonicalizes the four time columns of event_outbox,
// keyed by id.
func (o *Outbox) normalizeParentTimes(ctx context.Context, layout string) error {
	if !o.tableExists(ctx, o.table) {
		return nil
	}
	// Read every row first and close the iterator before any UPDATE: the
	// outbox is typically opened with SetMaxOpenConns(1) (SQLite serialises
	// writers on a single in-memory page), and an UPDATE issued while the
	// SELECT's rows cursor still holds the one connection deadlocks.
	rows, err := o.db.QueryContext(ctx, o.parentTimeSelect())
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
		fixes, err := query.SQLiteTimeFixes(layout, []query.StoredTime{
			{Col: "created_at", Raw: r.created},
			{Col: "dispatched_at", Raw: r.dispatched},
			{Col: "next_attempt_at", Raw: r.next},
			{Col: "claimed_until", Raw: r.claimed},
		})
		if err != nil {
			return fmt.Errorf("outbox: %w", err)
		}
		if err := o.applyTimeFixes(ctx, o.qt(), []string{"id"}, []any{r.id}, fixes); err != nil {
			return err
		}
	}
	return nil
}

// normalizeDeliveryTimes canonicalizes the four time columns of
// event_outbox_delivery, keyed by (row_id, consumer). Reads are fully
// drained before any UPDATE for the same MaxOpenConns(1) reason as the
// parent path.
func (o *Outbox) normalizeDeliveryTimes(ctx context.Context, layout string) error {
	if !o.tableExists(ctx, o.deliveryTable) {
		return nil
	}
	rows, err := o.db.QueryContext(ctx, fmt.Sprintf(
		`SELECT row_id, consumer, %s, %s, %s, %s FROM %s`,
		timeText("created_at"), timeText("next_attempt_at"),
		timeText("claimed_until"), timeText("dispatched_at"), o.qd()))
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
		fixes, err := query.SQLiteTimeFixes(layout, []query.StoredTime{
			{Col: "created_at", Raw: r.created},
			{Col: "next_attempt_at", Raw: r.next},
			{Col: "claimed_until", Raw: r.claimed},
			{Col: "dispatched_at", Raw: r.dispatched},
		})
		if err != nil {
			return fmt.Errorf("outbox: %w", err)
		}
		if err := o.applyTimeFixes(ctx, o.qd(), []string{"row_id", "consumer"}, []any{r.rowID, r.consumer}, fixes); err != nil {
			return err
		}
	}
	return nil
}

// applyTimeFixes writes one row's fixes, a statement per column, each
// guarded on the text the column held when it was read. The read and the
// writes are not one transaction, and a relay that claimed the delivery
// in between wrote its fresh claimed_until in the driver's own layout: an
// unguarded write put the stale lease back, and another relay could claim
// the delivery at once (2026-10-27 B10). One column per statement, so a
// column the relay rewrote does not hold back the others.
func (o *Outbox) applyTimeFixes(ctx context.Context, table string, keyCols []string, key []any, fixes []query.TimeFix) error {
	where := make([]string, len(keyCols))
	for i, col := range keyCols {
		where[i] = fmt.Sprintf("%s=$%d", col, i+2)
	}
	guard := len(keyCols) + 2
	for _, f := range fixes {
		q := fmt.Sprintf(`UPDATE %s SET %s=$1 WHERE %s AND CAST(%s AS TEXT)=$%d`,
			table, f.Col, strings.Join(where, " AND "), f.Col, guard)
		args := append(append([]any{f.At}, key...), f.Read)
		if _, err := o.db.ExecContext(ctx, q, args...); err != nil {
			return err
		}
	}
	return nil
}

// tableExists reports whether the bare-named table is queryable. The probe is
// a SELECT … LIMIT 1 so it works on both sqlite drivers (the pure driver
// implements PRAGMA but does not expose sqlite_master, and we want one code
// path). Errors are treated as "absent": ensureTable has already run by the
// time normalization is reached (when not WithoutEnsureTable), so a real
// error here most likely means the host set WithoutEnsureTable and the
// migration hasn't created the table yet, nothing to normalize, and the
// relay's own queries will surface any genuine DB fault.
func (o *Outbox) tableExists(ctx context.Context, bareName string) bool {
	var n int
	q := fmt.Sprintf("SELECT 1 FROM %s LIMIT 1", query.QuoteFolded(bareName))
	err := o.db.QueryRowContext(ctx, q).Scan(&n)
	if err == nil || err == sql.ErrNoRows {
		return true
	}
	// "no such table" is the expected WithoutEnsureTable-before-migration
	// case and stays silent. Anything else (e.g. SQLITE_BUSY from another
	// process holding the file lock) also skips normalization, the relay
	// keeps working via the post-scan parse fallback, but is worth a
	// trace, since silence here would hide a persistent fault.
	if !strings.Contains(strings.ToLower(err.Error()), "no such table") {
		slog.Default().Warn("outbox: table probe failed; skipping timestamp normalization this start",
			"table", bareName, "err", err)
	}
	return false
}
