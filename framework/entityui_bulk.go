package framework

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/DonaldMurillo/gofastr/core/datexport"
	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/framework/entityui"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
)

const (
	bulkJobsTable  = "gofastr_bulk_jobs"
	bulkItemsTable = "gofastr_bulk_items"
)

const bulkInsertBatch = 500

// AuditEvent writes a bulk run's summary row to the audit log, if any.
func (h entityUIHost) AuditEvent(ctx context.Context, ent, op, recordID string, detail map[string]any) error {
	if h.a.auditTable == "" || h.a.DB == nil {
		return nil
	}
	actor := ""
	if h.a.auditActor != nil {
		actor = h.a.auditActor(ctx)
	}
	return AppendAuditEvent(ctx, h.a.DB, h.a.auditTable, ent, op, recordID, actor, detail)
}

// BulkStore is the snapshot store, nil unless EntityUI had Extensions.Jobs.
func (h entityUIHost) BulkStore() entityui.BulkStore {
	if h.store == nil {
		return nil
	}
	return h.store
}

type sqlBulkStore struct {
	db      *sql.DB
	dialect migrate.Dialect
}

func newSQLBulkStore(ctx context.Context, db *sql.DB) (*sqlBulkStore, error) {
	s := &sqlBulkStore{db: db, dialect: migrate.DetectDialect(db)}
	ts := "DATETIME"
	if s.dialect == migrate.DialectPostgres {
		ts = "TIMESTAMPTZ"
	}
	jobs := query.QuoteFolded(bulkJobsTable)
	stmts := []string{
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
			id          TEXT PRIMARY KEY,
			entity      TEXT NOT NULL,
			action      TEXT NOT NULL,
			count       INTEGER NOT NULL,
			creator     TEXT NOT NULL,
			tenant      TEXT NOT NULL DEFAULT '',
			filter_hash TEXT NOT NULL,
			run_key     TEXT NOT NULL,
			status      TEXT NOT NULL,
			enqueued    INTEGER NOT NULL DEFAULT 0,
			claim_token TEXT NOT NULL DEFAULT '',
			lease_until %[2]s,
			done        INTEGER NOT NULL DEFAULT 0,
			skipped     INTEGER NOT NULL DEFAULT 0,
			failed      INTEGER NOT NULL DEFAULT 0,
			created_at  %[2]s NOT NULL,
			finished_at %[2]s
		)`, jobs, ts),
		fmt.Sprintf(`CREATE UNIQUE INDEX IF NOT EXISTS %s (run_key) WHERE status = 'queued'`,
			query.IndexOn(s.dialect == migrate.DialectSQLite, query.QuoteIndexName(bulkJobsTable+"_run_key"), jobs)),
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
			job_id    TEXT NOT NULL,
			record_id TEXT NOT NULL,
			seq       INTEGER NOT NULL,
			outcome   TEXT,
			PRIMARY KEY (job_id, record_id)
		)`, query.QuoteFolded(bulkItemsTable)),
		fmt.Sprintf(`CREATE INDEX IF NOT EXISTS %s (job_id, seq)`,
			query.IndexOn(s.dialect == migrate.DialectSQLite, query.QuoteIndexName(bulkItemsTable+"_seq"), query.QuoteFolded(bulkItemsTable))),
	}
	for _, q := range stmts {
		if _, err := db.ExecContext(ctx, q); err != nil {
			return nil, fmt.Errorf("bulk store: ensure schema: %w", err)
		}
	}
	registerBulkTables()
	return s, nil
}

func registerBulkTables() {
	datexport.Register(datexport.DataExporter{
		Name: bulkJobsTable, Source: "entityui", Table: bulkJobsTable, PrimaryKey: "id",
		Columns: []string{"id", "entity", "action", "count", "creator", "tenant", "filter_hash", "run_key", "status",
			"enqueued", "claim_token", "lease_until", "done", "skipped", "failed", "created_at", "finished_at"},
	})
	datexport.Register(datexport.DataExporter{
		Name: bulkItemsTable, Source: "entityui", Table: bulkItemsTable, PrimaryKey: "job_id,record_id",
		Columns: []string{"job_id", "record_id", "seq", "outcome"},
	})
	datexport.RegisterEraser(datexport.DataEraser{
		Name: bulkJobsTable, Source: "entityui", Table: bulkJobsTable,
		Column: "creator", Mode: datexport.EraseAnonymize, ScrubColumns: []string{"creator"},
	})
}

const bulkJobColumns = `id, entity, action, count, creator, tenant, filter_hash, run_key, status, done, skipped, failed`

