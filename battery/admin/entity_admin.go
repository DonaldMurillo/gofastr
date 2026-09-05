package admin

// Entity CRUD admin, rendered THROUGH the app's mounted UI host so the screens
// hydrate with runtime.js: the list is a DataTable island (sort/paginate via
// RPC, no reload), delete is a `data-fui-confirm` + `data-fui-rpc` button, and
// forms are server-rendered. Every read/write is an in-process call into the
// entity's OWN CrudHandler with the caller's context forwarded, so validation,
// owner/tenant scoping, hooks, and events all apply exactly as on the JSON API.
// The admin never re-implements CRUD/pagination/filter logic.

import (
	"context"
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

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
// no UI host is mounted it errors, the entity admin cannot work without one.
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
	// Standalone: the admin ships its OWN full shell, so the host App's default
	// layout (often the app's own sidebar) must NOT wrap it, otherwise the
	// back-office renders a double sidebar.
	layout := appui.NewLayout("admin").WithSidebar(b.adminSidebar(ents))
	group := appui.NewScreenGroup(b.cfg.PathPrefix+"/e", layout, b.gatePolicy()).Standalone()
	for _, ent := range ents {
		b.registerEntityScreens(group, ent)
		b.registerEntityRoutes(ent)
	}
	b.screens.Router.ScreenGroup(group)

	// Mount the mobile nav drawer ONCE (the framework's proven responsive-nav
	// pattern, the same one the docs/components site uses): on < 900px the
	// SectionMenu shows a trigger button that opens this slide-in sheet
	// (backdrop, click-outside / Esc close, focus trap, scroll lock, all from
	// preset.Drawer, none re-implemented). On ≥ 900px it's a sticky rail.
	// Mounted behind the SAME authorizer as every other admin surface.
	// widget.MountBuilder registers its own routes, so mounting on the bare
	// router left the drawer's /chrome endpoint unauthenticated -- and that
	// chrome IS the back-office entity map, one anonymous GET away. The
	// package contract is "There is no unauthenticated or self-service
	// path"; a nav drawer is not an exception to it.
	widget.MountBuilder(b.router.Group("", b.gateMiddleware()),
		interactive.SectionMenuDrawer(b.navConfig(ents)))
	return nil
}

// adminSidebar builds the admin nav as an interactive.SectionMenu, a sticky
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
	// paths (/new, /edit/{id}), a Go ServeMux pattern that matches a path on
	// the wrong method returns 405, which would otherwise shadow the screens
	// served by the host's catch-all.
	b.router.Get(base+"/_rows", b.gate(b.entityRows(ent)))
	b.router.Post(base+"/_create", b.gate(b.entitySave(ent, false)))
	b.router.Post(base+"/_update/{id}", b.gate(b.entitySave(ent, true)))
	b.router.Delete(base+"/_delete/{id}", b.gate(b.entityDelete(ent)))
}

