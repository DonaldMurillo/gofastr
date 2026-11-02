package a2a

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/DonaldMurillo/gofastr/core/datexport"
	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/internal/jsonexact"
)

type sqlDialect int

const (
	dialectSQLite sqlDialect = iota
	dialectPostgres
)

// SQLStore persists tasks and push configs in an existing *sql.DB
// (SQLite or Postgres, probed like battery/queue's detectDBDialect).
// The wire Task lives in a task_json column; the scalar columns exist
// only for owner scoping, filtering, and paging, so the JSON shape can
// evolve without a migration for every field. Construct with
// NewSQLStore; tables are created IF NOT EXISTS. Safe for concurrent
// use and shared across replicas.
type SQLStore struct {
	db        *sql.DB
	dialect   sqlDialect
	taskTable string
	pushTable string
}

// SQLStoreOption configures NewSQLStore.
type SQLStoreOption func(*SQLStore)

// WithTablePrefix puts both tables behind a prefix (e.g. "tenant1_"),
// for schemas shared by several services.
func WithTablePrefix(prefix string) SQLStoreOption {
	return func(s *SQLStore) { s.taskTable = prefix + s.taskTable; s.pushTable = prefix + s.pushTable }
}

// NewSQLStore constructs the store and ensures its tables exist.
func NewSQLStore(db *sql.DB, opts ...SQLStoreOption) (*SQLStore, error) {
	if db == nil {
		return nil, errors.New("a2a: nil db")
	}
	s := &SQLStore{
		db:        db,
		taskTable: "a2a_tasks",
		pushTable: "a2a_push_configs",
	}
	for _, opt := range opts {
		opt(s)
	}
	if _, err := query.SafeIdent(s.taskTable); err != nil {
		return nil, fmt.Errorf("a2a: task table name: %w", err)
	}
	if _, err := query.SafeIdent(s.pushTable); err != nil {
		return nil, fmt.Errorf("a2a: push table name: %w", err)
	}
	s.dialect = detectSQLDialect(db)
	if err := s.ensureTables(); err != nil {
		return nil, fmt.Errorf("a2a: ensure tables: %w", err)
	}
	s.registerData()
	return s, nil
}

// registerData declares both tables to App.ExportData and
// App.EraseUserData under their configured names. The owner column is
// the caller's user id (framework a2aOwnerPrincipal), and every task
// and push config holds that caller's messages, artifacts and callback
// URLs, so an erasure deletes them.
func (s *SQLStore) registerData() {
	datexport.Register(datexport.DataExporter{
		Name: datexport.ScopedName("a2a_tasks", s.taskTable), Source: "a2a", Table: s.taskTable, PrimaryKey: "id",
		Columns: []string{"id", "owner", "context_id", "skill_id", "scope", "tenant", "state", "status_ts", "version", "task_json", "created_at", "updated_at"},
	})
	datexport.RegisterEraser(datexport.DataEraser{
		Name: datexport.ScopedName("a2a_tasks", s.taskTable), Source: "a2a", Table: s.taskTable,
		Column: "owner", Mode: datexport.EraseDelete,
	})
	datexport.Register(datexport.DataExporter{
		Name: datexport.ScopedName("a2a_push_configs", s.pushTable), Source: "a2a", Table: s.pushTable, PrimaryKey: "task_id,id",
		Columns: []string{"task_id", "id", "owner", "config_json", "created_at"},
	})
	datexport.RegisterEraser(datexport.DataEraser{
		Name: datexport.ScopedName("a2a_push_configs", s.pushTable), Source: "a2a", Table: s.pushTable,
		Column: "owner", Mode: datexport.EraseDelete,
	})
}

// detectSQLDialect maps the shared query.IsPostgres probe onto the local
// dialect enum: a driver whose SELECT version() banner contains
// "postgresql" is Postgres, everything else (SQLite drivers return an
// error or a SQLite banner) is treated as SQLite.
func detectSQLDialect(db *sql.DB) sqlDialect {
	if query.IsPostgres(db) {
		return dialectPostgres
	}
	return dialectSQLite
}

