package entityui

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"path"
	"slices"
	"strings"

	appui "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/interactive"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/access"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/filter"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
	"github.com/DonaldMurillo/gofastr/framework/ui"
	"github.com/DonaldMurillo/gofastr/internal/clientip"
	"github.com/DonaldMurillo/gofastr/internal/fieldtext"
)

func (b *RecordBuilder) render(ctx context.Context) (render.HTML, error) {
	m, err := b.ui.meta(b.entity)
	if err != nil {
		return "", err
	}
	if err := b.check(m); err != nil {
		return "", err
	}
	ctx = i18n.WithTranslator(ctx, m.tr)
	base := b.base
	if base == "" {
		n := len(m.keys)
		if b.id == "" {
			n = 1
		}
		base = requestBase(ctx, n)
	}
	if b.id == "" {
		return b.createScreen(ctx, m, base), nil
	}
	return b.recordScreen(ctx, m, base)
}

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

func (b *RecordBuilder) checkRelated(m *meta, name string) error {
	other, err := b.ui.entityFor(name)
	if err != nil {
		return fmt.Errorf("entityui: record %q: Related: %v", m.name, err)
	}
	if _, fks := relationTo(other, m.name); len(fks) == 0 {
		return fmt.Errorf("entityui: record %q: Related entity %q has no relation pointing at %q", m.name, name, m.name)
	}
	return nil
}

func relationTo(other *entity.Entity, target string) (rel entity.Relation, fks []string) {
	for _, r := range other.Config.Relations {
		if r.Entity == target && len(r.ForeignKey) > 0 {
			return r, r.ForeignKey
		}
	}
	return entity.Relation{}, nil
}

func relatedPins(fks []string, id string) ([]filter.Predicate, bool) {
	parts, err := entity.SplitKey(id, len(fks))
	if err != nil {
		return nil, false
	}
	pins := make([]filter.Predicate, len(fks))
	for i, fk := range fks {
		pins[i] = filter.Predicate{Field: fk, Op: filter.OpEq, Value: parts[i]}
	}
	return pins, true
}

func requestBase(ctx context.Context, n int) string {
	r := appui.RequestFromContext(ctx)
	if r == nil || r.URL == nil || r.URL.Path == "" || r.URL.Path == "/" {
		return "/"
	}
	p := path.Clean(r.URL.Path)
	for range n {
		p = path.Dir(p)
	}
	return p
}

func under(base, tail string) string {
	return strings.TrimSuffix(base, "/") + "/" + tail
}

func currentURL(ctx context.Context) string {
	r := appui.RequestFromContext(ctx)
	if r == nil || r.URL == nil {
		return "/"
	}
	return r.URL.RequestURI()
}

func (b *RecordBuilder) recordScreen(ctx context.Context, m *meta, base string) (render.HTML, error) {
	if !canRead(ctx, m.ch) {
		return accessDenied(ctx, m.plural(ctx), m.plural(ctx), 1), nil
	}
	if !canReadRecord(ctx, m.ch, b.id) {
		return m.notFound(ctx), nil
	}
	row, err := m.getRow(ctx, b.id)
	if err != nil || row == nil {
		return m.notFound(ctx), nil
	}
	raw, rerr := m.getRowUnhooked(ctx, b.id)
	if rerr != nil || raw == nil {
		return m.notFound(ctx), nil
	}
	masked := maskedFields(m, raw, row)

	if m.ext.Record != nil {
		header := b.header(ctx, m, row, base, false)
		body := contain(ctx, m.name, "record", func() (render.HTML, error) {
			cctx := asCaller(ctx)
			c, err := m.ext.Record(RecordContext{Ctx: cctx, UI: b.ui, Entity: m.name, Record: Record{ID: b.id, Values: row}})
			if err != nil {
				return "", err
			}
			if c == nil {
				return "", fmt.Errorf("record body is nil")
			}
			return renderComponent(cctx, c), nil
		})
		return render.Join(header, body), nil
	}
	body, active, err := b.tabbedBody(ctx, m, row, raw, masked, base)
	if err != nil {
		return "", err
	}
	header := b.header(ctx, m, row, base, active == "edit" && canUpdate(ctx, m, b.id))
	return render.Join(header, body, b.overridePanel(ctx, m, row, base)), nil
}

