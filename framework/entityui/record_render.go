package entityui

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"path"
	"slices"
	"strings"

	appui "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/interactive"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/access"
	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

// render draws the record or create screen: the read gates, the header
// with its actions, and the tabbed body (Edit, Related, extension tabs,
// Activity). A bad name anywhere in the builder fails this slot.
func (b *RecordBuilder) render(ctx context.Context) (render.HTML, error) {
	m, err := b.ui.meta(b.entity)
	if err != nil {
		return "", err
	}
	if err := b.check(m); err != nil {
		return "", err
	}
	// The app's translator rides the context: every chrome string the
	// screen draws (and every string an extension tab draws through
	// i18nui.T) resolves through it.
	ctx = i18nui.WithTranslator(ctx, m.tr)
	base := b.base
	if base == "" {
		base = requestBase(ctx)
	}
	if b.id == "" {
		return b.createScreen(ctx, m, base), nil
	}
	return b.recordScreen(ctx, m, base)
}

// check validates the builder's own names against the entity: Omit,
// Tab keys, Related entities and a Form that replaces Display.Form. A
// bad name fails the slot, the way entityui.New fails the app at boot.
func (b *RecordBuilder) check(m *meta) error {
	for _, name := range b.omit {
		if _, ok := m.field(name); !ok {
			return fmt.Errorf("entityui: record %q: Omit names unknown field %q", m.name, name)
		}
	}
	seen := map[string]bool{}
	for _, t := range b.tabs {
		if !entity.ValidKey(t.Key) {
			return fmt.Errorf("entityui: record %q: tab key %q is not a key", m.name, t.Key)
		}
		if isBuiltinTab(t.Key) {
			return fmt.Errorf("entityui: record %q: tab key %q is built in", m.name, t.Key)
		}
		if seen[t.Key] {
			return fmt.Errorf("entityui: record %q: tab key %q repeats", m.name, t.Key)
		}
		seen[t.Key] = true
		for _, xt := range m.ext.Tabs {
			if xt.Key == t.Key {
				return fmt.Errorf("entityui: record %q: tab key %q collides with an extension tab", m.name, t.Key)
			}
		}
	}
	for _, rl := range b.related {
		if err := b.checkRelated(m, rl.name); err != nil {
			return err
		}
	}
	if b.form != nil {
		if err := checkForm(m, b.form); err != nil {
			return fmt.Errorf("entityui: record %q: %w", m.name, err)
		}
	}
	return nil
}

// checkRelated reports whether name is a registered entity holding a
// relation field that points at this one: the Related tab lists that
// entity's rows whose foreign key holds this record's id.
func (b *RecordBuilder) checkRelated(m *meta, name string) error {
	other, err := b.ui.entityFor(name)
	if err != nil {
		return fmt.Errorf("entityui: record %q: Related: %v", m.name, err)
	}
	if _, fk := relationTo(other, m.name); fk == "" {
		return fmt.Errorf("entityui: record %q: Related entity %q has no relation pointing at %q", m.name, name, m.name)
	}
	return nil
}

// relationTo finds other's relation that points at target: a BelongsTo
// on the child side or a HasMany/HasOne declared on the parent, both
// carry the foreign key column on other's table.
func relationTo(other *entity.Entity, target string) (rel entity.Relation, fk string) {
	for _, r := range other.Config.Relations {
		if r.Entity == target && r.ForeignKey != "" {
			return r, r.ForeignKey
		}
	}
	return entity.Relation{}, ""
}

// requestBase is the record's default base path: the current request
// path with its last segment removed, so /admin/entities/invoices/42
// bases at /admin/entities/invoices.
func requestBase(ctx context.Context) string {
	r := appui.RequestFromContext(ctx)
	if r == nil || r.URL == nil || r.URL.Path == "" || r.URL.Path == "/" {
		return "/"
	}
	return path.Dir(r.URL.Path)
}

