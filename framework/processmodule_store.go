package framework

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/framework/access"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
)

// ProcessModuleStore is the SQL-backed coordination substrate for
// process-isolated modules (design §8 "coordination substrate"). It persists
// the authoritative desired state and a replica-liveness registry on top of
// the same *sql.DB the app already uses, via an idempotent CREATE TABLE IF
// NOT EXISTS schema (mirrors battery/auth's EnsureSchema pattern — NOT a
// #33 migrate group; this is host coordination state, not module-owned DDL).
//
// It works on SQLite and Postgres. DDL is chosen per-dialect at
// [SQLProcessModuleStore.EnsureSchema] time.
//
// # State lease vs in-process fail-open (design §8)
//
// In-process module toggling [ModuleManager.handleRemoteToggle] FAILS OPEN on
// a store-read error (logs WARN, keeps cache). For a third-party module under
// revoke that is a security hole, so process modules INVERT it: the
// supervisor (above this store) holds a short-TTL state lease per replica; if
// it cannot refresh [GetDesired] past the lease TTL it drains children and
// serves 503/404. The lease policy lives in the supervisor, not here — this
// store is just rows. The divergence is documented at
// [ProcessModuleSupervisor.refreshDesired] and tested by the
// "store_unreachable_drains" test.
//
// # Revoke / upgrade lever
//
// Revoke (a grants change) and upgrade (an artifact change) are both
// expressed as a desired_generation bump: [BumpGeneration] atomically
// increments the row's counter and every replica's reconcile loop sees the
// higher value on its next [GetDesired] read. A grant-set change via
// [SetEffectiveGrants] also bumps generation (§5: a grant change is a
// generation change).
type ProcessModuleStore interface {
	// EnsureSchema creates the desired-state and heartbeat tables if
	// absent. Idempotent; safe to call on every boot.
	EnsureSchema(ctx context.Context) error

	// Install writes a new desired-state row for module. Returns
	// [ErrModuleInstalled] if a row already exists — re-install is an
	// explicit generation bump via [SetEffectiveGrants] /
	// [BumpGeneration].
	Install(ctx context.Context, d DesiredState) error

	// GetDesired returns the desired-state row for module. Returns
	// [ErrNoDesiredRow] if no row exists, and the underlying SQL error if
	// the store is unreachable (the lease-enforcing read path).
	GetDesired(ctx context.Context, module string) (DesiredState, error)

	// ListDesired returns every desired-state row, ordered by module name.
	// Used by introspection / a coordinator process.
	ListDesired(ctx context.Context) ([]DesiredState, error)

	// SetEnabled toggles enabled WITHOUT bumping generation (design §8:
	// enable/disable is not a revoke lever; the cache flip is the gate).
	SetEnabled(ctx context.Context, module string, enabled bool) error

	// BumpGeneration atomically increments desired_generation and returns
	// the new value. The revoke / upgrade lever: every replica's reconcile
	// loop sees the higher value on its next GetDesired read.
	BumpGeneration(ctx context.Context, module string) (uint64, error)

	// SetEffectiveGrants replaces the effective grant JSON AND bumps
	// generation (§5: a grant change is a generation change). Returns the
	// new generation.
	SetEffectiveGrants(ctx context.Context, module string, grants []access.Permission) (uint64, error)

	// SetMigrationsAppliedAt records the timestamp pending migrations
	// finished at. Nil clears it.
	SetMigrationsAppliedAt(ctx context.Context, module string, at *time.Time) error

	// RecordHeartbeat upserts a replica heartbeat row (module, replica_id)
	// with observed_generation, phase, and updated_at = now. The TTL
	// semantics are READ-side (LiveReplicas): a row older than 3× the
	// heartbeat interval is treated as a dead replica, not a blocker.
	RecordHeartbeat(ctx context.Context, module, replicaID string, observedGen uint64, phase string) error

	// LiveReplicas returns heartbeat rows for module whose updated_at is
	// within now-ttl..now. Rows older than ttl are dead and excluded.
	LiveReplicas(ctx context.Context, module string, ttl time.Duration) ([]ReplicaHeartbeat, error)

	// DeleteHeartbeat removes a replica's heartbeat row. Called on
	// graceful drain so the next LiveReplicas reflects the departure.
	DeleteHeartbeat(ctx context.Context, module, replicaID string) error
}

