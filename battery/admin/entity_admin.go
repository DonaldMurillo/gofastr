package admin

// Entity CRUD admin, rendered THROUGH the app's mounted UI host so the screens
// hydrate with runtime.js: the list is a DataTable island (sort/paginate via
// RPC, no reload), delete is a `data-fui-confirm` + `data-fui-rpc` button, and
// forms are server-rendered. Every read/write is an in-process call into the
// entity's OWN CrudHandler with the caller's context forwarded, so validation,
// owner/tenant scoping, hooks, and events all apply exactly as on the JSON API
// — the admin never re-implements CRUD/pagination/filter logic.

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"time"

	appui "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/app/decide"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core-ui/interactive"
	"github.com/DonaldMurillo/gofastr/core-ui/widget"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// registerEntityAdmin wires the entity CRUD screens + RPC/form routes. Screens
// register on the host's app.App (so they render with chrome + runtime); the
// RPC/form/delete endpoints register on the framework router (gated by gate()).
//
// When no entities are exposable it is a no-op. When entities ARE requested but
// no UI host is mounted it errors — the entity admin cannot work without one.
func (b *Battery) registerEntityAdmin() error {
	if b.registry == nil || b.db == nil {
		return nil
	}
	ents := b.entitiesToExpose()
	if len(ents) == 0 {
		return nil
	}
	if b.host == nil || b.screens == nil {
		if len(b.cfg.Entities) > 0 {
			return fmt.Errorf("admin: entity screens for %v require a mounted UI host "+
				"(build the app with framework.NewUIHostApp / uihost.New)", b.cfg.Entities)
		}
		// Auto mode (Entities unset) degrades quietly: an app with no UI host
		// still gets the queue/audit ops pages.
		return nil
	}

	// One ScreenGroup for all entity screens, wrapped in an admin shell (a
	// sticky nav rail of entity links) and gated by the policy chain so the
	// host's render pipeline refuses unauthorized callers before Load runs.
	layout := appui.NewLayout("admin").WithSidebar(b.adminSidebar(ents))
	group := appui.NewScreenGroup(b.cfg.PathPrefix+"/e", layout, b.gatePolicy())
	for _, ent := range ents {
		b.registerEntityScreens(group, ent)
		b.registerEntityRoutes(ent)
	}
	b.screens.Router.ScreenGroup(group)

	// Mount the mobile nav drawer ONCE (the framework's proven responsive-nav
	// pattern — the same one the docs/components site uses): on < 900px the
	// SectionMenu shows a trigger button that opens this slide-in sheet
	// (backdrop, click-outside / Esc close, focus trap, scroll lock — all from
	// preset.Drawer, none re-implemented). On ≥ 900px it's a sticky rail.
	widget.MountBuilder(b.router, interactive.SectionMenuDrawer(b.navConfig(ents)))
	return nil
}

// adminSidebar builds the admin nav as an interactive.SectionMenu — a sticky
// rail on desktop, a mounted slide-in drawer on mobile. Theme-tokenized, so it
// reads as part of the host app, not a bolted-on tool.
func (b *Battery) adminSidebar(ents []*entity.Entity) component.Component {
	return sectionNav{cfg: b.navConfig(ents)}
}

// navConfig is the single source of truth for the admin nav, shared by the
// rail (adminSidebar) and the mounted mobile drawer (registerEntityAdmin) so
// the two stay in lock-step.
func (b *Battery) navConfig(ents []*entity.Entity) interactive.SectionMenuConfig {
	title := b.cfg.Title
	if title == "" {
		title = "Admin"
	}
	items := make([]interactive.SectionItem, 0, len(ents))
	for _, ent := range ents {
		items = append(items, interactive.SectionItem{
			Label: titleCase(ent.GetName()),
			Href:  b.entityBase(ent),
		})
	}
	return interactive.SectionMenuConfig{
		AriaLabel:    title + " navigation",
		TriggerLabel: "Menu",
		DrawerName:   "admin-nav",
		Groups:       []interactive.SectionGroup{{Label: title, Items: items}},
	}
}

// sectionNav adapts a SectionMenuConfig to a component.Component for the
// Layout's sidebar slot.
type sectionNav struct{ cfg interactive.SectionMenuConfig }

func (n sectionNav) Render() render.HTML { return interactive.SectionMenu(n.cfg) }

// gatePolicy mirrors gate() for the SSR screen path: a Block decision when the
// request is not authorized, short-circuiting before Load/Render. The status
// distinguishes unauthenticated (401) from authenticated-but-not-admin (403).
func (b *Battery) gatePolicy() appui.Policy {
	return appui.PolicyFunc(func(ctx context.Context) appui.Decision {
		if b.authorized(ctx) {
			return decide.Allow()
		}
		status := b.authzStatus(ctx)
		return decide.Block(status, http.StatusText(status))
	})
}