// currentURL is the page's own URL, path and query, the destination a
// transition's success navigates back to so the move is visible.
func currentURL(ctx context.Context) string {
	r := appui.RequestFromContext(ctx)
	if r == nil || r.URL == nil {
		return "/"
	}
	return r.URL.RequestURI()
}

// recordScreen draws one record: gates, header, tabs.
func (b *RecordBuilder) recordScreen(ctx context.Context, m *meta, base string) (render.HTML, error) {
	if !canRead(ctx, m.ch) {
		return accessDenied(ctx, m.plural(ctx)), nil
	}
	// The per-record gate is asked before any read, so a denied id
	// runs no query and no hook; it answers the same not-found body a
	// missing id does.
	if !canReadRecord(ctx, m.ch, b.id) {
		return m.notFound(ctx), nil
	}
	// WithReadHooks: the header and every display value show what an
	// AfterGet redaction shows, never the stored column.
	row, err := m.ch.GetOne(crud.WithReadHooks(ctx), b.id, nil)
	if err != nil || row == nil {
		return m.notFound(ctx), nil
	}
	// Deliberately NOT WithReadHooks: the edit form's inputs
	// round-trip on submit, so prefilling from a redacted read would
	// write the mask over the stored value. The diff of the two reads
	// also names the columns a hook rewrites, which the form renders
	// write-only (see maskedFields).
	raw, rerr := m.ch.GetOne(ctx, b.id, nil)
	if rerr != nil || raw == nil {
		return m.notFound(ctx), nil
	}
	masked := maskedFields(m, raw, row)

	header := b.header(ctx, m, row, base)
	if m.ext.Record != nil {
		body := contain(ctx, m.name, "record", func() (render.HTML, error) {
			c, err := m.ext.Record(RecordContext{Ctx: asCaller(ctx), UI: b.ui, Entity: m.name, Record: Record{ID: b.id, Values: row}})
			if err != nil {
				return "", err
			}
			if c == nil {
				return "", fmt.Errorf("record body is nil")
			}
			return renderComponent(ctx, c), nil
		})
		return render.Join(header, body), nil
	}
	body, err := b.tabbedBody(ctx, m, row, raw, masked, base)
	if err != nil {
		return "", err
	}
	// The override panel rides below the tabs, not inside one: it is an
	// operator action on the record, not a view of it.
	return render.Join(header, body, b.overridePanel(ctx, m, row, base)), nil
}

// notFound is the one answer for a missing id and an id the caller may
// not see: never say which of the two it was.
func (m *meta) notFound(ctx context.Context) render.HTML {
	return ui.EmptyState(ui.EmptyStateConfig{
		Title:        i18nui.T(ctx, i18nui.KeyEntityNotFound),
		Description:  i18nui.TVars(ctx, i18nui.KeyEntityNotFoundBody, map[string]string{"entity": m.singular(ctx)}),
		HeadingLevel: 1,
	})
}

// header draws the record's page header: the title, the singular
// eyebrow, the state badge, and the action row (moves, copy link,
// duplicate, delete, back).
func (b *RecordBuilder) header(ctx context.Context, m *meta, row map[string]any, base string) render.HTML {
	cfg := ui.PageHeaderConfig{Title: m.recordTitle(ctx, row), Eyebrow: m.singular(ctx)}
	if m.states != nil {
		if v := cell(rowValue(row, m.states.Field)); v != "" {
			cfg.Badge = ui.StatusBadge(ui.StatusBadgeConfig{
				Label:   m.valueLabel(ctx, m.states.Field, v),
				Variant: enumVariant(v),
			})
		}
	}
	cfg.Actions = ui.Cluster(ui.ClusterConfig{Gap: ui.GapSM, Align: ui.AlignCenter}, b.actions(ctx, m, row, base)...)
	return ui.PageHeader(cfg)
}