// DesiredState is the authoritative desired-state row for one module
// (design §8). Every field here is operator-/host-authored; the child never
// writes any of it.
type DesiredState struct {
	Module string

	// DesiredGeneration is the monotonic desired-state counter. READ at
	// spawn (not minted); bumped only on a grant change, revoke, or
	// upgrade. The restart circuit is keyed to (module, generation).
	DesiredGeneration uint64

	// Enabled is the desired enable/disable state. The supervisor's
	// reconcile loop reads this; the cache flip is what the route gate
	// actually checks.
	Enabled bool

	// ArtifactSHA256 is the approved executable digest. Verified by the
	// runner before exec.
	ArtifactSHA256 string

	// EffectiveGrants is the post-approval, post-carve-out grant set. A
	// change here bumps DesiredGeneration.
	EffectiveGrants []access.Permission

	// MigrationsAppliedAt is nil until the migration coordinator has run
	// pending migrations; the supervisor refuses Ready while nil for a
	// module with a declared migration group.
	MigrationsAppliedAt *time.Time
}

// ReplicaHeartbeat is one replica's liveness row for a module (design §8).
type ReplicaHeartbeat struct {
	Module      string
	ReplicaID   string
	Generation  uint64
	Phase       string
	UpdatedAtMs int64
}

// ErrModuleInstalled is returned by [ProcessModuleStore.Install] when a row
// already exists for the module.
var ErrModuleInstalled = errors.New("processmodule: module already installed")

// ErrNoDesiredRow is returned by [ProcessModuleStore.GetDesired] when the
// module has no desired-state row.
var ErrNoDesiredRow = errors.New("processmodule: no desired-state row")

// SQLProcessModuleStore is the SQL-backed [ProcessModuleStore]. It owns no
// state beyond the *sql.DB; concurrent supervisors (one per replica) share
// the same store.
type SQLProcessModuleStore struct {
	db      *sql.DB
	dialect migrate.Dialect
}

// NewSQLProcessModuleStore constructs a SQL-backed store over db. EnsureSchema
// is NOT called here; callers (the app wiring) call it explicitly at boot so
// the schema-creation error surfaces where the operator expects it.
func NewSQLProcessModuleStore(db *sql.DB) (*SQLProcessModuleStore, error) {
	if db == nil {
		return nil, errors.New("processmodule: NewSQLProcessModuleStore(nil db)")
	}
	return &SQLProcessModuleStore{db: db, dialect: migrate.DetectDialect(db)}, nil
}

// Dialect reports the detected dialect (debug / introspection).
func (s *SQLProcessModuleStore) Dialect() migrate.Dialect { return s.dialect }

// Tables.
const (
	desiredTable   = "gofastr_process_modules"
	heartbeatTable = "gofastr_process_module_heartbeats"
)

// EnsureSchema creates the desired-state and heartbeat tables idempotently.
// DDL is dialect-aware (Postgres vs SQLite). The schema is intentionally
// narrow: no module-owned DDL touches this store (design §7 — host
// coordination state, NOT module bookkeeping).
func (s *SQLProcessModuleStore) EnsureSchema(ctx context.Context) error {
	if s.dialect == migrate.DialectPostgres {
		return s.ensureSchemaPostgres(ctx)
	}
	return s.ensureSchemaSQLite(ctx)
}

func (s *SQLProcessModuleStore) ensureSchemaPostgres(ctx context.Context) error {
	stmts := []string{
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
			module            TEXT PRIMARY KEY,
			desired_generation BIGINT NOT NULL DEFAULT 1,
			enabled           BOOLEAN NOT NULL DEFAULT FALSE,
			artifact_sha256   TEXT NOT NULL,
			effective_grants  JSONB NOT NULL DEFAULT '[]'::jsonb,
			migrations_applied_at BIGINT NULL
		)`, query.QuoteIdent(desiredTable)),
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
			module            TEXT NOT NULL,
			replica_id        TEXT NOT NULL,
			observed_generation BIGINT NOT NULL DEFAULT 0,
			phase             TEXT NOT NULL DEFAULT '',
			updated_at_ms     BIGINT NOT NULL,
			PRIMARY KEY (module, replica_id)
		)`, query.QuoteIdent(heartbeatTable)),
	}
	for _, q := range stmts {
		if _, err := s.db.ExecContext(ctx, q); err != nil {
			return fmt.Errorf("processmodule: ensure schema (postgres): %w", err)
		}
	}
	return nil
}

