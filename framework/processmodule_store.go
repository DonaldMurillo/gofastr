package framework

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
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
// NOT EXISTS schema (mirrors battery/auth's EnsureSchema pattern, NOT a
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
// serves 503/404. The lease policy lives in the supervisor, not here. This
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
	// [ErrModuleInstalled] if a row already exists, re-install is an
	// explicit generation bump via [SetEffectiveGrants] /
	// [BumpGeneration], or the boot-time [InstallOrUpdate].
	Install(ctx context.Context, d DesiredState) error

	// InstallOrUpdate inserts the row when absent and otherwise
	// reconciles the stored row with d: the path a module takes when the
	// app boots again against a database an earlier boot (or another
	// replica) wrote. [InstallOutcome] spells out each case; a grant-set
	// change disables the module.
	InstallOrUpdate(ctx context.Context, d DesiredState) (InstallResult, error)

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

	// RemoveEffectiveGrants takes grants out of the effective grant set,
	// keeping the rest, AND bumps generation. Two removals at once both
	// stick. It refuses an empty grants. Returns the new generation.
	RemoveEffectiveGrants(ctx context.Context, module string, grants []access.Permission) (uint64, error)

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

	// RevokedGrants is every grant an operator took away at runtime
	// ([ProcessModuleStore.RemoveEffectiveGrants], or a narrower
	// [ProcessModuleStore.SetEffectiveGrants]). The boot-time
	// [ProcessModuleStore.InstallOrUpdate] subtracts it from the
	// descriptor's grants, so a revoke outlives the next boot. A grant
	// SetEffectiveGrants gives back leaves it.
	RevokedGrants []access.Permission

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
	// afterGrantsRead, when set, runs between RemoveEffectiveGrants'
	// read and its write: a test's window for a concurrent write.
	afterGrantsRead func()
}

// NewSQLProcessModuleStore constructs a SQL-backed store over db. EnsureSchema
// is NOT called here; callers (the app wiring) call it explicitly at boot so
// the schema-creation error surfaces where the operator expects it.
func NewSQLProcessModuleStore(db *sql.DB) (*SQLProcessModuleStore, error) {
	if db == nil {
		return nil, errors.New("processmodule: NewSQLProcessModuleStore(nil db)")
	}
	// Schema decisions (column types, INSERT OR IGNORE vs ON CONFLICT) hang
	// off this answer and it is cached for the store's lifetime, so a
	// transient probe failure must not resolve to a guessed SQLite.
	dialect, err := migrate.DetectDialectStrict(db)
	if err != nil {
		return nil, fmt.Errorf("processmodule: NewSQLProcessModuleStore: %w", err)
	}
	return &SQLProcessModuleStore{db: db, dialect: dialect}, nil
}

// Dialect reports the detected dialect (debug / introspection).
func (s *SQLProcessModuleStore) Dialect() migrate.Dialect { return s.dialect }

// Tables.
const (
	desiredTable   = "gofastr_process_modules"
	heartbeatTable = "gofastr_process_module_heartbeats"
)

