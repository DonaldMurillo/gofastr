package framework

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	coremig "github.com/DonaldMurillo/gofastr/core/migrate"
	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
)

// Migration isolation (design §7): a process module holds ZERO database
// credentials. The HOST runs all DDL, keyed to the module's #33 migration
// group. This file is the short-lived migration coordinator — a
// deploy/CLI-style operation that loads approved SQL from the
// digest-verified artifact (never from the running child), validates it
// against the group rules, provisions a restricted per-module Postgres
// schema+role, runs Up under the advisory lock authenticated AS that role,
// and stamps MigrationsAppliedAt so the supervisor will let the module
// reach Ready.
//
// `core/migrate` is FED, never forked: the group-equality / digest /
// no-default-group checks here are new host-side logic placed ABOVE the
// runner, which stays untouched.

// moduleMigrationGroupDefault is the sentinel that a module's migrations
// landed in the DEFAULT group — explicitly forbidden for third-party
// modules (design §7: a module must own a named group so its DDL stream
// is addressable and isolatable from the host's own migrations).
const errModuleMigrationDefaultGroup = "processmodule: module migrations must declare a named group"

// CoordinatorValidationError is the typed error Plan/Apply return for a
// group/digest/duplicate rule violation. Field + Rule let a CLI or
// install UI map it to a form, mirroring [DescriptorValidationError].
type CoordinatorValidationError struct {
	Field string
	Rule  string
	msg   string
}

func (e *CoordinatorValidationError) Error() string {
	if e == nil {
		return "<nil>"
	}
	return e.msg
}

// Is lets callers errors.Is against the rule even when wrapped.
func (e *CoordinatorValidationError) Is(target error) bool {
	var cv *CoordinatorValidationError
	if errors.As(target, &cv) {
		return e.Field == cv.Field && e.Rule == cv.Rule
	}
	return false
}

func coordErr(field, rule, format string, args ...any) *CoordinatorValidationError {
	return &CoordinatorValidationError{Field: field, Rule: rule, msg: fmt.Sprintf(format, args...)}
}

// ApprovedMigration is one operator-approved migration loaded from the
// digest-verified module artifact. SHA256 is the approved digest of Up
// (hex); the coordinator computes the same digest and rejects a mismatch
// (a child cannot substitute SQL). An empty SHA256 skips the per-migration
// digest check (the artifact-level digest is still the trust anchor); the
// install tool SHOULD always set it.
type ApprovedMigration struct {
	Version uint64
	Name    string
	Up      string
	Down    string
	SHA256  string
}

// PlannedStep is one migration in a [CoordinatorPlan], with its computed
// digest and any non-authoritative lint warnings the operator should
// review before approving Apply.
type PlannedStep struct {
	Version   uint64
	Name      string
	Up        string
	Down      string
	Computed  string // SHA256(Up), hex
	Approved  string // ApprovedMigration.SHA256
	LintHints []string
}

// CoordinatorPlan is the reviewable artifact Plan returns: the group the
// migrations will run under, the ordered steps with digests, and the
// install-time lint warnings (advisory only — the role is the real
// boundary, design §7 "no parse-allowlist").
type CoordinatorPlan struct {
	Group    string
	Steps    []PlannedStep
	Warnings []string
}

// MigrationCoordinator runs a process module's migrations under the §7
// isolation model. Construct one per Apply; it is short-lived (deploy
// job / CLI), never kept alive across the app's runtime. The supervisor
// refuses Ready while MigrationsAppliedAt is nil (design §7 step 4 +
// [migrationsPending]); this coordinator is what flips it.
type MigrationCoordinator struct {
	store    ProcessModuleStore
	adminDB  *sql.DB // host-privileged pool (provisions schema+role; SQLite apply)
	dialect  migrate.Dialect
	adminDSN string // Postgres only: URL DSN the role DSN is derived from
	now      func() time.Time
}

// CoordinatorOption configures a [MigrationCoordinator].
type CoordinatorOption func(*MigrationCoordinator)

// WithCoordinatorClock injects the clock Apply stamps MigrationsAppliedAt
// with. Tests override to avoid real time.
func WithCoordinatorClock(now func() time.Time) CoordinatorOption {
	return func(c *MigrationCoordinator) { c.now = now }
}

