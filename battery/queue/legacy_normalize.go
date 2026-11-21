package queue

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/query"
)

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

func (q *DBQueue) queueTableExists(ctx context.Context, quotedTable string) bool {
	var n int
	err := q.db.QueryRowContext(ctx, fmt.Sprintf("SELECT 1 FROM %s LIMIT 1", quotedTable)).Scan(&n)
	if err == nil || err == sql.ErrNoRows {
		return true
	}
	return false
}
