package auth

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/core/textsafe"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
)

// EntityUserStore is a UserStore over a SQLite or PostgreSQL users table, with raw SQL.
type EntityUserStore struct {
	db           *sql.DB
	table        string
	fieldMap     UserFieldMap
	dialect      storeDialect
	hidesDeleted atomic.Bool
}

const softDeleteColumn = "deleted_at"

func (s *EntityUserStore) liveRows() string {
	if !s.hidesDeleted.Load() {
		return ""
	}
	return query.QuoteFolded(softDeleteColumn) + " IS NULL"
}

// UserFieldMap maps logical auth fields to actual DB column names.
type UserFieldMap struct {
	ID           string // default: "id"
	Email        string // default: "email"
	PasswordHash string // default: "password_hash"
	Roles        string // default: "roles"
	// PasswordSet is the column flagging a real password over the placeholder; without it HasPassword returns ErrPasswordSetNotTracked.
	PasswordSet string // default: "password_set"
	// EmailVerified is the column recording that the owner proved the address; EnsureSchema adds it as FALSE.
	EmailVerified string // default: "email_verified"
	// Name is the display-name column; EnsureSchema adds it as ''.
	Name string // default: "name"
}

// DefaultUserFieldMap returns the standard field mapping.
func DefaultUserFieldMap() UserFieldMap {
	return UserFieldMap{
		ID:            "id",
		Email:         "email",
		PasswordHash:  "password_hash",
		Roles:         "roles",
		PasswordSet:   "password_set",
		EmailVerified: "email_verified",
		Name:          "name",
	}
}

// NewEntityUserStore creates a UserStore backed by a database table.
func NewEntityUserStore(db *sql.DB, table string, fieldMap ...UserFieldMap) *EntityUserStore {
	fm := DefaultUserFieldMap()
	if len(fieldMap) > 0 {
		fm = fieldMap[0]
	}
	if fm.EmailVerified == "" {
		fm.EmailVerified = "email_verified"
	}
	if fm.Name == "" {
		fm.Name = "name"
	}
	query.MustIdent(table)
	query.MustIdent(fm.ID)
	query.MustIdent(fm.Email)
	query.MustIdent(fm.PasswordHash)
	query.MustIdent(fm.Roles)
	query.MustIdent(fm.PasswordSet)
	query.MustIdent(fm.EmailVerified)
	query.MustIdent(fm.Name)
	return &EntityUserStore{
		db:       db,
		table:    table,
		fieldMap: fm,
	}
}

// EnsureSchema creates the users table, adds missing columns and the OAuth link table, and refuses a case-kept legacy table.
func (s *EntityUserStore) EnsureSchema(ctx context.Context) error {
	fm := s.fieldMap
	if err := refuseCaseKeptTable(ctx, s.db, s.dialect.of(s.db), s.table,
		fm.ID, fm.Email, fm.PasswordHash, fm.Roles, fm.PasswordSet, fm.EmailVerified, fm.Name); err != nil {
		return err
	}
	boolType, boolFalse := "INTEGER", "0"
	if s.dialect.of(s.db) == migrate.DialectPostgres {
		boolType, boolFalse = "BOOLEAN", "FALSE"
	}
	stmt := fmt.Sprintf(
		"CREATE TABLE IF NOT EXISTS %s (%s TEXT PRIMARY KEY NOT NULL, %s TEXT UNIQUE NOT NULL, %s TEXT NOT NULL DEFAULT '', %s TEXT NOT NULL DEFAULT '[]', %s %s NOT NULL DEFAULT %s)",
		query.QuoteFolded(s.table),
		query.QuoteFolded(s.fieldMap.ID),
		query.QuoteFolded(s.fieldMap.Email),
		query.QuoteFolded(s.fieldMap.PasswordHash),
		query.QuoteFolded(s.fieldMap.Roles),
		query.QuoteFolded(s.fieldMap.PasswordSet),
		boolType,
		boolFalse,
	)
	_, err := s.db.ExecContext(ctx, stmt)
	if err != nil {
		return err
	}
	if err := s.ensureEmailVerifiedColumn(ctx, boolType, boolFalse); err != nil {
		return err
	}
	if err := s.ensureNameColumn(ctx); err != nil {
		return err
	}
	if err := ensurePostgresBoolColumns(ctx, s.db, s.dialect.of(s.db), s.table, s.fieldMap.PasswordSet, s.fieldMap.EmailVerified); err != nil {
		return err
	}
	hides, err := tableHasColumn(ctx, s.db, s.dialect.of(s.db), s.table, softDeleteColumn)
	if err != nil {
		return err
	}
	s.hidesDeleted.Store(hides)
	return s.EnsureOAuthLinksSchema(ctx)
}

