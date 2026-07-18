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
	if err := s.ensureScheduleVersionColumn(); err != nil {
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

func (s *DurableScheduler) ensureScheduleVersionColumn() error {
	table := s.queue.schedulerSchedulesTable()
	if s.queue.dialect == dialectPostgres {
		_, err := s.queue.db.Exec("ALTER TABLE " + table +
			" ADD COLUMN IF NOT EXISTS version BIGINT NOT NULL DEFAULT 0")
		return err
	}

	rows, err := s.queue.db.Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		return err
	}
	found := false
	for rows.Next() {
		var cid, notNull, pk int
		var name, typ string
		var defaultValue any
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			_ = rows.Close()
			return err
		}
		if strings.EqualFold(name, "version") {
			found = true
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if found {
		return nil
	}
	_, err = s.queue.db.Exec("ALTER TABLE " + table +
		" ADD COLUMN version BIGINT NOT NULL DEFAULT 0")
	if err != nil && isDuplicateColumnErr(err) {
		return nil
	}
	return err
}

func (q *DBQueue) schedulerIndex(suffix string) string {
	name, err := query.SafeIdent(q.table + "_" + suffix)
	if err != nil {
		panic(err)
	}
	return query.QuoteIdent(name)
}
