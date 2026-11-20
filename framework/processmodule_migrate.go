package framework

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
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

const errModuleMigrationDefaultGroup = "processmodule: module migrations must declare a named group"

// CoordinatorValidationError is the error Plan and Apply return for a
// group, digest or duplicate rule violation.
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
	if cv, ok := errors.AsType[*CoordinatorValidationError](target); ok {
		return e.Field == cv.Field && e.Rule == cv.Rule
	}
	return false
}

func coordErr(field, rule, format string, args ...any) *CoordinatorValidationError {
	return &CoordinatorValidationError{Field: field, Rule: rule, msg: fmt.Sprintf(format, args...)}
}

// ApprovedMigration is one operator-approved migration from the module
// artifact. SHA256 is the approved hex digest of Up; empty skips the check.
type ApprovedMigration struct {
	Version uint64
	Name    string
	Up      string
	Down    string
	SHA256  string
}

// PlannedStep is one migration in a [CoordinatorPlan].
type PlannedStep struct {
	Version   uint64
	Name      string
	Up        string
	Down      string
	Computed  string
	Approved  string
	LintHints []string
}

// CoordinatorPlan is what Plan returns for review: the group, the ordered
// steps and advisory lint warnings.
type CoordinatorPlan struct {
	Group    string
	Steps    []PlannedStep
	Warnings []string
}

// MigrationCoordinator runs a process module's migrations on the host's
// behalf. Construct one per Apply.
type MigrationCoordinator struct {
	store    ProcessModuleStore
	adminDB  *sql.DB
	dialect  migrate.Dialect
	adminDSN string
	now      func() time.Time
}

// CoordinatorOption configures a [MigrationCoordinator].
type CoordinatorOption func(*MigrationCoordinator)

// WithCoordinatorClock injects the clock Apply stamps MigrationsAppliedAt
// with.
func WithCoordinatorClock(now func() time.Time) CoordinatorOption {
	return func(c *MigrationCoordinator) { c.now = now }
}

// WithCoordinatorAdminDSN supplies the host's URL-form Postgres DSN the
// module-role DSN is derived from. Required on Postgres.
func WithCoordinatorAdminDSN(dsn string) CoordinatorOption {
	return func(c *MigrationCoordinator) { c.adminDSN = dsn }
}

// NewMigrationCoordinator constructs a coordinator over store and the
// host-privileged adminDB. It errors on a nil argument or an undetectable
// dialect.
func NewMigrationCoordinator(store ProcessModuleStore, adminDB *sql.DB, opts ...CoordinatorOption) (*MigrationCoordinator, error) {
	if store == nil {
		return nil, errors.New("processmodule: NewMigrationCoordinator: nil store")
	}
	if adminDB == nil {
		return nil, errors.New("processmodule: NewMigrationCoordinator: nil admin db")
	}
	dialect, err := migrate.DetectDialectStrict(adminDB)
	if err != nil {
		return nil, fmt.Errorf("processmodule: NewMigrationCoordinator: %w", err)
	}
	c := &MigrationCoordinator{
		store:   store,
		adminDB: adminDB,
		dialect: dialect,
		now:     time.Now,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c, nil
}

// Dialect reports the detected dialect.
func (c *MigrationCoordinator) Dialect() migrate.Dialect { return c.dialect }

// Plan validates migs against desc without touching the database: a named
// group, unique versions and matching digests. Lint warnings never block.
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

// Apply runs Plan, then the migrations, then stamps MigrationsAppliedAt
// and bumps the generation. On Postgres they run as a restricted
// per-module role; on SQLite an untrusted module is refused.
func (c *MigrationCoordinator) Apply(ctx context.Context, desc ProcessModuleDescriptor, migs []ApprovedMigration) error {
	plan, err := c.Plan(desc, migs)
	if err != nil {
		return err
	}
	if len(plan.Steps) == 0 {
		return c.stampApplied(ctx, desc.Name, desc.ArtifactSHA256)
	}
	switch c.dialect {
	case migrate.DialectSQLite:
		if desc.TrustTier == TrustUntrusted {
			return coordErr("dialect", "sqlite_untrusted",
				"processmodule: SQLite is not a third-party DDL boundary (design §7); an untrusted module's migrations cannot run on SQLite: fail closed")
		}
		if err := c.applyWithMigrator(ctx, c.adminDB, migrate.DialectSQLite, desc.MigrationGroup, plan, ""); err != nil {
			return err
		}
	case migrate.DialectPostgres:
		if err := c.applyPostgres(ctx, desc, plan); err != nil {
			return err
		}
	default:
		return fmt.Errorf("processmodule: unknown dialect %q", c.dialect)
	}
	return c.stampApplied(ctx, desc.Name, desc.ArtifactSHA256)
}

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
	roleDB, err := sql.Open("postgres", roleDSN)
	if err != nil {
		return fmt.Errorf("open module-role db: %w", err)
	}
	defer roleDB.Close()
	roleDB.SetMaxOpenConns(1)
	if err := roleDB.PingContext(ctx); err != nil {
		return fmt.Errorf("ping module-role db: %w", err)
	}
	if err := c.applyWithMigrator(ctx, roleDB, migrate.DialectPostgres, plan.Group, plan, ""); err != nil {
		return err
	}
	return nil
}

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

type artifactStamper interface {
	setMigrationsAppliedFor(ctx context.Context, module, artifactSHA256 string, at time.Time) (bool, error)
}