func (s *SQLProcessModuleStore) ensureSchemaSQLite(ctx context.Context) error {
	stmts := []string{
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
			module            TEXT PRIMARY KEY,
			desired_generation INTEGER NOT NULL DEFAULT 1,
			enabled           INTEGER NOT NULL DEFAULT 0,
			artifact_sha256   TEXT NOT NULL,
			effective_grants  TEXT NOT NULL DEFAULT '[]',
			migrations_applied_at INTEGER NULL
		)`, query.QuoteIdent(desiredTable)),
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
			module            TEXT NOT NULL,
			replica_id        TEXT NOT NULL,
			observed_generation INTEGER NOT NULL DEFAULT 0,
			phase             TEXT NOT NULL DEFAULT '',
			updated_at_ms     INTEGER NOT NULL,
			PRIMARY KEY (module, replica_id)
		)`, query.QuoteIdent(heartbeatTable)),
	}
	for _, q := range stmts {
		if _, err := s.db.ExecContext(ctx, q); err != nil {
			return fmt.Errorf("processmodule: ensure schema (sqlite): %w", err)
		}
	}
	return nil
}

// Install writes a new desired-state row. Returns ErrModuleInstalled if a
// row already exists for the module.
func (s *SQLProcessModuleStore) Install(ctx context.Context, d DesiredState) error {
	if d.Module == "" {
		return errors.New("processmodule: Install: empty module name")
	}
	if d.DesiredGeneration == 0 {
		d.DesiredGeneration = 1 // first install starts at gen 1
	}
	grantsJSON, err := marshalGrants(d.EffectiveGrants)
	if err != nil {
		return fmt.Errorf("processmodule: Install %q: %w", d.Module, err)
	}
	enabledInt := boolToInt(d.Enabled)
	// INSERT OR FAIL semantics: on the PK collision, return ErrModuleInstalled.
	// On Postgres we use ON CONFLICT DO NOTHING + check affected rows; on
	// SQLite we use INSERT OR IGNORE + check. Both let us distinguish
	// "row existed" from a real SQL error.
	var q string
	var args []any
	if s.dialect == migrate.DialectPostgres {
		q = fmt.Sprintf(`INSERT INTO %s (module, desired_generation, enabled, artifact_sha256, effective_grants, migrations_applied_at)
			VALUES ($1, $2, $3, $4, $5, $6) ON CONFLICT (module) DO NOTHING`, query.QuoteIdent(desiredTable))
		args = []any{d.Module, int64(d.DesiredGeneration), enabledInt, d.ArtifactSHA256, grantsJSON, timeToMillis(d.MigrationsAppliedAt)}
	} else {
		q = fmt.Sprintf(`INSERT OR IGNORE INTO %s (module, desired_generation, enabled, artifact_sha256, effective_grants, migrations_applied_at)
			VALUES (?, ?, ?, ?, ?, ?)`, query.QuoteIdent(desiredTable))
		args = []any{d.Module, int64(d.DesiredGeneration), enabledInt, d.ArtifactSHA256, string(grantsJSON), timeToMillis(d.MigrationsAppliedAt)}
	}
	res, err := s.db.ExecContext(ctx, q, args...)
	if err != nil {
		return fmt.Errorf("processmodule: Install %q: %w", d.Module, err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("processmodule: %w: %q", ErrModuleInstalled, d.Module)
	}
	return nil
}

// GetDesired returns the desired-state row, or ErrNoDesiredRow if absent.
func (s *SQLProcessModuleStore) GetDesired(ctx context.Context, module string) (DesiredState, error) {
	q := fmt.Sprintf(`SELECT module, desired_generation, enabled, artifact_sha256, effective_grants, migrations_applied_at FROM %s WHERE module = %s`,
		query.QuoteIdent(desiredTable), place(s.dialect, 1))
	row := s.db.QueryRowContext(ctx, q, module)
	d, err := scanDesired(row, s.dialect)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return DesiredState{}, fmt.Errorf("processmodule: %w: %q", ErrNoDesiredRow, module)
		}
		return DesiredState{}, err
	}
	return d, nil
}

