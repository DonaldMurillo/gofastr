package queue

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/DonaldMurillo/gofastr/core/query"
)

const defaultOccurrenceRetention = 30 * 24 * time.Hour
const defaultRetentionSweepPeriod = time.Hour

func (s *DurableScheduler) sweepOccurrences(ctx context.Context, now time.Time) error {
	if s.occurrenceRetention <= 0 {
		return nil
	}
	s.retentionMu.Lock()
	defer s.retentionMu.Unlock()
	if !s.nextRetentionSweep.IsZero() && now.Before(s.nextRetentionSweep) {
		return nil
	}

	//gofastr:allow(iounderlock) only the scheduler's own evaluation loop (and a direct RunOnce) reach this sweep, once per tick; no request path or other worker waits on retentionMu, which exists to keep two RunOnce calls from sweeping at once
	_, err := s.queue.db.ExecContext(ctx, fmt.Sprintf(`DELETE FROM %s
		WHERE created_at < $1 AND (
			status='skipped'
			OR (
				status='enqueued'
				AND NOT EXISTS (
					SELECT 1 FROM %s j
					WHERE j.id=enqueued_job_id
					AND j.status <> 'done'
				)
			)
		)`, s.queue.schedulerOccurrencesTable(), s.queue.qt()),
		now.UTC().Add(-s.occurrenceRetention),
	)
	if err == nil {
		s.nextRetentionSweep = now.Add(defaultRetentionSweepPeriod)
	}
	return err
}

func (s *DurableScheduler) ensureHardeningSchema() error {
	if err := s.ensureScheduleColumns([]scheduleColumn{
		{"version", "BIGINT", "0"},
	}); err != nil {
		return err
	}
	if err := s.ensureScheduleColumns([]scheduleColumn{
		{"lane", "TEXT", "''"},
		{"priority", "INTEGER", "0"},
		{"max_attempts", "INTEGER", "0"},
	}); err != nil {
		return err
	}
	if err := s.ensureScheduleColumns([]scheduleColumn{
		{"tz", "TEXT", "''"},
	}); err != nil {
		return err
	}
	if err := s.ensureScheduleColumns([]scheduleColumn{
		{"user_id", "TEXT", "''"},
	}); err != nil {
		return err
	}
	for _, statement := range []string{
		fmt.Sprintf("CREATE INDEX IF NOT EXISTS %s (schedule_id, enqueued_job_id)",
			s.queue.indexOn("scheduler_occurrences_schedule_job_idx", s.queue.schedulerOccurrencesTable())),
		fmt.Sprintf("CREATE INDEX IF NOT EXISTS %s (created_at)",
			s.queue.indexOn("scheduler_occurrences_created_at_idx", s.queue.schedulerOccurrencesTable())),
	} {
		if _, err := s.queue.db.Exec(statement); err != nil {
			return err
		}
	}
	return nil
}

type scheduleColumn struct {
	name, typ, dflt string
}

func (s *DurableScheduler) ensureScheduleColumns(want []scheduleColumn) error {
	table := s.queue.schedulerSchedulesTable()
	if s.queue.dialect == dialectPostgres {
		for _, c := range want {
			ident, err := query.SafeIdent(c.name)
			if err != nil {
				return fmt.Errorf("queue: add column %q: %w", c.name, err)
			}
			if _, err := s.queue.db.Exec(fmt.Sprintf(
				"ALTER TABLE %s ADD COLUMN IF NOT EXISTS %s %s NOT NULL DEFAULT %s",
				table, ident, c.typ, c.dflt)); err != nil {
				return err
			}
		}
		return nil
	}
	rows, err := s.queue.db.Query(query.SQLitePragma("table_info", table))
	if err != nil {
		return err
	}
	present := map[string]bool{}
	for rows.Next() {
		var cid, notNull, pk int
		var name, typ string
		var dflt any
		if err := rows.Scan(&cid, &name, &typ, &notNull, &dflt, &pk); err != nil {
			_ = rows.Close()
			return err
		}
		present[strings.ToLower(name)] = true
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, c := range want {
		if present[strings.ToLower(c.name)] {
			continue
		}
		ident, err := query.SafeIdent(c.name)
		if err != nil {
			return fmt.Errorf("queue: add column %q: %w", c.name, err)
		}
		_, err = s.queue.db.Exec(fmt.Sprintf(
			"ALTER TABLE %s ADD COLUMN %s %s NOT NULL DEFAULT %s",
			table, ident, c.typ, c.dflt))
		if err != nil && !isDuplicateColumnErr(err) {
			return err
		}
	}
	return nil
}

func (q *DBQueue) indexOn(suffix, table string) string {
	name, err := query.SafeIdent(q.schedulerName("_" + suffix))
	if err != nil {
		panic(err)
	}
	return query.IndexOn(q.dialect == dialectSQLite, query.QuoteIndexName(name), table)
}