// entitiesToExpose resolves the entities to surface. Explicit Config.Entities
// wins (in order, unknown names skipped). AllEntities exposes every registered
// entity whose CRUD is enabled (credential tables shipped CRUD=false stay
// hidden). Neither set → nothing: an admin must name what it manages rather
// than default every table to an editable back-office.
func (b *Battery) entitiesToExpose() []*entity.Entity {
	// Use All() which resolves one entity per name (unversioned, or sole
	// version, or lex-first when multiple versions exist). This prevents
	// duplicate admin screens for versioned entities that share a table.
	byName := b.registry.All()
	if len(b.cfg.Entities) > 0 {
		out := make([]*entity.Entity, 0, len(b.cfg.Entities))
		seenTable := make(map[string]bool)
		for _, name := range b.cfg.Entities {
			ent, ok := byName[name]
			if !ok || seenTable[ent.GetTable()] {
				continue
			}
			seenTable[ent.GetTable()] = true
			out = append(out, ent)
		}
		return out
	}
	if !b.cfg.AllEntities {
		return nil
	}
	// Deduplicate by table: one admin screen per table, not per version.
	seenTable := make(map[string]bool)
	var out []*entity.Entity
	for _, ent := range b.registry.AllSorted() {
		if !crudEnabled(ent) {
			continue
		}
		table := ent.GetTable()
		if seenTable[table] {
			continue
		}
		seenTable[table] = true
		out = append(out, ent)
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
	return ent.Config.Exposure.CRUD == nil || *ent.Config.Exposure.CRUD
}

// ----- CrudHandler proxy ----------------------------------------------------

// crudFor builds the app's canonical CrudHandler for ent, preserving the
// host's JSON casing so lifecycle hooks and audit redactors receive the same
// payload shape for admin and public requests.
func (b *Battery) crudFor(ent *entity.Entity) (*crud.CrudHandler, error) {
	// Start from App's canonical handler so audit/lifecycle hooks, events,
	// storage, outbox, and registry wiring match the public JSON routes. A
	// fresh crud.NewCrudHandler has Hooks=nil and silently bypasses all of it.
	// CrudHandlerForEntity takes the entity directly, avoids the name-based
	// Registry.Get lookup that fails for multi-version entities.
	ch, err := b.app.CrudHandlerForEntity(ent)
	if err != nil {
		// A wiring failure (no DB, entity not the registered instance) is
		// DETERMINISTIC, it fails the same way every request. Panicking per
		// request (the old behaviour) just hands the noise to Recovery and
		// logs a stack trace on every hit. Return the error and let each call
		// site render its normal failure shape.
		return nil, err
	}
	// Preserve Config.DB's documented override for admin entity operations.
	ch.DB = b.db
	return ch, nil
}

// crudFor500 renders a 500 for a crudFor wiring failure at an HTTP boundary.
// The real error is logged (driver/wiring text can leak schema, DSNs, or
// paths, so it never goes in the body); the response carries a generic,
// operator-facing message in the style of the other admin 500s.
func (b *Battery) crudFor500(w http.ResponseWriter, ent *entity.Entity, err error) {
	b.logger().Error("admin: crud handler unavailable", "entity", ent.GetName(), "error", err)
	http.Error(w, ent.GetName()+" admin is unavailable; check server logs", http.StatusInternalServerError)
}

// field names at the admin boundary. Admin screens index rows by schema field
// name; hooks and redactors must still observe the host's configured JSONCase.
func adminResponseKeys(ent *entity.Entity, ch *crud.CrudHandler) map[string]string {
	if ch.JSONCase == crud.CaseSnake {
		return nil
	}
	fields := ent.GetFields()
	keys := make(map[string]string, len(fields))
	for _, field := range fields {
		keys[adminJSONKey(field.Name)] = field.Name
	}
	return keys
}

func adminJSONKey(field string) string {
	parts := strings.Split(field, "_")
	for i := 1; i < len(parts); i++ {
		if parts[i] != "" {
			parts[i] = strings.ToUpper(parts[i][:1]) + parts[i][1:]
		}
	}
	return strings.Join(parts, "")
}

func adminResponseRow(row map[string]any, keys map[string]string) map[string]any {
	if keys == nil {
		return row
	}
	out := make(map[string]any, len(row))
	for key, value := range row {
		if field, ok := keys[key]; ok {
			key = field
		}
		out[key] = value
	}
	return out
}

// callCrud invokes a CrudHandler http.HandlerFunc in-process, forwarding the
// parent request's context, connection address, and headers. Context preserves
// user/tenant scoping; request metadata keeps audit hooks equivalent to public
// writes instead of recording httptest's synthetic defaults.
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
	req.Header = parent.Header.Clone()
	req.RemoteAddr = parent.RemoteAddr
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
		// timestamp columns, they clutter the grid and are rarely the thing
		// you scan a list for.
		if f.Hidden || f.Name == "id" || isTimestampCol(f.Name) {
			continue
		}
		cols = append(cols, f.Name)
	}
	return cols
}