// registerEntityScreens registers the list/new/edit screens for ent on group.
func (b *Battery) registerEntityScreens(group *appui.ScreenGroup, ent *entity.Entity) {
	base := b.entityBase(ent)
	name := ent.GetName()
	group.Screen(appui.NewScreen(base, &entityListScreen{b: b, ent: ent}).
		WithTitle(name), nil)
	group.Screen(appui.NewScreen(base+"/new", &entityFormScreen{b: b, ent: ent}).
		WithTitle("New "+singular(name)), nil)
	// Read-only detail/show screen, linked from each list row.
	group.Screen(appui.NewScreen(base+"/view/:id", &entityDetailScreen{b: b, ent: ent}).
		WithTitle(singular(name)), nil)
	// core-ui screen router uses :param syntax (not the {id} of the framework
	// HTTP router).
	group.Screen(appui.NewScreen(base+"/edit/:id", &entityFormScreen{b: b, ent: ent, edit: true}).
		WithTitle("Edit "+singular(name)), nil)
}

// registerEntityRoutes registers the explicit (non-screen) endpoints: the list
// island fragment, the create/update form targets, and the delete RPC.
func (b *Battery) registerEntityRoutes(ent *entity.Entity) {
	base := b.entityBase(ent)
	// Mutation endpoints live on underscore paths distinct from the GET screen
	// paths (/new, /edit/{id}) — a Go ServeMux pattern that matches a path on
	// the wrong method returns 405, which would otherwise shadow the screens
	// served by the host's catch-all.
	b.router.Get(base+"/_rows", b.gate(b.entityRows(ent)))
	b.router.Post(base+"/_create", b.gate(b.entitySave(ent, false)))
	b.router.Post(base+"/_update/{id}", b.gate(b.entitySave(ent, true)))
	b.router.Delete(base+"/_delete/{id}", b.gate(b.entityDelete(ent)))
}

// entitiesToExpose resolves the entities to surface. Explicit Config.Entities
// wins (in order, unknown names skipped). Empty → every registered entity whose
// CRUD is enabled (so credential tables shipped CRUD=false stay hidden).
func (b *Battery) entitiesToExpose() []*entity.Entity {
	if len(b.cfg.Entities) > 0 {
		out := make([]*entity.Entity, 0, len(b.cfg.Entities))
		for _, name := range b.cfg.Entities {
			if ent, err := b.registry.Get(name); err == nil {
				out = append(out, ent)
			}
		}
		return out
	}
	var out []*entity.Entity
	for _, ent := range b.registry.AllSorted() {
		if crudEnabled(ent) {
			out = append(out, ent)
		}
	}
	return out
}

// isTimestampCol reports whether name is a framework-managed timestamp column
// (hidden from the list grid by default).
func isTimestampCol(name string) bool {
	switch name {
	case "created_at", "updated_at", "deleted_at":
		return true
	}
	return false
}

// crudEnabled reports whether ent has auto-CRUD (nil = auto-true when a DB is
// set, which is always the case here).
func crudEnabled(ent *entity.Entity) bool {
	return ent.Config.CRUD == nil || *ent.Config.CRUD
}

// ----- CrudHandler proxy ----------------------------------------------------

// crudFor builds a CrudHandler for ent and the registry (for scope + relation
// resolution). CaseSnake is the framework's *identity* casing — convertKey
// returns the column verbatim and convertMapKeys is a no-op in both directions
// — so JSON keys equal the entity's field/column names regardless of the host's
// naming convention. That lets these screens index result rows and build write
// bodies by f.Name with no casing translation.
func (b *Battery) crudFor(ent *entity.Entity) *crud.CrudHandler {
	ch := crud.NewCrudHandler(ent, b.db)
	ch.JSONCase = crud.CaseSnake
	ch.Registry = b.registry
	return ch
}

// callCrud invokes a CrudHandler http.HandlerFunc in-process, forwarding the
// PARENT request's context so the user/tenant the framework auth chain put on
// it flows into owner/tenant scoping — the admin sees exactly what the signed-in
// user is allowed to, never more.
func callCrud(parent *http.Request, h http.HandlerFunc, method, rawQuery, id, body string) (int, []byte) {
	var rdr io.Reader
	if body != "" {
		rdr = strings.NewReader(body)
	}
	target := "/"
	if rawQuery != "" {
		target += "?" + rawQuery
	}
	req := httptest.NewRequest(method, target, rdr).WithContext(parent.Context())
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if id != "" {
		req.SetPathValue("id", id)
	}
	rec := httptest.NewRecorder()
	h(rec, req)
	return rec.Code, rec.Body.Bytes()
}