// ph renders placeholder number n for the dialect: $n on Postgres, ?
// on SQLite.
func (s *SQLStore) ph(n int) string {
	if s.dialect == dialectPostgres {
		return fmt.Sprintf("$%d", n)
	}
	return "?"
}

// where is a tiny WHERE builder that keeps placeholder numbering
// aligned with the argument slice across dialects.
type where struct {
	store *SQLStore
	claus []string
	args  []any
	n     int
}

func (s *SQLStore) where(first string, v any) *where {
	w := &where{store: s}
	w.and(first, v)
	return w
}

func (w *where) and(clause string, v any) {
	w.n++
	w.claus = append(w.claus, fmt.Sprintf(clause, w.store.ph(w.n)))
	w.args = append(w.args, v)
}

func (w *where) sql() string {
	return strings.Join(w.claus, " AND ")
}

// addTextColumn upgrades a task table an earlier release created
// without column (scope, tenant): a TEXT NOT NULL column defaulting to
// the empty string, so the old rows read as made by an unscoped caller in
// no tenant. A scoped token, or a caller in a tenant, no longer sees the
// tasks it ran before the upgrade. column is one of this file's
// constants, never input.
func (s *SQLStore) addTextColumn(column string) error {
	if s.dialect == dialectPostgres {
		_, err := s.db.Exec(fmt.Sprintf(`ALTER TABLE %s ADD COLUMN IF NOT EXISTS %s TEXT NOT NULL DEFAULT ''`, query.QuoteFolded(s.taskTable), column))
		return err
	}
	_, err := s.db.Exec(fmt.Sprintf(`ALTER TABLE %s ADD COLUMN %s TEXT NOT NULL DEFAULT ''`, query.QuoteFolded(s.taskTable), column))
	if err != nil && strings.Contains(err.Error(), "duplicate column") {
		return nil
	}
	return err
}

func (s *SQLStore) ensureTables() error {
	tsType := "DATETIME"
	if s.dialect == dialectPostgres {
		tsType = "TIMESTAMPTZ"
		if err := query.RefuseCaseKeptTables(context.Background(), s.db, s.taskTable, s.pushTable); err != nil {
			return fmt.Errorf("a2a: %w", err)
		}
	}
	tasks := fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
		id         TEXT PRIMARY KEY,
		owner      TEXT NOT NULL,
		context_id TEXT NOT NULL DEFAULT '',
		skill_id   TEXT NOT NULL DEFAULT '',
		scope      TEXT NOT NULL DEFAULT '',
		tenant     TEXT NOT NULL DEFAULT '',
		state      TEXT NOT NULL,
		status_ts  %s NOT NULL,
		version    BIGINT NOT NULL DEFAULT 0,
		task_json  TEXT NOT NULL,
		created_at %s NOT NULL,
		updated_at %s NOT NULL
	)`, query.QuoteFolded(s.taskTable), tsType, tsType, tsType)
	if _, err := s.db.Exec(tasks); err != nil {
		return err
	}
	for _, column := range []string{"scope", "tenant"} {
		if err := s.addTextColumn(column); err != nil {
			return err
		}
	}
	push := fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
		task_id     TEXT NOT NULL,
		id          TEXT NOT NULL,
		owner       TEXT NOT NULL,
		config_json TEXT NOT NULL,
		created_at  %s NOT NULL,
		PRIMARY KEY (task_id, id)
	)`, query.QuoteFolded(s.pushTable), tsType)
	if _, err := s.db.Exec(push); err != nil {
		return err
	}
	// Serves ListTasks' owner filter plus its newest-first ordering.
	idx := fmt.Sprintf("CREATE INDEX IF NOT EXISTS %s (owner, status_ts)",
		query.IndexOn(s.dialect == dialectSQLite, query.QuoteIndexName(s.taskTable+"_owner_ts_idx"), query.QuoteFolded(s.taskTable)))
	if _, err := s.db.Exec(idx); err != nil {
		return err
	}
	return nil
}