// ListDesired returns every desired-state row, ordered by module name.
func (s *SQLProcessModuleStore) ListDesired(ctx context.Context) ([]DesiredState, error) {
	q := fmt.Sprintf(`SELECT module, desired_generation, enabled, artifact_sha256, effective_grants, migrations_applied_at FROM %s ORDER BY module ASC`,
		query.QuoteIdent(desiredTable))
	rows, err := s.db.QueryContext(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("processmodule: ListDesired: %w", err)
	}
	defer rows.Close()
	var out []DesiredState
	for rows.Next() {
		d, err := scanDesired(rows, s.dialect)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// SetEnabled toggles enabled without bumping generation.
func (s *SQLProcessModuleStore) SetEnabled(ctx context.Context, module string, enabled bool) error {
	q := fmt.Sprintf(`UPDATE %s SET enabled = %s WHERE module = %s`,
		query.QuoteIdent(desiredTable), place(s.dialect, 1), place(s.dialect, 2))
	res, err := s.db.ExecContext(ctx, q, boolToInt(enabled), module)
	if err != nil {
		return fmt.Errorf("processmodule: SetEnabled %q: %w", module, err)
	}
	return AssertRow(res, module)
}

// BumpGeneration atomically increments desired_generation and returns the
// new value.
func (s *SQLProcessModuleStore) BumpGeneration(ctx context.Context, module string) (uint64, error) {
	// Postgres supports RETURNING; SQLite (pre 3.35) does too in modern
	// drivers, but we play safe with a read-after-update under a tx.
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("processmodule: BumpGeneration %q: begin: %w", module, err)
	}
	defer tx.Rollback() //nolint:errcheck
	upd := fmt.Sprintf(`UPDATE %s SET desired_generation = desired_generation + 1 WHERE module = %s`,
		query.QuoteIdent(desiredTable), place(s.dialect, 1))
	res, err := tx.ExecContext(ctx, upd, module)
	if err != nil {
		return 0, fmt.Errorf("processmodule: BumpGeneration %q: update: %w", module, err)
	}
	if err := AssertRow(res, module); err != nil {
		return 0, err
	}
	var gen int64
	sel := fmt.Sprintf(`SELECT desired_generation FROM %s WHERE module = %s`,
		query.QuoteIdent(desiredTable), place(s.dialect, 1))
	if err := tx.QueryRowContext(ctx, sel, module).Scan(&gen); err != nil {
		return 0, fmt.Errorf("processmodule: BumpGeneration %q: readback: %w", module, err)
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("processmodule: BumpGeneration %q: commit: %w", module, err)
	}
	return uint64(gen), nil
}

// SetEffectiveGrants replaces the effective grant JSON AND bumps generation.
func (s *SQLProcessModuleStore) SetEffectiveGrants(ctx context.Context, module string, grants []access.Permission) (uint64, error) {
	grantsJSON, err := marshalGrants(grants)
	if err != nil {
		return 0, fmt.Errorf("processmodule: SetEffectiveGrants %q: %w", module, err)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("processmodule: SetEffectiveGrants %q: begin: %w", module, err)
	}
	defer tx.Rollback() //nolint:errcheck
	upd := fmt.Sprintf(`UPDATE %s SET effective_grants = %s, desired_generation = desired_generation + 1 WHERE module = %s`,
		query.QuoteIdent(desiredTable), place(s.dialect, 1), place(s.dialect, 2))
	res, err := tx.ExecContext(ctx, upd, string(grantsJSON), module)
	if err != nil {
		return 0, fmt.Errorf("processmodule: SetEffectiveGrants %q: update: %w", module, err)
	}
	if err := AssertRow(res, module); err != nil {
		return 0, err
	}
	var gen int64
	sel := fmt.Sprintf(`SELECT desired_generation FROM %s WHERE module = %s`,
		query.QuoteIdent(desiredTable), place(s.dialect, 1))
	if err := tx.QueryRowContext(ctx, sel, module).Scan(&gen); err != nil {
		return 0, fmt.Errorf("processmodule: SetEffectiveGrants %q: readback: %w", module, err)
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("processmodule: SetEffectiveGrants %q: commit: %w", module, err)
	}
	return uint64(gen), nil
}

// SetMigrationsAppliedAt records (or clears) the migrations-applied timestamp.
func (s *SQLProcessModuleStore) SetMigrationsAppliedAt(ctx context.Context, module string, at *time.Time) error {
	q := fmt.Sprintf(`UPDATE %s SET migrations_applied_at = %s WHERE module = %s`,
		query.QuoteIdent(desiredTable), place(s.dialect, 1), place(s.dialect, 2))
	res, err := s.db.ExecContext(ctx, q, timeToMillis(at), module)
	if err != nil {
		return fmt.Errorf("processmodule: SetMigrationsAppliedAt %q: %w", module, err)
	}
	return AssertRow(res, module)
}

// RecordHeartbeat upserts a replica heartbeat row.
func (s *SQLProcessModuleStore) RecordHeartbeat(ctx context.Context, module, replicaID string, observedGen uint64, phase string) error {
	if module == "" || replicaID == "" {
		return errors.New("processmodule: RecordHeartbeat: empty module or replica_id")
	}
	now := time.Now().UnixMilli()
	var q string
	var args []any
	if s.dialect == migrate.DialectPostgres {
		q = fmt.Sprintf(`INSERT INTO %s (module, replica_id, observed_generation, phase, updated_at_ms)
			VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT (module, replica_id) DO UPDATE SET
				observed_generation = EXCLUDED.observed_generation,
				phase = EXCLUDED.phase,
				updated_at_ms = EXCLUDED.updated_at_ms`,
			query.QuoteIdent(heartbeatTable))
		args = []any{module, replicaID, int64(observedGen), phase, now}
	} else {
		q = fmt.Sprintf(`INSERT INTO %s (module, replica_id, observed_generation, phase, updated_at_ms)
			VALUES (?, ?, ?, ?, ?)
			ON CONFLICT (module, replica_id) DO UPDATE SET
				observed_generation = excluded.observed_generation,
				phase = excluded.phase,
				updated_at_ms = excluded.updated_at_ms`,
			query.QuoteIdent(heartbeatTable))
		args = []any{module, replicaID, int64(observedGen), phase, now}
	}
	if _, err := s.db.ExecContext(ctx, q, args...); err != nil {
		return fmt.Errorf("processmodule: RecordHeartbeat %q/%q: %w", module, replicaID, err)
	}
	return nil
}

// LiveReplicas returns heartbeat rows whose updated_at is within now-ttl..now.
func (s *SQLProcessModuleStore) LiveReplicas(ctx context.Context, module string, ttl time.Duration) ([]ReplicaHeartbeat, error) {
	cutoff := time.Now().Add(-ttl).UnixMilli()
	q := fmt.Sprintf(`SELECT module, replica_id, observed_generation, phase, updated_at_ms FROM %s
		WHERE module = %s AND updated_at_ms >= %s ORDER BY replica_id ASC`,
		query.QuoteIdent(heartbeatTable), place(s.dialect, 1), place(s.dialect, 2))
	rows, err := s.db.QueryContext(ctx, q, module, cutoff)
	if err != nil {
		return nil, fmt.Errorf("processmodule: LiveReplicas %q: %w", module, err)
	}
	defer rows.Close()
	var out []ReplicaHeartbeat
	for rows.Next() {
		var h ReplicaHeartbeat
		var gen int64
		if err := rows.Scan(&h.Module, &h.ReplicaID, &gen, &h.Phase, &h.UpdatedAtMs); err != nil {
			return nil, err
		}
		h.Generation = uint64(gen)
		out = append(out, h)
	}
	return out, rows.Err()
}

// DeleteHeartbeat removes a replica's heartbeat row.
func (s *SQLProcessModuleStore) DeleteHeartbeat(ctx context.Context, module, replicaID string) error {
	q := fmt.Sprintf(`DELETE FROM %s WHERE module = %s AND replica_id = %s`,
		query.QuoteIdent(heartbeatTable), place(s.dialect, 1), place(s.dialect, 2))
	_, err := s.db.ExecContext(ctx, q, module, replicaID)
	if err != nil {
		return fmt.Errorf("processmodule: DeleteHeartbeat %q/%q: %w", module, replicaID, err)
	}
	return nil
}

// AssertRow wraps a sql.Result from an UPDATE, translating zero-rows-affected
// into ErrNoDesiredRow so callers see a single typed error.
func AssertRow(res sql.Result, module string) error {
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("processmodule: %q: rows affected: %w", module, err)
	}
	if n == 0 {
		return fmt.Errorf("processmodule: %w: %q", ErrNoDesiredRow, module)
	}
	return nil
}