func (m *meta) notFound(ctx context.Context) render.HTML {
	appui.SetStatus(ctx, http.StatusNotFound)
	return ui.EmptyState(ui.EmptyStateConfig{
		Title:        i18nui.EntityNotFound.Text(ctx),
		Description:  i18nui.EntityNotFoundBody.Text(ctx, map[string]any{"entity": m.singular(ctx)}),
		HeadingLevel: 1,
	})
}

func (b *RecordBuilder) header(ctx context.Context, m *meta, row map[string]any, base string, save bool) render.HTML {
	title := b.ui.recordTitle(ctx, m, row)
	cfg := ui.PageHeaderConfig{Title: title, Subtitle: stampLine(ctx, m, row)}
	if m.states != nil {
		if v := fieldtext.Cell(m.rowValue(row, m.states.Field)); v != "" {
			cfg.Badge = ui.StatusBadge(ui.StatusBadgeConfig{
				Label:   m.valueLabel(ctx, m.states.Field, v),
				Variant: enumVariant(v),
			})
		}
	}
	actions := append(b.pageTools(ctx, base), b.actions(ctx, m, row, title, base, save)...)
	if save {
		actions = append(actions, saveButton(ctx, m, false))
	}
	cfg.Actions = ui.Cluster(ui.ClusterConfig{Gap: ui.GapSM, Align: ui.AlignCenter}, actions...)
	var prev, next string
	if b.steps != nil && inDrawer(ctx) {
		prev, next = b.ui.neighbours(ctx, m, b.steps, base, b.id)
	}
	return render.Join(drawerBar(ctx, prev, next), ui.PageHeader(cfg))
}

func inDrawer(ctx context.Context) bool {
	as, ok := appui.OverlayFromContext(ctx)
	return ok && as == appui.ScreenDrawer
}

func drawerBar(ctx context.Context, prev, next string) render.HTML {
	if !inDrawer(ctx) {
		return ""
	}
	p := currentURLPath(ctx)
	return ui.DrawerBar(ui.DrawerBarConfig{Path: p, CopyURL: absoluteURL(ctx), PageURL: p, Prev: prev, Next: next, Ctx: ctx})
}

func saveButton(ctx context.Context, m *meta, create bool) render.HTML {
	return ui.Button(ui.ButtonConfig{
		Ctx:             ctx,
		Label:           submitLabel(ctx, m, create),
		Type:            "submit",
		ID:              "eui-" + m.name + "-save",
		Shortcut:        "Mod+S",
		QuietUntilDirty: true,
		ExtraAttrs:      html.Attrs{"form": recordFormID(m)},
	})
}

func recordFormID(m *meta) string { return "eui-" + m.name + "-form" }

func stampLine(ctx context.Context, m *meta, row map[string]any) string {
	var parts []string
	for _, s := range []struct {
		col string
		key i18n.Phrase
	}{{"created_at", i18nui.EntityCreatedOn}, {"updated_at", i18nui.EntityUpdatedOn}} {
		f, ok := m.field(s.col)
		if !ok || f.Hidden {
			continue
		}
		if t, ok := parseTime(m.rowValue(row, s.col)); ok {
			parts = append(parts, s.key.Text(ctx, map[string]any{"date": i18n.Date(ctx, t, i18n.DateMedium)}))
		}
	}
	return strings.Join(parts, " · ")
}