func scanJob(row interface{ Scan(...any) error }) (entityui.BulkJob, error) {
	var j entityui.BulkJob
	err := row.Scan(&j.ID, &j.Entity, &j.Action, &j.Count, &j.Creator, &j.Tenant, &j.FilterHash, &j.Key, &j.Status, &j.Done, &j.Skipped, &j.Failed)
	return j, err
}

func (s *sqlBulkStore) queuedByKey(ctx context.Context, key string) (entityui.BulkJob, bool, error) {
	j, err := scanJob(s.db.QueryRowContext(ctx, fmt.Sprintf(
		`SELECT %s FROM %s WHERE run_key = $1 AND status = $2`, bulkJobColumns, query.QuoteFolded(bulkJobsTable)),
		key, entityui.BulkQueued))
	if errors.Is(err, sql.ErrNoRows) {
		return entityui.BulkJob{}, false, nil
	}
	return j, err == nil, err
}

func (s *sqlBulkStore) Create(ctx context.Context, job entityui.BulkJob, ids []string) (entityui.BulkJob, error) {
	if job.Key == "" {
		return entityui.BulkJob{}, errors.New("bulk store: job has no run key")
	}
	held, err := s.create(ctx, job, ids)
	if err == nil {
		return held, nil
	}
	if j, ok, lerr := s.queuedByKey(ctx, job.Key); lerr == nil && ok {
		return j, nil
	}
	return entityui.BulkJob{}, err
}

