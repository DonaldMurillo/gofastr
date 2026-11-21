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
	"github.com/DonaldMurillo/gofastr/framework/entityui"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
)

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

func (b *Battery) exposed(e *entity.Entity) bool {
	return slices.ContainsFunc(b.ents, func(x *entity.Entity) bool { return x.GetName() == e.GetName() })
}

func (b *Battery) writable(e *entity.Entity) bool {
	return b.exposed(e) && !b.app.EntityReadOnlyMounted(e)
}

func (b *Battery) exposedNamed(name string) (*entity.Entity, bool) {
	for _, e := range b.ents {
		if e.GetName() == name {
			return e, true
		}
	}
	return nil, false
}

func (b *Battery) entityBase(e *entity.Entity) string {
	return b.cfg.PathPrefix + "/entities/" + e.GetName()
}

func (b *Battery) apiBase(e *entity.Entity) string {
	return b.cfg.PathPrefix + "/api/" + e.GetName()
}

func (b *Battery) entityPath(rest string, m appui.Match) (*entity.Entity, string, bool) {
	tail, ok := strings.CutPrefix(rest, "/entities/")
	if !ok {
		return nil, "", false
	}
	name, id, _ := strings.Cut(tail, "/")
	e, ok := b.exposedNamed(name)
	if !ok {
		return nil, "", false
	}
	if id == "" || id == "create" {
		return e, id, true
	}
	if m.ScreenID() != b.entityBase(e)+recordPattern(e) {
		return nil, "", false
	}
	p := map[string]string{"id": m.Param("id")}
	for _, c := range e.KeyColumns() {
		p[c] = m.Param(c)
	}
	return e, recordID(e, p), true
}

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

func (b *Battery) relatedTo(e *entity.Entity) []string {
	var out []string
	for _, other := range b.ents {
		if slices.ContainsFunc(other.Config.Relations, func(r entity.Relation) bool {
			return r.Entity == e.GetName() && len(r.ForeignKey) > 0
		}) {
			out = append(out, other.GetName())
		}
	}
	return out
}

func (b *Battery) parentRecords(e *entity.Entity) []string {
	var out []string
	for _, other := range b.ents {
		if slices.ContainsFunc(e.Config.Relations, func(r entity.Relation) bool {
			return r.Entity == other.GetName() && len(r.ForeignKey) > 0
		}) {
			out = append(out, b.entityBase(other)+recordPattern(other))
		}
	}
	return out
}

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

func (b *Battery) mountEntities(group *appui.ScreenGroup, r *router.Router) {
	for _, e := range b.ents {
		name := e.GetName()
		base := "/entities/" + name
		listPath := b.entityBase(e)
		related := b.relatedTo(e)
		entityList := func() *entityui.ListBuilder {
			return b.ui.List(name).Base(listPath).Bulk().Delete().Undo().Duplicate().
				QueryBox().ColumnsMenu().LayoutSwitch().InlineEdit().Deleted().SavedViews().TabCounts()
		}
		list := b.screen(group, base, i18nui.AdminEntities, true, func(ctx context.Context, _ map[string]string) render.HTML {
			return entityList().RenderCtx(ctx)
		})
		b.entityTitle(list, e, func(ctx context.Context, _ map[string]string) string { return b.plural(ctx, e) })

		create := b.screen(group, base+"/create", i18nui.AdminEntities, true, func(ctx context.Context, _ map[string]string) render.HTML {
			return b.ui.Create(name).Base(listPath).RenderCtx(ctx)
		})
		b.entityTitle(create, e, func(ctx context.Context, _ map[string]string) string {
			return i18nui.AdminPaletteNew.Text(ctx, map[string]any{"entity": b.singular(ctx, e)})
		})
		create.Intercept = &appui.Intercept{From: listPath, AlsoFrom: b.parentRecords(e), As: appui.ScreenDrawer}

		record := b.screen(group, base+recordPattern(e), i18nui.AdminEntities, true, func(ctx context.Context, p map[string]string) render.HTML {
			return b.ui.Record(name, recordID(e, p)).Base(listPath).Related(related...).Activity().Delete().Undo().Duplicate().Steps(entityList()).Panel().
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

func (b *Battery) entityTitle(s *appui.Screen, e *entity.Entity, title func(ctx context.Context, p map[string]string) string) {
	as := s.Component.(*adminScreen)
	as.title = title
	s.Title = title(context.Background(), nil)
}

func (b *Battery) mountEntityAPI(r *router.Router, e *entity.Entity) {
	if !b.writable(e) {
		return
	}
	ch, err := b.app.CrudHandlerForEntity(e)
	if err != nil {
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
	r = crud.EntityRouter(r, e)
	r.Post(api, elevated(ch.Create()))
	r.Put(rec, elevated(ch.Update()))
	r.Patch(rec, elevated(ch.Update()))
	r.Delete(rec, elevated(ch.Delete()))
	if len(crud.RoutableTransitions(e.Config.States)) > 0 {
		r.Post(api+crud.TransitionPath(e.KeyColumns()), elevated(ch.Transition()))
	}
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
	if b.savedViews != nil {
		views := elevated(b.ui.SavedViewsHandler(e.GetName()))
		r.Post(api+"/_views", views)
		r.Post(api+"/_views/_delete", views)
	}
}

const countPoll = "60s"

const manyRows = 10000

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

func countText(ctx context.Context, n int, ok bool) string {
	switch {
	case !ok:
		return "—"
	case n >= manyRows:
		return i18nui.AdminCountMany.Text(ctx, map[string]any{"count": manyRows})
	}
	return i18n.Number(ctx, n)
}
