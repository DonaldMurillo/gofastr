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

	_, err := s.queue.db.ExecContext(ctx, fmt.Sprintf(`DELETE FROM %s
		WHERE created_at < $1 AND (
			status='skipped'
			OR (
				status='enqueued'
				AND NOT EXISTS (
					SELECT 1 FROM %s j
					WHERE j.id=enqueued_job_id
					AND j.status IN ('pending','claimed')
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
	// version: optimistic-concurrency fencing on schedules created
	// before durable-scheduler plan fencing shipped.
	if err := s.ensureScheduleColumns([]scheduleColumn{
		{"version", "BIGINT", "0"},
	}); err != nil {
		return err
	}
	// lane / priority / max_attempts: per-schedule options. All three
	// are additive NOT NULL with defaults, so the change is safe on
	// existing rows.
	if err := s.ensureScheduleColumns([]scheduleColumn{
		{"lane", "TEXT", "''"},
		{"priority", "INTEGER", "0"},
		{"max_attempts", "INTEGER", "0"},
	}); err != nil {
		return err
	}
	// tz: the IANA location a cron schedule was registered in, so cron
	// field evaluation happens in the schedule's wall-clock location
	// instead of UTC. Additive NOT NULL DEFAULT '', so existing rows and
	// interval schedules (which never consult tz) evaluate as before.
	if err := s.ensureScheduleColumns([]scheduleColumn{
		{"tz", "TEXT", "''"},
	}); err != nil {
		return err
	}
	for _, statement := range []string{
		fmt.Sprintf("CREATE INDEX IF NOT EXISTS %s ON %s (schedule_id, enqueued_job_id)",
			s.queue.schedulerIndex("scheduler_occurrences_schedule_job_idx"),
			s.queue.schedulerOccurrencesTable()),
		fmt.Sprintf("CREATE INDEX IF NOT EXISTS %s ON %s (created_at)",
			s.queue.schedulerIndex("scheduler_occurrences_created_at_idx"),
			s.queue.schedulerOccurrencesTable()),
	} {
		if _, err := s.queue.db.Exec(statement); err != nil {
			return err
		}
	}
	return nil
}

// scheduleColumn names one additive column ensureScheduleColumns adds to
// the schedules table: identifier, SQL type, default literal.
type scheduleColumn struct {
	name, typ, dflt string
}

// ensureScheduleColumns idempotently adds the named columns to the
// schedules table: Postgres uses ADD COLUMN IF NOT EXISTS; SQLite has no
// such form, so it scans PRAGMA table_info first and ALTERs only the
// missing columns, tolerating the duplicate-column race. It replaces the
// three formerly duplicated column-ensure bodies in this file (the
// version, lane/priority/max_attempts, and tz migrations), which shared
// the same strategy and differed only in the column list.
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
	// SQLite has no IF NOT EXISTS for ADD COLUMN: probe PRAGMA once and
	// ALTER only the missing columns, tolerating the duplicate-column race.
	rows, err := s.queue.db.Query("PRAGMA table_info(" + table + ")")
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

func (q *DBQueue) schedulerIndex(suffix string) string {
	name, err := query.SafeIdent(q.table + "_" + suffix)
	if err != nil {
		panic(err)
	}
	return query.QuoteIdent(name)
}
