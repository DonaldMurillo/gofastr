package admin

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"

	appui "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
)

// The entity screens are framework/entityui's list and record, read in
// process with the caller's context plus the admin's elevation, and
// written through routes under <prefix>/api/<entity> behind the gate.

// entitiesToExpose resolves the entities the admin manages: Entities in
// order (an unknown name, or one whose key cannot name a record, fails
// boot), or with AllEntities every entity whose CRUD routes the app
// mounted, which leaves out a raw App.Table, registered for migration
// only. Neither exposes nothing. One entity per table, so a versioned
// entity is listed once.
func (b *Battery) entitiesToExpose() ([]*entity.Entity, error) {
	byName := b.app.Registry.All()
	seen := map[string]bool{}
	var out []*entity.Entity
	if len(b.cfg.Entities) > 0 {
		for _, name := range b.cfg.Entities {
			e, ok := byName[name]
			if !ok {
				return nil, fmt.Errorf("admin: Config.Entities names %q, which the app does not register", name)
			}
			// The admin serves records by id; a raw table skipped
			// Define's key check.
			if err := e.CheckRecordKey(); err != nil {
				return nil, fmt.Errorf("admin: Config.Entities names %q: %w", name, err)
			}
			if seen[e.GetTable()] {
				continue
			}
			seen[e.GetTable()] = true
			out = append(out, e)
		}
		return out, nil
	}
	if !b.cfg.AllEntities {
		return nil, nil
	}
	for _, e := range b.app.Registry.AllSorted() {
		if !b.app.EntityCRUDMounted(e) || seen[e.GetTable()] {
			continue
		}
		seen[e.GetTable()] = true
		out = append(out, e)
	}
	return out, nil
}

// exposed reports whether the admin manages e.
func (b *Battery) exposed(e *entity.Entity) bool {
	return slices.ContainsFunc(b.ents, func(x *entity.Entity) bool { return x.GetName() == e.GetName() })
}

// exposedNamed returns the exposed entity with this name.
func (b *Battery) exposedNamed(name string) (*entity.Entity, bool) {
	for _, e := range b.ents {
		if e.GetName() == name {
			return e, true
		}
	}
	return nil, false
}

// entityBase is an entity's list path in the admin.
func (b *Battery) entityBase(e *entity.Entity) string {
	return b.cfg.PathPrefix + "/entities/" + e.GetName()
}

// apiBase is where the admin's entity screens send their writes.
func (b *Battery) apiBase(e *entity.Entity) string {
	return b.cfg.PathPrefix + "/api/" + e.GetName()
}

// entityPath splits an admin-relative path under /entities into the
// exposed entity and what follows it: "" for the list, "create", or a
// record id, which for a composite key is one segment per key column,
// joined as entity.JoinKey renders the id.
func (b *Battery) entityPath(rest string) (*entity.Entity, string, bool) {
	tail, ok := strings.CutPrefix(rest, "/entities/")
	if !ok {
		return nil, "", false
	}
	name, id, _ := strings.Cut(tail, "/")
	e, ok := b.exposedNamed(name)
	if !ok {
		return nil, "", false
	}
	if n := len(e.KeyColumns()); n > 1 && id != "" && id != "create" {
		parts := strings.Split(id, "/")
		if len(parts) != n || slices.Contains(parts, "") {
			return nil, "", false
		}
		return e, entity.JoinKey(parts), true
	}
	if strings.Contains(id, "/") {
		return nil, "", false
	}
	return e, id, true
}

// recordPattern is the screen pattern tail naming one of e's records:
// "/:id", or one segment per key column of a composite key.
func recordPattern(e *entity.Entity) string {
	cols := e.KeyColumns()
	if len(cols) == 1 {
		return "/:id"
	}
	var b strings.Builder
	for _, c := range cols {
		b.WriteString("/:" + c)
	}
	return b.String()
}

// recordHref is the screen path of e's record id: the escaped id for a
// one-column key; for a composite key the id itself, already one escaped
// segment per key column. "" when the id does not split into e's key.
func (b *Battery) recordHref(e *entity.Entity, id string) string {
	n := len(e.KeyColumns())
	if n == 1 {
		return b.entityBase(e) + "/" + url.PathEscape(id)
	}
	if _, err := entity.SplitKey(id, n); err != nil {
		return ""
	}
	return b.entityBase(e) + "/" + id
}