func (c *MigrationCoordinator) stampApplied(ctx context.Context, module, artifactSHA256 string) error {
	now := c.now()
	if st, ok := c.store.(artifactStamper); ok {
		stamped, err := st.setMigrationsAppliedFor(ctx, module, artifactSHA256, now)
		if err != nil {
			return fmt.Errorf("set migrations_applied_at: %w", err)
		}
		if !stamped {
			return fmt.Errorf("processmodule: %s: the desired artifact is no longer %s; run the coordinator for the artifact the row names", module, artifactSHA256)
		}
	} else if err := c.store.SetMigrationsAppliedAt(ctx, module, &now); err != nil {
		return fmt.Errorf("set migrations_applied_at: %w", err)
	}
	if _, err := c.store.BumpGeneration(ctx, module); err != nil {
		return fmt.Errorf("bump generation (non-fatal, poll will reconcile): %w", err)
	}
	return nil
}

func provisionModuleSchemaRole(ctx context.Context, adminDB *sql.DB, schema, role, password string) error {
	stmts := moduleSchemaRoleStmts(schema, role, password)
	return execModuleSchemaRoleStmts(ctx, adminDB, stmts)
}

func moduleSchemaRoleStmts(schema, role, password string) []string {
	return []string{
		`CREATE SCHEMA IF NOT EXISTS ` + query.QuoteFolded(schema),
		`DO $$ BEGIN
  CREATE ROLE ` + query.QuoteFolded(role) + ` LOGIN PASSWORD '` + password + `' NOINHERIT NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS;
EXCEPTION WHEN duplicate_object THEN null;
END $$`,
		`ALTER ROLE ` + query.QuoteFolded(role) + ` WITH LOGIN PASSWORD '` + password + `' NOINHERIT NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS`,
		`GRANT USAGE, CREATE ON SCHEMA ` + query.QuoteFolded(schema) + ` TO ` + query.QuoteFolded(role),
		`ALTER ROLE ` + query.QuoteFolded(role) + ` SET search_path = ` + query.QuoteFolded(schema),
		`ALTER ROLE ` + query.QuoteFolded(role) + ` SET statement_timeout = '30s'`,
		`ALTER ROLE ` + query.QuoteFolded(role) + ` SET lock_timeout = '5s'`,
		`REVOKE ALL ON SCHEMA public FROM ` + query.QuoteFolded(role),
	}
}

func execModuleSchemaRoleStmts(ctx context.Context, adminDB *sql.DB, stmts []string) error {
	for _, s := range stmts {
		if _, err := adminDB.ExecContext(ctx, s); err != nil {
			return fmt.Errorf("exec %q: %w", firstLine(redactRolePassword(s)), err)
		}
	}
	// best-effort: a locked-down cluster must not block provisioning
	_, _ = adminDB.ExecContext(ctx, `REVOKE CREATE ON SCHEMA public FROM PUBLIC`)
	return nil
}

func moduleSchemaRole(moduleName string) (schema, role string) {
	base := sanitizeIdent("module_" + moduleName)
	for _, r := range moduleName {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			continue
		}
		sum := sha256.Sum256([]byte(moduleName))
		base += "_" + hex.EncodeToString(sum[:6])
		break
	}
	return base, base + "_role"
}

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

func moduleRoleDSN(adminDSN, role, password string) (string, error) {
	u, err := url.Parse(adminDSN)
	if err != nil {
		return "", errors.New("admin DSN must be a valid URL (redacted)")
	}
	if u.Scheme == "" || !strings.HasPrefix(u.Scheme, "postgres") {
		if u.Opaque != "" {
			return "", errors.New("admin DSN must be URL-form (postgres://...), got (redacted)")
		}
		return "", fmt.Errorf("admin DSN must be URL-form (postgres://...), got scheme %q", u.Scheme)
	}
	u.User = url.UserPassword(role, password)
	q := u.Query()
	for k := range q {
		if !roleDSNParams[k] {
			q.Del(k)
		}
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}

var roleDSNParams = map[string]bool{
	"host": true, "hostaddr": true, "port": true, "dbname": true,
	"sslmode": true, "sslrootcert": true, "sslcert": true, "sslkey": true,
	"sslinline": true, "sslsni": true, "sslnegotiation": true,
	"ssl_min_protocol_version": true, "ssl_max_protocol_version": true,
	"connect_timeout": true, "target_session_attrs": true, "load_balance_hosts": true,
	"application_name": true, "fallback_application_name": true,
	"krbsrvname": true, "krbspn": true,
}

func shortHash(s string) string {
	if len(s) <= 12 {
		return s
	}
	return s[:12]
}

func redactRolePassword(s string) string {
	const marker = "PASSWORD '"
	i := strings.Index(s, marker)
	if i < 0 {
		return s
	}
	rest := s[i+len(marker):]
	j := strings.IndexByte(rest, '\'')
	if j < 0 {
		return s[:i+len(marker)] + "[redacted]"
	}
	return s[:i+len(marker)] + "[redacted]" + rest[j:]
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func lintMigrationSQL(up string) []string {
	up = strings.TrimSpace(up)
	if up == "" {
		return nil
	}
	var hints []string
	for raw := range strings.SplitSeq(up, ";") {
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

func isPlainDDL(upper string) bool {
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

func stripSQLComments(s string) string {
	var out strings.Builder
	out.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if i+1 < len(s) && s[i] == '-' && s[i+1] == '-' {
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
			i++
			continue
		}
		out.WriteByte(s[i])
	}
	return strings.TrimSpace(out.String())
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
