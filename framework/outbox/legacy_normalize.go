package outbox

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/query"
)

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

func (o *Outbox) probeBindLayout(ctx context.Context) string {
	return query.ProbeSQLiteBindLayout(ctx, o.db)
}

func timeText(col string) string {
	return fmt.Sprintf("CAST(%s AS TEXT) AS %s", col, col)
}

func (o *Outbox) parentTimeSelect() string {
	return fmt.Sprintf(`SELECT id, %s, %s, %s, %s FROM %s`,
		timeText("created_at"), timeText("dispatched_at"),
		timeText("next_attempt_at"), timeText("claimed_until"), o.qt())
}

func (o *Outbox) normalizeParentTimes(ctx context.Context, layout string) error {
	if !o.tableExists(ctx, o.table) {
		return nil
	}
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

func (o *Outbox) tableExists(ctx context.Context, bareName string) bool {
	var n int
	q := fmt.Sprintf("SELECT 1 FROM %s LIMIT 1", query.QuoteFolded(bareName))
	err := o.db.QueryRowContext(ctx, q).Scan(&n)
	if err == nil || err == sql.ErrNoRows {
		return true
	}
	if !strings.Contains(strings.ToLower(err.Error()), "no such table") {
		slog.Default().Warn("outbox: table probe failed; skipping timestamp normalization this start",
			"table", bareName, "err", err)
	}
	return false
}