// scanner is the common subset of *sql.Row and *sql.Rows.
type scanner interface {
	Scan(dest ...any) error
}

func scanDesired(sc scanner, dialect migrate.Dialect) (DesiredState, error) {
	var d DesiredState
	var gen int64
	var grantsRaw string
	var migMs sql.NullInt64
	// `enabled` is BOOLEAN on Postgres and INTEGER (0/1) on SQLite; scanning
	// a PG bool into *int fails ("converting driver.Value type bool to int"),
	// which used to abort the whole row scan before migrations_applied_at
	// was reached. Scan into an any and normalize per-dialect so both paths
	// read every column.
	var enabledVal any
	if err := sc.Scan(&d.Module, &gen, &enabledVal, &d.ArtifactSHA256, &grantsRaw, &migMs); err != nil {
		return DesiredState{}, err
	}
	d.DesiredGeneration = uint64(gen)
	d.Enabled = enabledBool(enabledVal)
	d.EffectiveGrants = unmarshalGrants(grantsRaw)
	if migMs.Valid {
		t := time.UnixMilli(migMs.Int64)
		d.MigrationsAppliedAt = &t
	}
	_ = dialect
	return d, nil
}

// enabledBool normalizes the `enabled` column across dialects: Postgres
// returns a bool, SQLite returns an int64 (0/1). Any other shape is false.
func enabledBool(v any) bool {
	switch x := v.(type) {
	case bool:
		return x
	case int64:
		return x != 0
	case int:
		return x != 0
	case []byte:
		return len(x) > 0 && (x[0] == '1' || x[0] == 't' || x[0] == 'T')
	case string:
		return x == "t" || x == "true" || x == "1"
	}
	return false
}

