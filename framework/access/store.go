package access

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/DonaldMurillo/gofastr/core/query"
)

// GrantStore persists role→permission grants to a database table so RBAC
// edits survive restarts. It wraps a live *RolePolicy: Grant/Revoke write
// the DB row AND mutate the in-memory policy in one call, keeping the two
// in sync. The policy's own RWMutex covers concurrent Can checks, so a
// Grant/Revoke call is "atomic enough" — a reader may see the state
// before or after the change, never a torn map.
//
// The store holds a reference to the live *RolePolicy (store-holds-policy
// shape). Bind the policy at construction with NewGrantStore(db, policy),
// then call LoadInto once at boot to hydrate the policy from persisted
// rows. Subsequent Grant/Revoke calls mutate both layers.
//
// All role and permission VALUES are passed as $n bound parameters — never
// interpolated into SQL. The table name is validated via query.SafeIdent
// at construction time and quoted via query.QuoteIdent in every statement.
//
// Both SQLite (mattn/go-sqlite3) and PostgreSQL (lib/pq) accept $N
// placeholders and ON CONFLICT DO NOTHING, so the same SQL works on both.
type GrantStore struct {
	db     *sql.DB
	table  string
	policy *RolePolicy
}

// GrantStoreOption configures a GrantStore.
type GrantStoreOption func(*GrantStore)

// WithGrantTable overrides the default table name ("access_grants").
// The name is validated via query.SafeIdent — an unsafe identifier
// panics at construction time, not at query time.
func WithGrantTable(name string) GrantStoreOption {
	return func(gs *GrantStore) {
		// MustIdent panics on unsafe identifiers; construction-time fail-fast
		// is the right posture for a config-time value.
		gs.table = query.MustIdent(name)
	}
}

// NewGrantStore creates a GrantStore bound to the given policy. The policy
// reference is retained — Grant/Revoke mutate it directly so concurrent
// Can checks see the change without a reload. Call LoadInto once at boot
// to hydrate the policy from persisted rows.
//
// A nil policy is allowed only if you intend to call LoadInto with a
// policy before any Grant/Revoke; Grant/Revoke on a store with a nil
// policy return an error.
func NewGrantStore(db *sql.DB, policy *RolePolicy, opts ...GrantStoreOption) *GrantStore {
	gs := &GrantStore{
		db:     db,
		table:  "access_grants",
		policy: policy,
	}
	for _, opt := range opts {
		opt(gs)
	}
	return gs
}

// Policy returns the live *RolePolicy the store mutates. May be nil if
// LoadInto has not yet been called and no policy was passed to
// NewGrantStore.
func (s *GrantStore) Policy() *RolePolicy {
	return s.policy
}

// EnsureSchema creates the grants table if it does not already exist.
// Idempotent (CREATE TABLE IF NOT EXISTS). The column types (TEXT) are
// portable across SQLite and PostgreSQL. The (role, permission) pair has
// a UNIQUE constraint so INSERT ... ON CONFLICT DO NOTHING is a no-op for
// duplicates.
func (s *GrantStore) EnsureSchema(ctx context.Context) error {
	stmt := fmt.Sprintf(
		"CREATE TABLE IF NOT EXISTS %s (role TEXT NOT NULL, permission TEXT NOT NULL, UNIQUE(role, permission))",
		query.QuoteIdent(s.table),
	)
	_, err := s.db.ExecContext(ctx, stmt)
	return err
}

// LoadInto reads all persisted grant rows and calls policy.Grant for each,
// hydrating the live *RolePolicy from the database. The policy is also
// retained as the store's active policy (overwriting any previously bound
// one) so subsequent Grant/Revoke calls mutate it. Call once at boot,
// after constructing the policy and after EnsureSchema.
//
// If the store was constructed with a policy and policy is nil, the
// store's existing policy is used.
func (s *GrantStore) LoadInto(ctx context.Context, policy *RolePolicy) error {
	if policy != nil {
		s.policy = policy
	}
	if s.policy == nil {
		return fmt.Errorf("access: GrantStore.LoadInto called with no policy (pass a *RolePolicy or construct with NewGrantStore(db, policy))")
	}
	q := fmt.Sprintf("SELECT role, permission FROM %s", query.QuoteIdent(s.table))
	rows, err := s.db.QueryContext(ctx, q)
	if err != nil {
		return fmt.Errorf("access: load grants: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var role, perm string
		if err := rows.Scan(&role, &perm); err != nil {
			return fmt.Errorf("access: scan grant row: %w", err)
		}
		if err := s.policy.Grant(role, Permission(perm)); err != nil {
			return fmt.Errorf("access: load grant %q→%q: %w", role, perm, err)
		}
	}
	return rows.Err()
}

// Grant validates and expands permissions, persists the resulting
// (role, permission) rows to the database (INSERT ... ON CONFLICT DO NOTHING),
// and then updates the live policy. Idempotent: granting an already-held
// permission is a no-op in both layers. In strict capability mode, validation
// happens before any database write.
//
// Role and permission are bound as $n parameters — never interpolated.
func (s *GrantStore) Grant(ctx context.Context, role string, perms ...Permission) error {
	if s.policy == nil {
		return fmt.Errorf("access: GrantStore has no policy — call LoadInto first")
	}
	if len(perms) == 0 {
		return nil
	}
	prepared, err := s.policy.prepareGrants(perms)
	if err != nil {
		return err
	}
	// One INSERT per (role, perm) with ON CONFLICT DO NOTHING. A batch
	// VALUES clause would be marginally faster but complicates the
	// placeholder math; the grant matrix is small and admin-driven, so
	// clarity wins.
	for _, permission := range prepared {
		q := fmt.Sprintf(
			"INSERT INTO %s (role, permission) VALUES ($1, $2) ON CONFLICT DO NOTHING",
			query.QuoteIdent(s.table),
		)
		if _, err := s.db.ExecContext(ctx, q, role, string(permission)); err != nil {
			return fmt.Errorf("access: persist grant %q→%q: %w", role, permission, err)
		}
	}
	// The DB write succeeded. The prepared set has already been validated and
	// expanded, so update memory without warning a second time.
	s.policy.grantPrepared(role, prepared)
	return nil
}

// Revoke deletes (role, permission) rows from the database and then calls
// policy.Revoke on the live policy. Idempotent: revoking a permission the
// role doesn't hold is a no-op in both layers.
//
// Role and permission are bound as $n parameters — never interpolated.
func (s *GrantStore) Revoke(ctx context.Context, role string, perms ...Permission) error {
	if s.policy == nil {
		return fmt.Errorf("access: GrantStore has no policy — call LoadInto first")
	}
	if len(perms) == 0 {
		return nil
	}
	for _, p := range perms {
		q := fmt.Sprintf(
			"DELETE FROM %s WHERE role = $1 AND permission = $2",
			query.QuoteIdent(s.table),
		)
		if _, err := s.db.ExecContext(ctx, q, role, string(p)); err != nil {
			return fmt.Errorf("access: persist revoke %q→%q: %w", role, p, err)
		}
	}
	// DB write succeeded — update the live policy.
	s.policy.Revoke(role, perms...)
	return nil
}