// EnsureSchema creates the desired-state and heartbeat tables idempotently.
// DDL is dialect-aware (Postgres vs SQLite): the two dialects differ only
// in type words (BIGINT/INTEGER, BOOLEAN/INTEGER, JSONB/TEXT grants), so
// one statement pair carries both. It replaces the former per-dialect
// ensureSchemaPostgres / ensureSchemaSQLite copies. The schema is
// intentionally narrow: no module-owned DDL touches this store (design
// §7, host coordination state, NOT module bookkeeping).
func (s *SQLProcessModuleStore) EnsureSchema(ctx context.Context) error {
	label := "sqlite"
	intType, boolType, boolDefault := "INTEGER", "INTEGER", "0"
	grantsType := "TEXT NOT NULL DEFAULT '[]'"
	if s.dialect == migrate.DialectPostgres {
		label = "postgres"
		intType, boolType, boolDefault = "BIGINT", "BOOLEAN", "FALSE"
		grantsType = "JSONB NOT NULL DEFAULT '[]'::jsonb"
	}
	stmts := []string{
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
			module            TEXT PRIMARY KEY,
			desired_generation %s NOT NULL DEFAULT 1,
			enabled           %s NOT NULL DEFAULT %s,
			artifact_sha256   TEXT NOT NULL,
			effective_grants  %s,
			migrations_applied_at %s NULL,
			revoked_grants    %s
		)`, query.QuoteFolded(desiredTable), intType, boolType, boolDefault, grantsType, intType, grantsType),
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
			module            TEXT NOT NULL,
			replica_id        TEXT NOT NULL,
			observed_generation %s NOT NULL DEFAULT 0,
			phase             TEXT NOT NULL DEFAULT '',
			updated_at_ms     %s NOT NULL,
			PRIMARY KEY (module, replica_id)
		)`, query.QuoteFolded(heartbeatTable), intType, intType),
	}
	for _, q := range stmts {
		if _, err := s.db.ExecContext(ctx, q); err != nil {
			return fmt.Errorf("processmodule: ensure schema (%s): %w", label, err)
		}
	}
	return s.addRevokedColumn(ctx, grantsType)
}

// addRevokedColumn adds revoked_grants to a desired-state table an older
// build created without it. Postgres says ADD COLUMN IF NOT EXISTS; SQLite
// has no such clause, so the catalog is read first, and a failed ADD
// rereads it and passes when another replica added the column meanwhile.
func (s *SQLProcessModuleStore) addRevokedColumn(ctx context.Context, grantsType string) error {
	const col = "revoked_grants"
	clause := "ADD COLUMN"
	if s.dialect == migrate.DialectPostgres {
		clause = "ADD COLUMN IF NOT EXISTS"
	} else {
		live, err := migrate.ReadLiveColumns(ctx, s.db, desiredTable, s.dialect)
		if err != nil {
			return fmt.Errorf("processmodule: ensure schema: read columns: %w", err)
		}
		if _, ok := live[col]; ok {
			return nil
		}
	}
	alter := fmt.Sprintf("ALTER TABLE %s %s %s %s", query.QuoteFolded(desiredTable), clause, col, grantsType)
	if _, err := s.db.ExecContext(ctx, alter); err != nil {
		now, rerr := migrate.ReadLiveColumns(ctx, s.db, desiredTable, s.dialect)
		if _, ok := now[col]; rerr != nil || !ok {
			return fmt.Errorf("processmodule: ensure schema: add %s: %w", col, err)
		}
	}
	return nil
}

// desiredColumns is the column list scanDesired reads, in its order.
const desiredColumns = "module, desired_generation, enabled, artifact_sha256, effective_grants, migrations_applied_at, revoked_grants"

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
	inserted, err := s.insertIfAbsent(ctx, s.db, d, grantsJSON)
	if err != nil {
		return fmt.Errorf("processmodule: Install %q: %w", d.Module, err)
	}
	if !inserted {
		return fmt.Errorf("processmodule: %w: %q", ErrModuleInstalled, d.Module)
	}
	return nil
}

