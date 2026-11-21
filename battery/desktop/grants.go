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

const (
	grantAllow = "allow"
	grantDeny  = "deny"
)

// GrantStore persists per-permission decisions.
type GrantStore interface {
	// Get returns the stored decision ("allow" or "deny") for one capability's permission.
	Get(ctx context.Context, capability, permission string) (decision string, found bool, err error)
	// Set persists a decision, replacing any previous one.
	Set(ctx context.Context, capability, permission, decision string) error
	// Reset drops every stored decision.
	Reset(ctx context.Context) error
}

type sqlGrantStore struct {
	db *sql.DB
}

func newSQLGrantStore(db *sql.DB) *sqlGrantStore {
	return &sqlGrantStore{db: db}
}

const grantsTable = "desktop_grants"

func (s *sqlGrantStore) ensureSchema(ctx context.Context) error {
	tsType := "DATETIME"
	if migrate.DetectDialect(s.db) == migrate.DialectPostgres {
		tsType = "TIMESTAMP"
	}
	stmt := fmt.Sprintf(
		`CREATE TABLE IF NOT EXISTS %s (capability TEXT NOT NULL, permission TEXT NOT NULL, `+
			`decision TEXT NOT NULL, granted_at %s NOT NULL, PRIMARY KEY (capability, permission))`,
		query.QuoteFolded(grantsTable), tsType,
	)
	if _, err := s.db.ExecContext(ctx, stmt); err != nil {
		return fmt.Errorf("desktop: create %s table: %w", grantsTable, err)
	}
	return nil
}

func (s *sqlGrantStore) Get(ctx context.Context, capability, permission string) (string, bool, error) {
	var decision string
	err := s.db.QueryRowContext(ctx,
		fmt.Sprintf(`SELECT decision FROM %s WHERE capability = $1 AND permission = $2`, query.QuoteFolded(grantsTable)),
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
	if err := checkDecision(decision); err != nil {
		return err
	}
	stmt := fmt.Sprintf(
		`INSERT INTO %s (capability, permission, decision, granted_at) VALUES ($1, $2, $3, $4) `+
			`ON CONFLICT(capability, permission) DO UPDATE SET decision = excluded.decision, granted_at = excluded.granted_at`,
		query.QuoteFolded(grantsTable))
	_, err := s.db.ExecContext(ctx, stmt, capability, permission, decision, time.Now().UTC())
	if err != nil {
		return fmt.Errorf("desktop: write grant: %w", err)
	}
	return nil
}

func (s *sqlGrantStore) Reset(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx,
		fmt.Sprintf(`DELETE FROM %s`, query.QuoteFolded(grantsTable))); err != nil {
		return fmt.Errorf("desktop: reset grants: %w", err)
	}
	return nil
}

type grantKey struct {
	capability string
	permission string
}

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
	if err := checkDecision(decision); err != nil {
		return err
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

func checkDecision(decision string) error {
	if decision != grantAllow && decision != grantDeny {
		return fmt.Errorf("desktop: grant decision must be %q or %q, got %q", grantAllow, grantDeny, decision)
	}
	return nil
}
