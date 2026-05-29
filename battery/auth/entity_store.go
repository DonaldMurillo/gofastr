package auth

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// Both SQLite (via mattn/go-sqlite3) and PostgreSQL (via lib/pq) accept
// $N placeholders, so the entity-backed stores use them uniformly. lib/pq
// does NOT accept ? placeholders, so do not introduce new SQL with them.

// EntityUserStore adapts GoFastr's entity/CRUD system to the UserStore
// interface. It uses raw SQL against the entity's table so auth doesn't
// need to import the full CRUD handler.
//
// The backing entity must have at minimum: id (string/uuid), email (string,
// unique), password_hash (string), roles (string/json).
//
// Usage:
//
//	app.Entity("users", entity.EntityConfig{
//	    Fields: auth.UserEntityFields(),
//	})
//	mgr.SetUserStore(auth.NewEntityUserStore(db, "users"))
type EntityUserStore struct {
	db       *sql.DB
	table    string
	fieldMap UserFieldMap
}

// UserFieldMap maps logical auth fields to actual DB column names.
// Defaults to snake_case standard names.
type UserFieldMap struct {
	ID           string // default: "id"
	Email        string // default: "email"
	PasswordHash string // default: "password_hash"
	Roles        string // default: "roles"
	// PasswordSet flags whether the user has a real password vs the
	// placeholder hash used by OAuth / magic-link auto-create. Defaults
	// to "password_set". The column is optional — if missing from the
	// physical table, HasPassword returns ErrPasswordSetNotTracked and
	// AccountsPlugin falls back to the conservative links-only rule.
	PasswordSet string // default: "password_set"
}

// DefaultUserFieldMap returns the standard field mapping.
func DefaultUserFieldMap() UserFieldMap {
	return UserFieldMap{
		ID:           "id",
		Email:        "email",
		PasswordHash: "password_hash",
		Roles:        "roles",
		PasswordSet:  "password_set",
	}
}

// NewEntityUserStore creates a UserStore backed by a database table.
// The table must have the columns described by fieldMap.
// Panics if the table name or any field mapping contains unsafe characters.
func NewEntityUserStore(db *sql.DB, table string, fieldMap ...UserFieldMap) *EntityUserStore {
	fm := DefaultUserFieldMap()
	if len(fieldMap) > 0 {
		fm = fieldMap[0]
	}
	// Validate all identifiers at construction time — fail fast.
	query.MustIdent(table)
	query.MustIdent(fm.ID)
	query.MustIdent(fm.Email)
	query.MustIdent(fm.PasswordHash)
	query.MustIdent(fm.Roles)
	query.MustIdent(fm.PasswordSet)
	return &EntityUserStore{
		db:       db,
		table:    table,
		fieldMap: fm,
	}
}

// q builds a query using validated identifiers. All table/field names were
// validated at construction time in NewEntityUserStore.
func (s *EntityUserStore) q(stmt string) string {
	return fmt.Sprintf(stmt,
		query.QuoteIdent(s.fieldMap.ID),
		query.QuoteIdent(s.fieldMap.Email),
		query.QuoteIdent(s.fieldMap.PasswordHash),
		query.QuoteIdent(s.fieldMap.Roles),
		query.QuoteIdent(s.table),
		query.QuoteIdent(s.fieldMap.Email),
	)
}

// qByID builds a query selecting by ID.
func (s *EntityUserStore) qByID(stmt string) string {
	return fmt.Sprintf(stmt,
		query.QuoteIdent(s.fieldMap.ID),
		query.QuoteIdent(s.fieldMap.Email),
		query.QuoteIdent(s.fieldMap.Roles),
		query.QuoteIdent(s.table),
		query.QuoteIdent(s.fieldMap.ID),
	)
}