func (b *RecordBuilder) actions(ctx context.Context, m *meta, row map[string]any, base string) []render.HTML {
	var out []render.HTML
	if m.states != nil && canUpdate(ctx, m, b.id) {
		current := cell(rowValue(row, m.states.Field))
		for _, t := range m.states.Transitions {
			if t.System || !slices.Contains(t.From, current) {
				continue
			}
			// The checks the transition route runs: the entity's update
			// access (above), then the move's Permission by name, so a
			// caller who lacks it (a Wildcard role included) never sees
			// its button.
			if t.Permission != "" && !access.CanResourceExact(ctx, access.Permission(t.Permission), access.Ref{Type: m.name, ID: b.id}) {
				continue
			}
			label := i18nui.TransitionLabel(ctx, m.tr, m.name, t.Key, t.Label)
			variant, ok := ui.ParseButtonVariant(t.Variant)
			if !ok || variant == "" {
				variant = ui.ButtonSecondary
			}
			out = append(out, ui.Button(ui.ButtonConfig{
				Label:   label,
				Variant: variant,
				ExtraAttrs: interactive.Post(m.api + "/" + url.PathEscape(b.id) + "/transitions/" + url.PathEscape(t.Key)).
					OnSuccessToast(i18nui.TVars(ctx, i18nui.KeyEntityMoved, map[string]string{"entity": m.singular(ctx)})).
					OnSuccess(interactive.Navigate(currentURL(ctx))).
					OnErrorToast(i18nui.TVars(ctx, i18nui.KeyEntityMoveFailed, map[string]string{"action": strings.ToLower(label)})).
					Attrs(),
			}))
		}
	}
	out = append(out, b.appActions(ctx, m)...)
	if link := b.copyLink(ctx, m, base); link != "" {
		out = append(out, link)
	}
	if b.dup && !m.d.NoDuplicate && canCreate(ctx, m) {
		out = append(out, ui.LinkButton(ui.LinkButtonConfig{
			Label:   i18nui.T(ctx, i18nui.KeyEntityDuplicate),
			Href:    base + "/create?duplicate=" + url.QueryEscape(b.id),
			Variant: ui.ButtonSecondary,
		}))
	}
	if b.delete && canDelete(ctx, m, b.id) {
		singular := m.singular(ctx)
		out = append(out, ui.Button(ui.ButtonConfig{
			Label:   i18nui.T(ctx, i18nui.KeyEntityDelete),
			Variant: ui.ButtonDanger,
			ExtraAttrs: interactive.Delete(m.api + "/" + url.PathEscape(b.id)).
				WithConfirm(i18nui.TVars(ctx, i18nui.KeyEntityDeleteConfirm, map[string]string{"entity": singular})).
				OnSuccessToast(i18nui.TVars(ctx, i18nui.KeyEntityDeleted, map[string]string{"entity": singular})).
				OnSuccess(interactive.Navigate(base)).
				OnErrorToast(i18nui.TVars(ctx, i18nui.KeyEntityDeleteFailed, map[string]string{"entity": singular})).
				Attrs(),
		}))
	}
	out = append(out, ui.LinkButton(ui.LinkButtonConfig{
		Label:   i18nui.T(ctx, i18nui.KeyEntityBack),
		Href:    base,
		Variant: ui.ButtonGhost,
		Icon:    "chevron-left",
	}))
	return out
}