func (s *sqlBulkStore) create(ctx context.Context, job entityui.BulkJob, ids []string) (_ entityui.BulkJob, err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return entityui.BulkJob{}, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	if _, err = tx.ExecContext(ctx, fmt.Sprintf(
		`INSERT INTO %s (id, entity, action, count, creator, tenant, filter_hash, run_key, status, created_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		query.QuoteFolded(bulkJobsTable)),
		job.ID, job.Entity, job.Action, job.Count, job.Creator, job.Tenant, job.FilterHash, job.Key, entityui.BulkQueued, time.Now().UTC(),
	); err != nil {
		return entityui.BulkJob{}, err
	}
	for start := 0; start < len(ids); start += bulkInsertBatch {
		end := min(start+bulkInsertBatch, len(ids))
		var sb strings.Builder
		args := make([]any, 0, (end-start)*3)
		for i, id := range ids[start:end] {
			if i > 0 {
				sb.WriteString(", ")
			}
			n := len(args)
			sb.WriteString("($" + strconv.Itoa(n+1) + ", $" + strconv.Itoa(n+2) + ", $" + strconv.Itoa(n+3) + ")")
			args = append(args, job.ID, id, start+i)
		}
		if _, err = tx.ExecContext(ctx, fmt.Sprintf(`INSERT INTO %s (job_id, record_id, seq) VALUES `, query.QuoteFolded(bulkItemsTable))+sb.String(), args...); err != nil {
			return entityui.BulkJob{}, err
		}
	}
	job.Status = entityui.BulkQueued
	return job, tx.Commit()
}

func (s *sqlBulkStore) Enqueued(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, fmt.Sprintf(`UPDATE %s SET enqueued = 1 WHERE id = $1`, query.QuoteFolded(bulkJobsTable)), id)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return errBulkJobUnknown
	}
	return nil
}

var errBulkJobUnknown = errors.New("bulk store: unknown job")

func (s *sqlBulkStore) Job(ctx context.Context, id string) (entityui.BulkJob, error) {
	j, err := scanJob(s.db.QueryRowContext(ctx, fmt.Sprintf(
		`SELECT %s FROM %s WHERE id = $1`, bulkJobColumns, query.QuoteFolded(bulkJobsTable)), id))
	if errors.Is(err, sql.ErrNoRows) {
		return entityui.BulkJob{}, errBulkJobUnknown
	}
	return j, err
}

func (s *sqlBulkStore) Claim(ctx context.Context, id, runner string, now, until time.Time) (bool, error) {
	if runner == "" {
		return false, errors.New("bulk store: claim needs a runner token")
	}
	res, err := s.db.ExecContext(ctx, fmt.Sprintf(
		`UPDATE %s SET claim_token = $1, lease_until = $2
		 WHERE id = $3 AND status = $4 AND (claim_token = $1 OR claim_token = '' OR lease_until <= $5)`,
		query.QuoteFolded(bulkJobsTable)),
		runner, until.UTC(), id, entityui.BulkQueued, now.UTC())
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

func (s *sqlBulkStore) Pending(ctx context.Context, id string, limit int) ([]string, error) {
	if limit <= 0 {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(
		`SELECT record_id FROM %s WHERE job_id = $1 AND outcome IS NULL ORDER BY seq LIMIT %d`,
		query.QuoteFolded(bulkItemsTable), limit), id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var rid string
		if err := rows.Scan(&rid); err != nil {
			return nil, err
		}
		out = append(out, rid)
	}
	return out, rows.Err()
}

func (s *sqlBulkStore) holdLease(ctx context.Context, tx *sql.Tx, id, runner string) error {
	res, err := tx.ExecContext(ctx, fmt.Sprintf(
		`UPDATE %s SET claim_token = claim_token WHERE id = $1 AND claim_token = $2 AND status = $3`,
		query.QuoteFolded(bulkJobsTable)), id, runner, entityui.BulkQueued)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err != nil {
		return err
	} else if n == 0 {
		return entityui.ErrBulkLeaseLost
	}
	return nil
}

func (s *sqlBulkStore) Settle(ctx context.Context, id, runner string, outcomes map[string]string) (err error) {
	for _, rid := range slices.Sorted(maps.Keys(outcomes)) {
		switch outcomes[rid] {
		case entityui.BulkRowDone, entityui.BulkRowSkipped, entityui.BulkRowFailed:
		default:
			return fmt.Errorf("bulk store: outcome %q is not done, skipped or failed", outcomes[rid])
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	if err = s.holdLease(ctx, tx, id, runner); err != nil {
		return err
	}
	q := fmt.Sprintf(`UPDATE %s SET outcome = $1 WHERE job_id = $2 AND record_id = $3 AND outcome IS NULL`, query.QuoteFolded(bulkItemsTable))
	for _, rid := range slices.Sorted(maps.Keys(outcomes)) {
		if _, err = tx.ExecContext(ctx, q, outcomes[rid], id, rid); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *sqlBulkStore) Tally(ctx context.Context, id string) (map[string]int, error) {
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(
		`SELECT outcome, COUNT(*) FROM %s WHERE job_id = $1 AND outcome IS NOT NULL GROUP BY outcome`,
		query.QuoteFolded(bulkItemsTable)), id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var outcome string
		var n int
		if err := rows.Scan(&outcome, &n); err != nil {
			return nil, err
		}
		out[outcome] = n
	}
	return out, rows.Err()
}

func (s *sqlBulkStore) Finish(ctx context.Context, id, runner, status string, at time.Time) (err error) {
	switch status {
	case entityui.BulkDone, entityui.BulkStopped:
	default:
		return fmt.Errorf("bulk store: status %q is not done or stopped", status)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	if err = s.holdLease(ctx, tx, id, runner); err != nil {
		if errors.Is(err, entityui.ErrBulkLeaseLost) {
			_ = tx.Rollback()
			if _, jerr := s.Job(ctx, id); errors.Is(jerr, errBulkJobUnknown) {
				return errBulkJobUnknown
			}
		}
		return err
	}
	items := query.QuoteFolded(bulkItemsTable)
	count := func(n int) string {
		return fmt.Sprintf(`(SELECT COUNT(*) FROM %s WHERE job_id = $%d AND outcome = $%d)`, items, n, n+1)
	}
	if _, err = tx.ExecContext(ctx, fmt.Sprintf(
		`UPDATE %s SET status = $1, finished_at = $2, claim_token = '', lease_until = NULL, done = %s, skipped = %s, failed = %s
		 WHERE id = $3 AND claim_token = $4 AND status = $5`,
		query.QuoteFolded(bulkJobsTable), count(6), count(8), count(10)),
		status, at.UTC(), id, runner, entityui.BulkQueued,
		id, entityui.BulkRowDone, id, entityui.BulkRowSkipped, id, entityui.BulkRowFailed,
	); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, fmt.Sprintf(`DELETE FROM %s WHERE job_id = $1`, items), id); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *sqlBulkStore) Unenqueued(ctx context.Context, before time.Time) ([]entityui.BulkJob, error) {
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(
		`SELECT %s FROM %s WHERE status = $1 AND created_at < $2 AND (enqueued = 0 OR lease_until < $2) ORDER BY created_at, id`,
		bulkJobColumns, query.QuoteFolded(bulkJobsTable)), entityui.BulkQueued, before.UTC())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []entityui.BulkJob
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

func (s *sqlBulkStore) Prune(ctx context.Context, before time.Time) (_ int, err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	jobs := query.QuoteFolded(bulkJobsTable)
	finished := `status <> $1 AND finished_at < $2`
	if _, err = tx.ExecContext(ctx, fmt.Sprintf(`DELETE FROM %s WHERE job_id IN (SELECT id FROM %s WHERE %s)`,
		query.QuoteFolded(bulkItemsTable), jobs, finished), entityui.BulkQueued, before.UTC()); err != nil {
		return 0, err
	}
	res, err := tx.ExecContext(ctx, fmt.Sprintf(`DELETE FROM %s WHERE %s`, jobs, finished), entityui.BulkQueued, before.UTC())
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	return int(n), tx.Commit()
}
