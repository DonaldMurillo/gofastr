package admin

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/framework/entityui"
	"github.com/DonaldMurillo/gofastr/framework/tenant"
)

// The SQL SavedViewStore behind Config.SavedViews: one table the admin
// creates the way the queue battery creates its own (CREATE TABLE IF NOT
// EXISTS per dialect, DATETIME on SQLite, TIMESTAMPTZ on Postgres). The
// store is identity-blind: it reads the owner and the tenant from ctx
// only, never from an argument or the view, and every query carries
// owner and tenant in its WHERE clause.

// savedViewsTableRe is the shape a Config.SavedViewsTable name must
// have: a lowercase identifier, so it can never need quoting tricks or
// alias a differently-cased sibling on a case-insensitive filesystem
// or collation.
var savedViewsTableRe = regexp.MustCompile(`^[a-z_][a-z0-9_]*$`)

// errSavedViewNoUser answers a ctx with no user: saved views are
// per-user state, so there is nothing to act on.
var errSavedViewNoUser = errors.New("admin: saved views need a signed-in user")

// sqlSavedViews keeps saved views in the admin's database.
type sqlSavedViews struct {
	db    *sql.DB
	table string // validated by savedViewsTableRe, quoted into SQL
	// pg is the dialect, read once here: query.IsPostgres runs a query,
	// which inside create's transaction would wait on a one-connection
	// pool forever.
	pg bool
}

var _ entityui.SavedViewStore = (*sqlSavedViews)(nil)