// statusTsOf extracts the ordering timestamp. The server always stamps
// a status; a nil timestamp (only possible via direct store use)
// degrades to the zero instant.
func statusTsOf(t Task) time.Time {
	if t.Status.Timestamp != nil {
		return t.Status.Timestamp.Time
	}
	return time.Time{}
}

func (s *SQLStore) CreateTask(ctx context.Context, rec *TaskRecord) error {
	if rec == nil {
		return errors.New("a2a: nil task record")
	}
	if rec.CreatedAt.IsZero() {
		rec.CreatedAt = time.Now()
	}
	rec.UpdatedAt = rec.CreatedAt
	taskJSON, err := json.Marshal(rec.Task)
	if err != nil {
		return fmt.Errorf("a2a: marshal task: %w", err)
	}
	stmt := fmt.Sprintf(
		`INSERT INTO %s (id, owner, context_id, skill_id, scope, tenant, state, status_ts, version, task_json, created_at, updated_at)
		 VALUES (%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)`,
		query.QuoteFolded(s.taskTable),
		s.ph(1), s.ph(2), s.ph(3), s.ph(4), s.ph(5), s.ph(6), s.ph(7), s.ph(8), s.ph(9), s.ph(10), s.ph(11), s.ph(12))
	_, err = s.db.ExecContext(ctx, stmt,
		rec.Task.ID, rec.Owner, rec.Task.ContextID, rec.SkillID, rec.Scope, rec.Tenant,
		string(rec.Task.Status.State), statusTsOf(rec.Task), rec.Version,
		string(taskJSON), rec.CreatedAt, rec.UpdatedAt)
	if err != nil && isDuplicateRowErr(err) {
		return ErrConflict
	}
	return err
}

const taskCols = "task_json, skill_id, scope, tenant, version, created_at, updated_at"

func scanTask(row interface{ Scan(dest ...any) error }, owner string) (*TaskRecord, error) {
	var taskJSON, skillID, scope, tenant string
	var version int64
	var createdAt, updatedAt time.Time
	if err := row.Scan(&taskJSON, &skillID, &scope, &tenant, &version, &createdAt, &updatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	var task Task
	if err := jsonexact.Unmarshal([]byte(taskJSON), &task); err != nil {
		return nil, fmt.Errorf("a2a: unmarshal task: %w", err)
	}
	return &TaskRecord{
		Task:      task,
		Owner:     owner,
		SkillID:   skillID,
		Scope:     scope,
		Tenant:    tenant,
		Version:   version,
		CreatedAt: createdAt,
		UpdatedAt: updatedAt,
	}, nil
}

func (s *SQLStore) GetTask(ctx context.Context, owner, id string) (*TaskRecord, error) {
	stmt := fmt.Sprintf(`SELECT %s FROM %s WHERE id = %s AND owner = %s`,
		taskCols, query.QuoteFolded(s.taskTable), s.ph(1), s.ph(2))
	return scanTask(s.db.QueryRowContext(ctx, stmt, id, owner), owner)
}

// UpdateTask persists rec only when its Version matches the stored row;
// on success rec.Version is the new stored version and UpdatedAt is
// refreshed. Zero rows updated means conflict or absence, distinguished
// by one follow-up select on that path only.
func (s *SQLStore) UpdateTask(ctx context.Context, rec *TaskRecord) error {
	if rec == nil {
		return errors.New("a2a: nil task record")
	}
	taskJSON, err := json.Marshal(rec.Task)
	if err != nil {
		return fmt.Errorf("a2a: marshal task: %w", err)
	}
	now := time.Now()
	stmt := fmt.Sprintf(
		`UPDATE %s SET context_id = %s, skill_id = %s, state = %s, status_ts = %s,
		 version = version + 1, task_json = %s, updated_at = %s
		 WHERE id = %s AND owner = %s AND version = %s`,
		query.QuoteFolded(s.taskTable),
		s.ph(1), s.ph(2), s.ph(3), s.ph(4), s.ph(5), s.ph(6), s.ph(7), s.ph(8), s.ph(9))
	res, err := s.db.ExecContext(ctx, stmt,
		rec.Task.ContextID, rec.SkillID, string(rec.Task.Status.State), statusTsOf(rec.Task),
		string(taskJSON), now, rec.Task.ID, rec.Owner, rec.Version)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		// Distinguish a missing row from a version clash; both are
		// failures, with different meanings to the caller.
		var stored int64
		stmt := fmt.Sprintf(`SELECT version FROM %s WHERE id = %s AND owner = %s`,
			query.QuoteFolded(s.taskTable), s.ph(1), s.ph(2))
		err := s.db.QueryRowContext(ctx, stmt, rec.Task.ID, rec.Owner).Scan(&stored)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		return ErrConflict
	}
	rec.Version++
	rec.UpdatedAt = now
	return nil
}