// marshalGrants serializes a grant set as a JSON string array.
func marshalGrants(grants []access.Permission) ([]byte, error) {
	if grants == nil {
		grants = []access.Permission{}
	}
	strs := make([]string, len(grants))
	for i, g := range grants {
		strs[i] = string(g)
	}
	b, err := json.Marshal(strs)
	if err != nil {
		return nil, fmt.Errorf("marshal grants: %w", err)
	}
	return b, nil
}

// unmarshalGrants parses a JSON string array back into a grant set.
func unmarshalGrants(raw string) []access.Permission {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "null" {
		return nil
	}
	var strs []string
	if err := json.Unmarshal([]byte(raw), &strs); err != nil {
		return nil
	}
	if len(strs) == 0 {
		return nil
	}
	out := make([]access.Permission, len(strs))
	for i, s := range strs {
		out[i] = access.Permission(s)
	}
	return out
}

// place returns the n-th dialect placeholder (Postgres: $n; SQLite: ?).
func place(dialect migrate.Dialect, n int) string {
	if dialect == migrate.DialectPostgres {
		return fmt.Sprintf("$%d", n)
	}
	return "?"
}

// boolToInt maps a bool to the integer encoding used in both dialects.
func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// timeToMillis encodes a *time.Time as epoch millis (sql.NullInt64-friendly).
func timeToMillis(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.UnixMilli()
}
