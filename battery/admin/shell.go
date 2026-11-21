package admin

import (
	"cmp"
	"context"
	"net/http"
	"slices"
	"strings"

	appui "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/app/decide"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/interactive"
	"github.com/DonaldMurillo/gofastr/core-ui/widget"
	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/framework/access"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

const (
	navDrawer   = "admin-nav"
	paletteName = "admin-palette"
	keysName    = "admin-keys"
)

func (b *Battery) mount() {
	b.app.Use(b.headers)
	r := b.routes()
	group := appui.NewScreenGroup(b.cfg.PathPrefix, b.layout(), b.gatePolicy()).Standalone()

	b.screen(group, "", i18nui.AdminDashboard, true, b.renderDashboard)
	b.screen(group, "/search", i18nui.AdminSearch, true, b.renderSearch)
	b.screen(group, "/shortcuts", i18nui.ShortcutSheetTitle, false, b.renderShortcuts)
	b.screen(group, "/account", i18nui.AdminAccountSettings, false, b.renderAccount)
	b.mountOpsPages(group)
	b.mountEntities(group, r)
	b.mountPages(group)
	b.host.App.Router.ScreenGroup(group)

	b.mountOps(r)
	b.mountEmails(r)
	r.Post(b.cfg.PathPrefix+"/_palette", http.HandlerFunc(b.handlePalette))

	mounter := adminMounter{r: r, under: b.underPrefix, listed: b.offered}
	ui.MountSidebarFunc(mounter, b.sidebar)
	palette := ui.CommandPaletteFunc(b.paletteConfig)
	widget.MountBuilder(r, palette.PagesMatch(b.underPrefix).ListedFor(b.offered))
	keys := ui.ShortcutSheetFunc(b.keysConfig)
	widget.MountBuilder(r, keys.PagesMatch(b.underPrefix).ListedFor(b.offered))
}

type opsPage struct {
	path, icon string
	title      i18n.Phrase
	draw       func(context.Context, map[string]string) render.HTML
	platform   bool
}

func (b *Battery) mountOpsPages(group *appui.ScreenGroup) {
	b.ops = nil
	mount := func(enabled bool, p opsPage) {
		if !enabled {
			return
		}
		s := b.screen(group, p.path, p.title, false, p.draw)
		if p.platform {
			s.WithPolicy(platformPolicy())
		}
		b.ops = append(b.ops, p)
	}
	mount(b.cfg.Queue != nil, opsPage{"/queue", "layers", i18nui.AdminQueue, b.renderQueue, true})
	mount(b.db != nil, opsPage{"/audit", "activity", i18nui.AdminAudit, b.renderAudit, false})
	mount(b.cfg.Policy != nil, opsPage{"/rbac/roles", "shield", i18nui.AdminRoles, b.renderRoles, true})
	mount(b.cfg.Auth != nil, opsPage{"/rbac/users", "users", i18nui.AdminUserRoles, b.renderUsers, true})
	mount(b.cfg.ProcessModules != nil, opsPage{"/modules", "cpu", i18nui.AdminModules, b.renderModules, true})
	mount(len(b.cfg.EmailPreviews) > 0, opsPage{"/emails", "mail", i18nui.AdminEmails, b.renderEmails, false})
}

func (b *Battery) offered(r *http.Request) bool { return b.authorized(r.Context()) }

func (b *Battery) underPrefix(path string) bool {
	return path == b.cfg.PathPrefix || strings.HasPrefix(path, b.cfg.PathPrefix+"/")
}

type adminMounter struct {
	r      *router.Router
	under  func(string) bool
	listed func(*http.Request) bool
}

func (m adminMounter) MountWidget(def *widget.Definition) {
	def.Routes = append(def.Routes, m.under)
	def.ListedFor = m.listed
	widget.Mount(m.r, def)
}

func (b *Battery) gatePolicy() appui.Policy {
	return appui.PolicyFunc(func(ctx context.Context) appui.Decision {
		if b.authorized(ctx) {
			return decide.Allow()
		}
		status := b.authzStatus(ctx)
		if status == http.StatusUnauthorized && b.cfg.LoginPath != "" {
			next := b.cfg.PathPrefix
			if r := appui.RequestFromContext(ctx); r != nil && r.URL != nil {
				next = loginNext(r)
			}
			handler.NoteSessionRefusal(ctx)
			return decide.Redirect(b.cfg.LoginPath + "?next=" + urlQueryEscape(next))
		}
		return decide.Block(status)
	})
}

func (b *Battery) screen(group *appui.ScreenGroup, path string, title i18n.Phrase, elevate bool,
	draw func(ctx context.Context, p map[string]string) render.HTML) *appui.Screen {
	return b.screenNamed(group, path, title, nil, elevate, draw)
}

func (b *Battery) screenNamed(group *appui.ScreenGroup, path string, title i18n.Phrase, name func(ctx context.Context, p map[string]string) string, elevate bool,
	draw func(ctx context.Context, p map[string]string) render.HTML) *appui.Screen {
	if name == nil {
		name = func(ctx context.Context, _ map[string]string) string { return title.Text(ctx) }
	}
	s := appui.NewScreen(b.cfg.PathPrefix+path, &adminScreen{
		b:       b,
		title:   name,
		draw:    draw,
		elevate: elevate,
	}).WithTitle(title.Text(context.Background()))
	group.Screen(s, nil)
	return s
}

type adminScreen struct {
	component.ContextOnly
	b       *Battery
	title   func(ctx context.Context, p map[string]string) string
	draw    func(ctx context.Context, p map[string]string) render.HTML
	elevate bool
	params  map[string]string
	loaded  string
}

func (s *adminScreen) SetParams(p map[string]string) { s.params = p }

// Load resolves the page title in the caller's locale.
func (s *adminScreen) Load(ctx context.Context) error {
	if s.b.authorized(ctx) {
		s.loaded = s.title(s.ctx(ctx), s.params)
	}
	return nil
}

func (s *adminScreen) ScreenTitle(ctx context.Context) string { return s.loaded }

func (s *adminScreen) ctx(ctx context.Context) context.Context {
	if s.elevate && s.b.authorized(ctx) {
		return s.b.elevate(ctx)
	}
	return ctx
}

func (s *adminScreen) RenderCtx(ctx context.Context) render.HTML {
	if !s.b.authorized(ctx) {
		return ""
	}
	return s.draw(s.ctx(ctx), s.params)
}

func (b *Battery) layout() *appui.Layout {
	spec := appui.LayoutSpec{Areas: []appui.AreaSpec{{Name: "crumbs", Transition: appui.Instant()}}}
	for _, e := range b.ents {
		if countsInNav(e) {
			spec.Areas = append(spec.Areas, appui.AreaSpec{Name: navCountArea(e), Transition: appui.Instant(), Inline: true})
		}
	}
	return appui.NewLayout("gofastr-admin", spec, func(ctx context.Context, l *appui.LayoutTree) render.HTML {
		cfg := b.sidebarWith(ctx, func(e *entity.Entity) render.HTML {
			return l.RouteArea(navCountArea(e), func(ctx context.Context, _ appui.Match) render.HTML {
				return b.navCount(ctx, e)
			})
		})
		nav, _ := component.SafeRenderCtx(ctx, ui.Sidebar(cfg))
		crumbs := l.RouteArea("crumbs", func(ctx context.Context, m appui.Match) render.HTML {
			return b.crumbs(ctx, m)
		})
		trigger := ui.CommandPaletteTrigger(ctx, b.paletteConfig(ctx))
		keys := ui.ShortcutSheetTrigger(ctx, b.keysConfig(ctx))
		toolbar := ui.Cluster(ui.ClusterConfig{Justify: ui.JustifyBetween, Align: ui.AlignCenter, NoWrap: true},
			ui.Cluster(ui.ClusterConfig{Gap: ui.GapSM, Align: ui.AlignCenter, NoWrap: true, Shrink: true},
				ui.SidebarDrawerTrigger(cfg), crumbs),
			ui.Cluster(ui.ClusterConfig{Gap: ui.GapSM, Align: ui.AlignCenter, NoWrap: true},
				trigger, keys,
				ui.ThemeToggle(ui.ThemeToggleConfig{Variant: ui.ThemeToggleIcon, Ctx: ctx}),
				b.accountMenu(ctx)),
		)
		return ui.Stack(ui.StackConfig{Screen: true, Gap: ui.GapNone},
			ui.ContentRow(ui.ContentRowConfig{
				Ctx:          ctx,
				Sidebar:      nav,
				NavLabel:     i18nui.AdminSidebar.Text(ctx),
				Toolbar:      toolbar,
				ToolbarLabel: i18nui.AdminToolbar.Text(ctx),
				Sticky:       true, PhoneNavFlush: true, Dense: true,
			}, l.Primary()))
	})
}

func (b *Battery) paletteConfig(ctx context.Context) ui.CommandPaletteConfig {
	return ui.CommandPaletteConfig{
		Name:         paletteName,
		RPCPath:      b.cfg.PathPrefix + "/_palette",
		Placeholder:  i18nui.AdminSearch.Text(ctx),
		Trigger:      ui.PaletteTriggerField,
		FallbackHref: b.cfg.PathPrefix + "/search",
		Ctx:          ctx,
	}
}

func (b *Battery) themePicker(ctx context.Context, fill bool) render.HTML {
	if len(b.cfg.Themes) == 0 {
		return ""
	}
	return ui.ThemePicker(ui.ThemePickerConfig{Themes: b.cfg.Themes, Fill: fill, Ctx: ctx})
}

func (b *Battery) keysConfig(ctx context.Context) ui.ShortcutSheetConfig {
	return ui.ShortcutSheetConfig{
		Name:         keysName,
		FallbackHref: b.cfg.PathPrefix + "/shortcuts",
		Items:        b.shortcuts(ctx),
		Ctx:          ctx,
	}
}

func (b *Battery) shortcuts(ctx context.Context) []ui.ShortcutItem {
	return []ui.ShortcutItem{
		{Chord: "Meta+K", Label: i18nui.AdminKeyPalette.Text(ctx)},
		{Chord: "/", Label: i18nui.AdminKeySearch.Text(ctx)},
		{Chord: "Mod+S", Label: i18nui.AdminKeySave.Text(ctx)},
		{Chord: "Escape", Label: i18nui.AdminKeyClose.Text(ctx)},
		{Chord: "?", Label: i18nui.AdminKeyHelp.Text(ctx)},
	}
}

func (b *Battery) renderShortcuts(ctx context.Context, _ map[string]string) render.HTML {
	return ui.Stack(ui.StackConfig{Gap: ui.GapLG},
		ui.PageHeader(ui.PageHeaderConfig{Title: i18nui.ShortcutSheetTitle.Text(ctx)}),
		ui.ShortcutList(ctx, b.shortcuts(ctx)))
}

func (b *Battery) title(ctx context.Context) string {
	if b.cfg.Title != "" {
		return b.cfg.Title
	}
	return i18nui.AdminTitle.Text(ctx)
}

type navItem struct {
	group string
	order int
	label string
	href  string
	icon  string
	count render.HTML
}

func (b *Battery) sidebar(ctx context.Context) ui.SidebarConfig {
	return b.sidebarWith(ctx, nil)
}

func (b *Battery) sidebarWith(ctx context.Context, count func(*entity.Entity) render.HTML) ui.SidebarConfig {
	path := ""
	if r := appui.RequestFromContext(ctx); r != nil && r.URL != nil {
		path = r.URL.Path
	}
	items := []ui.SidebarItem{{
		Label: i18nui.AdminDashboard.Text(ctx),
		Href:  b.cfg.PathPrefix,
		Icon:  ui.Icon("home", ui.IconConfig{}),
	}}
	items = append(items, b.navGroups(ctx, count)...)
	if ops := b.opsItems(ctx); len(ops) > 0 {
		items = append(items, ui.SidebarItem{Label: i18nui.AdminSystemNav.Text(ctx), Children: ops, Open: true})
	}
	return ui.SidebarConfig{
		Ctx:                   ctx,
		NavLabel:              i18nui.AdminNav.Text(ctx),
		Items:                 items,
		CurrentPath:           path,
		Variant:               ui.SidebarCollapsible,
		SectionLabels:         true,
		RaisedCurrent:         true,
		DrawerName:            navDrawer,
		DrawerTitle:           b.title(ctx),
		Prepend:               brand{b: b},
		SuppressDrawerTrigger: true,
	}
}

func (b *Battery) navGroups(ctx context.Context, count func(*entity.Entity) render.HTML) []ui.SidebarItem {
	var all []navItem
	for _, e := range b.ents {
		n := navOf(e)
		if n != nil && n.Hide {
			continue
		}
		it := navItem{label: b.plural(ctx, e), href: b.entityBase(e)}
		if n != nil {
			it.group, it.order, it.icon = n.Group, n.Order, n.Icon
		}
		if count != nil && countsInNav(e) {
			it.count = count(e)
		}
		all = append(all, it)
	}
	for _, p := range b.cfg.Pages {
		if p.Nav == nil || p.Nav.Hide || !b.pageAllows(ctx, p) {
			continue
		}
		all = append(all, navItem{group: p.Nav.Group, order: p.Nav.Order, label: p.Title.Text(ctx),
			href: b.cfg.PathPrefix + p.Path, icon: p.Nav.Icon})
	}
	for _, l := range b.cfg.Links {
		all = append(all, navItem{group: l.Group, label: l.Label.Text(ctx), href: l.Href, icon: l.Icon})
	}
	var order []string
	byGroup := map[string][]navItem{}
	for _, it := range all {
		if _, seen := byGroup[it.group]; !seen {
			order = append(order, it.group)
		}
		byGroup[it.group] = append(byGroup[it.group], it)
	}
	out := make([]ui.SidebarItem, 0, len(order))
	for _, g := range order {
		entries := byGroup[g]
		slices.SortStableFunc(entries, func(a, b navItem) int {
			return cmp.Or(cmp.Compare(a.order, b.order), cmp.Compare(a.label, b.label))
		})
		children := make([]ui.SidebarItem, len(entries))
		for i, it := range entries {
			children[i] = ui.SidebarItem{Label: it.label, Href: it.href, MatchPath: it.href, Icon: navIcon(it.icon), Count: it.count}
		}
		label := i18nui.AdminEntities.Text(ctx)
		if g != "" {
			label = i18nui.NavGroupLabel(ctx, nil, g, "")
		}
		out = append(out, ui.SidebarItem{Label: label, Children: children, Open: true})
	}
	return out
}

func (b *Battery) opsItems(ctx context.Context) []ui.SidebarItem {
	var out []ui.SidebarItem
	platform := platformCaller(ctx)
	for _, p := range b.ops {
		if !p.platform || platform {
			href := b.cfg.PathPrefix + p.path
			out = append(out, ui.SidebarItem{Label: p.title.Text(ctx), Href: href, MatchPath: href, Icon: navIcon(p.icon)})
		}
	}
	return out
}

func navIcon(name string) render.HTML {
	if name == "" {
		return ""
	}
	return ui.Icon(name, ui.IconConfig{})
}

func countsInNav(e *entity.Entity) bool {
	n := navOf(e)
	return n == nil || (!n.Hide && !n.HideCount)
}

func navCountArea(e *entity.Entity) string { return "count-" + e.GetName() }

func (b *Battery) navCount(ctx context.Context, e *entity.Entity) render.HTML {
	if !b.authorized(ctx) {
		return ""
	}
	n, ok := b.ui.Count(b.elevate(ctx), e.GetName(), "")
	if !ok {
		return ""
	}
	return render.Text(n)
}

func navOf(e *entity.Entity) *entity.EntityNav {
	if e.Config.Display == nil {
		return nil
	}
	return e.Config.Display.Nav
}

type brand struct {
	component.ContextOnly
	b *Battery
}

func (s brand) RenderCtx(ctx context.Context) render.HTML {
	return ui.SidebarBrand(ui.SidebarBrandConfig{
		Name: s.b.title(ctx),
		Sub:  i18nui.AdminBrandSub.Text(ctx),
		Logo: s.b.cfg.Logo,
	})
}

func (b *Battery) accountMenu(ctx context.Context) render.HTML {
	name := b.displayName(ctx)
	if name == "" {
		return ""
	}
	var who []string
	if email := userEmail(ctx); email != "" && email != name {
		who = append(who, email)
	}
	if roles := callerHeldRoles(ctx); len(roles) > 0 {
		who = append(who, strings.Join(roles, ", "))
	}
	head := []render.HTML{html.Strong(html.TextConfig{}, render.Text(name))}
	if len(who) > 0 {
		head = append(head, ui.Muted(html.Small(html.TextConfig{}, render.Text(strings.Join(who, " · ")))))
	}
	section := func(label string, control render.HTML) render.HTML {
		return ui.Stack(ui.StackConfig{Gap: ui.GapXS}, ui.Muted(html.Small(html.TextConfig{}, render.Text(label))), control)
	}
	parts := []render.HTML{
		ui.Stack(ui.StackConfig{Gap: ui.GapNone, ExtraAttrs: html.Attrs{
			"role":       "group",
			"aria-label": i18nui.AdminSignedInAs.Text(ctx, map[string]any{"name": name}),
		}}, head...),
		ui.Divider(ui.DividerConfig{}),
		section(i18nui.AdminThemeLabel.Text(ctx), ui.ThemeToggle(ui.ThemeToggleConfig{Variant: ui.ThemeTogglePill, Fill: true, Ctx: ctx})),
	}
	if look := b.themePicker(ctx, true); look != "" {
		parts = append(parts, section(i18nui.AdminLook.Text(ctx), look))
	}
	rows := []ui.ActionListItem{{
		Label: i18nui.AdminAccountSettings.Text(ctx),
		Href:  b.cfg.PathPrefix + "/account",
		Icon:  "user",
	}}
	if b.cfg.SignOutPath != "" {
		dest := b.cfg.LoginPath
		if dest == "" {
			dest = "/"
		}
		out := interactive.Post(b.cfg.SignOutPath).OnSuccess(interactive.Navigate(dest))
		rows = append(rows, ui.ActionListItem{Label: i18nui.SignOut.Text(ctx), Do: &out, Icon: "log-out"})
	}
	parts = append(parts, ui.Divider(ui.DividerConfig{}), ui.ActionList(ui.ActionListConfig{Items: rows}))
	return ui.Dropdown(ui.DropdownConfig{
		Ctx:     ctx,
		Label:   i18nui.AdminAccount.Text(ctx),
		Avatar:  &ui.AvatarConfig{Ctx: ctx, Name: name},
		Align:   ui.DropdownEnd,
		Content: ui.Stack(ui.StackConfig{Gap: ui.GapMD}, parts...),
	})
}

func (b *Battery) displayName(ctx context.Context) string {
	if handlerUser(ctx) == nil {
		return ""
	}
	return cmp.Or(b.userName(ctx), userEmail(ctx), access.UserID(ctx))
}

func handlerUser(ctx context.Context) any {
	u, _ := handler.GetUser(ctx)
	return u
}

func (b *Battery) crumbs(ctx context.Context, m appui.Match) render.HTML {
	items := []ui.Crumb{{Text: b.title(ctx), Href: b.cfg.PathPrefix}}
	items = append(items, b.trail(ctx, m)...)
	items[len(items)-1].Current = true
	items[len(items)-1].Href = ""
	return ui.Breadcrumbs(ui.BreadcrumbsConfig{CompactMobile: true, Ctx: ctx}, items...)
}

func (b *Battery) trail(ctx context.Context, m appui.Match) []ui.Crumb {
	rest := strings.TrimPrefix(m.Path(), b.cfg.PathPrefix)
	switch rest {
	case "", "/":
		return []ui.Crumb{{Text: i18nui.AdminDashboard.Text(ctx)}}
	case "/search":
		return []ui.Crumb{{Text: i18nui.AdminSearch.Text(ctx)}}
	case "/account":
		return []ui.Crumb{{Text: i18nui.AdminAccountSettings.Text(ctx)}}
	}
	for _, p := range b.ops {
		if rest == p.path {
			return []ui.Crumb{{Text: p.title.Text(ctx)}}
		}
	}
	for _, p := range b.cfg.Pages {
		if rest == p.Path {
			return []ui.Crumb{{Text: p.Title.Text(ctx)}}
		}
	}
	if e, id, ok := b.entityPath(rest, m); ok {
		list := ui.Crumb{Text: b.plural(ctx, e), Href: b.entityBase(e)}
		switch id {
		case "":
			return []ui.Crumb{list}
		case "create":
			return []ui.Crumb{list, {Text: i18nui.AdminPaletteNew.Text(ctx, map[string]any{"entity": b.singular(ctx, e)})}}
		}
		title, ok := "", false
		if b.authorized(ctx) {
			title, ok = b.ui.RecordTitle(b.elevate(ctx), e.GetName(), id)
		}
		if !ok {
			title = b.singular(ctx, e)
		}
		return []ui.Crumb{list, {Text: title}}
	}
	return nil
}