// FindByEmail looks up a user by email. Returns ErrUserNotFound when no
// row matches; any other error is returned verbatim.
func (s *EntityUserStore) FindByEmail(ctx context.Context, email string) (User, string, error) {
	q := s.q("SELECT %s, %s, %s, %s FROM %s WHERE %s = $1")
	var id, emailOut, hash, rolesStr string
	err := s.db.QueryRowContext(ctx, q, email).Scan(&id, &emailOut, &hash, &rolesStr)
	if err == sql.ErrNoRows {
		return nil, "", ErrUserNotFound
	}
	if err != nil {
		return nil, "", err
	}
	return &BasicUser{
		ID:    id,
		Email: emailOut,
		Roles: parseRoles(rolesStr),
	}, hash, nil
}

// FindByID looks up a user by their unique ID. Returns ErrUserNotFound
// when no row matches; any other error is returned verbatim.
func (s *EntityUserStore) FindByID(ctx context.Context, id string) (User, error) {
	q := s.qByID("SELECT %s, %s, %s FROM %s WHERE %s = $1")
	var idOut, email, rolesStr string
	err := s.db.QueryRowContext(ctx, q, id).Scan(&idOut, &email, &rolesStr)
	if err == sql.ErrNoRows {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, err
	}
	return &BasicUser{
		ID:    idOut,
		Email: email,
		Roles: parseRoles(rolesStr),
	}, nil
}

// CreateUser inserts a new user and returns it. Returns ErrEmailTaken
// when the email already exists; other errors are returned verbatim.
//
// CreateUser marks password_set=true. Callers that auto-create an
// account without a user-chosen password (OAuth, magic-link) should
// prefer CreateUserNoPassword so HasPassword reports the right answer
// later.
func (s *EntityUserStore) CreateUser(ctx context.Context, email, hashedPassword string, roles []string) (User, error) {
	return s.create(ctx, email, hashedPassword, roles, true)
}

// CreateUserNoPassword auto-creates a user using the package-level
// placeholder hash and records password_set=false. Used by the OAuth /
// magic-link auto-create flows. Implements OAuthUserCreator.
func (s *EntityUserStore) CreateUserNoPassword(ctx context.Context, email string, roles []string) (User, error) {
	return s.create(ctx, email, passwordPlaceholderHash, roles, false)
}

func (s *EntityUserStore) create(ctx context.Context, email, hashedPassword string, roles []string, passwordSet bool) (User, error) {
	id := generateUserID()
	rolesJSON := formatRoles(roles)

	q := fmt.Sprintf(
		"INSERT INTO %s (%s, %s, %s, %s, %s) VALUES ($1, $2, $3, $4, $5)",
		query.QuoteIdent(s.table),
		query.QuoteIdent(s.fieldMap.ID),
		query.QuoteIdent(s.fieldMap.Email),
		query.QuoteIdent(s.fieldMap.PasswordHash),
		query.QuoteIdent(s.fieldMap.Roles),
		query.QuoteIdent(s.fieldMap.PasswordSet),
	)
	_, err := s.db.ExecContext(ctx, q, id, email, hashedPassword, rolesJSON, passwordSet)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, ErrEmailTaken
		}
		return nil, err
	}
	return &BasicUser{
		ID:    id,
		Email: email,
		Roles: roles,
	}, nil
}

// SetPassword updates a user's password hash and marks password_set=true.
// Implements PasswordSetter (used by PasswordResetPlugin).
func (s *EntityUserStore) SetPassword(ctx context.Context, userID, hashedPassword string) error {
	q := fmt.Sprintf(
		"UPDATE %s SET %s = $1, %s = TRUE WHERE %s = $2",
		query.QuoteIdent(s.table),
		query.QuoteIdent(s.fieldMap.PasswordHash),
		query.QuoteIdent(s.fieldMap.PasswordSet),
		query.QuoteIdent(s.fieldMap.ID),
	)
	res, err := s.db.ExecContext(ctx, q, hashedPassword, userID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrUserNotFound
	}
	return nil
}

