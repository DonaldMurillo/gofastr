package entityui

import (
	"context"
	"database/sql"
	"fmt"
	"github.com/DonaldMurillo/gofastr/core/upload"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
	"github.com/DonaldMurillo/gofastr/framework/owner"

	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
)

// The list tests stand up a real SQLite database, real entities and real
// CRUD handlers, the shape framework/crud's own tests use: the guards
// under test are about what reaches SQL, so the data source cannot be a
// stub. newTestUI is shared with the record tests; keep it general.

// testUser is the minimal user shape handler.SetUser carries.
type testUser struct{ id string }

func (u *testUser) GetID() string { return u.id }

// testRegistry is the entity.Registry the Host sees.
type testRegistry struct{ ents map[string]*entity.Entity }

func (r *testRegistry) All() map[string]*entity.Entity { return r.ents }

func (r *testRegistry) AllSorted() []*entity.Entity {
	names := make([]string, 0, len(r.ents))
	for n := range r.ents {
		names = append(names, n)
	}
	slices.Sort(names)
	out := make([]*entity.Entity, 0, len(names))
	for _, n := range names {
		out = append(out, r.ents[n])
	}
	return out
}

func (r *testRegistry) Get(name string) (*entity.Entity, error) {
	e, ok := r.ents[name]
	if !ok {
		return nil, fmt.Errorf("entity: %q is not registered", name)
	}
	return e, nil
}

// testHost is entityui.Host over the test registry and database.
type testHost struct {
	reg   *testRegistry
	db    *sql.DB
	cruds map[string]*crud.CrudHandler
	apis  map[string]string
	tr    *i18n.Translator
	audit AuditReader
	store upload.Storage
}

func (h *testHost) Registry() entity.Registry { return h.reg }

func (h *testHost) Crud(e *entity.Entity) (*crud.CrudHandler, error) {
	if ch, ok := h.cruds[e.GetName()]; ok {
		return ch, nil
	}
	ch := crud.NewCrudHandler(e, h.db).WithJSONCase(crud.CaseSnake)
	ch.Storage = h.store
	h.cruds[e.GetName()] = ch
	return ch, nil
}

func (h *testHost) APIPath(e *entity.Entity) (string, bool) {
	p, ok := h.apis[e.GetName()]
	return p, ok && p != ""
}

func (h *testHost) Translator() *i18n.Translator { return h.tr }

func (h *testHost) Audit() AuditReader { return h.audit }

// testUI is one test's app: its entities, database and entityui.UI.
type testUI struct {
	tb   testing.TB
	ui   *UI
	host *testHost
	db   *sql.DB
}

type testUIOption func(*testUI)

// withAPI mounts REST write routes for the named entities, the shape
// App.APIPath reports: an entity with none renders read-only.
func withAPI(paths map[string]string) testUIOption {
	return func(x *testUI) {
		for k, v := range paths {
			x.host.apis[k] = v
		}
	}
}

// withStorage gives every CRUD handler a file store, as
// framework.WithFileStorage does.
func withStorage(st upload.Storage) testUIOption {
	return func(x *testUI) { x.host.store = st }
}

// withTranslator installs a catalog-backed translator.
func withTranslator(tr *i18n.Translator) testUIOption {
	return func(x *testUI) { x.host.tr = tr }
}

// withAudit installs an audit reader.
func withAudit(a AuditReader) testUIOption {
	return func(x *testUI) { x.host.audit = a }
}

// newTestUI registers the entities over one in-memory SQLite database,
// migrates every table, seeds rows inserted as raw column maps, and
// builds the UI with no extensions.
func newTestUI(t testing.TB, ents map[string]entity.EntityConfig, rows map[string][]map[string]any, opts ...testUIOption) *testUI {
	t.Helper()
	return newTestUIExt(t, ents, rows, Extensions{}, opts...)
}

// newTestUIExt is newTestUI with the Extensions New checks.
func newTestUIExt(t testing.TB, ents map[string]entity.EntityConfig, rows map[string][]map[string]any, ext Extensions, opts ...testUIOption) *testUI {
	t.Helper()
	x := newTestHost(t, ents, rows, opts...)
	ui, err := New(x.host, ext)
	if err != nil {
		t.Fatalf("entityui.New: %v", err)
	}
	x.ui = ui
	return x
}