// FiltersByScope implements ScopeFilter: ListTasks filters by
// ListQuery.Scoped in its query.
func (s *SQLStore) FiltersByScope() {}

// FiltersByTenant implements TenantFilter: ListTasks filters by
// ListQuery.Tenanted in its query.
func (s *SQLStore) FiltersByTenant() {}

func (s *SQLStore) ListTasks(ctx context.Context, owner string, q ListQuery) ([]*TaskRecord, int, error) {
	w := s.where("owner = %s", owner)
	if q.ContextID != "" {
		w.and("context_id = %s", q.ContextID)
	}
	if q.Status != "" {
		w.and("state = %s", q.Status)
	}
	if q.Scoped {
		w.and("scope = %s", q.Scope)
	}
	if q.Tenanted {
		w.and("tenant = %s", q.Tenant)
	}
	if !q.After.IsZero() {
		w.and("status_ts > %s", q.After)
	}
	countStmt := fmt.Sprintf(`SELECT COUNT(*) FROM %s WHERE %s`,
		query.QuoteFolded(s.taskTable), w.sql())
	var total int
	if err := s.db.QueryRowContext(ctx, countStmt, w.args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	listStmt := fmt.Sprintf(`SELECT %s FROM %s WHERE %s ORDER BY status_ts DESC, updated_at DESC, id DESC`,
		taskCols, query.QuoteFolded(s.taskTable), w.sql())
	args := append([]any(nil), w.args...)
	if q.Limit > 0 {
		w.n++
		listStmt += fmt.Sprintf(" LIMIT %s", s.ph(w.n))
		args = append(args, q.Limit)
	} else if q.Offset > 0 && s.dialect == dialectSQLite {
		// SQLite takes OFFSET only after a LIMIT; -1 is none.
		listStmt += " LIMIT -1"
	}
	if q.Offset > 0 {
		w.n++
		listStmt += fmt.Sprintf(" OFFSET %s", s.ph(w.n))
		args = append(args, q.Offset)
	}
	rows, err := s.db.QueryContext(ctx, listStmt, args...)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = rows.Close() }()
	var recs []*TaskRecord
	for rows.Next() {
		rec, err := scanTask(rows, owner)
		if err != nil {
			return nil, 0, err
		}
		recs = append(recs, rec)
	}
	return recs, total, rows.Err()
}

func (s *SQLStore) CreatePushConfig(ctx context.Context, rec *PushConfigRecord) error {
	if rec == nil {
		return errors.New("a2a: nil push config record")
	}
	if rec.CreatedAt.IsZero() {
		rec.CreatedAt = time.Now()
	}
	cfgJSON, err := json.Marshal(rec.Config)
	if err != nil {
		return fmt.Errorf("a2a: marshal push config: %w", err)
	}
	stmt := fmt.Sprintf(`INSERT INTO %s (task_id, id, owner, config_json, created_at) VALUES (%s,%s,%s,%s,%s)`,
		query.QuoteFolded(s.pushTable), s.ph(1), s.ph(2), s.ph(3), s.ph(4), s.ph(5))
	_, err = s.db.ExecContext(ctx, stmt, rec.Config.TaskID, rec.Config.ID, rec.Owner, string(cfgJSON), rec.CreatedAt)
	if err != nil && isDuplicateRowErr(err) {
		return ErrConflict
	}
	return err
}