// HasPassword reports whether the user has a real password set vs the
// placeholder hash. Implements PasswordChecker.
func (s *EntityUserStore) HasPassword(ctx context.Context, userID string) (bool, error) {
	q := fmt.Sprintf(
		"SELECT %s FROM %s WHERE %s = $1",
		query.QuoteIdent(s.fieldMap.PasswordSet),
		query.QuoteIdent(s.table),
		query.QuoteIdent(s.fieldMap.ID),
	)
	var has bool
	err := s.db.QueryRowContext(ctx, q, userID).Scan(&has)
	if err == sql.ErrNoRows {
		return false, ErrUserNotFound
	}
	return has, err
}

// isUniqueViolation detects unique-constraint violations across the
// dialects we support (SQLite + Postgres). The driver-specific error
// strings are stable enough to match on textually.
func isUniqueViolation(err error) bool {
	msg := err.Error()
	// SQLite (mattn/go-sqlite3): "UNIQUE constraint failed: users.email"
	if strings.Contains(msg, "UNIQUE constraint failed") {
		return true
	}
	// Postgres (lib/pq): "pq: duplicate key value violates unique constraint"
	// Postgres SQLSTATE 23505. The error message includes the SQLSTATE
	// in lib/pq's standard format.
	if strings.Contains(msg, "23505") || strings.Contains(msg, "duplicate key value") {
		return true
	}
	return false
}

// UserEntityConfig returns an EntityConfig pre-configured for the auth
// users table: standard fields + CRUD=false + MCP=false. Auto-private by
// default so host apps don't accidentally expose user rows through
// auto-generated REST or MCP tools.
//
// Hosts that DO want to expose a /users endpoint can override after the
// fact (e.g. `cfg := auth.UserEntityConfig(); enabled := true; cfg.CRUD =
// &enabled`).
//
//	app.Entity("users", auth.UserEntityConfig())
func UserEntityConfig() entity.EntityConfig {
	crudOff := false
	return entity.EntityConfig{
		Fields: UserEntityFields(),
		CRUD:   &crudOff,
		MCP:    false,
	}
}

// SessionEntityConfig returns an EntityConfig pre-configured for the auth
// sessions table: standard fields + CRUD=false + MCP=false. Auto-private
// by default. See UserEntityConfig for the rationale.
//
//	app.Entity("sessions", auth.SessionEntityConfig())
func SessionEntityConfig() entity.EntityConfig {
	crudOff := false
	return entity.EntityConfig{
		Fields: SessionEntityFields(),
		CRUD:   &crudOff,
		MCP:    false,
	}
}

// UserEntityFields returns the standard field definitions for a user entity.
// Apps call this when defining their user entity:
//
//	app.Entity("users", entity.EntityConfig{
//	    Fields: auth.UserEntityFields(),
//	})
//
// For most apps, prefer UserEntityConfig() which also disables the
// dangerous-by-default auto-CRUD on user rows.
func UserEntityFields() UserFields {
	return UserFields{
		{Name: "email", Type: schema.String, Required: true, Unique: true},
		{Name: "password_hash", Type: schema.String, Required: true, Hidden: true},
		{Name: "roles", Type: schema.String, Default: `["user"]`},
		{Name: "password_set", Type: schema.Bool, Default: "false"},
	}
}

// UserFields is the canonical user-entity field list returned by
// UserEntityFields. Its underlying type is []schema.Field so it
// remains assignable to entity.EntityConfig.Fields without any cast.
// The named type exists solely to attach the fluent With method.
type UserFields []schema.Field

// With returns a new UserFields containing the canonical fields plus
// any additional fields the host wants to attach (typically domain
// columns like username, disabled_at, or display_name).
//
// The returned slice is independent — repeated calls don't mutate the
// receiver or the canonical list.
//
// Example:
//
//	app.Entity("users", entity.EntityConfig{
//	    Fields: auth.UserEntityFields().With(
//	        schema.Field{Name: "username", Type: schema.String, Unique: true},
//	        schema.Field{Name: "disabled_at", Type: schema.Timestamp},
//	    ),
//	})
func (uf UserFields) With(extra ...schema.Field) UserFields {
	out := make(UserFields, 0, len(uf)+len(extra))
	out = append(out, uf...)
	out = append(out, extra...)
	return out
}