// newTestHost builds the host half: database, entities, rows, options.
func newTestHost(t testing.TB, ents map[string]entity.EntityConfig, rows map[string][]map[string]any, opts ...testUIOption) *testUI {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Skip("sqlite3 driver not available")
	}
	t.Cleanup(func() { db.Close() })
	reg := &testRegistry{ents: map[string]*entity.Entity{}}
	for name, cfg := range ents {
		cfg.Name = name
		e := entity.Define(name, cfg)
		e.SetDB(db)
		if err := e.Validate(); err != nil {
			t.Fatalf("entity %s: %v", name, err)
		}
		reg.ents[name] = e
	}
	if err := migrate.AutoMigrate(db, reg); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	x := &testUI{
		tb: t,
		host: &testHost{
			reg:   reg,
			db:    db,
			cruds: map[string]*crud.CrudHandler{},
			apis:  map[string]string{},
		},
		db: db,
	}
	for _, o := range opts {
		o(x)
	}
	// Seed in dependency order: a relation's target table fills before
	// the table that points at it, so foreign keys hold.
	names := make([]string, 0, len(rows))
	for name := range rows {
		names = append(names, name)
	}
	slices.SortFunc(names, func(a, b string) int {
		if d := relationDepth(reg.ents[a], reg, 0) - relationDepth(reg.ents[b], reg, 0); d != 0 {
			return d
		}
		return strings.Compare(a, b)
	})
	for _, name := range names {
		e := reg.ents[name]
		for _, row := range rows[name] {
			insertRow(t, db, e.GetTable(), row)
		}
	}
	return x
}

// relationDepth is how deep an entity's chain of relation targets nests.
func relationDepth(e *entity.Entity, reg *testRegistry, seen int) int {
	if seen > 8 {
		return seen
	}
	depth := 0
	for _, f := range e.GetFields() {
		if f.Type != schema.Relation || f.To == "" || f.To == e.GetName() {
			continue
		}
		if target, ok := reg.ents[f.To]; ok {
			if d := relationDepth(target, reg, seen+1) + 1; d > depth {
				depth = d
			}
		}
	}
	return depth
}

// insertRow writes one row by its column map.
func insertRow(t testing.TB, db *sql.DB, table string, row map[string]any) {
	t.Helper()
	cols := make([]string, 0, len(row))
	for c := range row {
		cols = append(cols, c)
	}
	slices.Sort(cols)
	ph := make([]string, len(cols))
	vals := make([]any, len(cols))
	for i, c := range cols {
		ph[i] = "?"
		vals[i] = row[c]
	}
	q := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)", query.QuoteIdent(table), strings.Join(cols, ","), strings.Join(ph, ","))
	if _, err := db.Exec(q, vals...); err != nil {
		t.Fatalf("seed %s: %v", table, err)
	}
}

// ctx builds a request context for path+query the way the UI host does.
func (x *testUI) ctx(path, query string) context.Context {
	req := httptest.NewRequest(http.MethodGet, path+query, nil)
	return app.WithRequest(context.Background(), req)
}

// userCtx is ctx with a signed-in user (and an owner, once the owner
// extractor below is installed).
func (x *testUI) userCtx(path, query, userID string) context.Context {
	req := httptest.NewRequest(http.MethodGet, path+query, nil)
	return handler.SetUser(app.WithRequest(context.Background(), req), &testUser{id: userID})
}

// installOwnerExtractor wires framework/owner against testUser, the way
// auth middleware would.
func installOwnerExtractor(t testing.TB) {
	t.Helper()
	prev := owner.GetExtractor()
	owner.SetExtractor(func(ctx context.Context) (any, bool) {
		raw, ok := handler.GetUser(ctx)
		if !ok || raw == nil {
			return nil, false
		}
		if u, ok := raw.(*testUser); ok {
			return u.GetID(), true
		}
		return nil, false
	})
	t.Cleanup(func() { owner.SetExtractor(prev) })
}

// listHTML renders one builder for a context and returns its HTML.
func listHTML(t testing.TB, b *ListBuilder, ctx context.Context) string {
	t.Helper()
	return string(b.RenderCtx(ctx))
}

// A visible-fields shorthand for the entity configs in the tests.
func fields(fs ...schema.Field) []schema.Field { return fs }