// callCrudCtx is callCrud for the screen path, where we have a context (not a
// request). It builds a minimal request carrying ctx so owner/tenant scope and
// the CrudHandler's request reads still work.
func callCrudCtx(ctx context.Context, h http.HandlerFunc, method, rawQuery, id, body string) (int, []byte) {
	parent := httptest.NewRequest(http.MethodGet, "/", nil).WithContext(ctx)
	return callCrud(parent, h, method, rawQuery, id, body)
}

// entityBase is the URL prefix for an entity's admin screens.
func (b *Battery) entityBase(ent *entity.Entity) string {
	return b.cfg.PathPrefix + "/e/" + ent.GetTable()
}

// editableFields returns the fields a form should expose: skip Hidden,
// AutoGenerate, and ReadOnly (server-owned) fields.
func editableFields(ent *entity.Entity) []schema.Field {
	out := make([]schema.Field, 0, len(ent.GetFields()))
	for _, f := range ent.GetFields() {
		if f.Hidden || f.ReadOnly || f.AutoGenerate != schema.AutoNone {
			continue
		}
		out = append(out, f)
	}
	return out
}

// listColumns returns the columns to show on the list table: id first, then
// every non-hidden field.
func listColumns(ent *entity.Entity) []string {
	cols := []string{"id"}
	for _, f := range ent.GetFields() {
		// Skip the id (prepended), hidden fields, and the framework-managed
		// timestamp columns — they clutter the grid and are rarely the thing
		// you scan a list for.
		if f.Hidden || f.Name == "id" || isTimestampCol(f.Name) {
			continue
		}
		cols = append(cols, f.Name)
	}
	return cols
}