func (b *RecordBuilder) actions(ctx context.Context, m *meta, row map[string]any, title, base string, save bool) []render.HTML {
	var out []render.HTML
	var danger []ui.MenuItem
	if m.states != nil && canUpdate(ctx, m, b.id) {
		current := fieldtext.Cell(m.rowValue(row, m.states.Field))
		for _, t := range m.states.Transitions {
			if t.System || !slices.Contains(t.From, current) {
				continue
			}
			if t.Permission != "" && !access.CanResourceExact(ctx, access.Permission(t.Permission), access.Ref{Type: m.name, ID: b.id}) {
				continue
			}
			label := i18nui.TransitionLabel(ctx, m.tr, m.name, t.Key, t.Label)
			variant, ok := ui.ParseButtonVariant(t.Variant)
			if !ok || variant == "" || (save && variant == ui.ButtonPrimary) {
				variant = ui.ButtonSecondary
			}
			act := interactive.Post(m.api + "/" + m.idPath(b.id) + "/transitions/" + url.PathEscape(t.Key)).
				OnSuccessToast(i18nui.EntityMoved.Text(ctx, map[string]any{"entity": m.singular(ctx)})).
				OnSuccess(interactive.Navigate(currentURL(ctx))).
				OnErrorToast(i18nui.EntityMoveFailed.Text(ctx, map[string]any{"action": strings.ToLower(label)}))
			if variant == ui.ButtonDanger {
				act = act.WithConfirmDialog(interactive.Confirm{
					Title: i18nui.EntityMoveConfirmTitle.Text(ctx, map[string]any{"action": label, "entity": m.noun(ctx, false)}),
					Message: i18nui.EntityMoveConfirm.Text(ctx, map[string]any{
						"from": m.valueLabel(ctx, m.states.Field, current),
						"to":   m.valueLabel(ctx, m.states.Field, t.To),
					}),
					Accept: i18nui.EntityMoveAccept.Text(ctx, map[string]any{"action": label, "entity": m.noun(ctx, false)}),
					Danger: true,
				})
				danger = append(danger, ui.MenuItem{Label: label, Danger: true, Do: &act})
				continue
			}
			out = append(out, ui.Button(ui.ButtonConfig{Ctx: ctx, Label: label, Variant: variant, ExtraAttrs: act.Attrs()}))
		}
	}
	buttons, dangerActs := b.appActions(ctx, m, title, save)
	out = append(out, buttons...)
	danger = append(danger, dangerActs...)
	if menu := b.menu(ctx, m, title, base, danger); menu != "" {
		out = append(out, menu)
	}
	return out
}

func (b *RecordBuilder) appActions(ctx context.Context, m *meta, title string, save bool) (out []render.HTML, danger []ui.MenuItem) {
	if !m.hasAPI {
		return nil, nil
	}
	for _, act := range recordActions(ctx, m) {
		if !mayRun(ctx, m, act, b.id) {
			continue
		}
		variant := act.app.Variant
		if variant == "" || (save && variant == ui.ButtonPrimary) {
			variant = ui.ButtonSecondary
		}
		body, err := json.Marshal(map[string]string{"action": act.key, "scope": bulkScopeRecord, "ids": b.id})
		if err != nil {
			continue
		}
		name := strings.ToLower(act.label)
		run := interactive.Post(m.api + "/_bulk").WithBody(string(body)).
			OnSuccessToast(i18nui.EntityActionRan.Text(ctx, map[string]any{"action": name})).
			OnSuccess(interactive.Navigate(currentURL(ctx))).
			OnErrorToast(i18nui.EntityMoveFailed.Text(ctx, map[string]any{"action": name}))
		if variant == ui.ButtonDanger {
			run = run.WithConfirmDialog(interactive.Confirm{
				Title:   i18nui.EntityActionConfirmTitle.Text(ctx, map[string]any{"action": act.label}),
				Message: i18nui.EntityActionConfirm.Text(ctx, map[string]any{"title": title}),
				Accept:  act.label,
				Danger:  true,
			})
			danger = append(danger, ui.MenuItem{Label: act.label, Danger: true, Do: &run})
			continue
		}
		out = append(out, ui.Button(ui.ButtonConfig{Ctx: ctx, Label: act.label, Variant: variant, ExtraAttrs: run.Attrs()}))
	}
	return out, danger
}

