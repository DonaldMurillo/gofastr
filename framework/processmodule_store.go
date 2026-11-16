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

	"github.com/DonaldMurillo/gofastr/core/datexport"
	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/framework/access"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
	"github.com/DonaldMurillo/gofastr/internal/asciicase"
)

// ProcessModuleStore persists process modules' desired state and replica
// heartbeats. A revoke or upgrade is a desired_generation bump.
type ProcessModuleStore interface {
	// EnsureSchema creates the tables if absent; safe on every boot.
	EnsureSchema(ctx context.Context) error

	// Install writes a new row, or returns [ErrModuleInstalled].
	Install(ctx context.Context, d DesiredState) error

	// InstallOrUpdate inserts the row when absent and otherwise reconciles
	// it with d, as [InstallOutcome] describes.
	InstallOrUpdate(ctx context.Context, d DesiredState) (InstallResult, error)

	// GetDesired returns the row, [ErrNoDesiredRow], or the SQL error.
	GetDesired(ctx context.Context, module string) (DesiredState, error)

	// ListDesired returns every row, ordered by module name.
	ListDesired(ctx context.Context) ([]DesiredState, error)

	// SetEnabled toggles enabled without bumping generation.
	SetEnabled(ctx context.Context, module string, enabled bool) error

	// BumpGeneration increments desired_generation and returns it.
	BumpGeneration(ctx context.Context, module string) (uint64, error)

	// SetEffectiveGrants replaces the grant set and bumps generation.
	SetEffectiveGrants(ctx context.Context, module string, grants []access.Permission) (uint64, error)

	// RemoveEffectiveGrants removes grants from the set and bumps
	// generation. It refuses an empty grants.
	RemoveEffectiveGrants(ctx context.Context, module string, grants []access.Permission) (uint64, error)

	// SetMigrationsAppliedAt records when migrations finished; nil clears.
	SetMigrationsAppliedAt(ctx context.Context, module string, at *time.Time) error

	// RecordHeartbeat upserts a replica heartbeat row stamped now.
	RecordHeartbeat(ctx context.Context, module, replicaID string, observedGen uint64, phase string) error

	// LiveReplicas returns heartbeat rows newer than ttl.
	LiveReplicas(ctx context.Context, module string, ttl time.Duration) ([]ReplicaHeartbeat, error)

	// DeleteHeartbeat removes a replica's heartbeat row.
	DeleteHeartbeat(ctx context.Context, module, replicaID string) error
}

// DesiredState is one module's host-authored desired-state row.
type DesiredState struct {
	Module string

	// DesiredGeneration is bumped on a grant change, revoke or upgrade.
	DesiredGeneration uint64

	Enabled bool

	// ArtifactSHA256 is the approved executable digest.
	ArtifactSHA256 string

	// EffectiveGrants is the approved grant set, non-grantables removed.
	EffectiveGrants []access.Permission

	// RevokedGrants are grants an operator took away at runtime;
	// InstallOrUpdate does not hand them back.
	RevokedGrants []access.Permission

	// MigrationsAppliedAt is nil until the migration coordinator ran.
	MigrationsAppliedAt *time.Time
}

// ReplicaHeartbeat is one replica's liveness row for a module.
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

// SQLProcessModuleStore is the SQL-backed [ProcessModuleStore], on SQLite
// or Postgres.
type SQLProcessModuleStore struct {
	db              *sql.DB
	dialect         migrate.Dialect
	afterGrantsRead func()
}

// NewSQLProcessModuleStore constructs a store over db. It does not call
// EnsureSchema, and errors when the dialect cannot be detected.
func NewSQLProcessModuleStore(db *sql.DB) (*SQLProcessModuleStore, error) {
	if db == nil {
		return nil, errors.New("processmodule: NewSQLProcessModuleStore(nil db)")
	}
	dialect, err := migrate.DetectDialectStrict(db)
	if err != nil {
		return nil, fmt.Errorf("processmodule: NewSQLProcessModuleStore: %w", err)
	}
	return &SQLProcessModuleStore{db: db, dialect: dialect}, nil
}

// Dialect reports the detected dialect (debug / introspection).
func (s *SQLProcessModuleStore) Dialect() migrate.Dialect { return s.dialect }

const (
	desiredTable   = "gofastr_process_modules"
	heartbeatTable = "gofastr_process_module_heartbeats"
)

// EnsureSchema creates the desired-state and heartbeat tables idempotently.
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
			module            TEXT PRIMARY KEY NOT NULL,
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
	if err := s.addRevokedColumn(ctx, grantsType); err != nil {
		return err
	}
	registerProcessModuleExport()
	return nil
}

func registerProcessModuleExport() {
	datexport.Register(datexport.DataExporter{
		Name: desiredTable, Source: "processmodules", Table: desiredTable, PrimaryKey: "module",
		Columns: []string{"module", "desired_generation", "enabled", "artifact_sha256",
			"effective_grants", "migrations_applied_at", "revoked_grants"},
	})
}

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