// listRows runs the CrudHandler List with the given query and returns the
// decoded rows plus the total count for pagination.
func (b *Battery) listRows(ctx context.Context, ent *entity.Entity, query string) (rows []map[string]any, total int, err error) {
	ch := b.crudFor(ent)
	// Screen renders bypass b.gate, so inject the admin superuser policy here too
	// — the admin reads every entity it manages, regardless of per-entity access RBAC.
	code, raw := callCrudCtx(adminSuperuserCtx(ctx), ch.List(), http.MethodGet, query, "", "")
	if code != http.StatusOK {
		return nil, 0, fmt.Errorf("list returned %d", code)
	}
	var env struct {
		Data  []map[string]any `json:"data"`
		Total int              `json:"total"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, 0, err
	}
	return env.Data, env.Total, nil
}

// getRow loads a single record by id (owner-scoped via ctx).
func (b *Battery) getRow(ctx context.Context, ent *entity.Entity, id string) (map[string]any, error) {
	ch := b.crudFor(ent)
	code, raw := callCrudCtx(adminSuperuserCtx(ctx), ch.Get(), http.MethodGet, "", id, "")
	if code != http.StatusOK {
		return nil, fmt.Errorf("get returned %d", code)
	}
	var row map[string]any
	if err := json.Unmarshal(raw, &row); err != nil {
		return nil, err
	}
	return row, nil
}

// ----- write handlers (explicit routes) -------------------------------------

// entitySave handles create (edit=false) and update (edit=true). On success it
// 303-redirects to the list. On a validation error it stashes the submitted
// values + field errors in a short-lived flash and redirects back to the form,
// so the re-render is a full host-rendered page (chrome + runtime).
func (b *Battery) entitySave(ent *entity.Entity, edit bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}
		id := ""
		if edit {
			id = r.PathValue("id")
		}
		body := formToJSON(ent, r)
		ch := b.crudFor(ent)
		var code int
		var raw []byte
		if edit {
			code, raw = callCrud(r, ch.Update(), http.MethodPut, "", id, body)
		} else {
			code, raw = callCrud(r, ch.Create(), http.MethodPost, "", "", body)
		}
		if code >= 200 && code < 300 {
			http.Redirect(w, r, b.entityBase(ent), http.StatusSeeOther)
			return
		}
		// Re-render the form with the submitted values + the server's message.
		vals := map[string]string{}
		for _, f := range editableFields(ent) {
			vals[f.Name] = r.PostForm.Get(f.Name)
		}
		token := b.flash.put(&formFlash{values: vals, fieldErrs: crudFieldErrors(raw), general: crudErrorMessage(raw)})
		dest := b.entityBase(ent) + "/new"
		if edit {
			dest = b.entityBase(ent) + "/edit/" + id
		}
		http.Redirect(w, r, dest+"?e="+token, http.StatusSeeOther)
	}
}

// entityDelete handles the DELETE RPC fired by the confirm button. It deletes
// then returns the refreshed table fragment (200) — the delete button binds the
// list's island signal, so the runtime swaps the response in place. Returning
// the fragment unconditionally (rather than a non-2xx on a scope miss) keeps the
// signal value valid HTML; a row the caller can't delete simply isn't in their
// re-rendered list anyway.
func (b *Battery) entityDelete(ent *entity.Entity) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ch := b.crudFor(ent)
		_, _ = callCrud(r, ch.Delete(), http.MethodDelete, "", r.PathValue("id"), "")
		html := b.renderTable(r.Context(), ent, r.URL.Query())
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, string(html))
	}
}

// entityRows is the DataTable island endpoint: returns just the table fragment
// (wrapped in its signal div) for sort/paginate RPC swaps.
func (b *Battery) entityRows(ent *entity.Entity) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		html := b.renderTable(r.Context(), ent, r.URL.Query())
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		// Keep the URL in sync so refresh/share/back reproduce the view.
		w.Header().Set("X-Gofastr-Push-State", b.entityBase(ent)+listQueryString(r.URL.Query()))
		_, _ = io.WriteString(w, string(html))
	}
}

// ----- form → JSON ----------------------------------------------------------

// formToJSON converts the posted form into a JSON object the CrudHandler
// accepts, coercing by field type. Empty optional values are omitted so the
// handler's defaults/validation behave as on the API; booleans always send (an
// unchecked checkbox is absent → false).
func formToJSON(ent *entity.Entity, r *http.Request) string {
	obj := map[string]any{}
	for _, f := range editableFields(ent) {
		raw := strings.TrimSpace(r.PostForm.Get(f.Name))
		switch f.Type {
		case schema.Bool:
			obj[f.Name] = raw == "on" || raw == "true" || raw == "1"
		case schema.Int:
			if raw != "" {
				if n, err := strconv.ParseInt(raw, 10, 64); err == nil {
					obj[f.Name] = n
				}
			}
		case schema.Float:
			if raw != "" {
				if x, err := strconv.ParseFloat(raw, 64); err == nil {
					obj[f.Name] = x
				}
			}
		default:
			if raw != "" {
				obj[f.Name] = raw
			}
		}
	}
	out, _ := json.Marshal(obj)
	return string(out)
}

// crudErrorMessage extracts a human message from a CrudHandler error body.
func crudErrorMessage(raw []byte) string {
	var e struct {
		Error  string              `json:"error"`
		Fields map[string][]string `json:"fields"`
	}
	if json.Unmarshal(raw, &e) == nil {
		msg := e.Error
		if msg != "" {
			return msg
		}
	}
	if len(raw) > 0 {
		return strings.TrimSpace(string(raw))
	}
	return "request failed"
}

// crudFieldErrors extracts per-field validation errors (first message per
// field) from a CrudHandler error body.
func crudFieldErrors(raw []byte) map[string]string {
	var e struct {
		Fields map[string][]string `json:"fields"`
	}
	out := map[string]string{}
	if json.Unmarshal(raw, &e) == nil {
		for field, msgs := range e.Fields {
			if len(msgs) > 0 {
				out[field] = strings.Join(msgs, ", ")
			}
		}
	}
	return out
}

func singular(name string) string { return strings.TrimSuffix(name, "s") }

// titleCase upper-cases the first rune (for nav labels). ASCII-simple; entity
// names are identifiers, not prose.
func titleCase(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// ----- flash store ----------------------------------------------------------

// formFlash carries a failed submission back to the re-rendered form.
type formFlash struct {
	values    map[string]string
	fieldErrs map[string]string
	general   string
	created   time.Time
}

// flashStore holds short-lived form re-render payloads keyed by an opaque
// token carried in the redirect URL. Entries are one-shot (popped on read) and
// expire after flashTTL.
type flashStore struct {
	mu   sync.Mutex
	data map[string]*formFlash
}

const flashTTL = 2 * time.Minute

func newFlashStore() *flashStore { return &flashStore{data: map[string]*formFlash{}} }

func (s *flashStore) put(f *formFlash) string {
	f.created = time.Now()
	tok := randToken()
	s.mu.Lock()
	defer s.mu.Unlock()
	// Opportunistic GC so the map can't grow unbounded under abandoned flashes.
	for k, v := range s.data {
		if time.Since(v.created) > flashTTL {
			delete(s.data, k)
		}
	}
	s.data[tok] = f
	return tok
}

func (s *flashStore) pop(tok string) *formFlash {
	if tok == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	f := s.data[tok]
	delete(s.data, tok)
	if f == nil || time.Since(f.created) > flashTTL {
		return nil
	}
	return f
}

func randToken() string {
	var buf [16]byte
	_, _ = rand.Read(buf[:])
	return hex.EncodeToString(buf[:])
}