// WithCoordinatorAdminDSN supplies the URL-form Postgres DSN the
// per-module role DSN is derived from (same host/db, user=module_role,
// generated password). Required for the Postgres path; ignored on SQLite.
// This is the host's privileged DSN — keep it off any path reachable by
// untrusted callers.
func WithCoordinatorAdminDSN(dsn string) CoordinatorOption {
	return func(c *MigrationCoordinator) { c.adminDSN = dsn }
}

// NewMigrationCoordinator constructs a coordinator over the given store +
// admin pool. The admin pool MUST be the host-privileged connection (it
// creates schemas and roles); detect the dialect from it. EnsureSchema on
// the store is the caller's job (the app does it at RegisterProcessModule).
func NewMigrationCoordinator(store ProcessModuleStore, adminDB *sql.DB, opts ...CoordinatorOption) (*MigrationCoordinator, error) {
	if store == nil {
		return nil, errors.New("processmodule: NewMigrationCoordinator: nil store")
	}
	if adminDB == nil {
		return nil, errors.New("processmodule: NewMigrationCoordinator: nil admin db")
	}
	c := &MigrationCoordinator{
		store:   store,
		adminDB: adminDB,
		dialect: migrate.DetectDialect(adminDB),
		now:     time.Now,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c, nil
}

// Dialect reports the detected dialect (debug / introspection).
func (c *MigrationCoordinator) Dialect() migrate.Dialect { return c.dialect }

// Plan is the pure, DB-free validation of an approved migration set
// against the descriptor's group. It enforces (design §7):
//
//   - the descriptor MUST declare a named migration group (default-group
//     migrations are rejected — a module cannot inject into the host's
//     own group);
//   - every migration runs under that one group (group-equality);
//   - no duplicate (Version) within the set;
//   - each migration's computed SHA256(Up) matches its approved digest.
//
// It also produces non-authoritative lint warnings (statements beyond
// plain CREATE TABLE / ALTER TABLE ADD COLUMN / CREATE INDEX) for
// operator review. The role is the real boundary; this lint never blocks
// (design §7 "no parse-allowlist").
func (c *MigrationCoordinator) Plan(desc ProcessModuleDescriptor, migs []ApprovedMigration) (*CoordinatorPlan, error) {
	if desc.MigrationGroup == "" {
		return nil, coordErr("migration_group", "empty", errModuleMigrationDefaultGroup)
	}
	seen := make(map[uint64]struct{}, len(migs))
	plan := &CoordinatorPlan{Group: desc.MigrationGroup}
	for i, m := range migs {
		field := fmt.Sprintf("migrations[%d]", i)
		if m.Name == "" {
			return nil, coordErr(field+".name", "empty", "processmodule: migration %d has empty name", m.Version)
		}
		if _, dup := seen[m.Version]; dup {
			return nil, coordErr(field+".version", "duplicate", "processmodule: duplicate migration version %d in group %q", m.Version, desc.MigrationGroup)
		}
		seen[m.Version] = struct{}{}
		computed := sha256Hex([]byte(m.Up))
		if m.SHA256 != "" && m.SHA256 != computed {
			return nil, coordErr(field+".sha256", "mismatch", "processmodule: migration %d (%s) digest mismatch: approved %s, computed %s", m.Version, m.Name, shortHash(m.SHA256), shortHash(computed))
		}
		hints := lintMigrationSQL(m.Up)
		step := PlannedStep{
			Version:   m.Version,
			Name:      m.Name,
			Up:        m.Up,
			Down:      m.Down,
			Computed:  computed,
			Approved:  m.SHA256,
			LintHints: hints,
		}
		plan.Steps = append(plan.Steps, step)
		for _, h := range hints {
			plan.Warnings = append(plan.Warnings, fmt.Sprintf("migration %d (%s): %s", m.Version, m.Name, h))
		}
	}
	return plan, nil
}

// Apply validates the migration set (Plan), then runs it under the §7
// isolation model. On success it stamps MigrationsAppliedAt and advances
// the store generation so the supervisor lets the module reach Ready.
//
// Postgres: provisions a restricted per-module schema + role, opens a
// separate *sql.DB AUTHENTICATED AS that role (not SET ROLE down from a
// powerful session — design §7 load-bearing point 2), and runs Up under
// the advisory lock with the schema-local _migrations tracking table.
//
// SQLite: there is no roles/GRANT/schemas boundary (design §7 / decision
// F). Untrusted modules are rejected loud (fail-closed). Trusted/dev-only
// modules run Up against the host pool — trusted-only, never a claim that
// groups make SQLite DDL safe.
func (c *MigrationCoordinator) Apply(ctx context.Context, desc ProcessModuleDescriptor, migs []ApprovedMigration) error {
	plan, err := c.Plan(desc, migs)
	if err != nil {
		return err
	}
	if len(plan.Steps) == 0 {
		// No migrations: still stamp applied-at so the module may proceed.
		return c.stampApplied(ctx, desc.Name)
	}
	switch c.dialect {
	case migrate.DialectSQLite:
		if desc.TrustTier == TrustUntrusted {
			return coordErr("dialect", "sqlite_untrusted",
				"processmodule: SQLite is not a third-party DDL boundary (design §7); an untrusted module's migrations cannot run on SQLite — fail closed")
		}
		// Trusted/dev-only: run Up against the host pool under the group.
		if err := c.applyWithMigrator(ctx, c.adminDB, migrate.DialectSQLite, desc.MigrationGroup, plan, "" /* no role DSN */); err != nil {
			return err
		}
	case migrate.DialectPostgres:
		if err := c.applyPostgres(ctx, desc, plan); err != nil {
			return err
		}
	default:
		return fmt.Errorf("processmodule: unknown dialect %q", c.dialect)
	}
	return c.stampApplied(ctx, desc.Name)
}

// applyPostgres provisions the per-module schema+role, opens a role DB,
// and runs Up authenticated as the restricted role.
func (c *MigrationCoordinator) applyPostgres(ctx context.Context, desc ProcessModuleDescriptor, plan *CoordinatorPlan) error {
	if c.adminDSN == "" {
		return errors.New("processmodule: postgres apply requires WithCoordinatorAdminDSN (the host's URL-form DSN to derive the module-role DSN from)")
	}
	schema, role := moduleSchemaRole(desc.Name)
	password := randomHex(24)

	if err := provisionModuleSchemaRole(ctx, c.adminDB, schema, role, password); err != nil {
		return fmt.Errorf("provision schema/role: %w", err)
	}
	roleDSN, err := moduleRoleDSN(c.adminDSN, role, password)
	if err != nil {
		return fmt.Errorf("derive module-role DSN: %w", err)
	}
	// IMPORTANT (design §7 point 2): authenticate AS module_<M>_role, not
	// SET ROLE down from a powerful session. A separate login pool means
	// the role's own (NOINHERIT/NOSUPERUSER) privileges are the ceiling;
	// `RESET ROLE` is a no-op and `SET ROLE <powerful>` fails ("not a
	// member"). Opening a one-connection pool keeps the runner's
	// advisory-lock + single-connection contract.
	roleDB, err := sql.Open("postgres", roleDSN)
	if err != nil {
		return fmt.Errorf("open module-role db: %w", err)
	}
	defer roleDB.Close()
	roleDB.SetMaxOpenConns(1)
	if err := roleDB.PingContext(ctx); err != nil {
		return fmt.Errorf("ping module-role db: %w", err)
	}
	if err := c.applyWithMigrator(ctx, roleDB, migrate.DialectPostgres, plan.Group, plan, "" /* role DB used directly */); err != nil {
		return err
	}
	return nil
}

// applyWithMigrator registers the plan's migrations under the group and
// runs Up. This is the "feed, don't fork" seam: the runner's own
// advisory-lock + checksum-integrity + schema-local tracking do all the
// real work; the coordinator only (a) sets Group on every migration, (b)
// pins the tracking table name to _migrations (resolved to module_<M>
// via the role's search_path), and (c) limits the Up to THIS group.
func (c *MigrationCoordinator) applyWithMigrator(ctx context.Context, db *sql.DB, dialect migrate.Dialect, group string, plan *CoordinatorPlan, _ string) error {
	m := coremig.New(db, coremig.WithDialect(dialect), coremig.WithTableName("_migrations"))
	for _, step := range plan.Steps {
		if err := m.Register(coremig.Migration{
			Version: step.Version,
			Name:    step.Name,
			Up:      step.Up,
			Down:    step.Down,
			Group:   group,
		}); err != nil {
			return fmt.Errorf("register migration %d: %w", step.Version, err)
		}
	}
	if err := m.Up(ctx, group); err != nil {
		return fmt.Errorf("up group %q: %w", group, err)
	}
	return nil
}

// stampApplied records MigrationsAppliedAt and advances the store
// generation so the supervisor re-reconciles and lets the module spawn
// (design §7: "on success advance generation + set MigrationsAppliedAt,
// THEN spawn to Ready").
func (c *MigrationCoordinator) stampApplied(ctx context.Context, module string) error {
	now := c.now()
	if err := c.store.SetMigrationsAppliedAt(ctx, module, &now); err != nil {
		return fmt.Errorf("set migrations_applied_at: %w", err)
	}
	if _, err := c.store.BumpGeneration(ctx, module); err != nil {
		// Non-fatal: the periodic poll will still observe
		// MigrationsAppliedAt. Log-shape: surface the error so the caller
		// knows the wake signal did not land.
		return fmt.Errorf("bump generation (non-fatal — poll will reconcile): %w", err)
	}
	return nil
}

// ---- provisioning SQL (design §7) ----

// provisionModuleSchemaRole creates the module's schema + restricted role
// idempotently. The role is LOGIN (with a generated password) so the DDL
// session can AUTHENTICATE AS it — the design's §7 SQL block lists
// NOLOGIN, but that is internally inconsistent with the load-bearing
// point 2 ("a genuinely separate LOGIN role ... authenticate as"); we
// resolve in favour of the property (LOGIN + NOINHERIT/NOSUPERUSER +
// the privilege REVOKE is the fence), not the literal NOLOGIN which would
// make authenticate-as impossible. search_path is a convenience default;
// the REVOKE on public is the real fence (design §7 point 1).
func provisionModuleSchemaRole(ctx context.Context, adminDB *sql.DB, schema, role, password string) error {
	stmts := []string{
		// Own schema (idempotent).
		`CREATE SCHEMA IF NOT EXISTS ` + query.QuoteIdent(schema),
		// Restricted role. CREATE ROLE fails on duplicate; DO $$ swallows
		// the duplicate_object error so re-runs are idempotent. LOGIN +
		// password so the coordinator can authenticate as it; every other
		// flag strips privilege.
		`DO $$ BEGIN
  CREATE ROLE ` + query.QuoteIdent(role) + ` LOGIN PASSWORD '` + password + `' NOINHERIT NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS;
EXCEPTION WHEN duplicate_object THEN null;
END $$`,
		// Re-assert the privilege flags even if the role pre-existed (a
		// prior run may have left it with broader rights).
		`ALTER ROLE ` + query.QuoteIdent(role) + ` NOINHERIT NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION`,
		// USAGE+CREATE on its own schema ONLY.
		`GRANT USAGE, CREATE ON SCHEMA ` + query.QuoteIdent(schema) + ` TO ` + query.QuoteIdent(role),
		// search_path = module_<M> — convenience, NOT the fence.
		`ALTER ROLE ` + query.QuoteIdent(role) + ` SET search_path = ` + query.QuoteIdent(schema),
		`ALTER ROLE ` + query.QuoteIdent(role) + ` SET statement_timeout = '30s'`,
		`ALTER ROLE ` + query.QuoteIdent(role) + ` SET lock_timeout = '5s'`,
		// THE FENCE: no privileges outside module_<M>. A migration that
		// writes to public is permission-denied regardless of search_path.
		`REVOKE ALL ON SCHEMA public FROM ` + query.QuoteIdent(role),
	}
	for _, s := range stmts {
		if _, err := adminDB.ExecContext(ctx, s); err != nil {
			return fmt.Errorf("exec %q: %w", firstLine(s), err)
		}
	}
	// Best-effort hardening of the public schema (design §7 lists it).
	// This is a host-wide change (removes CREATE from PUBLIC); ignore
	// errors here so a locked-down cluster does not block provisioning.
	_, _ = adminDB.ExecContext(ctx, `REVOKE CREATE ON SCHEMA public FROM PUBLIC`)
	return nil
}

// ---- helpers ----

// moduleSchemaRole derives the schema + role names from a module name.
// Non-identifier characters collapse to '_' so the names stay
// dash-free and quote-safe (module names may contain '-' / '_').
func moduleSchemaRole(moduleName string) (schema, role string) {
	base := sanitizeIdent("module_" + moduleName)
	return base, base + "_role"
}

// sanitizeIdent lowercases + replaces every non [a-z0-9] rune with '_'.
func sanitizeIdent(s string) string {
	sb := strings.Builder{}
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			sb.WriteRune(r)
		} else {
			sb.WriteByte('_')
		}
	}
	out := sb.String()
	if out == "" {
		out = "module"
	}
	return out
}

