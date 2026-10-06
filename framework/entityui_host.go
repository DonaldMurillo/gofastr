package framework

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/entityui"
	"github.com/DonaldMurillo/gofastr/framework/tenant"
)

// EntityUI returns this app's entity screens: lists and records drawn from
// each entity's schema, Display and States, with ext's kinds, view funcs,
// tabs and actions. Call it once. It checks every name ext uses and panics
// at boot on a bad one, naming it, the way App.Entity refuses a bad
// declaration, so ext can only name entities registered before the call.
//
// It mounts the bulk bar's routes beside each entity's write routes:
// POST <api>/_bulk and GET <api>/_export.csv, for the entities registered
// before the call and for every one registered after it. With ext.Jobs set it also
// creates the snapshot tables queued runs walk (gofastr_bulk_jobs and
// gofastr_bulk_items), which needs a database, and at App.Start re-hands
// the JobRunner any job a crash left unenqueued and deletes finished jobs
// older than entityui.BulkRetention (UI.ResumeBulkJobs and
// UI.PruneBulkJobs; an app that runs for long schedules both). A second
// call panics: the routes belong to one UI, so share the one it returned.
func (a *App) EntityUI(ext entityui.Extensions) *entityui.UI {
	if a.entityUI != nil {
		panic("framework: EntityUI was already called on this app; its bulk and export routes belong to that UI, so pass the *entityui.UI it returned instead of building a second")
	}
	host := entityUIHost{a: a}
	if ext.Jobs != nil {
		if a.DB == nil {
			panic("framework: EntityUI: Extensions.Jobs needs a database (WithDB): queued bulk runs keep their selection there")
		}
		store, err := newSQLBulkStore(context.Background(), a.DB)
		if err != nil {
			panic(fmt.Sprintf("framework: EntityUI: %v", err))
		}
		host.store = store
	}
	u, err := entityui.New(host, ext)
	if err != nil {
		panic(fmt.Sprintf("framework: EntityUI: %v", err))
	}
	a.entityUI = u
	for _, e := range a.Registry.AllSorted() {
		a.mountEntityUIRoutes(e)
	}
	if ext.Jobs != nil {
		a.OnStart(func(ctx context.Context) error {
			resumeAndPruneBulkJobs(ctx, u)
			return nil
		})
	}
	return u
}

// bulkResumeGrace is how old an unenqueued job must be before a start
// hands it over again: younger ones may belong to a confirm still between
// writing its snapshot and enqueuing it.
const bulkResumeGrace = time.Minute

// resumeAndPruneBulkJobs hands over the jobs a crash left unenqueued and
// drops finished jobs past entityui.BulkRetention. A failure is logged,
// not fatal: the app serves without it, and the next start tries again.
func resumeAndPruneBulkJobs(ctx context.Context, u *entityui.UI) {
	if n, err := u.ResumeBulkJobs(ctx, bulkResumeGrace); err != nil {
		slog.ErrorContext(ctx, "framework: EntityUI: resume bulk jobs", "resumed", n, "error", err)
	} else if n > 0 {
		slog.InfoContext(ctx, "framework: EntityUI: resumed bulk jobs", "count", n)
	}
	if _, err := u.PruneBulkJobs(ctx, entityui.BulkRetention); err != nil {
		slog.ErrorContext(ctx, "framework: EntityUI: prune bulk jobs", "error", err)
	}
}

// mountEntityUIRoutes mounts e's bulk and export routes once EntityUI has
// run, when e has write routes and its name resolves to it. recordCrudMount
// calls it too, so an entity registered after EntityUI is not left with a
// bar whose posts 404.
func (a *App) mountEntityUIRoutes(e *entity.Entity) {
	if a.entityUI == nil || !a.entityUIOwns(e) {
		return
	}
	if _, ok := (entityUIHost{a: a}).APIPath(e); !ok {
		return
	}
	// On the router the entity's CRUD routes went on: a group's
	// sub-router carries the group's middleware, and a route beside it
	// would skip that guard.
	m := a.crudMounts[e]
	m.r.Post(m.rel+"/_bulk", a.entityUIOwned(e, a.entityUI.BulkHandler(e.GetName())))
	m.r.Get(m.rel+"/_export.csv", a.entityUIOwned(e, a.entityUI.ExportHandler(e.GetName())))
}