func (b *RecordBuilder) menu(ctx context.Context, m *meta, title, base string, danger []ui.MenuItem) render.HTML {
	var span render.HTML
	var items []ui.MenuItem
	if !inDrawer(ctx) && !b.panelPage(ctx) {
		span, items = b.copyLink(ctx)
	}
	if createPath := b.ui.createScreenPath(m.e, base); b.dup && createPath != "" && canCreate(ctx, m) {
		if !m.d.NoDuplicate {
			items = append(items, ui.MenuItem{
				Label: i18nui.EntityDuplicate.Text(ctx),
				Href:  createPath + "?duplicate=" + url.QueryEscape(b.id),
			})
		}
		items = append(items, ui.MenuItem{
			Label: i18nui.EntityCreateAnother.Text(ctx),
			Href:  createPath,
		})
	}
	apiSpan, apiItem := b.copyAPIURL(ctx, m)
	span = render.Join(span, apiSpan)
	items = append(items, apiItem...)
	if len(danger) > 0 {
		if len(items) > 0 {
			items = append(items, ui.MenuItem{Separator: true})
		}
		items = append(items, danger...)
	}
	if b.delete && canDelete(ctx, m, b.id) {
		del := deleteAction(ctx, m, b.id, base, b.undo)
		if len(items) > 0 && len(danger) == 0 {
			items = append(items, ui.MenuItem{Separator: true})
		}
		items = append(items, ui.MenuItem{Label: i18nui.EntityDelete.Text(ctx), Danger: true, Do: &del})
	}
	if len(items) == 0 {
		return ""
	}
	return render.Join(span, ui.Menu(ui.MenuConfig{
		Label:    i18nui.EntityRowActions.Text(ctx, map[string]any{"title": title}),
		IconOnly: true,
		Items:    items,
		Position: ui.MenuBottomEnd,
	}))
}

func (b *RecordBuilder) copyLink(ctx context.Context) (render.HTML, []ui.MenuItem) {
	abs := absoluteURL(ctx)
	if abs == "" {
		return "", nil
	}
	id := "eui-rec-link"
	return html.Span(html.TextConfig{ID: id, Class: "cui-visually-hidden"}, render.Text(abs)),
		[]ui.MenuItem{{Label: i18nui.EntityCopyLink.Text(ctx), Copy: &ui.MenuCopy{Target: id, Toast: i18nui.CopyCopied.Text(ctx)}}}
}

func (b *RecordBuilder) copyAPIURL(ctx context.Context, m *meta) (render.HTML, []ui.MenuItem) {
	base, ok := b.ui.restBase(m.e)
	if !ok {
		return "", nil
	}
	origin := requestOrigin(ctx)
	if origin == "" {
		return "", nil
	}
	id := "eui-rec-api"
	return html.Span(html.TextConfig{ID: id, Class: "cui-visually-hidden"}, render.Text(origin+under(base, m.idPath(b.id)))),
		[]ui.MenuItem{{Label: i18nui.EntityCopyAPIURL.Text(ctx), Copy: &ui.MenuCopy{Target: id, Toast: i18nui.CopyCopied.Text(ctx)}}}
}