func (s *SQLStore) scanPushConfig(row interface{ Scan(dest ...any) error }) (*PushConfigRecord, error) {
	var cfgJSON string
	var owner string
	var createdAt time.Time
	if err := row.Scan(&cfgJSON, &owner, &createdAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	var cfg PushNotificationConfig
	if err := json.Unmarshal([]byte(cfgJSON), &cfg); err != nil {
		return nil, fmt.Errorf("a2a: unmarshal push config: %w", err)
	}
	return &PushConfigRecord{Config: cfg, Owner: owner, CreatedAt: createdAt}, nil
}

func (s *SQLStore) GetPushConfig(ctx context.Context, owner, taskID, id string) (*PushConfigRecord, error) {
	stmt := fmt.Sprintf(`SELECT config_json, owner, created_at FROM %s WHERE task_id = %s AND id = %s AND owner = %s`,
		query.QuoteFolded(s.pushTable), s.ph(1), s.ph(2), s.ph(3))
	return s.scanPushConfig(s.db.QueryRowContext(ctx, stmt, taskID, id, owner))
}

func (s *SQLStore) ListPushConfigs(ctx context.Context, owner, taskID string) ([]*PushConfigRecord, error) {
	stmt := fmt.Sprintf(`SELECT config_json, owner, created_at FROM %s WHERE task_id = %s AND owner = %s ORDER BY created_at, id`,
		query.QuoteFolded(s.pushTable), s.ph(1), s.ph(2))
	rows, err := s.db.QueryContext(ctx, stmt, taskID, owner)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var recs []*PushConfigRecord
	for rows.Next() {
		rec, err := s.scanPushConfig(rows)
		if err != nil {
			return nil, err
		}
		recs = append(recs, rec)
	}
	return recs, rows.Err()
}

func (s *SQLStore) DeletePushConfig(ctx context.Context, owner, taskID, id string) error {
	stmt := fmt.Sprintf(`DELETE FROM %s WHERE task_id = %s AND id = %s AND owner = %s`,
		query.QuoteFolded(s.pushTable), s.ph(1), s.ph(2), s.ph(3))
	res, err := s.db.ExecContext(ctx, stmt, taskID, id, owner)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// TrimTerminalTasks implements RetentionTrimmer: it keeps the newest
// `keep` terminal rows of owner in tier (tenant and scope columns equal)
// and deletes the older ones; other tiers' rows are never read or
// deleted. The id walk then delete (instead of one DELETE ... LIMIT
// statement) keeps both dialects happy; ids are deleted in bounded
// batches.
func (s *SQLStore) TrimTerminalTasks(ctx context.Context, owner string, tier RetentionTier, keep int) error {
	if keep < 0 {
		return nil
	}
	stmt := fmt.Sprintf(`SELECT id FROM %s WHERE owner = %s AND tenant = %s AND scope = %s AND state IN (%s,%s,%s,%s) ORDER BY status_ts DESC, updated_at DESC, id DESC`,
		query.QuoteFolded(s.taskTable), s.ph(1), s.ph(2), s.ph(3), s.ph(4), s.ph(5), s.ph(6), s.ph(7))
	rows, err := s.db.QueryContext(ctx, stmt,
		owner, tier.Tenant, tier.Scope, TaskStateCompleted, TaskStateFailed, TaskStateCanceled, TaskStateRejected)
	if err != nil {
		return err
	}
	var pastKeep []string
	for i := 0; rows.Next(); i++ {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		if i >= keep {
			pastKeep = append(pastKeep, id)
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	if len(pastKeep) == 0 {
		return nil
	}
	if err := s.deleteTasksByID(ctx, owner, pastKeep); err != nil {
		return err
	}
	// A config registered while a trim deleted its task lands after the
	// task and its configs are gone: the handler's task check ran first.
	// Sweep the owner's configs whose task no longer exists, so a client
	// racing the trim cannot grow the table past the bound. A config newer
	// than orphanPushGrace may be a send whose task row is still to come.
	pt, tt := query.QuoteFolded(s.pushTable), query.QuoteFolded(s.taskTable)
	sweep := fmt.Sprintf(`DELETE FROM %s WHERE owner = %s AND created_at < %s AND NOT EXISTS (SELECT 1 FROM %s WHERE %s.id = %s.task_id)`,
		pt, s.ph(1), s.ph(2), tt, tt, pt)
	_, err = s.db.ExecContext(ctx, sweep, owner, time.Now().Add(-orphanPushGrace))
	return err
}

// deleteTasksByID removes owner-scoped rows by id in bounded batches,
// each batch's push configs with it in one transaction: a config whose
// task is gone can never fire again, and leaving it behind grew the
// push table without bound.
func (s *SQLStore) deleteTasksByID(ctx context.Context, owner string, ids []string) error {
	for len(ids) > 0 {
		n := min(len(ids), 100)
		if err := s.deleteTaskBatch(ctx, owner, ids[:n]); err != nil {
			return err
		}
		ids = ids[n:]
	}
	return nil
}

func (s *SQLStore) deleteTaskBatch(ctx context.Context, owner string, batch []string) (err error) {
	ph := make([]string, len(batch))
	args := make([]any, 0, len(batch)+1)
	args = append(args, owner)
	for i, id := range batch {
		ph[i] = s.ph(i + 2)
		args = append(args, id)
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
	pushStmt := fmt.Sprintf(`DELETE FROM %s WHERE owner = %s AND task_id IN (%s)`,
		query.QuoteFolded(s.pushTable), s.ph(1), strings.Join(ph, ","))
	if _, err = tx.ExecContext(ctx, pushStmt, args...); err != nil {
		return err
	}
	taskStmt := fmt.Sprintf(`DELETE FROM %s WHERE owner = %s AND id IN (%s)`,
		query.QuoteFolded(s.taskTable), s.ph(1), strings.Join(ph, ","))
	if _, err = tx.ExecContext(ctx, taskStmt, args...); err != nil {
		return err
	}
	return tx.Commit()
}

// TrimPushConfigs implements RetentionTrimmer: it keeps the newest
// `keep` configs for (owner, taskID) — the order ListPushConfigs
// serves — and deletes the older ones.
func (s *SQLStore) TrimPushConfigs(ctx context.Context, owner, taskID string, keep int) error {
	if keep < 0 {
		return nil
	}
	stmt := fmt.Sprintf(`SELECT id FROM %s WHERE task_id = %s AND owner = %s ORDER BY created_at, id`,
		query.QuoteFolded(s.pushTable), s.ph(1), s.ph(2))
	rows, err := s.db.QueryContext(ctx, stmt, taskID, owner)
	if err != nil {
		return err
	}
	var ids []string // oldest first, the order the query serves
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	if len(ids) <= keep {
		return nil
	}
	return s.deletePushConfigsByID(ctx, owner, taskID, ids[:len(ids)-keep])
}

// deletePushConfigsByID removes (owner, taskID)-scoped config rows by
// id in bounded batches.
func (s *SQLStore) deletePushConfigsByID(ctx context.Context, owner, taskID string, ids []string) error {
	for len(ids) > 0 {
		n := min(len(ids), 100)
		batch := ids[:n]
		ph := make([]string, len(batch))
		args := make([]any, 0, len(batch)+2)
		args = append(args, taskID, owner)
		for i, id := range batch {
			ph[i] = s.ph(i + 3)
			args = append(args, id)
		}
		stmt := fmt.Sprintf(`DELETE FROM %s WHERE task_id = %s AND owner = %s AND id IN (%s)`,
			query.QuoteFolded(s.pushTable), s.ph(1), s.ph(2), strings.Join(ph, ","))
		if _, err := s.db.ExecContext(ctx, stmt, args...); err != nil {
			return err
		}
		ids = ids[n:]
	}
	return nil
}

// isDuplicateRowErr reports whether err is a unique-constraint failure
// from INSERT, for the SQLite ("UNIQUE constraint failed") and Postgres
// ("duplicate key value") drivers.
func isDuplicateRowErr(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unique constraint") ||
		strings.Contains(msg, "duplicate key") ||
		strings.Contains(msg, "primary key")
}
