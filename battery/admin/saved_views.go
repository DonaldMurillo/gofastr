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

	"github.com/DonaldMurillo/gofastr/core/datexport"
	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/framework/entityui"
	"github.com/DonaldMurillo/gofastr/framework/tenant"
)

var savedViewsTableRe = regexp.MustCompile(`^[a-z_][a-z0-9_]*$`)

var errSavedViewNoUser = errors.New("admin: saved views need a signed-in user")

type sqlSavedViews struct {
	db    *sql.DB
	table string
	pg    bool
}

var _ entityui.SavedViewStore = (*sqlSavedViews)(nil)

func newSavedViews(ctx context.Context, db *sql.DB, table string) (*sqlSavedViews, error) {
	ts := "DATETIME"
	pg := query.IsPostgres(db)
	if pg {
		ts = "TIMESTAMPTZ"
	}
	s := &sqlSavedViews{db: db, table: table, pg: pg}
	stmts := []string{
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
			id         TEXT PRIMARY KEY NOT NULL,
			owner      TEXT NOT NULL,
			tenant     TEXT NOT NULL DEFAULT '',
			entity     TEXT NOT NULL,
			name       TEXT NOT NULL,
			filter     TEXT NOT NULL DEFAULT '',
			columns    TEXT NOT NULL DEFAULT '',
			created_at %s NOT NULL
		)`, query.QuoteFolded(table), ts),
		fmt.Sprintf(`CREATE UNIQUE INDEX IF NOT EXISTS %s (owner, tenant, entity, name)`,
			query.IndexOn(!pg, query.QuoteIndexName(table+"_owner"), query.QuoteFolded(table))),
	}
	for _, q := range stmts {
		if _, err := db.ExecContext(ctx, q); err != nil {
			return nil, fmt.Errorf("ensure table: %w", err)
		}
	}
	registerSavedViews(table)
	return s, nil
}

func registerSavedViews(table string) {
	datexport.Register(datexport.DataExporter{
		Name: datexport.ScopedName("admin_saved_views", table), Source: "admin", Table: table, PrimaryKey: "id",
		Columns: []string{"id", "owner", "tenant", "entity", "name", "filter", "columns", "created_at"},
	})
	datexport.RegisterEraser(datexport.DataEraser{
		Name: datexport.ScopedName("admin_saved_views", table), Source: "admin", Table: table,
		Column: "owner", Mode: datexport.EraseDelete,
	})
}

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
		query.QuoteFolded(s.table)), owner, ten, entity)
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

// Get returns one of the caller's views of entity, or ErrSavedViewNotFound.
func (s *sqlSavedViews) Get(ctx context.Context, entity, id string) (entityui.SavedView, error) {
	owner, ten, err := savedViewCaller(ctx)
	if err != nil {
		return entityui.SavedView{}, err
	}
	v, err := scanView(s.db.QueryRowContext(ctx, fmt.Sprintf(
		`SELECT id, name, filter, columns FROM %s WHERE owner = $1 AND tenant = $2 AND entity = $3 AND id = $4`,
		query.QuoteFolded(s.table)), owner, ten, entity, id), entity)
	if errors.Is(err, sql.ErrNoRows) {
		return entityui.SavedView{}, entityui.ErrSavedViewNotFound
	}
	if err != nil {
		return entityui.SavedView{}, err
	}
	return v, nil
}

// Create stores v for the caller and returns it with its ID; a name already
// held is ErrSavedViewExists.
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
		if _, held := s.nameHeld(ctx, v.Entity, v.Name, owner, ten); held {
			return entityui.SavedView{}, entityui.ErrSavedViewExists
		}
		return entityui.SavedView{}, err
	}
	return v, nil
}

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
		query.QuoteFolded(s.table)), owner, ten, v.Entity).Scan(&n); err != nil {
		return err
	}
	if n >= entityui.SavedViewCap {
		return entityui.ErrSavedViewCap
	}
	if _, err = tx.ExecContext(ctx, fmt.Sprintf(
		`INSERT INTO %s (id, owner, tenant, entity, name, filter, columns, created_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		query.QuoteFolded(s.table)),
		v.ID, owner, ten, v.Entity, v.Name, v.Filter, cols, time.Now().UTC()); err != nil {
		return err
	}
	return tx.Commit()
}

func savedViewLockKey(parts ...string) string {
	var b strings.Builder
	for _, p := range parts {
		b.WriteString(strconv.Itoa(len(p)))
		b.WriteByte(':')
		b.WriteString(p)
	}
	return b.String()
}

func (s *sqlSavedViews) nameHeld(ctx context.Context, entity, name, owner, ten string) (entityui.SavedView, bool) {
	v, err := scanView(s.db.QueryRowContext(ctx, fmt.Sprintf(
		`SELECT id, name, filter, columns FROM %s WHERE owner = $1 AND tenant = $2 AND entity = $3 AND name = $4`,
		query.QuoteFolded(s.table)), owner, ten, entity, name), entity)
	if err != nil {
		return entityui.SavedView{}, false
	}
	return v, true
}

// Delete removes one of the caller's views of entity, or answers
// ErrSavedViewNotFound.
func (s *sqlSavedViews) Delete(ctx context.Context, entity, id string) error {
	owner, ten, err := savedViewCaller(ctx)
	if err != nil {
		return err
	}
	res, err := s.db.ExecContext(ctx, fmt.Sprintf(
		`DELETE FROM %s WHERE owner = $1 AND tenant = $2 AND entity = $3 AND id = $4`,
		query.QuoteFolded(s.table)), owner, ten, entity, id)
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

func mintSavedViewID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("admin: mint saved view id: %w", err)
	}
	return "sav_" + hex.EncodeToString(b[:]), nil
}