const desiredColumns = "module, desired_generation, enabled, artifact_sha256, effective_grants, migrations_applied_at, revoked_grants"

// Install writes a new desired-state row. Returns ErrModuleInstalled if a
// row already exists for the module.
func (s *SQLProcessModuleStore) Install(ctx context.Context, d DesiredState) error {
	if d.Module == "" {
		return errors.New("processmodule: Install: empty module name")
	}
	if d.DesiredGeneration == 0 {
		d.DesiredGeneration = 1
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

type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

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

// InstallOutcome says what [ProcessModuleStore.InstallOrUpdate] did.
type InstallOutcome int

const (
	// InstallCreated: no row existed; one was inserted.
	InstallCreated InstallOutcome = iota + 1
	// InstallUnchanged: digest and grants match; nothing was written.
	InstallUnchanged
	// InstallArtifactChanged: only the digest differs. It is updated, the
	// generation bumped and the migrations stamp cleared.
	InstallArtifactChanged
	// InstallGrantsChanged: the grants, less revokes, add a grant. The row
	// is updated, the generation bumped and the module disabled.
	InstallGrantsChanged
	// InstallGrantsNarrowed: the grants, less revokes, only drop grants. The
	// row is updated and the generation bumped; enabled stands.
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
	// Row is the row as stored after the call.
	Row DesiredState
	// PreviousGrants is the prior grant set for a grants change, else nil.
	PreviousGrants []access.Permission
}

// InstallOrUpdate is the boot-time install; see [ProcessModuleStore].
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
	unstamp := ""
	artifactChanged := asciicase.Lower(prev.ArtifactSHA256) != asciicase.Lower(d.ArtifactSHA256)
	if artifactChanged {
		unstamp = ", migrations_applied_at = NULL"
	}
	target := withoutRevoked(d.EffectiveGrants, prev.RevokedGrants)
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
	case artifactChanged:
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

func withoutRevoked(grants, revoked []access.Permission) []access.Permission {
	if len(revoked) == 0 {
		return grants
	}
	return slices.DeleteFunc(slices.Clone(grants), func(g access.Permission) bool {
		return access.ScopeMatch(revoked, g)
	})
}

func grantsWithin(a, b []access.Permission) bool {
	for _, g := range a {
		if !slices.Contains(b, g) {
			return false
		}
	}
	return true
}

func unionGrants(a, b []access.Permission) []access.Permission {
	out := slices.Clone(a)
	for _, g := range b {
		if !slices.Contains(out, g) {
			out = append(out, g)
		}
	}
	return out
}

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

var errNoGrantsNamed = errors.New("names no grant")

const removeGrantsAttempts = 8

// RemoveEffectiveGrants removes grants from the effective set and bumps
// generation, retrying while other writes move the generation.
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
		kept := withoutRevoked(d.EffectiveGrants, grants)
		for _, revoked := range grants {
			if !access.ValidScope(string(revoked)) || !slices.ContainsFunc(d.EffectiveGrants, func(held access.Permission) bool {
				return access.ScopeMatch([]access.Permission{revoked}, held)
			}) {
				return 0, fmt.Errorf("processmodule: RemoveEffectiveGrants %q: revoke %q covers no held grant; revoke a held wildcard in full or replace the effective set", module, revoked)
			}
			resource, verb, _ := strings.Cut(string(revoked), ":")
			for _, held := range kept {
				heldResource, heldVerb, _ := strings.Cut(string(held), ":")
				if (resource == heldResource || resource == "*" || heldResource == "*") && (verb == heldVerb || verb == "*" || heldVerb == "*") {
					return 0, fmt.Errorf("processmodule: RemoveEffectiveGrants %q: revoke %q partially overlaps held grant %q; revoke the wildcard in full or replace the effective set", module, revoked, held)
				}
			}
		}
		grantsJSON, err := marshalGrants(kept)
		if err != nil {
			return 0, fmt.Errorf("processmodule: RemoveEffectiveGrants %q: %w", module, err)
		}
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

func (s *SQLProcessModuleStore) setMigrationsAppliedFor(ctx context.Context, module, artifactSHA256 string, at time.Time) (bool, error) {
	q := fmt.Sprintf(`UPDATE %s SET migrations_applied_at = %s WHERE module = %s AND LOWER(artifact_sha256) = LOWER(%s)`,
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

// AssertRow returns [ErrNoDesiredRow] (wrapped) when res affected no row.
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

type scanner interface {
	Scan(dest ...any) error
}

func scanDesired(sc scanner, dialect migrate.Dialect) (DesiredState, error) {
	var d DesiredState
	var gen int64
	var grantsRaw, revokedRaw string
	var migMs sql.NullInt64
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

func place(dialect migrate.Dialect, n int) string {
	if dialect == migrate.DialectPostgres {
		return fmt.Sprintf("$%d", n)
	}
	return "?"
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func timeToMillis(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.UnixMilli()
}