// execer is the ExecContext half shared by *sql.DB and *sql.Tx.
type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// insertIfAbsent inserts d's row unless one exists for the module, and
// reports whether it inserted. On Postgres it is ON CONFLICT DO NOTHING, on
// SQLite INSERT OR IGNORE; both tell "row existed" apart from a SQL error
// through the affected-row count.
func (s *SQLProcessModuleStore) insertIfAbsent(ctx context.Context, ex execer, d DesiredState, grantsJSON []byte) (bool, error) {
	enabledInt := boolToInt(d.Enabled)
	revokedJSON, err := marshalGrants(d.RevokedGrants)
	if err != nil {
		return false, err
	}
	var q string
	var args []any
	if s.dialect == migrate.DialectPostgres {
		q = fmt.Sprintf(`INSERT INTO %s (module, desired_generation, enabled, artifact_sha256, effective_grants, migrations_applied_at, revoked_grants)
			VALUES ($1, $2, $3, $4, $5, $6, $7) ON CONFLICT (module) DO NOTHING`, query.QuoteFolded(desiredTable))
		args = []any{d.Module, int64(d.DesiredGeneration), enabledInt, d.ArtifactSHA256, grantsJSON, timeToMillis(d.MigrationsAppliedAt), revokedJSON}
	} else {
		q = fmt.Sprintf(`INSERT OR IGNORE INTO %s (module, desired_generation, enabled, artifact_sha256, effective_grants, migrations_applied_at, revoked_grants)
			VALUES (?, ?, ?, ?, ?, ?, ?)`, query.QuoteFolded(desiredTable))
		args = []any{d.Module, int64(d.DesiredGeneration), enabledInt, d.ArtifactSHA256, string(grantsJSON), timeToMillis(d.MigrationsAppliedAt), string(revokedJSON)}
	}
	res, err := ex.ExecContext(ctx, q, args...)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// InstallOutcome says what [ProcessModuleStore.InstallOrUpdate] did with
// the row it found.
type InstallOutcome int

const (
	// InstallCreated: no row existed; one was inserted as by Install.
	InstallCreated InstallOutcome = iota + 1
	// InstallUnchanged: the row's artifact digest and grant set match;
	// nothing was written, so the enabled flag and generation stand.
	InstallUnchanged
	// InstallArtifactChanged: only the artifact digest differs (a
	// deploy). The digest is updated and the generation bumped; the
	// enabled flag stands.
	InstallArtifactChanged
	// InstallGrantsChanged: the descriptor's grants, less the stored
	// revokes, add a grant the row does not hold. Grants and digest are
	// updated, the generation bumped, and the module disabled until an
	// operator re-enables it.
	InstallGrantsChanged
	// InstallGrantsNarrowed: the descriptor's grants, less the stored
	// revokes, drop grants the row holds and add none. Grants and digest
	// are updated and the generation bumped; the enabled flag stands,
	// since nothing new runs unseen.
	InstallGrantsNarrowed
)

// String names the outcome for logs.
func (o InstallOutcome) String() string {
	switch o {
	case InstallCreated:
		return "created"
	case InstallUnchanged:
		return "unchanged"
	case InstallArtifactChanged:
		return "artifact-changed"
	case InstallGrantsChanged:
		return "grants-changed"
	case InstallGrantsNarrowed:
		return "grants-narrowed"
	}
	return "unknown"
}

// InstallResult is what [ProcessModuleStore.InstallOrUpdate] returns.
type InstallResult struct {
	Outcome InstallOutcome
	// Row is the desired-state row as stored after the call.
	Row DesiredState
	// PreviousGrants is the grant set the row held before a grants
	// change or narrowing; nil for every other outcome.
	PreviousGrants []access.Permission
}

// InstallOrUpdate is the boot-time install: the same module registering
// again (a restart, or another replica) against a row an earlier boot
// wrote. It inserts the row when absent and otherwise reconciles it with
// d in one transaction, by the rules on [InstallOutcome]. d.Enabled is
// used only for a new row; an existing row's flag is never raised here.
// For an existing row the grant set written is d.EffectiveGrants less the
// row's RevokedGrants: a runtime revoke wins over the descriptor that
// still requests the grant.
func (s *SQLProcessModuleStore) InstallOrUpdate(ctx context.Context, d DesiredState) (InstallResult, error) {
	if d.Module == "" {
		return InstallResult{}, errors.New("processmodule: InstallOrUpdate: empty module name")
	}
	if d.DesiredGeneration == 0 {
		d.DesiredGeneration = 1
	}
	grantsJSON, err := marshalGrants(d.EffectiveGrants)
	if err != nil {
		return InstallResult{}, fmt.Errorf("processmodule: InstallOrUpdate %q: %w", d.Module, err)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return InstallResult{}, fmt.Errorf("processmodule: InstallOrUpdate %q: begin: %w", d.Module, err)
	}
	defer tx.Rollback() //nolint:errcheck
	inserted, err := s.insertIfAbsent(ctx, tx, d, grantsJSON)
	if err != nil {
		return InstallResult{}, fmt.Errorf("processmodule: InstallOrUpdate %q: insert: %w", d.Module, err)
	}
	if inserted {
		if err := tx.Commit(); err != nil {
			return InstallResult{}, fmt.Errorf("processmodule: InstallOrUpdate %q: commit: %w", d.Module, err)
		}
		return InstallResult{Outcome: InstallCreated, Row: d}, nil
	}
	// The row exists. Lock it (Postgres) so a concurrent boot of another
	// replica reconciles against this one's write, not a stale read.
	// SQLite's write lock is already held: the INSERT OR IGNORE took it.
	sel := fmt.Sprintf(`SELECT `+desiredColumns+` FROM %s WHERE module = %s`,
		query.QuoteFolded(desiredTable), place(s.dialect, 1))
	if s.dialect == migrate.DialectPostgres {
		sel += " FOR UPDATE"
	}
	prev, err := scanDesired(tx.QueryRowContext(ctx, sel, d.Module), s.dialect)
	if err != nil {
		return InstallResult{}, fmt.Errorf("processmodule: InstallOrUpdate %q: read: %w", d.Module, err)
	}
	res := InstallResult{Outcome: InstallUnchanged}
	var upd string
	var args []any
	// A new artifact brings its own migrations: the previous artifact's
	// approval does not cover them, so the migration coordinator must run
	// again before the module may reach Ready.
	unstamp := ""
	if prev.ArtifactSHA256 != d.ArtifactSHA256 {
		unstamp = ", migrations_applied_at = NULL"
	}
	target := withoutRevoked(d.EffectiveGrants, prev.RevokedGrants)
	// A grant given back narrower than its revoke (orders:read after
	// orders:* was revoked) is still a pattern match for the revoke, so
	// the subtraction drops it. The stored row holds it and the descriptor
	// still covers it: keep it.
	for _, g := range prev.EffectiveGrants {
		if !slices.Contains(target, g) && access.ScopeMatch(d.EffectiveGrants, g) {
			target = append(target, g)
		}
	}
	if grantsJSON, err = marshalGrants(target); err != nil {
		return InstallResult{}, fmt.Errorf("processmodule: InstallOrUpdate %q: %w", d.Module, err)
	}
	switch {
	case !grantsWithin(target, prev.EffectiveGrants):
		res.Outcome = InstallGrantsChanged
		res.PreviousGrants = prev.EffectiveGrants
		upd = fmt.Sprintf(`UPDATE %s SET effective_grants = %s, artifact_sha256 = %s, enabled = %s, desired_generation = desired_generation + 1%s WHERE module = %s`,
			query.QuoteFolded(desiredTable), place(s.dialect, 1), place(s.dialect, 2), place(s.dialect, 3), unstamp, place(s.dialect, 4))
		args = []any{string(grantsJSON), d.ArtifactSHA256, boolToInt(false), d.Module}
	case !sameGrantSet(prev.EffectiveGrants, target):
		res.Outcome = InstallGrantsNarrowed
		res.PreviousGrants = prev.EffectiveGrants
		upd = fmt.Sprintf(`UPDATE %s SET effective_grants = %s, artifact_sha256 = %s, desired_generation = desired_generation + 1%s WHERE module = %s`,
			query.QuoteFolded(desiredTable), place(s.dialect, 1), place(s.dialect, 2), unstamp, place(s.dialect, 3))
		args = []any{string(grantsJSON), d.ArtifactSHA256, d.Module}
	case prev.ArtifactSHA256 != d.ArtifactSHA256:
		res.Outcome = InstallArtifactChanged
		upd = fmt.Sprintf(`UPDATE %s SET artifact_sha256 = %s, desired_generation = desired_generation + 1%s WHERE module = %s`,
			query.QuoteFolded(desiredTable), place(s.dialect, 1), unstamp, place(s.dialect, 2))
		args = []any{d.ArtifactSHA256, d.Module}
	}
	if upd != "" {
		r, err := tx.ExecContext(ctx, upd, args...)
		if err != nil {
			return InstallResult{}, fmt.Errorf("processmodule: InstallOrUpdate %q: update: %w", d.Module, err)
		}
		if err := AssertRow(r, d.Module); err != nil {
			return InstallResult{}, err
		}
		prev, err = scanDesired(tx.QueryRowContext(ctx, sel, d.Module), s.dialect)
		if err != nil {
			return InstallResult{}, fmt.Errorf("processmodule: InstallOrUpdate %q: readback: %w", d.Module, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return InstallResult{}, fmt.Errorf("processmodule: InstallOrUpdate %q: commit: %w", d.Module, err)
	}
	res.Row = prev
	return res, nil
}

// withoutRevoked returns grants less every grant a revoked pattern covers.
func withoutRevoked(grants, revoked []access.Permission) []access.Permission {
	if len(revoked) == 0 {
		return grants
	}
	return slices.DeleteFunc(slices.Clone(grants), func(g access.Permission) bool {
		return access.ScopeMatch(revoked, g)
	})
}

// grantsWithin reports whether every grant in a is also in b: a adds
// nothing b does not already hold.
func grantsWithin(a, b []access.Permission) bool {
	for _, g := range a {
		if !slices.Contains(b, g) {
			return false
		}
	}
	return true
}

// unionGrants returns a with every grant of b it lacks appended.
func unionGrants(a, b []access.Permission) []access.Permission {
	out := slices.Clone(a)
	for _, g := range b {
		if !slices.Contains(out, g) {
			out = append(out, g)
		}
	}
	return out
}

// sameGrantSet reports whether a and b hold the same permissions,
// ignoring order and repeats.
func sameGrantSet(a, b []access.Permission) bool {
	norm := func(g []access.Permission) []access.Permission {
		out := slices.Clone(g)
		slices.Sort(out)
		return slices.Compact(out)
	}
	return slices.Equal(norm(a), norm(b))
}

// GetDesired returns the desired-state row, or ErrNoDesiredRow if absent.
func (s *SQLProcessModuleStore) GetDesired(ctx context.Context, module string) (DesiredState, error) {
	q := fmt.Sprintf(`SELECT `+desiredColumns+` FROM %s WHERE module = %s`,
		query.QuoteFolded(desiredTable), place(s.dialect, 1))
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
	q := fmt.Sprintf(`SELECT `+desiredColumns+` FROM %s ORDER BY module ASC`,
		query.QuoteFolded(desiredTable))
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
		query.QuoteFolded(desiredTable), place(s.dialect, 1), place(s.dialect, 2))
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
		query.QuoteFolded(desiredTable), place(s.dialect, 1))
	res, err := tx.ExecContext(ctx, upd, module)
	if err != nil {
		return 0, fmt.Errorf("processmodule: BumpGeneration %q: update: %w", module, err)
	}
	if err := AssertRow(res, module); err != nil {
		return 0, err
	}
	var gen int64
	sel := fmt.Sprintf(`SELECT desired_generation FROM %s WHERE module = %s`,
		query.QuoteFolded(desiredTable), place(s.dialect, 1))
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
	// The revoked set follows the replacement: a grant it drops is
	// remembered as revoked, a grant it gives back is no longer.
	sel := fmt.Sprintf(`SELECT `+desiredColumns+` FROM %s WHERE module = %s`,
		query.QuoteFolded(desiredTable), place(s.dialect, 1))
	if s.dialect == migrate.DialectPostgres {
		sel += " FOR UPDATE"
	}
	prev, err := scanDesired(tx.QueryRowContext(ctx, sel, module), s.dialect)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, fmt.Errorf("processmodule: %w: %q", ErrNoDesiredRow, module)
	}
	if err != nil {
		return 0, fmt.Errorf("processmodule: SetEffectiveGrants %q: read: %w", module, err)
	}
	dropped := slices.DeleteFunc(slices.Clone(prev.EffectiveGrants), func(g access.Permission) bool {
		return slices.Contains(grants, g)
	})
	revoked := slices.DeleteFunc(unionGrants(prev.RevokedGrants, dropped), func(g access.Permission) bool {
		return slices.Contains(grants, g)
	})
	revokedJSON, err := marshalGrants(revoked)
	if err != nil {
		return 0, fmt.Errorf("processmodule: SetEffectiveGrants %q: %w", module, err)
	}
	upd := fmt.Sprintf(`UPDATE %s SET effective_grants = %s, revoked_grants = %s, desired_generation = desired_generation + 1 WHERE module = %s`,
		query.QuoteFolded(desiredTable), place(s.dialect, 1), place(s.dialect, 2), place(s.dialect, 3))
	res, err := tx.ExecContext(ctx, upd, string(grantsJSON), string(revokedJSON), module)
	if err != nil {
		return 0, fmt.Errorf("processmodule: SetEffectiveGrants %q: update: %w", module, err)
	}
	if err := AssertRow(res, module); err != nil {
		return 0, err
	}
	var gen int64
	genSel := fmt.Sprintf(`SELECT desired_generation FROM %s WHERE module = %s`,
		query.QuoteFolded(desiredTable), place(s.dialect, 1))
	if err := tx.QueryRowContext(ctx, genSel, module).Scan(&gen); err != nil {
		return 0, fmt.Errorf("processmodule: SetEffectiveGrants %q: readback: %w", module, err)
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("processmodule: SetEffectiveGrants %q: commit: %w", module, err)
	}
	return uint64(gen), nil
}