func refuseCaseKeptTable(ctx context.Context, db *sql.DB, d migrate.Dialect, table string, cols ...string) error {
	if d != migrate.DialectPostgres {
		return nil
	}
	schema, name := foldedTableRef(table)
	folded := query.QuoteFolded(table)
	var legacy, legacySchema, legacyName string
	//gofastr:allow(quoteident) the case-kept spelling an older battery created, compared to detect it
	if query.QuoteIdent(table) != folded {
		cands := [][2]string{{"", table}}
		if i := strings.LastIndexByte(table, '.'); i >= 0 && table[i+1:] != name {
			cands = append(cands, [2]string{table[:i], table[i+1:]})
		}
		for _, c := range cands {
			found, ok, err := pgFindTable(ctx, db, c[0], c[1])
			if err != nil {
				return err
			}
			if ok {
				legacySchema, legacyName = found, c[1]
				legacy = quoteQualified(found, legacyName)
				break
			}
		}
	}
	colSchema, colTable := schema, name
	if legacy != "" {
		colSchema, colTable = legacySchema, legacyName
	}
	var keptCols []string
	for _, col := range cols {
		if col == strings.ToLower(col) || slices.Contains(keptCols, col) {
			continue
		}
		var n int
		//gofastr:allow(GOFASTR1502) one catalog read per configured mixed-case column, once at boot
		if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM information_schema.columns
			WHERE table_schema = COALESCE(NULLIF($1, ''), current_schema()) AND table_name = $2 AND column_name = $3`,
			colSchema, colTable, col).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			keptCols = append(keptCols, col)
		}
	}
	if legacy == "" && len(keptCols) == 0 {
		return nil
	}
	var steps []string
	if legacy != "" {
		steps = append(steps, fmt.Sprintf("ALTER TABLE %s RENAME TO %s", quoteQualified(legacySchema, legacyName), query.QuoteFolded(name)))
		if legacySchema != schema {
			steps = append(steps, fmt.Sprintf("ALTER TABLE %s SET SCHEMA %s", quoteQualified(legacySchema, name), query.QuoteFolded(schema)))
		}
	}
	for _, col := range keptCols {
		steps = append(steps, fmt.Sprintf("ALTER TABLE %s RENAME COLUMN %s TO %s",
			//gofastr:allow(quoteident) col is the case-kept column an older battery created
			folded, query.QuoteIdent(col), query.QuoteFolded(col)))
	}
	advice := strings.Join(steps, "; ")
	if legacy == "" {
		return fmt.Errorf("auth: Postgres table %s holds columns with their upper case kept (%s), made by an older auth battery; "+
			"the store now reads the folded names AutoMigrate creates. Rename them, then restart: %s",
			folded, strings.Join(keptCols, ", "), advice)
	}
	how := "with its upper case kept"
	if legacySchema == "" && strings.Contains(table, ".") {
		how = "as one name with the dot inside"
	}
	return fmt.Errorf("auth: Postgres holds table %s %s, made by an older auth battery; "+
		"the store now uses %s, the table AutoMigrate creates. Move the rows into %s and drop %s "+
		"(when %s does not exist: %s), then restart",
		legacy, how, folded, folded, legacy, folded, advice)
}

func foldedTableRef(table string) (schema, name string) {
	if i := strings.LastIndexByte(table, '.'); i >= 0 {
		return strings.ToLower(table[:i]), strings.ToLower(table[i+1:])
	}
	return "", strings.ToLower(table)
}

func quoteQualified(schema, name string) string {
	if schema == "" {
		//gofastr:allow(quoteident) name was found in the catalog with its case kept
		return query.QuoteIdent(name)
	}
	//gofastr:allow(quoteident) schema and name were found in the catalog with their case kept
	return query.QuoteIdent(schema) + "." + query.QuoteIdent(name)
}

func indexName(prefix, table, suffix string) string {
	_, name := foldedTableRef(table)
	return query.QuoteFolded(query.FitIdent(prefix + name + suffix))
}

func pgFindTable(ctx context.Context, db *sql.DB, schema, name string) (string, bool, error) {
	var found string
	err := db.QueryRowContext(ctx, `SELECT CASE WHEN $1 = '' THEN '' ELSE table_schema END
		FROM information_schema.tables
		WHERE table_schema IN (COALESCE(NULLIF($1, ''), current_schema()), lower(COALESCE(NULLIF($1, ''), current_schema())))
		  AND table_name = $2 LIMIT 1`, schema, name).Scan(&found)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	return found, err == nil, err
}

func tableHasColumn(ctx context.Context, db *sql.DB, d migrate.Dialect, table, column string) (bool, error) {
	if d != migrate.DialectPostgres {
		return sqliteTableHasColumn(ctx, db, table, column)
	}
	schema, name := foldedTableRef(table)
	var n int
	err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM information_schema.columns
		WHERE table_schema = COALESCE(NULLIF($1, ''), current_schema()) AND table_name = $2 AND column_name = $3`,
		schema, name, column).Scan(&n)
	return n > 0, err
}