// EntitySessionStore adapts a database table to the SessionStore interface.
// The backing table must have: token (string, PK), user_id (string),
// created_at (timestamp), expires_at (timestamp).
//
// Usage:
//
//	app.Entity("sessions", entity.EntityConfig{
//	    Fields: auth.SessionEntityFields(),
//	})
//	mgr.SetSessionStore(auth.NewEntitySessionStore(db, "sessions"))
type EntitySessionStore struct {
	db    *sql.DB
	table string
}

// NewEntitySessionStore creates a SessionStore backed by a database table.
// Panics if the table name contains unsafe characters.
func NewEntitySessionStore(db *sql.DB, table string) *EntitySessionStore {
	query.MustIdent(table)
	return &EntitySessionStore{
		db:    db,
		table: table,
	}
}

// qTable wraps a statement template with the validated table name.
func (s *EntitySessionStore) qTable(stmt string) string {
	return fmt.Sprintf(stmt, query.QuoteIdent(s.table))
}

// Create generates a random token, inserts a session row, and returns it.
// Rejects ttl <= 0 — the caller must supply a positive lifetime.
func (s *EntitySessionStore) Create(ctx context.Context, userID string, ttl time.Duration) (*Session, error) {
	if ttl <= 0 {
		return nil, fmt.Errorf("auth: EntitySessionStore.Create: ttl must be > 0 (got %v)", ttl)
	}
	tok, err := newSessionToken()
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	expiresAt := now.Add(ttl)

	// AutoUUID `id` column is created NOT NULL with no DEFAULT, so
	// PostgreSQL requires us to supply a value here even though the
	// session is keyed by token, not id. SQLite happens to accept NULL
	// for INTEGER-PK-like columns, masking this in dev.
	sessionID := generateUserID()
	q := s.qTable("INSERT INTO %s (id, token, user_id, created_at, expires_at) VALUES ($1, $2, $3, $4, $5)")
	_, err = s.db.ExecContext(ctx, q, sessionID, tok, userID, now, expiresAt)
	if err != nil {
		return nil, err
	}
	return &Session{
		Token:     tok,
		UserID:    userID,
		CreatedAt: now,
		ExpiresAt: expiresAt,
	}, nil
}

// Get returns the session for the given token.
func (s *EntitySessionStore) Get(ctx context.Context, token string) (*Session, error) {
	q := s.qTable("SELECT token, user_id, created_at, expires_at, two_factor_verified, pending_two_factor FROM %s WHERE token = $1")
	var tok, uid string
	var createdAtRaw, expiresAtRaw any
	var twoFA, pending bool
	err := s.db.QueryRowContext(ctx, q, token).Scan(&tok, &uid, &createdAtRaw, &expiresAtRaw, &twoFA, &pending)
	if err == sql.ErrNoRows {
		return nil, ErrSessionNotFound
	}
	if err != nil {
		return nil, err
	}

	sess := &Session{
		Token:             tok,
		UserID:            uid,
		CreatedAt:         coerceTime(createdAtRaw),
		ExpiresAt:         coerceTime(expiresAtRaw),
		TwoFactorVerified: twoFA,
		PendingTwoFactor:  pending,
	}
	if sess.Expired() {
		return nil, ErrSessionNotFound
	}
	return sess, nil
}

// MarkTwoFactorVerified sets two_factor_verified=TRUE and clears
// pending_two_factor. Implements SessionTwoFAMarker.
func (s *EntitySessionStore) MarkTwoFactorVerified(ctx context.Context, token string) error {
	_, err := s.db.ExecContext(ctx,
		s.qTable("UPDATE %s SET two_factor_verified = TRUE, pending_two_factor = FALSE WHERE token = $1"),
		token)
	return err
}

// MarkPendingTwoFactor flips pending_two_factor=TRUE for the given session.
// Implements SessionPendingMarker.
func (s *EntitySessionStore) MarkPendingTwoFactor(ctx context.Context, token string) error {
	_, err := s.db.ExecContext(ctx,
		s.qTable("UPDATE %s SET pending_two_factor = TRUE WHERE token = $1"),
		token)
	return err
}