// moduleRoleDSN derives a DSN that authenticates as role with password,
// against the same host/db the admin DSN points at. adminDSN must be
// URL-form (postgres://...). The role's search_path/statement_timeout are
// set by provisioning (ALTER ROLE ... SET), so the DSN needs no options.
func moduleRoleDSN(adminDSN, role, password string) (string, error) {
	u, err := url.Parse(adminDSN)
	if err != nil {
		return "", err
	}
	if u.Scheme == "" || !strings.HasPrefix(u.Scheme, "postgres") {
		return "", fmt.Errorf("admin DSN must be URL-form (postgres://...), got %q", adminDSN)
	}
	u.User = url.UserPassword(role, password)
	return u.String(), nil
}

// shortHash trims a hex digest to 12 chars for log/error lines.
func shortHash(s string) string {
	if len(s) <= 12 {
		return s
	}
	return s[:12]
}

// firstLine returns the first line of a (possibly multi-line) SQL
// statement, for error attribution.
func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

// randomHex returns n random bytes as a hex string (password material).
func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// lintMigrationSQL is the NON-AUTHORITATIVE install-time lint (design §7
// "no parse-allowlist"). It splits the Up SQL naively on ';' and flags any
// statement whose leading verb is not CREATE TABLE / ALTER TABLE ADD
// COLUMN / CREATE INDEX. It is advisory only — a keyword/regex allowlist
// fails OPEN (trivially bypassed by comments, $$-quoting, ;-chaining,
// CREATE TABLE AS SELECT, plpgsql blocks) and is redundant with the role,
// which bounds blast radius to module_<M> regardless of what DDL is
// written. Revisit only atop a genuine SQL parser.
func lintMigrationSQL(up string) []string {
	up = strings.TrimSpace(up)
	if up == "" {
		return nil
	}
	var hints []string
	for _, raw := range strings.Split(up, ";") {
		stmt := stripSQLComments(strings.TrimSpace(raw))
		if stmt == "" {
			continue
		}
		upper := strings.ToUpper(stmt)
		if isPlainDDL(upper) {
			continue
		}
		hints = append(hints, "non-plain-DDL for operator review: "+truncate(stmt, 60))
	}
	return hints
}

