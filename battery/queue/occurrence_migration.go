package queue

import "fmt"

// migrateOccurrenceIDColumn adds run-correlation identity to queue tables
// created before durable scheduler occurrences shipped.
func (q *DBQueue) migrateOccurrenceIDColumn() error {
	if q.dialect == dialectPostgres {
		_, err := q.db.Exec(fmt.Sprintf(
			"ALTER TABLE %s ADD COLUMN IF NOT EXISTS occurrence_id TEXT NOT NULL DEFAULT ''",
			q.qt(),
		))
		return err
	}
	_, err := q.db.Exec(fmt.Sprintf(
		"ALTER TABLE %s ADD COLUMN occurrence_id TEXT NOT NULL DEFAULT ''",
		q.qt(),
	))
	if err != nil && isDuplicateColumnErr(err) {
		return nil
	}
	return err
}