// Delete removes a session by token. Idempotent.
func (s *EntitySessionStore) Delete(ctx context.Context, token string) error {
	_, err := s.db.ExecContext(ctx, s.qTable("DELETE FROM %s WHERE token = $1"), token)
	return err
}

// DeleteByUser removes every session belonging to userID and returns the
// count purged. Implements SessionUserPurger.
func (s *EntitySessionStore) DeleteByUser(ctx context.Context, userID string) (int, error) {
	result, err := s.db.ExecContext(ctx, s.qTable("DELETE FROM %s WHERE user_id = $1"), userID)
	if err != nil {
		return 0, err
	}
	n, _ := result.RowsAffected()
	return int(n), nil
}

// Cleanup removes all expired sessions and returns the count purged.
func (s *EntitySessionStore) Cleanup(ctx context.Context) (int, error) {
	result, err := s.db.ExecContext(ctx, s.qTable("DELETE FROM %s WHERE expires_at < $1"), time.Now().UTC())
	if err != nil {
		return 0, err
	}
	n, _ := result.RowsAffected()
	return int(n), nil
}

// SessionEntityFields returns the standard field definitions for a session entity.
func SessionEntityFields() []schema.Field {
	return []schema.Field{
		{Name: "token", Type: schema.String, Required: true},
		{Name: "user_id", Type: schema.String, Required: true},
		{Name: "created_at", Type: schema.Timestamp},
		{Name: "expires_at", Type: schema.Timestamp, Required: true},
		{Name: "two_factor_verified", Type: schema.Bool, Default: "false"},
		{Name: "pending_two_factor", Type: schema.Bool, Default: "false"},
	}
}

// ─── Helpers ────────────────────────────────────────────────────────────

// generateUserID creates a random user ID using crypto/rand.
func generateUserID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// Fallback to timestamp-based ID if crypto/rand fails
		return fmt.Sprintf("user-%d", time.Now().UnixNano())
	}
	return fmt.Sprintf("%x", b)
}

// parseRoles parses a roles value from DB (JSON array or comma-separated).
func parseRoles(s string) []string {
	if s == "" {
		return []string{"user"}
	}
	// Try JSON first
	var roles []string
	if err := json.Unmarshal([]byte(s), &roles); err == nil {
		return roles
	}
	// Fallback: treat as single role or comma-separated
	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	if len(result) == 0 {
		return []string{"user"}
	}
	return result
}

// formatRoles serializes roles to JSON array.
func formatRoles(roles []string) string {
	if len(roles) == 0 {
		roles = []string{"user"}
	}
	b, _ := json.Marshal(roles)
	return string(b)
}

// parseTimeFlex tries multiple time layouts for maximum DB compatibility.
func parseTimeFlex(s string) time.Time {
	layouts := []string{
		"2006-01-02 15:04:05.999999999-07:00",
		"2006-01-02 15:04:05.999999999",
		"2006-01-02 15:04:05-07:00",
		"2006-01-02 15:04:05",
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02T15:04:05Z07:00",
		"2006-01-02T15:04:05",
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	// Fallback: try time.Parse which handles some Go time formats
	t, _ := time.Parse(time.RFC3339, s)
	return t
}

// coerceTime converts whatever the driver hands back (time.Time on PG,
// string on SQLite when DATETIME is declared as TEXT, []byte in some
// driver configurations) into a time.Time. Mirrors framework/auth's
// scanTime helper, but inlined to keep battery/auth self-contained.
func coerceTime(src any) time.Time {
	switch v := src.(type) {
	case time.Time:
		return v
	case *time.Time:
		if v == nil {
			return time.Time{}
		}
		return *v
	case string:
		return parseTimeFlex(v)
	case []byte:
		return parseTimeFlex(string(v))
	case nil:
		return time.Time{}
	}
	return time.Time{}
}
