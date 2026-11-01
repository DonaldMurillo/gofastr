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
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
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
	dialect  storeDialect
	// hidesDeleted is set by EnsureSchema when the table carries the
	// soft-delete column: the users entity has Scope.SoftDelete, and CRUD
	// delete only stamps it. Lookups then skip a trashed row.
	hidesDeleted atomic.Bool
}

// softDeleteColumn is the column CRUD's soft delete stamps. Its name is
// fixed by framework/crud, not configurable per entity.
const softDeleteColumn = "deleted_at"

// liveRows is the predicate that keeps a soft-deleted account out of a
// lookup, or "" when the table has no soft-delete column.
func (s *EntityUserStore) liveRows() string {
	if !s.hidesDeleted.Load() {
		return ""
	}
	return query.QuoteFolded(softDeleteColumn) + " IS NULL"
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
	// to "password_set". The column is optional, if missing from the
	// physical table, HasPassword returns ErrPasswordSetNotTracked and
	// AccountsPlugin falls back to the conservative links-only rule.
	PasswordSet string // default: "password_set"
	// EmailVerified records whether the account's owner proved control of
	// the address: a verification link, a magic link, a completed reset,
	// or an IdP login asserting email_verified for this exact address.
	// Defaults to "email_verified". EnsureSchema adds it to an existing
	// table as FALSE for every row, because nothing about a pre-existing
	// row says its address was proven.
	EmailVerified string // default: "email_verified"
	// Name is the optional display name (NameStore). Defaults to
	// "name". EnsureSchema adds it to an existing table as '' for every
	// row.
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
// The table must have the columns described by fieldMap.
// Panics if the table name or any field mapping contains unsafe characters.
func NewEntityUserStore(db *sql.DB, table string, fieldMap ...UserFieldMap) *EntityUserStore {
	fm := DefaultUserFieldMap()
	if len(fieldMap) > 0 {
		fm = fieldMap[0]
	}
	if fm.EmailVerified == "" {
		// A map written before the field existed keeps working.
		fm.EmailVerified = "email_verified"
	}
	if fm.Name == "" {
		fm.Name = "name"
	}
	// Validate all identifiers at construction time, fail fast.
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

// EnsureSchema creates the user table if it does not already exist. The auth
// battery owns its schema, so hosts never hand-roll the DDL, AuthManager.Init
// calls this. Idempotent. Boolean columns use the native PostgreSQL BOOLEAN
// type so CreateUser's Go bool bindings work on a fresh Postgres database.
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
		"CREATE TABLE IF NOT EXISTS %s (%s TEXT PRIMARY KEY, %s TEXT UNIQUE NOT NULL, %s TEXT NOT NULL DEFAULT '', %s TEXT NOT NULL DEFAULT '[]', %s %s NOT NULL DEFAULT %s)",
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
	// AutoMigrate ran before the plugins, so a soft-deleting users entity
	// has added its column by now. A trashed account resolved anyway: it
	// signed in, and its sessions, tokens and JWTs kept working.
	hides, err := tableHasColumn(ctx, s.db, s.dialect.of(s.db), s.table, softDeleteColumn)
	if err != nil {
		return err
	}
	s.hidesDeleted.Store(hides)
	// The OAuth link table backs OAuthLinker / AccountLister / AccountUnlinker
	// for this store. Creating it here (rather than in OAuth2Plugin.Init) means
	// the schema is right even for hosts that never turn OAuth on, and that a
	// host that adds OAuth later doesn't need a separate migration step.
	return s.EnsureOAuthLinksSchema(ctx)
}

// refuseCaseKeptTable stops EnsureSchema when Postgres holds what an older
// auth battery made for table. Those stores quoted the configured name
// whole, so "Members" got its own case-kept table beside the folded
// members that AutoMigrate creates, "q.TwoFA" became one table literally
// named q.TwoFA in the current schema, and a mapped column such as
// "PasswordHash" kept its case even on a lowercase table. The stores now
// name the folded objects (table twofa in schema q, column passwordhash);
// booting on would hide every account, session, OAuth link, enrolment or
// password hash the old objects hold, so the operator moves them first.
// cols are the store's configured column names.
func refuseCaseKeptTable(ctx context.Context, db *sql.DB, d migrate.Dialect, table string, cols ...string) error {
	if d != migrate.DialectPostgres {
		return nil
	}
	schema, name := foldedTableRef(table)
	folded := query.QuoteFolded(table)
	// The old table: the configured string whole in the current schema,
	// or a case-kept last part inside the named schema.
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
	// Columns the old store created with their case kept: on the old table
	// when there is one, else on the table the store now uses.
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
	// Every name in the advice is quoted: a folded target such as order is
	// a reserved word.
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

// foldedTableRef splits a configured table the way QuoteFolded names it:
// "q.Members" is table members in schema q, and an undotted name lives in
// the current schema (schema "").
func foldedTableRef(table string) (schema, name string) {
	if i := strings.LastIndexByte(table, '.'); i >= 0 {
		return strings.ToLower(table[:i]), strings.ToLower(table[i+1:])
	}
	return "", strings.ToLower(table)
}

// quoteQualified quotes name, behind its quoted schema unless schema is ""
// (the current schema).
func quoteQualified(schema, name string) string {
	if schema == "" {
		//gofastr:allow(quoteident) name was found in the catalog with its case kept
		return query.QuoteIdent(name)
	}
	//gofastr:allow(quoteident) schema and name were found in the catalog with their case kept
	return query.QuoteIdent(schema) + "." + query.QuoteIdent(name)
}

// indexName quotes prefix+table+suffix as an index name, fitted to 63
// bytes (query.FitIdent) so two long table names do not cut to one
// index. Postgres refuses a schema-qualified index name (an index lands
// in its table's schema), so a dotted "q.Members" contributes only its
// folded last part.
func indexName(prefix, table, suffix string) string {
	_, name := foldedTableRef(table)
	return query.QuoteFolded(query.FitIdent(prefix + name + suffix))
}

// pgFindTable reports the schema holding a table named exactly name: the
// current schema when schema is "" (reported back as ""), else schema as
// written or folded.
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

// tableHasColumn reports whether table has column, read from the catalog
// once at boot.
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

// ensurePostgresBoolColumns upgrades legacy INTEGER boolean columns owned by
// the auth battery. The conversion is lossless for the old 0/1 representation
// and keeps existing deployments compatible with native Go bool bindings.
func ensurePostgresBoolColumns(ctx context.Context, db *sql.DB, d migrate.Dialect, table string, columns ...string) error {
	if d != migrate.DialectPostgres {
		return nil
	}
	query.MustIdent(table)
	// A dotted "q.Members" is table members in schema q, the table the
	// store writes; matching the whole string never found it.
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

// q builds a query using validated identifiers. All table/field names were
// validated at construction time in NewEntityUserStore.
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

// qByID builds a query selecting by ID.
func (s *EntityUserStore) qByID(stmt string) string {
	return fmt.Sprintf(stmt,
		query.QuoteFolded(s.fieldMap.ID),
		query.QuoteFolded(s.fieldMap.Email),
		query.QuoteFolded(s.fieldMap.Roles),
		query.QuoteFolded(s.table),
		query.QuoteFolded(s.fieldMap.ID),
	)
}

// FindByEmail looks up a user by email. Returns ErrUserNotFound when no
// row matches; any other error is returned verbatim.
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

// FindByID looks up a user by their unique ID. Returns ErrUserNotFound
// when no row matches; any other error is returned verbatim.
func (s *EntityUserStore) FindByID(ctx context.Context, id string) (User, error) {
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

// ListUsers enumerates accounts ordered by the email column, returning
// one page plus the total row count. Implements UserLister.
//
// Only id/email/roles are selected, never password_hash, so a paged
// listing never surfaces password material. Roles are parsed with the
// store's existing parseRoles helper, matching FindByEmail/FindByID.
// The caller (AuthManager.ListUsers) clamps opts, so Limit>=1 and
// Offset>=0 hold here.
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
// placeholder hash (made by DefaultHasher, so a login against it costs
// what one against an unknown address does) and records
// password_set=false. Used by the OAuth /
// magic-link auto-create flows. Implements OAuthUserCreator.
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
// Implements PasswordSetter (used by PasswordResetPlugin).
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
// Implements PasswordSwapper. False means no row matched: the hash moved,
// or the user is gone.
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

// HasPassword reports whether the user has a real password set vs the
// placeholder hash. Implements PasswordChecker.
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

// ensureEmailVerifiedColumn adds the email_verified column to a table that
// predates it. Every existing row reads FALSE: the battery never recorded
// whether an address was proven, so an existing account is treated as
// unproven until its owner proves the mailbox (see claimAccount). Postgres
// uses ADD COLUMN IF NOT EXISTS; SQLite checks PRAGMA table_info first.
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

// MarkEmailVerified records that the user proved control of their email
// address. Implements EmailVerifier.
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

// IsEmailVerified reports whether the user's address is proven. Implements
// EmailVerifiedChecker.
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

// ClearPassword replaces the user's hash with a new unusable one and
// records password_set=false, so no password signs in until the owner
// sets one, and no JWT issued before the clear validates. Implements
// PasswordClearer.
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
// fact (e.g. `cfg := auth.UserEntityConfig(); enabled := true;
// cfg.Exposure.CRUD = &enabled`).
//
//	app.Entity("users", auth.UserEntityConfig())
func UserEntityConfig() entity.EntityConfig {
	crudOff := false
	return entity.EntityConfig{Fields: UserEntityFields(), Exposure: &entity.ExposureConfig{CRUD: &crudOff, MCP: false}}
}

// SessionEntityConfig returns an EntityConfig pre-configured for the auth
// sessions table: standard fields + CRUD=false + MCP=false. Auto-private
// by default. See UserEntityConfig for the rationale.
//
//	app.Entity("sessions", auth.SessionEntityConfig())
func SessionEntityConfig() entity.EntityConfig {
	crudOff := false
	return entity.EntityConfig{Fields: SessionEntityFields(), Exposure: &entity.ExposureConfig{CRUD: &crudOff, MCP: false}}
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
		{Name: "email_verified", Type: schema.Bool, Default: "false"},
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
// The returned slice is independent, repeated calls don't mutate the
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
	db      *sql.DB
	table   string
	dialect storeDialect

	sweepMu   sync.Mutex
	lastSweep time.Time
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

// EnsureSchema creates the session table if it does not already exist. Called
// by AuthManager.Init so hosts never hand-roll the DDL. Idempotent. The
// datetime and boolean column types are chosen per dialect (SQLite vs
// PostgreSQL) so the same battery boots on either.
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
	// The indexes are created whoever made the table, and they are the
	// only ones: the CREATE TABLE above and SessionEntityFields declare no
	// UNIQUE of their own, which left two unique indexes on token. The documented
	// wiring declares app.Entity("sessions", SessionEntityConfig()), so
	// AutoMigrate creates the table first and the CREATE TABLE above does
	// nothing: token then had no index, and every cookie lookup (a random
	// cookie included) scanned the whole table, as did DeleteByUser.
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

// qTable wraps a statement template with the validated table name.
func (s *EntitySessionStore) qTable(stmt string) string {
	return fmt.Sprintf(stmt, query.QuoteFolded(s.table))
}

// Create generates a random token, inserts a session row, and returns it.
// Rejects ttl <= 0, the caller must supply a positive lifetime.
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
	sessionID, err := generateUserID()
	if err != nil {
		return nil, err
	}
	// The token column stores sha256hex(tok), never the cookie value
	// (the APIToken grammar): the session cookie is a bearer
	// credential, so a read-only dump of the sessions table must not be
	// a pile of live cookies. Every lookup (Get / Delete / the 2FA
	// markers) keys on the digest of the presented token.
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

// sweepExpiredOnCreate reaps abandoned rows at most once per interval while
// allowing session creation to succeed if cleanup itself is unavailable.
func (s *EntitySessionStore) sweepExpiredOnCreate(ctx context.Context, now time.Time) {
	s.sweepMu.Lock()
	defer s.sweepMu.Unlock()
	if now.Sub(s.lastSweep) < sessionSweepInterval {
		return
	}
	if _, err := s.db.ExecContext(ctx, s.qTable("DELETE FROM %s WHERE expires_at < $1"), now); err != nil {
		slog.Warn("auth: expired session sweep failed", "err", err)
		return
	}
	s.lastSweep = now
}

// Get returns the session for the given token. The presented token is
// hashed for the lookup; Session.Token carries the PRESENTED value back
// so callers can treat it as the cookie value.
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

// MarkTwoFactorVerified sets two_factor_verified=TRUE and clears
// pending_two_factor. Implements SessionTwoFAMarker.
func (s *EntitySessionStore) MarkTwoFactorVerified(ctx context.Context, token string) error {
	_, err := s.db.ExecContext(ctx,
		s.qTable("UPDATE %s SET two_factor_verified = TRUE, pending_two_factor = FALSE WHERE token = $1"),
		sha256hex(token))
	return err
}

// MarkPendingTwoFactor sets pending_two_factor. Implements
// SessionPendingMarker.
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

// DeletePendingByUser removes userID's sessions that are still waiting on
// a 2FA challenge. Implements SessionPendingPurger.
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

// ─── Helpers ────────────────────────────────────────────────────────────

// generateUserID creates a random 32-hex-char user ID from crypto/rand.
// Entropy failure is an error, never a clock fallback: a timestamp id is
// enumerable across accounts, and a rand failure means the host's
// entropy source is broken — callers refuse the write.
func generateUserID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("auth: generate user id entropy: %w", err)
	}
	return fmt.Sprintf("%x", b), nil
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
