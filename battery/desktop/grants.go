package desktop

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"time"

	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
)

// Permission grants. The chokepoint consults the store before every
// gated call: "allow" proceeds, "deny" is a persisted refusal (so a
// page cannot re-prompt in a loop), a miss triggers the OS prompt.
// "Allow once" is deliberately not persisted.
//
// A grant is keyed on (capability, permission), not on the permission
// alone. The OS alert names the capability, the method AND the
// permission, so the user is answering about a capability; keying on
// the permission string alone let a SECOND capability that happened to
// declare the same string inherit that consent with no prompt at all,
// silently and across app upgrades. The two parts are separate COLUMNS,
// never a delimiter-joined string (the compositekey posture): a
// capability name and a permission both come from a plugin.

// Grant decisions. Anything else in the column reads as deny.
const (
	grantAllow = "allow"
	grantDeny  = "deny"
)

// GrantStore persists per-permission decisions.
type GrantStore interface {
	// Get returns the stored decision ("allow" or "deny") for one
	// capability's permission. found is false when nothing is stored.
	Get(ctx context.Context, capability, permission string) (decision string, found bool, err error)
	// Set persists a decision, replacing any previous one.
	Set(ctx context.Context, capability, permission, decision string) error
	// Reset drops every stored decision.
	Reset(ctx context.Context) error
}

// sqlGrantStore persists grants in the app's own database
// (desktop_grants), created in OnStart so the table exists before the
// listener binds.
type sqlGrantStore struct {
	db *sql.DB
}

// newSQLGrantStore wraps an open DB. The schema is ensured separately
// (Battery.OnStart) because Init-time entities bind the DB at
// registration, not at battery Init.
func newSQLGrantStore(db *sql.DB) *sqlGrantStore {
	return &sqlGrantStore{db: db}
}

// grantsTable is the fixed table name; it is a constant, not config,
// so SafeIdent/QuoteIdent below are belt-and-braces.
const grantsTable = "desktop_grants"

// ensureSchema creates the grants table. The timestamp type follows the
// battery/auth apitoken_sql.go dialect split: DATETIME on SQLite,
// TIMESTAMP on Postgres; both engines accept CREATE TABLE IF NOT
// EXISTS here.
func (s *sqlGrantStore) ensureSchema(ctx context.Context) error {
	tsType := "DATETIME"
	if migrate.DetectDialect(s.db) == migrate.DialectPostgres {
		tsType = "TIMESTAMP"
	}
	stmt := fmt.Sprintf(
		`CREATE TABLE IF NOT EXISTS %s (capability TEXT NOT NULL, permission TEXT NOT NULL, `+
			`decision TEXT NOT NULL, granted_at %s NOT NULL, PRIMARY KEY (capability, permission))`,
		query.QuoteIdent(grantsTable), tsType,
	)
	if _, err := s.db.ExecContext(ctx, stmt); err != nil {
		return fmt.Errorf("desktop: create %s table: %w", grantsTable, err)
	}
	return nil
}

func (s *sqlGrantStore) Get(ctx context.Context, capability, permission string) (string, bool, error) {
	var decision string
	err := s.db.QueryRowContext(ctx,
		fmt.Sprintf(`SELECT decision FROM %s WHERE capability = $1 AND permission = $2`, query.QuoteIdent(grantsTable)),
		capability, permission).Scan(&decision)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("desktop: read grant: %w", err)
	}
	return decision, true, nil
}

func (s *sqlGrantStore) Set(ctx context.Context, capability, permission, decision string) error {
	if decision != grantAllow && decision != grantDeny {
		return fmt.Errorf("desktop: grant decision must be %q or %q, got %q", grantAllow, grantDeny, decision)
	}
	stmt := fmt.Sprintf(
		`INSERT INTO %s (capability, permission, decision, granted_at) VALUES ($1, $2, $3, $4) `+
			`ON CONFLICT(capability, permission) DO UPDATE SET decision = excluded.decision, granted_at = excluded.granted_at`,
		query.QuoteIdent(grantsTable))
	_, err := s.db.ExecContext(ctx, stmt, capability, permission, decision, time.Now().UTC())
	if err != nil {
		return fmt.Errorf("desktop: write grant: %w", err)
	}
	return nil
}

func (s *sqlGrantStore) Reset(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx,
		fmt.Sprintf(`DELETE FROM %s`, query.QuoteIdent(grantsTable))); err != nil {
		return fmt.Errorf("desktop: reset grants: %w", err)
	}
	return nil
}

// grantKey is the composite key as a STRUCT, never a joined string:
// a control-character-joined concatenation of two plugin-supplied
// values is the compositekey shape the repo has an analyzer for.
type grantKey struct {
	capability string
	permission string
}

// memGrantStore is the no-database fallback.
type memGrantStore struct {
	mu  sync.Mutex
	grs map[grantKey]string
}

func newMemGrantStore() *memGrantStore {
	return &memGrantStore{grs: make(map[grantKey]string)}
}

func (s *memGrantStore) Get(_ context.Context, capability, permission string) (string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, ok := s.grs[grantKey{capability, permission}]
	return d, ok, nil
}

func (s *memGrantStore) Set(_ context.Context, capability, permission, decision string) error {
	if decision != grantAllow && decision != grantDeny {
		return fmt.Errorf("desktop: grant decision must be %q or %q, got %q", grantAllow, grantDeny, decision)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.grs[grantKey{capability, permission}] = decision
	return nil
}

func (s *memGrantStore) Reset(_ context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.grs = make(map[grantKey]string)
	return nil
}