// recordID is the id of the record a screen matched by recordPattern
// names.
func recordID(e *entity.Entity, p map[string]string) string {
	cols := e.KeyColumns()
	if len(cols) == 1 {
		return p["id"]
	}
	parts := make([]string, len(cols))
	for i, c := range cols {
		parts[i] = p[c]
	}
	return entity.JoinKey(parts)
}

// plural and singular are an entity's localized names.
func (b *Battery) plural(ctx context.Context, e *entity.Entity) string {
	d := ""
	if e.Config.Display != nil {
		d = e.Config.Display.Plural
	}
	return i18nui.EntityPlural(ctx, nil, e.GetName(), d)
}

func (b *Battery) singular(ctx context.Context, e *entity.Entity) string {
	d := ""
	if e.Config.Display != nil {
		d = e.Config.Display.Singular
	}
	return i18nui.EntitySingular(ctx, nil, e.GetName(), d)
}

// relatedTo lists the exposed entities holding a relation that points at
// e, for its record's Related tab. Each related list still passes its
// own read gate.
func (b *Battery) relatedTo(e *entity.Entity) []string {
	var out []string
	for _, other := range b.ents {
		if slices.ContainsFunc(other.Config.Relations, func(r entity.Relation) bool {
			return r.Entity == e.GetName() && len(r.FKColumns()) > 0
		}) {
			out = append(out, other.GetName())
		}
	}
	return out
}

// parentRecords lists the record patterns of the exposed entities e
// belongs to: the records whose Related tab lists e, the reverse of
// relatedTo.
func (b *Battery) parentRecords(e *entity.Entity) []string {
	var out []string
	for _, other := range b.ents {
		if slices.ContainsFunc(e.Config.Relations, func(r entity.Relation) bool {
			return r.Entity == other.GetName() && len(r.FKColumns()) > 0
		}) {
			out = append(out, b.entityBase(other)+recordPattern(other))
		}
	}
	return out
}

// relatedRecords lists the record patterns e's record opens as a drawer
// over, beside its list: the records it belongs to and the records that
// belong to it, so a relation's open button and a Related tab's row push
// a panel instead of leaving the drawer. Its own pattern is left out.
func (b *Battery) relatedRecords(e *entity.Entity) []string {
	own := b.entityBase(e) + recordPattern(e)
	var out []string
	add := func(p string) {
		if p != own && !slices.Contains(out, p) {
			out = append(out, p)
		}
	}
	for _, p := range b.parentRecords(e) {
		add(p)
	}
	children := b.relatedTo(e)
	for _, other := range b.ents {
		if slices.Contains(children, other.GetName()) {
			add(b.entityBase(other) + recordPattern(other))
		}
	}
	return out
}

// mountEntities registers each exposed entity's list, record and create
// screens, and its write routes.
func (b *Battery) mountEntities(group *appui.ScreenGroup, r *router.Router) {
	for _, e := range b.ents {
		name := e.GetName()
		base := "/entities/" + name
		listPath := b.entityBase(e)
		related := b.relatedTo(e)
		list := b.screen(group, base, i18nui.AdminEntities, true, func(ctx context.Context, _ map[string]string) render.HTML {
			return b.ui.List(name).Base(listPath).Bulk().Delete().Undo().Duplicate().
				QueryBox().ColumnsMenu().LayoutSwitch().InlineEdit().Deleted().SavedViews().TabCounts().RenderCtx(ctx)
		})
		b.entityTitle(list, e, func(ctx context.Context, _ map[string]string) string { return b.plural(ctx, e) })

		create := b.screen(group, base+"/create", i18nui.AdminEntities, true, func(ctx context.Context, _ map[string]string) render.HTML {
			return b.ui.Create(name).Base(listPath).RenderCtx(ctx)
		})
		b.entityTitle(create, e, func(ctx context.Context, _ map[string]string) string {
			return i18nui.AdminPaletteNew.Text(ctx, map[string]any{"entity": b.singular(ctx, e)})
		})
		// New, like a record, opens as a drawer over the list, and over
		// the record of each entity it belongs to, whose Related tab adds
		// to it; a direct load is the full page.
		create.Intercept = &appui.Intercept{From: listPath, AlsoFrom: b.parentRecords(e), As: appui.ScreenDrawer}

		record := b.screen(group, base+recordPattern(e), i18nui.AdminEntities, true, func(ctx context.Context, p map[string]string) render.HTML {
			return b.ui.Record(name, recordID(e, p)).Base(listPath).Related(related...).Activity().Delete().Undo().Duplicate().Steps().Panel().
				API().Override().RenderCtx(ctx)
		})
		b.entityTitle(record, e, func(ctx context.Context, p map[string]string) string {
			if t, ok := b.ui.RecordTitle(ctx, name, recordID(e, p)); ok {
				return t
			}
			return b.singular(ctx, e)
		})
		record.Intercept = &appui.Intercept{From: listPath, AlsoFrom: b.relatedRecords(e), As: appui.ScreenDrawer}

		b.mountEntityAPI(r, e)
	}
	b.mountCounts(r)
}

