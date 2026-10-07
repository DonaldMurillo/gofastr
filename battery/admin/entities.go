package admin

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"

	appui "github.com/DonaldMurillo/gofastr/core-ui/app"
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
// order (an unknown name fails boot), or with AllEntities every entity
// whose CRUD is on. Neither exposes nothing. One entity per table, so a
// versioned entity is listed once.
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
		if !crudEnabled(e) || seen[e.GetTable()] {
			continue
		}
		seen[e.GetTable()] = true
		out = append(out, e)
	}
	return out, nil
}

// crudEnabled reports whether e has auto-CRUD on (nil means on).
func crudEnabled(e *entity.Entity) bool {
	x := e.Config.Exposure
	return x == nil || x.CRUD == nil || *x.CRUD
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
// exposed entity and the segment after it ("" for the list, "create",
// or a record id).
func (b *Battery) entityPath(rest string) (*entity.Entity, string, bool) {
	tail, ok := strings.CutPrefix(rest, "/entities/")
	if !ok {
		return nil, "", false
	}
	name, id, _ := strings.Cut(tail, "/")
	e, ok := b.exposedNamed(name)
	if !ok || strings.Contains(id, "/") {
		return nil, "", false
	}
	return e, id, true
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
			return r.Entity == e.GetName() && r.ForeignKey != ""
		}) {
			out = append(out, other.GetName())
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
		list := b.screen(group, base, i18nui.KeyAdminEntities, true, func(ctx context.Context, _ map[string]string) render.HTML {
			return b.ui.List(name).Base(listPath).Bulk().Delete().Duplicate().RenderCtx(ctx)
		})
		b.entityTitle(list, e, func(ctx context.Context, _ map[string]string) string { return b.plural(ctx, e) })

		create := b.screen(group, base+"/create", i18nui.KeyAdminEntities, true, func(ctx context.Context, _ map[string]string) render.HTML {
			return b.ui.Create(name).Base(listPath).RenderCtx(ctx)
		})
		b.entityTitle(create, e, func(ctx context.Context, _ map[string]string) string {
			return i18nui.TVars(ctx, i18nui.KeyAdminPaletteNew, map[string]string{"entity": b.singular(ctx, e)})
		})

		record := b.screen(group, base+"/:id", i18nui.KeyAdminEntities, true, func(ctx context.Context, p map[string]string) render.HTML {
			return b.ui.Record(name, p["id"]).Base(listPath).Related(related...).Activity().Delete().Duplicate().RenderCtx(ctx)
		})
		b.entityTitle(record, e, func(ctx context.Context, p map[string]string) string {
			if t, ok := b.ui.RecordTitle(ctx, name, p["id"]); ok {
				return t
			}
			return b.singular(ctx, e)
		})
		record.Intercept = &appui.Intercept{From: listPath, As: appui.ScreenDrawer}

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
			h.ServeHTTP(w, r.WithContext(crud.WithElevation(r.Context())))
		})
	}
	api := b.apiBase(e)
	r.Post(api, elevated(ch.Create()))
	r.Put(api+"/{id}", elevated(ch.Update()))
	r.Patch(api+"/{id}", elevated(ch.Update()))
	r.Delete(api+"/{id}", elevated(ch.Delete()))
	if len(crud.RoutableTransitions(e.Config.States)) > 0 {
		r.Post(api+"/{id}/transitions/{key}", elevated(ch.Transition()))
	}
	r.Post(api+"/_bulk", elevated(b.ui.BulkHandler(e.GetName())))
	r.Get(api+"/_export.csv", elevated(b.ui.ExportHandler(e.GetName())))
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
		ctx := crud.WithElevation(r.Context())
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write([]byte(b.entityStat(appui.WithRequest(ctx, r), e)))
	}))
}

// countText formats a count for a card: StatValue's grouped number below
// manyRows, else "10k+". A refused, failed or late count reads "—"
// (StatValue's answer) and passes through.
func countText(ctx context.Context, raw string) string {
	n, err := strconv.Atoi(strings.ReplaceAll(raw, ",", ""))
	if err != nil {
		return raw
	}
	if n >= manyRows {
		return i18nui.TVars(ctx, i18nui.KeyAdminCountMany, map[string]string{"count": strconv.Itoa(manyRows/1000) + "k"})
	}
	return raw
}