func ensurePostgresBoolColumns(ctx context.Context, db *sql.DB, d migrate.Dialect, table string, columns ...string) error {
	if d != migrate.DialectPostgres {
		return nil
	}
	query.MustIdent(table)
	schema, name := foldedTableRef(table)
	for _, column := range columns {
		query.MustIdent(column)
		var dataType string
		//gofastr:allow(GOFASTR1502) one catalog read per battery-owned column, once at boot; the list is a handful of names, not rows
		err := db.QueryRowContext(ctx, `
			SELECT data_type
			FROM information_schema.columns
			WHERE table_schema = COALESCE(NULLIF($3, ''), current_schema())
			  AND table_name = $1
			  AND lower(column_name) = lower($2)
		`, name, column, schema).Scan(&dataType)
		if err == sql.ErrNoRows || dataType == "boolean" {
			continue
		}
		if err != nil {
			return err
		}
		switch dataType {
		case "smallint", "integer", "bigint":
		default:
			continue
		}
		qt, qc := query.QuoteFolded(table), query.QuoteFolded(column)
		for _, ddl := range []string{
			fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s DROP DEFAULT", qt, qc),
			fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s TYPE BOOLEAN USING (%s <> 0)", qt, qc, qc),
			fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s SET DEFAULT FALSE", qt, qc),
		} {
			if _, err := db.ExecContext(ctx, ddl); err != nil {
				return fmt.Errorf("convert %s.%s to BOOLEAN: %w", table, column, err)
			}
		}
	}
	return nil
}

func (s *EntityUserStore) q(stmt string) string {
	return fmt.Sprintf(stmt,
		query.QuoteFolded(s.fieldMap.ID),
		query.QuoteFolded(s.fieldMap.Email),
		query.QuoteFolded(s.fieldMap.PasswordHash),
		query.QuoteFolded(s.fieldMap.Roles),
		query.QuoteFolded(s.table),
		query.QuoteFolded(s.fieldMap.Email),
	)
}

func (s *EntityUserStore) qByID(stmt string) string {
	return fmt.Sprintf(stmt,
		query.QuoteFolded(s.fieldMap.ID),
		query.QuoteFolded(s.fieldMap.Email),
		query.QuoteFolded(s.fieldMap.Roles),
		query.QuoteFolded(s.table),
		query.QuoteFolded(s.fieldMap.ID),
	)
}