// sortableColumns is listColumns minus the NoQuery fields. A NoQuery column
// is shown in the grid (it is in the response, just masked) but the filter
// parser rejects ?sort= on it with a 400, so rendering its header as
// sortable would hand the user a link that breaks the whole list page.
func sortableColumns(ent *entity.Entity) []string {
	noQuery := noQueryColumns(ent)
	cols := listColumns(ent)
	out := cols[:0:0]
	for _, c := range cols {
		if !noQuery[c] {
			out = append(out, c)
		}
	}
	return out
}

// noQueryColumns is the set of column names the query surface refuses.
func noQueryColumns(ent *entity.Entity) map[string]bool {
	var set map[string]bool
	for _, f := range ent.GetFields() {
		if f.NoQuery {
			if set == nil {
				set = map[string]bool{}
			}
			set[f.Name] = true
		}
	}
	return set
}

// listRows runs the CrudHandler List with the given query and returns the
// decoded rows plus the total count for pagination.
func (b *Battery) listRows(ctx context.Context, ent *entity.Entity, query string) (rows []map[string]any, total int, err error) {
	ch, err := b.crudFor(ent)
	if err != nil {
		return nil, 0, err
	}
	// Screen renders bypass b.gate, so inject the admin superuser policy here too.
	// The admin reads every entity it manages, regardless of per-entity access RBAC.
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
	keys := adminResponseKeys(ent, ch)
	for i := range env.Data {
		env.Data[i] = adminResponseRow(env.Data[i], keys)
	}
	return env.Data, env.Total, nil
}

// getRow loads a single record by id (owner-scoped via ctx).
func (b *Battery) getRow(ctx context.Context, ent *entity.Entity, id string) (map[string]any, error) {
	ch, err := b.crudFor(ent)
	if err != nil {
		return nil, err
	}
	code, raw := callCrudCtx(adminSuperuserCtx(ctx), ch.Get(), http.MethodGet, "", id, "")
	if code != http.StatusOK {
		return nil, fmt.Errorf("get returned %d", code)
	}
	var response struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(raw, &response); err != nil {
		return nil, err
	}
	return adminResponseRow(response.Data, adminResponseKeys(ent, ch)), nil
}

// getRowForEdit loads a single record for the EDIT form, and reports which of
// its columns an AfterGet hook masks.
//
// Two failures meet on this form and only one fix clears both. Prefilling from
// a hooked read (what the detail screen does) writes the MASK back over the
// stored column, because the form posts every input on submit. Prefilling from
// a raw read shows an admin a value the API masks, the disclosure this
// release exists to close, aimed at the one reader who can see every row.
//
// So: read twice, diff, and treat the columns that differ as write-only. The
// form renders them empty with a placeholder, and formToJSON already omits an
// empty value, so leaving one alone updates nothing while typing into it
// replaces the stored value. It is the password-field pattern, applied to
// whatever the app decided to mask.
//
// The masked set comes from comparing the two reads rather than from NoQuery,
// because those answer different questions: NoQuery is about the query
// surface, and an app may mask a column it never marked.
func (b *Battery) getRowForEdit(ctx context.Context, ent *entity.Entity, id string) (map[string]any, map[string]bool, error) {
	ch, err := b.crudFor(ent)
	if err != nil {
		return nil, nil, err
	}
	sctx := adminSuperuserCtx(ctx)
	raw, err := ch.GetOne(sctx, id, nil)
	if err != nil {
		return nil, nil, err
	}
	keys := adminResponseKeys(ent, ch)
	rawRow := adminResponseRow(raw, keys)
	return rawRow, b.maskedFields(ctx, ent, id, rawRow, keys), nil
}