// appActions are the Extension actions the caller may run on this
// record, each a button posting the record scope to the bulk route: the
// same route, gates and audit as a bulk run over one record.
func (b *RecordBuilder) appActions(ctx context.Context, m *meta) []render.HTML {
	if !m.hasAPI {
		return nil
	}
	var out []render.HTML
	for _, act := range recordActions(m) {
		if !mayRun(ctx, m, act, b.id) {
			continue
		}
		variant := act.app.Variant
		if variant == "" {
			variant = ui.ButtonSecondary
		}
		body, err := json.Marshal(map[string]string{"action": act.key, "scope": bulkScopeRecord, "ids": b.id})
		if err != nil {
			continue
		}
		name := strings.ToLower(act.label)
		out = append(out, ui.Button(ui.ButtonConfig{
			Label:   act.label,
			Variant: variant,
			ExtraAttrs: interactive.Post(m.api + "/_bulk").WithBody(string(body)).
				OnSuccessToast(i18nui.TVars(ctx, i18nui.KeyEntityActionRan, map[string]string{"action": name})).
				OnSuccess(interactive.Navigate(currentURL(ctx))).
				OnErrorToast(i18nui.TVars(ctx, i18nui.KeyEntityMoveFailed, map[string]string{"action": name})).
				Attrs(),
		}))
	}
	return out
}