// newSavedViews creates the saved views table if it is missing and
// returns the store over it.
func newSavedViews(ctx context.Context, db *sql.DB, table string) (*sqlSavedViews, error) {
	ts := "DATETIME"
	pg := query.IsPostgres(db)
	if pg {
		ts = "TIMESTAMPTZ"
	}
	s := &sqlSavedViews{db: db, table: table, pg: pg}
	stmts := []string{
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
			id         TEXT PRIMARY KEY,
			owner      TEXT NOT NULL,
			tenant     TEXT NOT NULL DEFAULT '',
			entity     TEXT NOT NULL,
			name       TEXT NOT NULL,
			filter     TEXT NOT NULL DEFAULT '',
			columns    TEXT NOT NULL DEFAULT '',
			created_at %s NOT NULL
		)`, query.QuoteIdent(table), ts),
		fmt.Sprintf(`CREATE UNIQUE INDEX IF NOT EXISTS %s ON %s (owner, tenant, entity, name)`,
			query.QuoteIdent(table+"_owner"), query.QuoteIdent(table)),
	}
	for _, q := range stmts {
		if _, err := db.ExecContext(ctx, q); err != nil {
			return nil, fmt.Errorf("ensure table: %w", err)
		}
	}
	return s, nil
}

// savedViewCaller reads the caller the store acts for: the owner is
// handler.GetUser's GetID and the tenant comes from ctx, both read
// fresh on every call.
func savedViewCaller(ctx context.Context) (owner, ten string, err error) {
	u, ok := handler.GetUser(ctx)
	if !ok || u == nil {
		return "", "", errSavedViewNoUser
	}
	id, ok := u.(interface{ GetID() string })
	if !ok || id.GetID() == "" {
		return "", "", errSavedViewNoUser
	}
	return id.GetID(), tenant.GetTenantID(ctx), nil
}

// List returns the caller's views of entity, by name.
func (s *sqlSavedViews) List(ctx context.Context, entity string) ([]entityui.SavedView, error) {
	owner, ten, err := savedViewCaller(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(
		`SELECT id, name, filter, columns FROM %s WHERE owner = $1 AND tenant = $2 AND entity = $3 ORDER BY name`,
		query.QuoteIdent(s.table)), owner, ten, entity)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []entityui.SavedView
	for rows.Next() {
		v, err := scanView(rows, entity)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// Get returns one of the caller's views of entity. Another owner's or
// tenant's view is ErrSavedViewNotFound, the same answer as an id that
// does not exist.
func (s *sqlSavedViews) Get(ctx context.Context, entity, id string) (entityui.SavedView, error) {
	owner, ten, err := savedViewCaller(ctx)
	if err != nil {
		return entityui.SavedView{}, err
	}
	v, err := scanView(s.db.QueryRowContext(ctx, fmt.Sprintf(
		`SELECT id, name, filter, columns FROM %s WHERE owner = $1 AND tenant = $2 AND entity = $3 AND id = $4`,
		query.QuoteIdent(s.table)), owner, ten, entity, id), entity)
	if errors.Is(err, sql.ErrNoRows) {
		return entityui.SavedView{}, entityui.ErrSavedViewNotFound
	}
	if err != nil {
		return entityui.SavedView{}, err
	}
	return v, nil
}

// Create stores v for the caller and returns it with its ID. The cap
// check and the insert run in one transaction, under a per-caller lock
// on Postgres, so two concurrent creates cannot both slip past the cap;
// a name the caller already
// uses for the entity is refused by the unique index and answered
// ErrSavedViewExists.
func (s *sqlSavedViews) Create(ctx context.Context, v entityui.SavedView) (entityui.SavedView, error) {
	owner, ten, err := savedViewCaller(ctx)
	if err != nil {
		return entityui.SavedView{}, err
	}
	v.Name = strings.TrimSpace(v.Name)
	if v.Name == "" {
		return entityui.SavedView{}, entityui.ErrSavedViewBlank
	}
	if len(v.Name) > entityui.SavedViewNameMax || len(v.Filter) > entityui.SavedViewFilterMax || len(v.Columns) > entityui.SavedViewColumnsMax {
		return entityui.SavedView{}, entityui.ErrSavedViewTooLong
	}
	cols := ""
	if len(v.Columns) > 0 {
		b, err := json.Marshal(v.Columns)
		if err != nil {
			return entityui.SavedView{}, fmt.Errorf("admin: saved view columns: %w", err)
		}
		cols = string(b)
	}
	v.ID, err = mintSavedViewID()
	if err != nil {
		return entityui.SavedView{}, err
	}
	if err := s.create(ctx, v, cols, owner, ten); err != nil {
		// The unique index refused the name: the caller already holds
		// it. Anything else (the cap, a dead database) is the error
		// itself; the name re-read keeps the answer driver-agnostic.
		if _, held := s.nameHeld(ctx, v.Entity, v.Name, owner, ten); held {
			return entityui.SavedView{}, entityui.ErrSavedViewExists
		}
		return entityui.SavedView{}, err
	}
	return v, nil
}

// create writes the row inside one transaction that first counts the
// caller's views of the entity. Postgres runs READ COMMITTED, where each
// of two concurrent creates counts the other's insert out, so there the
// transaction first takes an advisory lock on the caller's views of the
// entity. SQLite allows one writer at a time: of two creates that both
// counted, only the first commits.
func (s *sqlSavedViews) create(ctx context.Context, v entityui.SavedView, cols, owner, ten string) (err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	if s.pg {
		if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`,
			savedViewLockKey(s.table, owner, ten, v.Entity)); err != nil {
			return err
		}
	}
	var n int
	if err = tx.QueryRowContext(ctx, fmt.Sprintf(
		`SELECT COUNT(*) FROM %s WHERE owner = $1 AND tenant = $2 AND entity = $3`,
		query.QuoteIdent(s.table)), owner, ten, v.Entity).Scan(&n); err != nil {
		return err
	}
	if n >= entityui.SavedViewCap {
		return entityui.ErrSavedViewCap
	}
	if _, err = tx.ExecContext(ctx, fmt.Sprintf(
		`INSERT INTO %s (id, owner, tenant, entity, name, filter, columns, created_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		query.QuoteIdent(s.table)),
		v.ID, owner, ten, v.Entity, v.Name, v.Filter, cols, time.Now().UTC()); err != nil {
		return err
	}
	return tx.Commit()
}

// savedViewLockKey names one caller's views of one entity for the
// advisory lock. Each part is length-prefixed, so no owner, tenant or
// entity spelling can alias another caller's key.
func savedViewLockKey(parts ...string) string {
	var b strings.Builder
	for _, p := range parts {
		b.WriteString(strconv.Itoa(len(p)))
		b.WriteByte(':')
		b.WriteString(p)
	}
	return b.String()
}

// nameHeld reports whether the caller already holds name for entity,
// the re-read behind ErrSavedViewExists.
func (s *sqlSavedViews) nameHeld(ctx context.Context, entity, name, owner, ten string) (entityui.SavedView, bool) {
	v, err := scanView(s.db.QueryRowContext(ctx, fmt.Sprintf(
		`SELECT id, name, filter, columns FROM %s WHERE owner = $1 AND tenant = $2 AND entity = $3 AND name = $4`,
		query.QuoteIdent(s.table)), owner, ten, entity, name), entity)
	if err != nil {
		return entityui.SavedView{}, false
	}
	return v, true
}

// Delete removes one of the caller's views of entity. Another owner's
// or tenant's view is ErrSavedViewNotFound.
func (s *sqlSavedViews) Delete(ctx context.Context, entity, id string) error {
	owner, ten, err := savedViewCaller(ctx)
	if err != nil {
		return err
	}
	res, err := s.db.ExecContext(ctx, fmt.Sprintf(
		`DELETE FROM %s WHERE owner = $1 AND tenant = $2 AND entity = $3 AND id = $4`,
		query.QuoteIdent(s.table)), owner, ten, entity, id)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return entityui.ErrSavedViewNotFound
	} else if err != nil {
		return err
	}
	return nil
}

// scanView reads one id, name, filter, columns row into a SavedView.
func scanView(row interface{ Scan(...any) error }, entity string) (entityui.SavedView, error) {
	var v entityui.SavedView
	var cols sql.NullString
	if err := row.Scan(&v.ID, &v.Name, &v.Filter, &cols); err != nil {
		return v, err
	}
	v.Entity = entity
	if cols.Valid && cols.String != "" {
		if err := json.Unmarshal([]byte(cols.String), &v.Columns); err != nil {
			return v, fmt.Errorf("admin: saved view columns: %w", err)
		}
	}
	return v, nil
}

// mintSavedViewID mints a row id from crypto/rand: never the clock, so
// two stores never race on the same id.
func mintSavedViewID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("admin: mint saved view id: %w", err)
	}
	return "sav_" + hex.EncodeToString(b[:]), nil
}