// maskedFields reports which of an entity's editable columns an AfterGet hook
// rewrites, by comparing the stored row against the hooked one.
//
// Split out from getRowForEdit because the SAVE path needs the same answer:
// the form omits masked columns, so the handler has to know which submitted
// blanks mean "unchanged" rather than "clear this". Recomputing it server-side
// rather than round-tripping it through a hidden input keeps it authoritative.
func (b *Battery) maskedFields(ctx context.Context, ent *entity.Entity, id string, rawRow map[string]any, keys map[string]string) map[string]bool {
	masked := map[string]bool{}
	ch, err := b.crudFor(ent)
	if err != nil {
		// Unreachable via the live callers (getRowForEdit / maskedFieldsForID
		// already failed at their own crudFor), but honour maskedFields'
		// "must not take the form down" contract: treat every editable column
		// as masked so the admin retypes rather than sees a prefilled secret.
		for _, f := range editableFields(ent) {
			masked[f.Name] = true
		}
		return masked
	}
	sctx := adminSuperuserCtx(ctx)
	// A hook failure here must not take the form down: it means we cannot
	// prove a column is safe to prefill, so treat every editable column as
	// masked and let the admin retype what they mean to change.
	hooked, hookErr := ch.GetOne(crud.WithReadHooks(sctx), id, nil)
	switch {
	case hookErr != nil:
		for _, f := range editableFields(ent) {
			masked[f.Name] = true
		}
	default:
		// Diff the rows AFTER adminResponseRow, so the keys are the schema
		// names editableFields yields. GetOne returns JSON casing, and keying
		// this set on `cardNumber` while the form looks up `card_number` would
		// mark nothing masked on exactly the multi-word columns, invisible in
		// any single-word test.
		hookedRow := adminResponseRow(hooked, keys)
		for k, v := range rawRow {
			if h, ok := hookedRow[k]; !ok || !sameStoredValue(v, h) {
				masked[k] = true
			}
		}
	}
	return masked
}

// maskedFieldsForID recomputes the masked set on the SAVE path, where there is
// no already-loaded row to diff against.
//
// A failure here refuses the save (entitySave flashes and redirects back)
// rather than degrading the way the read path's maskedFields does: on a read,
// "treat every editable column as masked" only hides prefill, but a save that
// guesses the masked set wrong emits blank booleans over stored columns — the
// is_admin-shaped write that cannot be taken back.
func (b *Battery) maskedFieldsForID(ctx context.Context, ent *entity.Entity, id string) (map[string]bool, error) {
	if id == "" {
		return nil, nil // create: nothing is stored yet, so nothing is masked
	}
	ch, err := b.crudFor(ent)
	if err != nil {
		return nil, err
	}
	raw, err := ch.GetOne(adminSuperuserCtx(ctx), id, nil)
	if err != nil {
		return nil, err
	}
	keys := adminResponseKeys(ent, ch)
	return b.maskedFields(ctx, ent, id, adminResponseRow(raw, keys), keys), nil
}

// sameStoredValue compares two reads of the same column. It is a value
// comparison, not an identity one: the two GetOne calls scan independently, so
// equal columns are equal values in different allocations.
func sameStoredValue(a, b any) bool {
	return reflect.DeepEqual(a, b)
}

// ----- write handlers (explicit routes) -------------------------------------