func absoluteURL(ctx context.Context) string {
	r := appui.RequestFromContext(ctx)
	if r == nil {
		return ""
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if clientip.TrustedFrom(r.Context()).ContainsAddr(r.RemoteAddr) {
		if p := r.Header.Get("X-Forwarded-Proto"); p == "http" || p == "https" {
			scheme = p
		}
	}
	return scheme + "://" + r.Host + r.URL.Path
}

func requestOrigin(ctx context.Context) string {
	r := appui.RequestFromContext(ctx)
	if r == nil {
		return ""
	}
	return strings.TrimSuffix(absoluteURL(ctx), r.URL.Path)
}

type tab struct {
	key   string
	label string
	count string
	build func() (render.HTML, error)
}

func (b *RecordBuilder) tabbedBody(ctx context.Context, m *meta, row, raw map[string]any, masked map[string]bool, base string) (render.HTML, string, error) {
	editTab := tab{key: "edit", label: i18nui.EntityTabEdit.Text(ctx), build: func() (render.HTML, error) {
		return b.editTab(ctx, m, raw, row, masked, base), nil
	}}
	tabs := []tab{editTab}
	if b.hasRelatedTab(ctx, m, base) {
		tabs = append(tabs, tab{key: "related", label: i18nui.EntityTabRelated.Text(ctx), count: b.relatedCount(ctx, m, base), build: func() (render.HTML, error) {
			return b.relatedTab(ctx, m, base), nil
		}})
	}
	rec := Record{ID: b.id, Values: row}
	for _, xt := range slices.Concat(m.ext.Tabs, b.tabs) {
		key, build := xt.Key, xt.Build
		label := key
		if !xt.Label.IsZero() {
			label = xt.Label.Text(ctx)
		}
		tabs = append(tabs, tab{key: key, label: label, build: func() (render.HTML, error) {
			return contain(ctx, m.name, "tab "+key, func() (render.HTML, error) {
				cctx := asCaller(ctx)
				c, err := build(TabContext{Ctx: cctx, UI: b.ui, Entity: m.name, Record: rec})
				if err != nil {
					return "", err
				}
				if c == nil {
					return "", fmt.Errorf("tab body is nil")
				}
				return renderComponent(cctx, c), nil
			}), nil
		}})
	}
	if b.activity && b.ui.host.Audit() != nil {
		tabs = append(tabs, tab{key: "activity", label: i18nui.EntityTabActivity.Text(ctx), count: b.activityCount(ctx, m), build: func() (render.HTML, error) {
			masked := rewrittenFields(m, raw, row)
			maps.Copy(masked, hookMaskedFields(ctx, m, b.id, raw))
			return b.activityTab(ctx, m, masked), nil
		}})
	}
	if b.apiTab {
		tabs = append(tabs, tab{key: "api", label: i18nui.EntityTabApi.Text(ctx), build: func() (render.HTML, error) {
			return b.apiTabBody(ctx, m, row), nil
		}})
	}

	active := 0
	if want := appui.QueryFromContext(ctx).Get("tab"); want != "" {
		for i, t := range tabs {
			if t.key == want {
				active = i
				break
			}
		}
	}

	items := make([]ui.TabNavItem, len(tabs))
	for i, t := range tabs {
		items[i] = ui.TabNavItem{
			Text:    t.label,
			Href:    currentURLPath(ctx) + "?tab=" + url.QueryEscape(t.key),
			Current: i == active,
			Badge:   t.count,
		}
	}
	body, err := tabs[active].build()
	if err != nil {
		return "", "", err
	}
	return ui.Stack(ui.StackConfig{},
		ui.TabNav(ui.TabNavConfig{
			ID:    "eui-" + m.name + "-tabs",
			Label: i18nui.EntityRecordSections.Text(ctx),
			Items: items,
		}),
		body,
	), tabs[active].key, nil
}

func currentURLPath(ctx context.Context) string {
	r := appui.RequestFromContext(ctx)
	if r == nil || r.URL == nil || r.URL.Path == "" {
		return "/"
	}
	return r.URL.Path
}

func renderComponent(ctx context.Context, c component.Component) render.HTML {
	if cc, ok := c.(component.ContextComponent); ok {
		return cc.RenderCtx(ctx)
	}
	return c.Render()
}

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

func deleteAction(ctx context.Context, m *meta, id, back string, undo bool) interactive.Action {
	vars := map[string]any{"entity": m.singular(ctx)}
	del := interactive.Delete(m.api + "/" + m.idPath(id)).
		WithConfirmDialog(deleteConfirm(ctx, m)).
		OnSuccessToast(i18nui.EntityDeleted.Text(ctx, vars)).
		OnSuccess(interactive.Navigate(back)).
		OnErrorToast(i18nui.EntityDeleteFailed.Text(ctx, vars))
	if undo && m.e.Config.Scope.SoftDelete && canUpdate(ctx, m, id) {
		del = del.OnSuccessToastAction(i18nui.EntityUndo.Text(ctx),
			interactive.Post(m.api+"/"+m.idPath(id)+"/_restore").WithBody(`{}`).
				OnSuccess(interactive.Navigate(back)).
				OnErrorToast(i18nui.EntityRestoreFailed.Text(ctx)))
	}
	return del
}

func deleteConfirm(ctx context.Context, m *meta) interactive.Confirm {
	vars := map[string]any{"entity": m.noun(ctx, false)}
	msg := i18nui.EntityDeleteConfirm
	if m.e.Config.Scope.SoftDelete {
		msg = i18nui.EntityDeleteSoft
	}
	return interactive.Confirm{
		Title:   i18nui.EntityDeleteTitle.Text(ctx, vars),
		Message: msg.Text(ctx),
		Accept:  i18nui.EntityDelete.Text(ctx),
		Danger:  true,
	}
}