// isPlainDDL reports whether an upper-cased statement's leading tokens
// are one of the v1 allowlist shapes. This is the lint's allowlist, NOT
// a security boundary.
func isPlainDDL(upper string) bool {
	// CREATE TABLE ... AS SELECT is the classic lint-bypass (it can read
	// any table the role can see); flag it even though it starts with
	// CREATE TABLE. Advisory only — the role is the real boundary.
	if strings.Contains(upper, " AS SELECT") {
		return false
	}
	switch {
	case strings.HasPrefix(upper, "CREATE TABLE"),
		strings.HasPrefix(upper, "CREATE INDEX"),
		strings.HasPrefix(upper, "CREATE UNIQUE INDEX"),
		strings.HasPrefix(upper, "ALTER TABLE") && strings.Contains(upper, "ADD COLUMN"),
		strings.HasPrefix(upper, "CREATE SCHEMA"):
		return true
	}
	return false
}

// stripSQLComments removes -- line comments and /* */ block comments so a
// comment can't smuggle a verb past the lint's leading-token check. This
// is lint hardening, NOT a security control (see lintMigrationSQL).
func stripSQLComments(s string) string {
	var out strings.Builder
	out.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if i+1 < len(s) && s[i] == '-' && s[i+1] == '-' {
			// skip to end of line
			for i < len(s) && s[i] != '\n' {
				i++
			}
			continue
		}
		if i+1 < len(s) && s[i] == '/' && s[i+1] == '*' {
			i += 2
			for i+1 < len(s) && !(s[i] == '*' && s[i+1] == '/') {
				i++
			}
			i++ // skip past final '/'
			continue
		}
		out.WriteByte(s[i])
	}
	return strings.TrimSpace(out.String())
}

// truncate clips s to n runes with a trailing ellipsis.
func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