// entitySave handles create (edit=false) and update (edit=true). On success it
// 303-redirects to the list. On a validation error it stashes the submitted
// values + field errors in a short-lived flash and redirects back to the form,
// so the re-render is a full host-rendered page (chrome + runtime).
func (b *Battery) entitySave(ent *entity.Entity, edit bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !parseCappedForm(w, r) {
			return
		}
		id := ""
		if edit {
			id = r.PathValue("id")
		}
		// Recompute server-side rather than trusting a hidden input: the form
		// omits masked columns, so the handler must know which blanks mean
		// "unchanged". A recompute that cannot decide refuses the save outright:
		// guessing the masked set wrong writes blank booleans over stored
		// columns, and a refused save is retryable while a flipped is_admin is
		// not.
		masked, err := b.maskedFieldsForID(r.Context(), ent, id)
		if err != nil {
			b.logger().Error("admin: masked-field recompute failed, refusing save",
				"entity", ent.GetName(), "id", url.PathEscape(id), "error", err)
			vals := map[string]string{}
			for _, f := range editableFields(ent) {
				vals[f.Name] = r.PostForm.Get(f.Name)
			}
			token := b.setFlash(w, r, &formFlash{Values: vals,
				General: "Could not re-verify which fields are write-only, so nothing was saved. Try the save again."})
			dest := b.entityBase(ent) + "/new"
			if edit {
				dest = b.entityBase(ent) + "/edit/" + url.PathEscape(id)
			}
			http.Redirect(w, r, dest+"?e="+token, http.StatusSeeOther)
			return
		}
		body, formErrs := formToJSON(ent, r, masked)
		if len(formErrs) > 0 {
			// A field-level coercion failure (e.g. "abc" in an Int field) used
			// to be silently dropped by formToJSON, creating the row with the
			// field omitted. Surface it exactly like a CrudHandler validation
			// error: stash the submitted values + field errors and redirect
			// back to the form so the field is named, not silently accepted.
			vals := map[string]string{}
			for _, f := range editableFields(ent) {
				vals[f.Name] = r.PostForm.Get(f.Name)
			}
			token := b.setFlash(w, r, &formFlash{Values: vals, FieldErrs: formErrs, General: "Please correct the highlighted fields."})
			dest := b.entityBase(ent) + "/new"
			if edit {
				dest = b.entityBase(ent) + "/edit/" + url.PathEscape(id)
			}
			http.Redirect(w, r, dest+"?e="+token, http.StatusSeeOther)
			return
		}
		ch, err := b.crudFor(ent)
		if err != nil {
			b.crudFor500(w, ent, err)
			return
		}
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
		token := b.setFlash(w, r, &formFlash{Values: vals, FieldErrs: crudFieldErrors(raw), General: crudErrorMessage(raw)})
		dest := b.entityBase(ent) + "/new"
		if edit {
			dest = b.entityBase(ent) + "/edit/" + url.PathEscape(id)
		}
		http.Redirect(w, r, dest+"?e="+token, http.StatusSeeOther)
	}
}