// entityUIOwns reports whether e is the entity its name resolves to. The
// screens and the bulk handlers look an entity up by name, so a version
// the name does not resolve to (another one is unversioned, or several
// versions share the name) gets no routes: they would run the other
// entity's handler, hooks and access rules under this version's path.
func (a *App) entityUIOwns(e *entity.Entity) bool {
	got, err := a.Registry.Get(e.GetName())
	return err == nil && got == e
}

// entityUIOwned answers 404 once e no longer owns its name: an entity
// registered after the routes mounted can take it over.
func (a *App) entityUIOwned(e *entity.Entity, h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !a.entityUIOwns(e) {
			http.NotFound(w, r)
			return
		}
		h.ServeHTTP(w, r)
	})
}

// entityUIMounted reports whether EntityUI's bulk and export routes answer
// for e, for the OpenAPI document.
func (a *App) entityUIMounted(e *entity.Entity) bool {
	if a.entityUI == nil || !a.entityUIOwns(e) {
		return false
	}
	_, ok := entityUIHost{a: a}.APIPath(e)
	return ok
}

// entityUIHost is entityui's view of one App.
type entityUIHost struct {
	a     *App
	store *sqlBulkStore
}

func (h entityUIHost) Registry() entity.Registry { return h.a.Registry }

func (h entityUIHost) Crud(e *entity.Entity) (*crud.CrudHandler, error) {
	return h.a.CrudHandlerForEntity(e)
}

// APIPath reports where the entity's write routes mounted: a grouped
// entity lives under its group's prefix, not the API prefix. Only an
// entity with CRUD routes is recorded there, so a read-only view
// (App.View) or CRUD turned off draws read-only screens.
func (h entityUIHost) APIPath(e *entity.Entity) (string, bool) {
	m, ok := h.a.crudMounts[e]
	return m.full, ok
}

func (h entityUIHost) Translator() *i18n.Translator { return h.a.Translator() }

func (h entityUIHost) Audit() entityui.AuditReader {
	if h.a.auditTable == "" || h.a.DB == nil {
		return nil
	}
	return auditTrail{db: h.a.DB, table: h.a.auditTable}
}

// auditTrail reads one record's audit rows for the Activity tab. The
// record's own read gate has already passed; the rows are further held to
// the caller's tenant, the scope the audit writer stamped.
type auditTrail struct {
	db    *sql.DB
	table string
}

func (t auditTrail) Trail(ctx context.Context, ent, recordID string, limit int) ([]entityui.AuditEntry, error) {
	safe, err := query.SafeIdent(t.table)
	if err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 200 {
		limit = 200
	}
	q := fmt.Sprintf("SELECT op, actor_id, created_at, diff, reason FROM %s WHERE entity = $1 AND record_id = $2", query.QuoteIdent(safe))
	args := []any{ent, recordID}
	if tid := tenant.GetTenantID(ctx); tid != "" {
		q += " AND tenant_id = $3"
		args = append(args, tid)
	}
	q += fmt.Sprintf(" ORDER BY created_at DESC LIMIT %d", limit)
	rows, err := t.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []entityui.AuditEntry
	for rows.Next() {
		var (
			op                  string
			actor, diff, reason sql.NullString
			at                  time.Time
		)
		if err := rows.Scan(&op, &actor, &at, &diff, &reason); err != nil {
			return nil, err
		}
		e := entityui.AuditEntry{At: at, Actor: actor.String, Operation: op, Reason: reason.String}
		if diff.Valid {
			var d struct {
				Before map[string]any `json:"old"`
				After  map[string]any `json:"new"`
			}
			if err := json.Unmarshal([]byte(diff.String), &d); err != nil {
				return nil, fmt.Errorf("audit row diff: %w", err)
			}
			e.Before, e.After = d.Before, d.After
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