// errNoGrantsNamed is a removal that names no grant.
var errNoGrantsNamed = errors.New("names no grant")

// removeGrantsAttempts bounds RemoveEffectiveGrants' retries when other
// writes keep moving the generation.
const removeGrantsAttempts = 8

// RemoveEffectiveGrants takes grants out of the effective grant set and
// bumps generation. The write is a compare-and-swap on the generation
// it read, retried when another write moved it, so a grant change that
// lands between the read and the write is kept: replacing the set from
// a stale read would put back a grant another revoke took away.
func (s *SQLProcessModuleStore) RemoveEffectiveGrants(ctx context.Context, module string, grants []access.Permission) (uint64, error) {
	if len(grants) == 0 {
		return 0, fmt.Errorf("processmodule: RemoveEffectiveGrants %q: %w", module, errNoGrantsNamed)
	}
	upd := fmt.Sprintf(`UPDATE %s SET effective_grants = %s, revoked_grants = %s, desired_generation = desired_generation + 1 WHERE module = %s AND desired_generation = %s`,
		query.QuoteFolded(desiredTable), place(s.dialect, 1), place(s.dialect, 2), place(s.dialect, 3), place(s.dialect, 4))
	for range removeGrantsAttempts {
		d, err := s.GetDesired(ctx, module)
		if err != nil {
			return 0, err
		}
		if d.DesiredGeneration > math.MaxInt64-1 {
			return 0, fmt.Errorf("processmodule: RemoveEffectiveGrants %q: generation %d out of range", module, d.DesiredGeneration)
		}
		kept := slices.DeleteFunc(slices.Clone(d.EffectiveGrants), func(g access.Permission) bool {
			return slices.Contains(grants, g)
		})
		grantsJSON, err := marshalGrants(kept)
		if err != nil {
			return 0, fmt.Errorf("processmodule: RemoveEffectiveGrants %q: %w", module, err)
		}
		// Remember the revoke, so the next boot's InstallOrUpdate does not
		// hand the grant back from the descriptor.
		revokedJSON, err := marshalGrants(unionGrants(d.RevokedGrants, grants))
		if err != nil {
			return 0, fmt.Errorf("processmodule: RemoveEffectiveGrants %q: %w", module, err)
		}
		if s.afterGrantsRead != nil {
			s.afterGrantsRead()
		}
		res, err := s.db.ExecContext(ctx, upd, string(grantsJSON), string(revokedJSON), module, int64(d.DesiredGeneration))
		if err != nil {
			return 0, fmt.Errorf("processmodule: RemoveEffectiveGrants %q: update: %w", module, err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return 0, fmt.Errorf("processmodule: RemoveEffectiveGrants %q: rows: %w", module, err)
		}
		if n == 1 {
			return d.DesiredGeneration + 1, nil
		}
	}
	return 0, fmt.Errorf("processmodule: RemoveEffectiveGrants %q: generation kept moving after %d attempts", module, removeGrantsAttempts)
}

// SetMigrationsAppliedAt records (or clears) the migrations-applied timestamp.
func (s *SQLProcessModuleStore) SetMigrationsAppliedAt(ctx context.Context, module string, at *time.Time) error {
	q := fmt.Sprintf(`UPDATE %s SET migrations_applied_at = %s WHERE module = %s`,
		query.QuoteFolded(desiredTable), place(s.dialect, 1), place(s.dialect, 2))
	res, err := s.db.ExecContext(ctx, q, timeToMillis(at), module)
	if err != nil {
		return fmt.Errorf("processmodule: SetMigrationsAppliedAt %q: %w", module, err)
	}
	return AssertRow(res, module)
}

// setMigrationsAppliedFor stamps the migrations-applied timestamp only
// while the row still names artifactSHA256, in one UPDATE. It reports
// false when the row names another artifact.
func (s *SQLProcessModuleStore) setMigrationsAppliedFor(ctx context.Context, module, artifactSHA256 string, at time.Time) (bool, error) {
	q := fmt.Sprintf(`UPDATE %s SET migrations_applied_at = %s WHERE module = %s AND artifact_sha256 = %s`,
		query.QuoteFolded(desiredTable), place(s.dialect, 1), place(s.dialect, 2), place(s.dialect, 3))
	res, err := s.db.ExecContext(ctx, q, timeToMillis(&at), module, artifactSHA256)
	if err != nil {
		return false, fmt.Errorf("processmodule: SetMigrationsAppliedAt %q: %w", module, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("processmodule: SetMigrationsAppliedAt %q: %w", module, err)
	}
	if n == 1 {
		return true, nil
	}
	if _, err := s.GetDesired(ctx, module); err != nil {
		return false, err
	}
	return false, nil
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
			query.QuoteFolded(heartbeatTable))
		args = []any{module, replicaID, int64(observedGen), phase, now}
	} else {
		q = fmt.Sprintf(`INSERT INTO %s (module, replica_id, observed_generation, phase, updated_at_ms)
			VALUES (?, ?, ?, ?, ?)
			ON CONFLICT (module, replica_id) DO UPDATE SET
				observed_generation = excluded.observed_generation,
				phase = excluded.phase,
				updated_at_ms = excluded.updated_at_ms`,
			query.QuoteFolded(heartbeatTable))
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
		query.QuoteFolded(heartbeatTable), place(s.dialect, 1), place(s.dialect, 2))
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
		query.QuoteFolded(heartbeatTable), place(s.dialect, 1), place(s.dialect, 2))
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
	var grantsRaw, revokedRaw string
	var migMs sql.NullInt64
	// `enabled` is BOOLEAN on Postgres and INTEGER (0/1) on SQLite; scanning
	// a PG bool into *int fails ("converting driver.Value type bool to int"),
	// which used to abort the whole row scan before migrations_applied_at
	// was reached. Scan into an any and normalize per-dialect so both paths
	// read every column.
	var enabledVal any
	if err := sc.Scan(&d.Module, &gen, &enabledVal, &d.ArtifactSHA256, &grantsRaw, &migMs, &revokedRaw); err != nil {
		return DesiredState{}, err
	}
	d.DesiredGeneration = uint64(gen)
	d.Enabled = enabledBool(enabledVal)
	d.EffectiveGrants = unmarshalGrants(grantsRaw)
	d.RevokedGrants = unmarshalGrants(revokedRaw)
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