// FindByEmail returns the user and hash, ErrUserNotFound, or the store's error.
func (s *EntityUserStore) FindByEmail(ctx context.Context, email string) (User, string, error) {
	q := s.q("SELECT %s, %s, %s, %s FROM %s WHERE %s = $1")
	if live := s.liveRows(); live != "" {
		q += " AND " + live
	}
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

// FindByID returns the user, ErrUserNotFound, or the store's error.
func (s *EntityUserStore) FindByID(ctx context.Context, id string) (User, error) {
	if !textsafe.Storable(id) {
		return nil, ErrUserNotFound
	}
	q := s.qByID("SELECT %s, %s, %s FROM %s WHERE %s = $1")
	if live := s.liveRows(); live != "" {
		q += " AND " + live
	}
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

// ListUsers returns one page of accounts ordered by email, and the total, without password material. Implements UserLister.
func (s *EntityUserStore) ListUsers(ctx context.Context, opts ListUsersOptions) ([]User, int, error) {
	where := ""
	if live := s.liveRows(); live != "" {
		where = " WHERE " + live
	}
	listQ := fmt.Sprintf(
		"SELECT %s, %s, %s FROM %s%s ORDER BY %s LIMIT $1 OFFSET $2",
		query.QuoteFolded(s.fieldMap.ID),
		query.QuoteFolded(s.fieldMap.Email),
		query.QuoteFolded(s.fieldMap.Roles),
		query.QuoteFolded(s.table),
		where,
		query.QuoteFolded(s.fieldMap.Email),
	)
	rows, err := s.db.QueryContext(ctx, listQ, opts.Limit, opts.Offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	users := make([]User, 0, opts.Limit)
	for rows.Next() {
		var id, email, rolesStr string
		if err := rows.Scan(&id, &email, &rolesStr); err != nil {
			return nil, 0, err
		}
		users = append(users, &BasicUser{
			ID:    id,
			Email: email,
			Roles: parseRoles(rolesStr),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}

	var total int
	countQ := fmt.Sprintf("SELECT COUNT(*) FROM %s%s", query.QuoteFolded(s.table), where)
	if err := s.db.QueryRowContext(ctx, countQ).Scan(&total); err != nil {
		return nil, 0, err
	}
	return users, total, nil
}

// CreateUser inserts a user with a real password, or returns ErrEmailTaken.
func (s *EntityUserStore) CreateUser(ctx context.Context, email, hashedPassword string, roles []string) (User, error) {
	return s.create(ctx, email, hashedPassword, roles, true)
}

// CreateUserNoPassword inserts a user holding the shared placeholder hash and password_set false. Implements OAuthUserCreator.
func (s *EntityUserStore) CreateUserNoPassword(ctx context.Context, email string, roles []string) (User, error) {
	return s.create(ctx, email, placeholderPasswordHash(), roles, false)
}

func (s *EntityUserStore) create(ctx context.Context, email, hashedPassword string, roles []string, passwordSet bool) (User, error) {
	id, err := generateUserID()
	if err != nil {
		return nil, err
	}
	rolesJSON := formatRoles(roles)
	q := fmt.Sprintf(
		"INSERT INTO %s (%s, %s, %s, %s, %s) VALUES ($1, $2, $3, $4, $5)",
		query.QuoteFolded(s.table),
		query.QuoteFolded(s.fieldMap.ID),
		query.QuoteFolded(s.fieldMap.Email),
		query.QuoteFolded(s.fieldMap.PasswordHash),
		query.QuoteFolded(s.fieldMap.Roles),
		query.QuoteFolded(s.fieldMap.PasswordSet),
	)
	_, err = s.db.ExecContext(ctx, q, id, email, hashedPassword, rolesJSON, passwordSet)
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
func (s *EntityUserStore) SetPassword(ctx context.Context, userID, hashedPassword string) error {
	q := fmt.Sprintf(
		"UPDATE %s SET %s = $1, %s = TRUE WHERE %s = $2",
		query.QuoteFolded(s.table),
		query.QuoteFolded(s.fieldMap.PasswordHash),
		query.QuoteFolded(s.fieldMap.PasswordSet),
		query.QuoteFolded(s.fieldMap.ID),
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

// SwapPassword sets the hash only while the stored one is still oldHash.
func (s *EntityUserStore) SwapPassword(ctx context.Context, userID, oldHash, newHash string) (bool, error) {
	q := fmt.Sprintf(
		"UPDATE %s SET %s = $1, %s = TRUE WHERE %s = $2 AND %s = $3",
		query.QuoteFolded(s.table),
		query.QuoteFolded(s.fieldMap.PasswordHash),
		query.QuoteFolded(s.fieldMap.PasswordSet),
		query.QuoteFolded(s.fieldMap.ID),
		query.QuoteFolded(s.fieldMap.PasswordHash),
	)
	res, err := s.db.ExecContext(ctx, q, newHash, userID, oldHash)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// HasPassword reports whether the user has a real password. Implements PasswordChecker.
func (s *EntityUserStore) HasPassword(ctx context.Context, userID string) (bool, error) {
	q := fmt.Sprintf(
		"SELECT %s FROM %s WHERE %s = $1",
		query.QuoteFolded(s.fieldMap.PasswordSet),
		query.QuoteFolded(s.table),
		query.QuoteFolded(s.fieldMap.ID),
	)
	var has bool
	err := s.db.QueryRowContext(ctx, q, userID).Scan(&has)
	if err == sql.ErrNoRows {
		return false, ErrUserNotFound
	}
	return has, err
}

func (s *EntityUserStore) ensureEmailVerifiedColumn(ctx context.Context, boolType, boolFalse string) error {
	ddl := fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s NOT NULL DEFAULT %s",
		query.QuoteFolded(s.table), query.QuoteFolded(s.fieldMap.EmailVerified), boolType, boolFalse)
	if s.dialect.of(s.db) == migrate.DialectPostgres {
		ddl = fmt.Sprintf("ALTER TABLE %s ADD COLUMN IF NOT EXISTS %s %s NOT NULL DEFAULT %s",
			query.QuoteFolded(s.table), query.QuoteFolded(s.fieldMap.EmailVerified), boolType, boolFalse)
	} else {
		has, err := sqliteTableHasColumn(ctx, s.db, s.table, s.fieldMap.EmailVerified)
		if err != nil || has {
			return err
		}
	}
	_, err := s.db.ExecContext(ctx, ddl)
	return err
}

// MarkEmailVerified records the address as proven. Implements EmailVerifier.
func (s *EntityUserStore) MarkEmailVerified(ctx context.Context, userID string) error {
	q := fmt.Sprintf("UPDATE %s SET %s = TRUE WHERE %s = $1",
		query.QuoteFolded(s.table),
		query.QuoteFolded(s.fieldMap.EmailVerified),
		query.QuoteFolded(s.fieldMap.ID),
	)
	res, err := s.db.ExecContext(ctx, q, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrUserNotFound
	}
	return nil
}

// IsEmailVerified reports whether the address is proven. Implements EmailVerifiedChecker.
func (s *EntityUserStore) IsEmailVerified(ctx context.Context, userID string) (bool, error) {
	q := fmt.Sprintf("SELECT %s FROM %s WHERE %s = $1",
		query.QuoteFolded(s.fieldMap.EmailVerified),
		query.QuoteFolded(s.table),
		query.QuoteFolded(s.fieldMap.ID),
	)
	var v bool
	err := s.db.QueryRowContext(ctx, q, userID).Scan(&v)
	if err == sql.ErrNoRows {
		return false, ErrUserNotFound
	}
	return v, err
}

// ClearPassword writes a fresh unusable hash and password_set false. Implements PasswordClearer.
func (s *EntityUserStore) ClearPassword(ctx context.Context, userID string) error {
	placeholder, err := newPlaceholderHash()
	if err != nil {
		return err
	}
	q := fmt.Sprintf("UPDATE %s SET %s = $1, %s = FALSE WHERE %s = $2",
		query.QuoteFolded(s.table),
		query.QuoteFolded(s.fieldMap.PasswordHash),
		query.QuoteFolded(s.fieldMap.PasswordSet),
		query.QuoteFolded(s.fieldMap.ID),
	)
	res, err := s.db.ExecContext(ctx, q, placeholder, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrUserNotFound
	}
	return nil
}

func isUniqueViolation(err error) bool {
	msg := err.Error()
	if strings.Contains(msg, "UNIQUE constraint failed") {
		return true
	}
	if strings.Contains(msg, "23505") || strings.Contains(msg, "duplicate key value") {
		return true
	}
	return false
}

// UserEntityConfig returns the users entity with CRUD and MCP off.
func UserEntityConfig() entity.EntityConfig {
	crudOff := false
	return entity.EntityConfig{Fields: UserEntityFields(), Exposure: &entity.ExposureConfig{CRUD: &crudOff, MCP: false}}
}

// SessionEntityConfig returns the sessions entity with CRUD and MCP off.
func SessionEntityConfig() entity.EntityConfig {
	crudOff := false
	return entity.EntityConfig{Fields: SessionEntityFields(), Exposure: &entity.ExposureConfig{CRUD: &crudOff, MCP: false}}
}

// UserEntityFields returns the standard field definitions for a user entity.
func UserEntityFields() UserFields {
	return UserFields{
		{Name: "email", Type: schema.String, Required: true, Unique: true},
		{Name: "password_hash", Type: schema.String, Required: true, Hidden: true},
		{Name: "roles", Type: schema.String, Default: `["user"]`},
		{Name: "password_set", Type: schema.Bool, Default: "false"},
		{Name: "email_verified", Type: schema.Bool, Default: "false"},
	}
}

// UserFields is the users entity's field list.
type UserFields []schema.Field

// With returns a copy of the fields plus extra.
func (uf UserFields) With(extra ...schema.Field) UserFields {
	out := make(UserFields, 0, len(uf)+len(extra))
	out = append(out, uf...)
	out = append(out, extra...)
	return out
}

// EntitySessionStore adapts a database table to the SessionStore interface.
type EntitySessionStore struct {
	db      *sql.DB
	table   string
	dialect storeDialect

	sweepMu   sync.Mutex
	lastSweep time.Time
}

// NewEntitySessionStore creates a SessionStore backed by a database table.
func NewEntitySessionStore(db *sql.DB, table string) *EntitySessionStore {
	query.MustIdent(table)
	return &EntitySessionStore{
		db:    db,
		table: table,
	}
}

// EnsureSchema creates the sessions table and its indexes.
func (s *EntitySessionStore) EnsureSchema(ctx context.Context) error {
	if err := refuseCaseKeptTable(ctx, s.db, s.dialect.of(s.db), s.table); err != nil {
		return err
	}
	tsType, boolType, boolFalse := "DATETIME", "INTEGER", "0"
	if s.dialect.of(s.db) == migrate.DialectPostgres {
		tsType, boolType, boolFalse = "TIMESTAMP", "BOOLEAN", "FALSE"
	}
	stmt := fmt.Sprintf(
		"CREATE TABLE IF NOT EXISTS %s (id TEXT NOT NULL, token TEXT NOT NULL, user_id TEXT NOT NULL, created_at %s NOT NULL, expires_at %s NOT NULL, two_factor_verified %s NOT NULL DEFAULT %s, pending_two_factor %s NOT NULL DEFAULT %s)",
		query.QuoteFolded(s.table), tsType, tsType, boolType, boolFalse, boolType, boolFalse,
	)
	_, err := s.db.ExecContext(ctx, stmt)
	if err != nil {
		return err
	}
	if err := ensurePostgresBoolColumns(ctx, s.db, s.dialect.of(s.db), s.table, "two_factor_verified", "pending_two_factor"); err != nil {
		return err
	}
	sqlite := s.dialect.of(s.db) == migrate.DialectSQLite
	on := func(suffix string) string {
		return query.IndexOn(sqlite, indexName("idx_", s.table, suffix), query.QuoteFolded(s.table))
	}
	for _, idx := range []string{
		fmt.Sprintf("CREATE INDEX IF NOT EXISTS %s (expires_at)", on("_expires")),
		fmt.Sprintf("CREATE UNIQUE INDEX IF NOT EXISTS %s (token)", on("_token")),
		fmt.Sprintf("CREATE INDEX IF NOT EXISTS %s (user_id)", on("_user")),
	} {
		if _, err := s.db.ExecContext(ctx, idx); err != nil {
			return err
		}
	}
	return nil
}

func (s *EntitySessionStore) qTable(stmt string) string {
	return fmt.Sprintf(stmt, query.QuoteFolded(s.table))
}

// Create generates a random token, inserts a session row, and returns it.
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

	sessionID, err := generateUserID()
	if err != nil {
		return nil, err
	}
	q := s.qTable("INSERT INTO %s (id, token, user_id, created_at, expires_at) VALUES ($1, $2, $3, $4, $5)")
	_, err = s.db.ExecContext(ctx, q, sessionID, sha256hex(tok), userID, now, expiresAt)
	if err != nil {
		return nil, err
	}
	s.sweepExpiredOnCreate(ctx, now)
	return &Session{
		Token:     tok,
		UserID:    userID,
		CreatedAt: now,
		ExpiresAt: expiresAt,
	}, nil
}

func (s *EntitySessionStore) sweepExpiredOnCreate(ctx context.Context, now time.Time) {
	s.sweepMu.Lock()
	if now.Sub(s.lastSweep) < sessionSweepInterval {
		s.sweepMu.Unlock()
		return
	}
	s.lastSweep = now
	s.sweepMu.Unlock()
	if _, err := s.db.ExecContext(ctx, s.qTable("DELETE FROM %s WHERE expires_at < $1"), now); err != nil {
		slog.Warn("auth: expired session sweep failed", "err", err)
		s.sweepMu.Lock()
		if s.lastSweep.Equal(now) {
			s.lastSweep = now.Add(sessionSweepRetry - sessionSweepInterval)
		}
		s.sweepMu.Unlock()
	}
}

const sessionSweepRetry = time.Minute

// Get returns the session for the presented token, looked up by its digest.
func (s *EntitySessionStore) Get(ctx context.Context, token string) (*Session, error) {
	q := s.qTable("SELECT user_id, created_at, expires_at, two_factor_verified, pending_two_factor FROM %s WHERE token = $1")
	var uid string
	var createdAtRaw, expiresAtRaw any
	var twoFA, pending bool
	err := s.db.QueryRowContext(ctx, q, sha256hex(token)).Scan(&uid, &createdAtRaw, &expiresAtRaw, &twoFA, &pending)
	if err == sql.ErrNoRows {
		return nil, ErrSessionNotFound
	}
	if err != nil {
		return nil, err
	}

	sess := &Session{
		Token:             token,
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

// MarkTwoFactorVerified sets two_factor_verified and clears pending_two_factor. Implements SessionTwoFAMarker.
func (s *EntitySessionStore) MarkTwoFactorVerified(ctx context.Context, token string) error {
	_, err := s.db.ExecContext(ctx,
		s.qTable("UPDATE %s SET two_factor_verified = TRUE, pending_two_factor = FALSE WHERE token = $1"),
		sha256hex(token))
	return err
}

// MarkPendingTwoFactor sets pending_two_factor. Implements SessionPendingMarker.
func (s *EntitySessionStore) MarkPendingTwoFactor(ctx context.Context, token string) error {
	_, err := s.db.ExecContext(ctx,
		s.qTable("UPDATE %s SET pending_two_factor = TRUE WHERE token = $1"),
		sha256hex(token))
	return err
}

// Delete removes a session by token. Idempotent.
func (s *EntitySessionStore) Delete(ctx context.Context, token string) error {
	_, err := s.db.ExecContext(ctx, s.qTable("DELETE FROM %s WHERE token = $1"), sha256hex(token))
	return err
}

// DeleteByUser deletes every session of userID. Implements SessionUserPurger.
func (s *EntitySessionStore) DeleteByUser(ctx context.Context, userID string) (int, error) {
	result, err := s.db.ExecContext(ctx, s.qTable("DELETE FROM %s WHERE user_id = $1"), userID)
	if err != nil {
		return 0, err
	}
	n, _ := result.RowsAffected()
	return int(n), nil
}

// DeletePendingByUser deletes userID's pending sessions. Implements SessionPendingPurger.
func (s *EntitySessionStore) DeletePendingByUser(ctx context.Context, userID string) (int, error) {
	result, err := s.db.ExecContext(ctx, s.qTable("DELETE FROM %s WHERE user_id = $1 AND pending_two_factor = TRUE"), userID)
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

func generateUserID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("auth: generate user id entropy: %w", err)
	}
	return fmt.Sprintf("%x", b), nil
}

func parseRoles(s string) []string {
	if s == "" {
		return []string{"user"}
	}
	var roles []string
	if err := json.Unmarshal([]byte(s), &roles); err == nil {
		return roles
	}
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

func formatRoles(roles []string) string {
	if len(roles) == 0 {
		roles = []string{"user"}
	}
	b, _ := json.Marshal(roles)
	return string(b)
}

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
	t, _ := time.Parse(time.RFC3339, s)
	return t
}

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