// entityTitle replaces a screen's title with an entity-aware one.
func (b *Battery) entityTitle(s *appui.Screen, e *entity.Entity, title func(ctx context.Context, p map[string]string) string) {
	as := s.Component.(*adminScreen)
	as.title = title
	s.Title = title(context.Background(), nil)
}

// mountEntityAPI mounts an entity's write routes: the app's own CRUD
// handler (hooks, audit, scope, the write checks) behind the gate, with
// the elevation that lifts only the entity's Exposure.Access check.
func (b *Battery) mountEntityAPI(r *router.Router, e *entity.Entity) {
	ch, err := b.app.CrudHandlerForEntity(e)
	if err != nil {
		// Init resolved e from the registry, so this is a programming
		// error; the screens draw without a write path.
		b.logger().Error("admin: no CRUD handler for exposed entity", "entity", e.GetName(), "error", err)
		return
	}
	elevated := func(h http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h.ServeHTTP(w, r.WithContext(b.elevate(r.Context())))
		})
	}
	api := b.apiBase(e)
	rec := api + crud.KeyPath(e.KeyColumns())
	r.Post(api, elevated(ch.Create()))
	r.Put(rec, elevated(ch.Update()))
	r.Patch(rec, elevated(ch.Update()))
	r.Delete(rec, elevated(ch.Delete()))
	if len(crud.RoutableTransitions(e.Config.States)) > 0 {
		r.Post(api+crud.TransitionPath(e.KeyColumns()), elevated(ch.Transition()))
	}
	// The override's capability is checked by name: elevation lifts the
	// entity's update permission for the write, never the capability.
	if e.Config.States != nil {
		r.Post(rec+"/_override", elevated(b.ui.OverrideHandler(e.GetName())))
	}
	if e.Config.Scope != nil && e.Config.Scope.SoftDelete {
		r.Post(rec+"/_restore", elevated(b.ui.RestoreHandler(e.GetName())))
		r.Post(rec+"/_purge", elevated(b.ui.PurgeHandler(e.GetName())))
	}
	r.Post(api+"/_bulk", elevated(b.ui.BulkHandler(e.GetName())))
	r.Get(api+"/_export.csv", elevated(b.ui.ExportHandler(e.GetName())))
	r.Post(api+"/_pick", elevated(b.ui.PickerHandler(e.GetName())))
	// Saved views belong to the caller and touch no entity row, but a
	// view needs the entity's read permission, and the admin reads its
	// lists elevated: the view is gated by the same read the list is.
	if b.savedViews != nil {
		views := elevated(b.ui.SavedViewsHandler(e.GetName()))
		r.Post(api+"/_views", views)
		r.Post(api+"/_views/_delete", views)
	}
}

// countPoll is how often a dashboard count refreshes. The poll module
// pauses while the tab is hidden.
const countPoll = "60s"

// manyRows is where a count stops being exact on the dashboard: past it
// the card reads "10k+".
const manyRows = 10000

// mountCounts mounts GET <prefix>/_count/<entity>, the fragment a
// dashboard card polls: the card, redrawn with a fresh count read in the
// caller's scope (never cached in server memory: counts are per caller).
func (b *Battery) mountCounts(r *router.Router) {
	r.Get(b.cfg.PathPrefix+"/_count/{entity}", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		e, ok := b.exposedNamed(r.PathValue("entity"))
		if !ok {
			http.NotFound(w, r)
			return
		}
		ctx := b.elevate(r.Context())
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write([]byte(b.entityStat(appui.WithRequest(ctx, r), e)))
	}))
}

// countText writes a count for a card in the reader's locale below
// manyRows, else "10,000+". A refused, failed or late count (ok false)
// reads "—".
func countText(ctx context.Context, n int, ok bool) string {
	switch {
	case !ok:
		return "—"
	case n >= manyRows:
		return i18nui.AdminCountMany.Text(ctx, map[string]any{"count": manyRows})
	}
	return i18n.Number(ctx, n)
}