// entityDelete handles the DELETE RPC fired by the confirm button. It deletes
// then returns the refreshed table fragment (200), the delete button binds the
// list's island signal, so the runtime swaps the response in place. Returning
// the fragment unconditionally (rather than a non-2xx on a scope miss) keeps the
// signal value valid HTML; a row the caller can't delete simply isn't in their
// re-rendered list anyway.
func (b *Battery) entityDelete(ent *entity.Entity) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ch, err := b.crudFor(ent)
		if err != nil {
			b.crudFor500(w, ent, err)
			return
		}
		code, _ := callCrud(r, ch.Delete(), http.MethodDelete, "", r.PathValue("id"), "")
		// The island-swap contract forces an HTML response (the runtime swaps
		// whatever this returns into the list's signal node), so a failed
		// delete can't become a non-2xx, that would blank the grid. But the
		// result must not be discarded either: a genuine server error (5xx) is
		// logged so it can't vanish silently. A 4xx (scope miss / not found)
		// is the documented benign case, the row is simply absent from the
		// caller's re-rendered list.
		if code >= http.StatusInternalServerError {
			// The id is request-borne and percent-decoded: escape it so
			// control bytes cannot forge lines in the operator's tail.
			b.logger().Error("admin: delete failed",
				"entity", ent.GetName(), "id", url.PathEscape(r.PathValue("id")), "status", code)
		}
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
func formToJSON(ent *entity.Entity, r *http.Request, masked map[string]bool) (string, map[string]string) {
	obj := map[string]any{}
	fieldErrs := map[string]string{}
	for _, f := range editableFields(ent) {
		raw := strings.TrimSpace(r.PostForm.Get(f.Name))
		// A masked column renders empty and is write-only: blank means "leave
		// the stored value alone". This has to run BEFORE the type switch,
		// because schema.Bool emits unconditionally, an unchecked box is
		// indistinguishable from an absent one, so a masked bool was written
		// back as false on every save, silently flipping exactly the
		// is_admin / is_active columns an app is most likely to mask.
		if raw == "" && masked[f.Name] {
			continue
		}
		switch f.Type {
		case schema.Bool:
			obj[f.Name] = raw == "on" || raw == "true" || raw == "1"
		case schema.Int:
			if raw != "" {
				if n, err := strconv.ParseInt(raw, 10, 64); err == nil {
					obj[f.Name] = n
				} else {
					// Don't silently drop a non-numeric value (it would create
					// the row with the field omitted → defaulted to zero).
					fieldErrs[f.Name] = "must be a whole number"
				}
			}
		case schema.Float:
			if raw != "" {
				if x, err := strconv.ParseFloat(raw, 64); err == nil {
					obj[f.Name] = x
				} else {
					fieldErrs[f.Name] = "must be a number"
				}
			}
		default:
			if raw != "" {
				obj[f.Name] = raw
			}
		}
	}
	out, _ := json.Marshal(obj)
	return string(out), fieldErrs
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

// ----- flash cookie ----------------------------------------------------------

// formFlash carries a failed submission back to the re-rendered form. It
// travels in a short-lived HMAC-signed cookie (the sessions pattern: signed
// with a key derived from the app secret), so ANY replica holding the secret
// renders a redirect issued by any other and no server RAM is involved.
type formFlash struct {
	Values    map[string]string `json:"v"`
	FieldErrs map[string]string `json:"f"`
	General   string            `json:"g"`
	Created   int64             `json:"c"` // unix seconds; TTL-checked on read
	Token     string            `json:"t"` // must match the ?e= query token on read
}

const (
	// flashTTL bounds the flash cookie's life (Max-Age on write, age check
	// on read; the signed timestamp wins over a doctored Max-Age).
	flashTTL = 2 * time.Minute

	// flashCookieName is the signed cookie carrying the form flash across
	// the PRG redirect.
	flashCookieName = "gofastr_admin_flash"

	// maxFlashBytes caps the signed flash payload. A larger flash drops
	// the submitted values first, then the field errors, keeping the
	// general error (truncated as the last resort) so the re-rendered
	// form always names WHY the save was refused.
	maxFlashBytes = 4 << 10 // 4 KiB

	// flashMACContext domain-separates the flash MAC (and its HKDF info
	// string) from any other use of the same app secret.
	flashMACContext = "gofastr-admin-flash-v1"

	// minFlashSecretLen is the floor below which Config.Secret is treated
	// as unset (self-minted per-boot key + one Warn): a too-short secret
	// weakens nothing else but would give the flash a false sense of
	// cross-replica support.
	minFlashSecretLen = 16
)

// flashSigningKey returns the HMAC key for the flash cookie, derived from
// Config.Secret (the same value as framework.WithSecret / GOFASTR_SECRET)
// via HKDF-SHA256. With no usable secret it self-mints a per-boot key — the
// single-replica fallback, the same posture as the uihost session key — and
// says so once: a multi-replica deployment that forgot the secret loses the
// flash exactly where it would also lose sessions, the failure the scaling
// docs already describe.
func (b *Battery) flashSigningKey() []byte {
	b.flashKeyOnce.Do(func() {
		if len(b.cfg.Secret) >= minFlashSecretLen {
			if k, err := hkdf.Key(sha256.New, []byte(b.cfg.Secret), nil, flashMACContext, 32); err == nil {
				b.flashKey = k
				return
			}
		}
		var k [32]byte
		if _, err := rand.Read(k[:]); err != nil {
			panic(fmt.Sprintf("admin: crypto/rand failed while minting the flash signing key: %v", err))
		}
		b.flashKey = k[:]
		b.logger().Warn("admin: Config.Secret is not set; the form-flash cookie signs with a per-boot key, so a redirect issued by one replica renders WITHOUT its flash on another. Set admin.Config.Secret to the same value as framework.WithSecret / GOFASTR_SECRET on every replica.")
	})
	return b.flashKey
}

func (b *Battery) setFlash(w http.ResponseWriter, r *http.Request, f *formFlash) string {
	f.Created = time.Now().Unix()
	f.Token = randToken()
	payload := marshalFlashCapped(f)
	mac := hmac.New(sha256.New, b.flashSigningKey())
	mac.Write([]byte(flashMACContext))
	mac.Write(payload)
	http.SetCookie(w, &http.Cookie{
		Name:     flashCookieName,
		Value:    base64.RawURLEncoding.EncodeToString(payload) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)),
		Path:     b.cfg.PathPrefix,
		MaxAge:   int(flashTTL.Seconds()),
		HttpOnly: true,
		// Secure everywhere except a plaintext loopback dev origin, the
		// uihost session-cookie posture: the flash carries the operator's
		// submitted values, so it must never ride a plaintext non-loopback
		// origin where a network observer could read it.
		Secure:   flashCookieSecure(r),
		SameSite: http.SameSiteLaxMode,
	})
	return f.Token
}

