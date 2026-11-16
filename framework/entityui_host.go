package framework

import (
	"context"
	"database/sql"
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
	"github.com/DonaldMurillo/gofastr/internal/jsonexact"
)

// EntityUI returns this app's entity screens, built from each entity's
// schema, Display and States plus ext, and mounts POST <api>/_bulk,
// GET <api>/_export.csv and POST <api>/_pick beside each entity's write
// routes, for entities registered before and after the call. With ext.Jobs
// set it creates the bulk snapshot tables and, at App.Start, resumes
// unenqueued jobs and prunes finished ones. It panics on a name in ext no
// registered entity has, on ext.Jobs without a database, on a route
// overlap, and on a second call.
func (a *App) EntityUI(ext entityui.Extensions) *entityui.UI {
	if a.entityUI != nil {
		panic("framework: EntityUI was already called on this app; its bulk and export routes belong to that UI, so pass the *entityui.UI it returned instead of building a second")
	}
	held := newMuxReplay(a.router.Routes())
	for _, ent := range a.Registry.AllSorted() {
		if !a.entityUIOwns(ent) {
			continue
		}
		if mount, ok := a.crudMounts[ent]; ok {
			for _, pattern := range entityUIRoutePatterns(mount.full) {
				if conflict, refused := held.add(pattern, "an EntityUI route"); refused {
					panic(fmt.Sprintf("framework: EntityUI route %s overlaps %s, %s", pattern, conflict.pattern, conflict.owner))
				}
			}
		}
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

const bulkResumeGrace = time.Minute

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

func entityUIRoutePatterns(path string) []string {
	return []string{"POST " + path + "/_bulk", "GET " + path + "/_export.csv", "POST " + path + "/_pick"}
}

func (a *App) entityUIWouldOwn(ent *entity.Entity) bool {
	if ent.Version == "" {
		return true
	}
	for _, existing := range a.Registry.AllSorted() {
		if existing.GetName() == ent.GetName() {
			return false
		}
	}
	return true
}

func (a *App) mountEntityUIRoutes(e *entity.Entity) {
	if a.entityUI == nil || !a.entityUIOwns(e) {
		return
	}
	if _, ok := (entityUIHost{a: a}).APIPath(e); !ok {
		return
	}
	m := a.crudMounts[e]
	r := crud.EntityRouter(m.r, e)
	r.Post(m.rel+"/_bulk", a.entityUIOwned(e, a.entityUI.BulkHandler(e.GetName())))
	r.Get(m.rel+"/_export.csv", a.entityUIOwned(e, a.entityUI.ExportHandler(e.GetName())))
	r.Post(m.rel+"/_pick", a.entityUIOwned(e, a.entityUI.PickerHandler(e.GetName())))
}

func (a *App) entityUIOwns(e *entity.Entity) bool {
	got, err := a.Registry.Get(e.GetName())
	return err == nil && got == e
}

func (a *App) entityUIOwned(e *entity.Entity, h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !a.entityUIOwns(e) {
			http.NotFound(w, r)
			return
		}
		h.ServeHTTP(w, r)
	})
}

func (a *App) entityUIMounted(e *entity.Entity) bool {
	if a.entityUI == nil || !a.entityUIOwns(e) {
		return false
	}
	_, ok := entityUIHost{a: a}.APIPath(e)
	return ok
}

type entityUIHost struct {
	a     *App
	store *sqlBulkStore
}

func (h entityUIHost) Registry() entity.Registry { return h.a.Registry }

func (h entityUIHost) Crud(e *entity.Entity) (*crud.CrudHandler, error) {
	return h.a.CrudHandlerForEntity(e)
}

// APIPath reports where e's write routes mounted; false when it has none.
func (h entityUIHost) APIPath(e *entity.Entity) (string, bool) {
	m, ok := h.a.crudMounts[e]
	return m.full, ok
}

func (h entityUIHost) Translator() *i18n.Translator { return h.a.Translator() }

// HasScreen asks every mounted ScreenHost whether a screen serves path;
// known is false when none answers.
func (h entityUIHost) HasScreen(path string) (has, known bool) {
	for _, m := range h.a.Mountables() {
		sh, ok := m.(entityui.ScreenHost)
		if !ok {
			continue
		}
		if got, ok := sh.HasScreen(path); ok {
			known = true
			if got {
				return true, true
			}
		}
	}
	return false, known
}

func (h entityUIHost) Audit() entityui.AuditReader {
	if h.a.auditTable == "" || h.a.DB == nil {
		return nil
	}
	return auditTrail{db: h.a.DB, table: h.a.auditTable, cfg: h.a.auditCfg, reg: h.a.Registry}
}

type auditTrail struct {
	db    *sql.DB
	table string
	cfg   *AuditConfig
	reg   *Registry
}

func (t auditTrail) softDeletes(ent string) bool {
	if t.reg == nil {
		return false
	}
	e, err := t.reg.Get(ent)
	return err == nil && e.Config.Scope != nil && e.Config.Scope.SoftDelete
}

func (t auditTrail) Trail(ctx context.Context, ent, recordID string, limit int) ([]entityui.AuditEntry, error) {
	safe, err := query.SafeIdent(t.table)
	if err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 200 {
		limit = 200
	}
	table := query.QuoteFolded(safe)
	if t.cfg != nil {
		recordID = t.cfg.keyID(auditKeyOf(t.reg, ent), recordID)
	}
	args := []any{ent, recordID}
	inTenant, endTenant, nextTenant := "", "", ""
	if tid := tenant.GetTenantID(ctx); tid != "" {
		inTenant, endTenant, nextTenant = " AND a.tenant_id = $3", " AND e.tenant_id = $3", " AND n.tenant_id = $3"
		args = append(args, tid)
	}
	ends := "'purge', 'delete'"
	if t.softDeletes(ent) {
		ends = "'purge'"
	}
	q := fmt.Sprintf("SELECT a.op, a.actor_id, a.created_at, a.diff, a.reason FROM %[1]s a"+
		" WHERE a.entity = $1 AND a.record_id = $2%[2]s"+
		" AND NOT EXISTS (SELECT 1 FROM %[1]s e WHERE e.entity = $1 AND e.record_id = $2%[3]s"+
		" AND e.op IN (%[4]s) AND e.created_at >= a.created_at"+
		" AND EXISTS (SELECT 1 FROM %[1]s n WHERE n.entity = $1 AND n.record_id = $2%[5]s AND n.created_at > e.created_at))"+
		" ORDER BY a.created_at DESC LIMIT %[6]d", table, inTenant, endTenant, ends, nextTenant, limit)
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
			if err := jsonexact.Unmarshal([]byte(diff.String), &d); err != nil {
				return nil, fmt.Errorf("audit row diff: %w", err)
			}
			e.Before, e.After = d.Before, d.After
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