// copyLink renders the record's absolute URL as a hidden span plus the
// button that copies it. The span carries the kernel's visually-hidden
// utility class: present for the copy module, absent to the eye.
func (b *RecordBuilder) copyLink(ctx context.Context, m *meta, base string) render.HTML {
	r := appui.RequestFromContext(ctx)
	if r == nil {
		return ""
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	// The forwarded scheme counts only as an exact "http"/"https": the
	// header is request input, and a value like "https://evil.example/p"
	// would put another origin in the link the reader copies. Same gate
	// as uihost's resolveBaseURL.
	if p := r.Header.Get("X-Forwarded-Proto"); p == "http" || p == "https" {
		scheme = p
	}
	abs := scheme + "://" + r.Host + r.URL.Path
	id := "eui-rec-link"
	return render.Join(
		html.Span(html.TextConfig{ID: id, Class: "cui-visually-hidden"}, render.Text(abs)),
		ui.CopyButton(ui.CopyButtonConfig{Target: id, Label: i18nui.T(ctx, i18nui.KeyEntityCopyLink), Ctx: ctx}),
	)
}

// tab is one entry of the record's tab strip.
type tab struct {
	key   string
	label string
	build func() (render.HTML, error)
}

// tabbedBody draws the tab strip (query-param navigation) and the
// active tab's body. Only the active tab's body is built.
func (b *RecordBuilder) tabbedBody(ctx context.Context, m *meta, row, raw map[string]any, masked map[string]bool, base string) (render.HTML, error) {
	editTab := tab{key: "edit", label: i18nui.T(ctx, i18nui.KeyEntityTabEdit), build: func() (render.HTML, error) {
		return b.editTab(ctx, m, raw, row, masked, base), nil
	}}
	tabs := []tab{editTab}
	if len(b.related) > 0 {
		tabs = append(tabs, tab{key: "related", label: i18nui.T(ctx, i18nui.KeyEntityTabRelated), build: func() (render.HTML, error) {
			return b.relatedTab(ctx, m, base), nil
		}})
	}
	rec := Record{ID: b.id, Values: row}
	// Extension tabs, then the builder's own. A panicking, erroring or
	// empty tab fails that tab alone, never the page.
	for _, xt := range slices.Concat(m.ext.Tabs, b.tabs) {
		key, build := xt.Key, xt.Build
		label := xt.Label
		if label == "" {
			label = key
		}
		tabs = append(tabs, tab{key: key, label: label, build: func() (render.HTML, error) {
			return contain(ctx, m.name, "tab "+key, func() (render.HTML, error) {
				c, err := build(TabContext{Ctx: asCaller(ctx), UI: b.ui, Entity: m.name, Record: rec})
				if err != nil {
					return "", err
				}
				if c == nil {
					return "", fmt.Errorf("tab body is nil")
				}
				return renderComponent(ctx, c), nil
			}), nil
		}})
	}
	if b.activity && b.ui.host.Audit() != nil {
		tabs = append(tabs, tab{key: "activity", label: i18nui.T(ctx, i18nui.KeyEntityTabActivity), build: func() (render.HTML, error) {
			return b.activityTab(ctx, m), nil
		}})
	}
	if b.apiTab {
		tabs = append(tabs, tab{key: "api", label: i18nui.T(ctx, i18nui.KeyEntityTabApi), build: func() (render.HTML, error) {
			return b.apiTabBody(ctx, m, row), nil
		}})
	}

	// An unknown ?tab= shows Edit: the strip always names a real tab.
	active := 0
	if want := appui.QueryFromContext(ctx).Get("tab"); want != "" {
		for i, t := range tabs {
			if t.key == want {
				active = i
				break
			}
		}
	}

	// Each tab is its own URL (?tab=key on this record's path), so only
	// the active tab's body is built and drawn: a tab switch is a
	// navigation the client router intercepts.
	items := make([]ui.TabNavItem, len(tabs))
	for i, t := range tabs {
		items[i] = ui.TabNavItem{
			Text:    t.label,
			Href:    currentURLPath(ctx) + "?tab=" + url.QueryEscape(t.key),
			Current: i == active,
		}
	}
	body, err := tabs[active].build()
	if err != nil {
		return "", err
	}
	return ui.Stack(ui.StackConfig{},
		ui.TabNav(ui.TabNavConfig{
			ID:    "eui-" + m.name + "-tabs",
			Label: i18nui.T(ctx, i18nui.KeyEntityRecordSections),
			Items: items,
		}),
		body,
	), nil
}

// currentURLPath is the request path with its query stripped: the page
// a tab link re-opens with its own ?tab= param.
func currentURLPath(ctx context.Context) string {
	r := appui.RequestFromContext(ctx)
	if r == nil || r.URL == nil || r.URL.Path == "" {
		return "/"
	}
	return r.URL.Path
}

// renderComponent draws an extension component with the request
// context when it takes one, and through Render when it does not.
func renderComponent(ctx context.Context, c component.Component) render.HTML {
	if cc, ok := c.(component.ContextComponent); ok {
		return cc.RenderCtx(ctx)
	}
	return c.Render()
}

// checkForm re-runs the form's boot shape against this page's entity:
// every field names a visible one, a row holds one to three distinct
// fields, a field appears once across Main and Side, and sections nest
// at most two deep. App.Entity already refused this shape at boot; a
// builder-supplied form gets the same answer here, as a slot failure.
func checkForm(m *meta, f *entity.EntityForm) error {
	seen := map[string]string{}
	var walk func(items []entity.FormItem, depth int) error
	check := func(what, field string) error {
		if _, ok := m.field(field); !ok {
			return fmt.Errorf("%s names unknown or Hidden field %q", what, field)
		}
		if where, dup := seen[field]; dup {
			return fmt.Errorf("field %q appears twice (%s and %s)", field, where, what)
		}
		seen[field] = what
		return nil
	}
	walk = func(items []entity.FormItem, depth int) error {
		for _, it := range items {
			switch {
			case it.Field != "":
				if err := check("field "+it.Field, it.Field); err != nil {
					return err
				}
			case len(it.Row) > 0:
				if len(it.Row) > 3 {
					return fmt.Errorf("row holds %d fields, at most 3", len(it.Row))
				}
				rowSeen := map[string]bool{}
				for _, name := range it.Row {
					if rowSeen[name] {
						return fmt.Errorf("row repeats field %q", name)
					}
					rowSeen[name] = true
					if err := check("row", name); err != nil {
						return err
					}
				}
			case it.Section != "":
				if depth >= 2 {
					return fmt.Errorf("section %q nests past two deep", it.Section)
				}
				if !entity.ValidKey(it.Section) {
					return fmt.Errorf("section key %q is not a key", it.Section)
				}
				if err := walk(it.Items, depth+1); err != nil {
					return err
				}
			default:
				return fmt.Errorf("an item sets none of Field, Row or Section")
			}
		}
		return nil
	}
	if err := walk(f.Main, 0); err != nil {
		return err
	}
	return walk(f.Side, 0)
}