// flashCookieSecure mirrors the uihost's useSecureSessionCookie: Secure on
// for TLS (directly or via X-Forwarded-Proto) and for every non-loopback
// origin, off only for plaintext loopback dev, where a Secure cookie can't
// round-trip.
func flashCookieSecure(r *http.Request) bool {
	if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		return true
	}
	host := r.Host
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.TrimPrefix(strings.TrimSuffix(host, "]"), "[")
	switch strings.ToLower(host) {
	case "localhost", "127.0.0.1", "::1":
		return false
	}
	return true
}

// readFlash verifies and decodes the flash cookie for the given ?e= token.
// Fail-closed on every anomaly (absent, oversized, malformed, bad MAC,
// expired, token mismatch): an unverifiable flash renders an empty form,
// never an attacker-chosen one.
func (b *Battery) readFlash(r *http.Request, token string) *formFlash {
	if token == "" {
		return nil
	}
	c, err := r.Cookie(flashCookieName)
	if err != nil || c.Value == "" {
		return nil
	}
	payloadB64, macB64, ok := strings.Cut(c.Value, ".")
	if !ok {
		return nil
	}
	// DecodeString with a length check BEFORE parsing anything: a giant
	// cookie is refused without allocating a buffer for it.
	if len(payloadB64) > base64.RawURLEncoding.EncodedLen(maxFlashBytes) {
		return nil
	}
	payload, err := base64.RawURLEncoding.DecodeString(payloadB64)
	if err != nil || len(payload) > maxFlashBytes {
		return nil
	}
	mac, err := base64.RawURLEncoding.DecodeString(macB64)
	if err != nil {
		return nil
	}
	h := hmac.New(sha256.New, b.flashSigningKey())
	h.Write([]byte(flashMACContext))
	h.Write(payload)
	if !hmac.Equal(mac, h.Sum(nil)) {
		return nil
	}
	var f formFlash
	if json.Unmarshal(payload, &f) != nil {
		return nil
	}
	if subtle.ConstantTimeCompare([]byte(f.Token), []byte(token)) != 1 {
		return nil
	}
	if time.Since(time.Unix(f.Created, 0)) > flashTTL {
		return nil
	}
	return &f
}

// marshalFlashCapped serialises f within maxFlashBytes, degrading in the
// documented order: submitted values first, then field errors, keeping the
// general error (halved on a rune boundary until it fits) so the re-render
// always names the failure. The empty flash always fits, so the loop
// terminates.
func marshalFlashCapped(f *formFlash) []byte {
	for {
		b, err := json.Marshal(f)
		if err == nil && len(b) <= maxFlashBytes {
			return b
		}
		switch {
		case f.Values != nil:
			f.Values = nil
		case f.FieldErrs != nil:
			f.FieldErrs = nil
		case len(f.General) > 0:
			cut := len(f.General) / 2
			for cut > 0 && !utf8.ValidString(f.General[:cut]) {
				cut--
			}
			f.General = f.General[:cut]
		default:
			return []byte("{}")
		}
	}
}

func randToken() string {
	var buf [16]byte
	_, _ = rand.Read(buf[:])
	return hex.EncodeToString(buf[:])
}
