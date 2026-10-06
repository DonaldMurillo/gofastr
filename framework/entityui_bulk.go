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

	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/framework/entityui"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
)

// Bulk snapshot tables: one row per queued run, one per record its
// selection resolved to at confirm.
const (
	bulkJobsTable  = "gofastr_bulk_jobs"
	bulkItemsTable = "gofastr_bulk_items"
)

// bulkInsertBatch is how many item rows one INSERT carries.
const bulkInsertBatch = 500

// AuditEvent writes a bulk run's summary row to the app's audit log, with
// the actor WithAuditLog resolves. An app with no audit log writes nothing.
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

// BulkStore is the snapshot store queued runs walk, present only when
// EntityUI was given Extensions.Jobs.
func (h entityUIHost) BulkStore() entityui.BulkStore {
	if h.store == nil {
		return nil
	}
	return h.store
}

// sqlBulkStore keeps queued bulk runs in the app's database.
type sqlBulkStore struct {
	db      *sql.DB
	dialect migrate.Dialect
}

// newSQLBulkStore creates the snapshot tables if they are missing.
func newSQLBulkStore(ctx context.Context, db *sql.DB) (*sqlBulkStore, error) {
	s := &sqlBulkStore{db: db, dialect: migrate.DetectDialect(db)}
	ts := "DATETIME"
	if s.dialect == migrate.DialectPostgres {
		ts = "TIMESTAMPTZ"
	}
	stmts := []string{
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
			id          TEXT PRIMARY KEY,
			entity      TEXT NOT NULL,
			action      TEXT NOT NULL,
			count       INTEGER NOT NULL,
			creator     TEXT NOT NULL,
			tenant      TEXT NOT NULL DEFAULT '',
			filter_hash TEXT NOT NULL,
			status      TEXT NOT NULL,
			created_at  %s NOT NULL
		)`, query.QuoteIdent(bulkJobsTable), ts),
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
			job_id    TEXT NOT NULL,
			record_id TEXT NOT NULL,
			seq       INTEGER NOT NULL,
			outcome   TEXT,
			PRIMARY KEY (job_id, record_id)
		)`, query.QuoteIdent(bulkItemsTable)),
		fmt.Sprintf(`CREATE INDEX IF NOT EXISTS %s ON %s (job_id, seq)`,
			query.QuoteIdent(bulkItemsTable+"_seq"), query.QuoteIdent(bulkItemsTable)),
	}
	for _, q := range stmts {
		if _, err := db.ExecContext(ctx, q); err != nil {
			return nil, fmt.Errorf("bulk store: ensure schema: %w", err)
		}
	}
	return s, nil
}

// Create writes the job and its ids in one transaction.
func (s *sqlBulkStore) Create(ctx context.Context, job entityui.BulkJob, ids []string) (err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	if _, err = tx.ExecContext(ctx, fmt.Sprintf(
		`INSERT INTO %s (id, entity, action, count, creator, tenant, filter_hash, status, created_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		query.QuoteIdent(bulkJobsTable)),
		job.ID, job.Entity, job.Action, job.Count, job.Creator, job.Tenant, job.FilterHash, entityui.BulkQueued, time.Now().UTC(),
	); err != nil {
		return err
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
		if _, err = tx.ExecContext(ctx, fmt.Sprintf(`INSERT INTO %s (job_id, record_id, seq) VALUES `, query.QuoteIdent(bulkItemsTable))+sb.String(), args...); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// errBulkJobUnknown answers a job id the store does not hold.
var errBulkJobUnknown = errors.New("bulk store: unknown job")

// Job reads one job.
func (s *sqlBulkStore) Job(ctx context.Context, id string) (entityui.BulkJob, error) {
	var j entityui.BulkJob
	err := s.db.QueryRowContext(ctx, fmt.Sprintf(
		`SELECT id, entity, action, count, creator, tenant, filter_hash, status FROM %s WHERE id = $1`,
		query.QuoteIdent(bulkJobsTable)), id,
	).Scan(&j.ID, &j.Entity, &j.Action, &j.Count, &j.Creator, &j.Tenant, &j.FilterHash, &j.Status)
	if errors.Is(err, sql.ErrNoRows) {
		return entityui.BulkJob{}, errBulkJobUnknown
	}
	return j, err
}

// Pending returns up to limit unsettled ids in confirm order.
func (s *sqlBulkStore) Pending(ctx context.Context, id string, limit int) ([]string, error) {
	if limit <= 0 {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(
		`SELECT record_id FROM %s WHERE job_id = $1 AND outcome IS NULL ORDER BY seq LIMIT %d`,
		query.QuoteIdent(bulkItemsTable), limit), id)
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

// Settle records each id's outcome. An id already settled keeps its first
// outcome, so a retried chunk never rewrites what ran.
func (s *sqlBulkStore) Settle(ctx context.Context, id string, outcomes map[string]string) (err error) {
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
	q := fmt.Sprintf(`UPDATE %s SET outcome = $1 WHERE job_id = $2 AND record_id = $3 AND outcome IS NULL`, query.QuoteIdent(bulkItemsTable))
	for _, rid := range slices.Sorted(maps.Keys(outcomes)) {
		if _, err = tx.ExecContext(ctx, q, outcomes[rid], id, rid); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Finish sets the job's status.
func (s *sqlBulkStore) Finish(ctx context.Context, id, status string) error {
	switch status {
	case entityui.BulkQueued, entityui.BulkDone, entityui.BulkStopped:
	default:
		return fmt.Errorf("bulk store: status %q is not queued, done or stopped", status)
	}
	res, err := s.db.ExecContext(ctx, fmt.Sprintf(`UPDATE %s SET status = $1 WHERE id = $2`, query.QuoteIdent(bulkJobsTable)), status, id)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return errBulkJobUnknown
	}
	return nil
}
