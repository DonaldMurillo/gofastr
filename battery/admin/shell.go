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
	"github.com/DonaldMurillo/gofastr/core-ui/interactive"
	"github.com/DonaldMurillo/gofastr/core-ui/widget"
	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

// The shell is the one frame every admin page draws in: a collapsible
// sidebar (a drawer on phones), a toolbar with breadcrumbs, the command
// palette, the theme toggle and the account menu, and the page itself.
// Every piece is a kit component; the battery adds no CSS.

const (
	navDrawer   = "admin-nav"
	paletteName = "admin-palette"
	keysName    = "admin-keys"
)

// mount registers the shell's screens on the UI host and the admin's
// routes on the app router, all behind the gate.
func (b *Battery) mount() {
	b.app.Use(b.headers)
	r := b.routes()
	group := appui.NewScreenGroup(b.cfg.PathPrefix, b.layout(), b.gatePolicy()).Standalone()

	b.screen(group, "", i18nui.KeyAdminDashboard, true, b.renderDashboard)
	b.screen(group, "/search", i18nui.KeyAdminSearch, true, b.renderSearch)
	b.screen(group, "/shortcuts", i18nui.KeyShortcutSheetTitle, false, b.renderShortcuts)
	b.screen(group, "/account", i18nui.KeyAdminAccountSettings, false, b.renderAccount)
	if b.cfg.Queue != nil {
		b.screen(group, "/queue", i18nui.KeyAdminQueue, false, b.renderQueue)
	}
	if b.db != nil {
		b.screen(group, "/audit", i18nui.KeyAdminAudit, false, b.renderAudit)
	}
	if b.cfg.Policy != nil {
		b.screen(group, "/rbac/roles", i18nui.KeyAdminRoles, false, b.renderRoles)
	}
	if b.cfg.Auth != nil {
		b.screen(group, "/rbac/users", i18nui.KeyAdminUserRoles, false, b.renderUsers)
	}
	if b.cfg.ProcessModules != nil {
		b.screen(group, "/modules", i18nui.KeyAdminModules, false, b.renderModules)
	}
	b.mountEntities(group, r)
	b.mountPages(group)
	b.host.App.Router.ScreenGroup(group)

	b.mountOps(r)
	r.Post(b.cfg.PathPrefix+"/_palette", http.HandlerFunc(b.handlePalette))

	// The drawer and the palette are widgets: each registers its own
	// routes, so both mount on the gated group, and each is offered only
	// on admin pages.
	mounter := adminMounter{r: r, under: b.underPrefix}
	ui.MountSidebarFunc(mounter, b.sidebar)
	_, palette := ui.CommandPalette(b.paletteConfig(context.Background()))
	widget.MountBuilder(r, palette.PagesMatch(b.underPrefix))
	_, keys := ui.ShortcutSheet(b.keysConfig(context.Background()))
	widget.MountBuilder(r, keys.PagesMatch(b.underPrefix))
}

// underPrefix reports a path inside the admin.
func (b *Battery) underPrefix(path string) bool {
	return path == b.cfg.PathPrefix || strings.HasPrefix(path, b.cfg.PathPrefix+"/")
}

// adminMounter mounts a widget on the gated router, offered only on
// admin pages.
type adminMounter struct {
	r     *router.Router
	under func(string) bool
}

func (m adminMounter) MountWidget(def *widget.Definition) {
	def.Routes = append(def.Routes, m.under)
	widget.Mount(m.r, def)
}

// gatePolicy is the gate for screens: the host's render pipeline refuses
// an unauthorized caller before anything loads. A signed-out caller goes
// to LoginPath when one is set.
func (b *Battery) gatePolicy() appui.Policy {
	return appui.PolicyFunc(func(ctx context.Context) appui.Decision {
		if b.authorized(ctx) {
			return decide.Allow()
		}
		status := b.authzStatus(ctx)
		if status == http.StatusUnauthorized && b.cfg.LoginPath != "" {
			next := b.cfg.PathPrefix
			if r := appui.RequestFromContext(ctx); r != nil && r.URL != nil {
				next = r.URL.Path
			}
			return decide.Redirect(b.cfg.LoginPath + "?next=" + urlQueryEscape(next))
		}
		return decide.Block(status, http.StatusText(status))
	})
}

// screen registers one admin page. elevate lifts the entity read
// gates for the admin's own reads (crud.WithElevation); app pages never
// get it.
func (b *Battery) screen(group *appui.ScreenGroup, path string, title i18nui.Key, elevate bool,
	draw func(ctx context.Context, p map[string]string) render.HTML) *appui.Screen {
	s := appui.NewScreen(b.cfg.PathPrefix+path, &adminScreen{
		b:       b,
		title:   func(ctx context.Context, _ map[string]string) string { return i18nui.T(ctx, title) },
		draw:    draw,
		elevate: elevate,
	}).WithTitle(i18nui.T(context.Background(), title))
	group.Screen(s, nil)
	return s
}

// adminScreen is one admin page as a screen component. The host copies
// it per request, sets the route params, calls Load, then renders.
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

// Load resolves the page title in the caller's locale, for <title>.
func (s *adminScreen) Load(ctx context.Context) error {
	if s.b.authorized(ctx) {
		s.loaded = s.title(s.ctx(ctx), s.params)
	}
	return nil
}

func (s *adminScreen) ScreenTitle() string { return s.loaded }

// ctx is the context the page reads with: elevated for the admin's own
// pages, and only once the caller passed the gate. The screen policy has
// already refused everyone else; this check keeps the elevation from
// ever depending on that alone.
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

// ----- layout ----------------------------------------------------------------

// layout is the shell: the sidebar beside a toolbar and the page.
func (b *Battery) layout() *appui.Layout {
	// The trail swaps in one frame: its root ("Meridian") is the same on
	// every page, and any fade would blink it.
	spec := appui.LayoutSpec{Areas: []appui.AreaSpec{{Name: "crumbs", Transition: appui.Instant()}}}
	// Each entity row's count is an inline route area: the sidebar
	// stays put across client navigations, and the count re-renders
	// with every one, so a create shows in it on the next click.
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
			return b.crumbs(ctx, m.Path())
		})
		trigger, _ := ui.CommandPalette(b.paletteConfig(ctx))
		keys, _ := ui.ShortcutSheet(b.keysConfig(ctx))
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
				Sidebar:      nav,
				NavLabel:     i18nui.T(ctx, i18nui.KeyAdminSidebar),
				Toolbar:      toolbar,
				ToolbarLabel: i18nui.T(ctx, i18nui.KeyAdminToolbar),
				Sticky:       true, PhoneNavFlush: true, Dense: true,
			}, l.Primary()))
	})
}

// paletteConfig is the command palette: ⌘K, record search at
// <prefix>/_palette, and the search page as the scriptless path.
func (b *Battery) paletteConfig(ctx context.Context) ui.CommandPaletteConfig {
	return ui.CommandPaletteConfig{
		Name:         paletteName,
		RPCPath:      b.cfg.PathPrefix + "/_palette",
		Placeholder:  i18nui.T(ctx, i18nui.KeyAdminSearch),
		Trigger:      ui.PaletteTriggerField,
		FallbackHref: b.cfg.PathPrefix + "/search",
		Ctx:          ctx,
	}
}

// themePicker offers Config.Themes on the account page; none draws
// nothing.
func (b *Battery) themePicker(ctx context.Context) render.HTML {
	if len(b.cfg.Themes) == 0 {
		return ""
	}
	return ui.ThemePicker(ui.ThemePickerConfig{Themes: b.cfg.Themes, Ctx: ctx})
}

// keysConfig is the keyboard help sheet, opened by "?", with the
// shortcuts page as the scriptless path.
func (b *Battery) keysConfig(ctx context.Context) ui.ShortcutSheetConfig {
	return ui.ShortcutSheetConfig{
		Name:         keysName,
		FallbackHref: b.cfg.PathPrefix + "/shortcuts",
		Items:        b.shortcuts(ctx),
		Ctx:          ctx,
	}
}

// shortcuts are the admin's keys, in the order a reader meets them.
func (b *Battery) shortcuts(ctx context.Context) []ui.ShortcutItem {
	return []ui.ShortcutItem{
		{Chord: "Meta+K", Label: i18nui.T(ctx, i18nui.KeyAdminKeyPalette)},
		{Chord: "/", Label: i18nui.T(ctx, i18nui.KeyAdminKeySearch)},
		{Chord: "Mod+S", Label: i18nui.T(ctx, i18nui.KeyAdminKeySave)},
		{Chord: "Escape", Label: i18nui.T(ctx, i18nui.KeyAdminKeyClose)},
		{Chord: "?", Label: i18nui.T(ctx, i18nui.KeyAdminKeyHelp)},
	}
}

// renderShortcuts is the shortcuts page, the help sheet's scriptless twin.
func (b *Battery) renderShortcuts(ctx context.Context, _ map[string]string) render.HTML {
	return ui.Stack(ui.StackConfig{Gap: ui.GapLG},
		ui.PageHeader(ui.PageHeaderConfig{Title: i18nui.T(ctx, i18nui.KeyShortcutSheetTitle)}),
		ui.ShortcutList(b.shortcuts(ctx)))
}

// title is the product name: Config.Title or the localized "Admin".
func (b *Battery) title(ctx context.Context) string {
	if b.cfg.Title != "" {
		return b.cfg.Title
	}
	return i18nui.T(ctx, i18nui.KeyAdminTitle)
}

// ----- sidebar ---------------------------------------------------------------

// navItem is one sidebar link before it is grouped.
type navItem struct {
	group string // nav group key; "" is the default group
	order int
	label string
	href  string
	icon  string
	count render.HTML
}

// sidebar builds the phone drawer's sidebar for the request in ctx: the
// inline column's, without counts. The drawer is a widget outside the
// layout, so a count there would keep the figure of the page it loaded
// with; it draws none rather than a stale one.
func (b *Battery) sidebar(ctx context.Context) ui.SidebarConfig {
	return b.sidebarWith(ctx, nil)
}

// sidebarWith builds the sidebar for the request in ctx, the count of
// each counted entity row from count (nil draws none). The drawer and
// the inline column both build through it, so the two never differ in
// anything else.
func (b *Battery) sidebarWith(ctx context.Context, count func(*entity.Entity) render.HTML) ui.SidebarConfig {
	path := ""
	if r := appui.RequestFromContext(ctx); r != nil && r.URL != nil {
		path = r.URL.Path
	}
	items := []ui.SidebarItem{{
		Label: i18nui.T(ctx, i18nui.KeyAdminDashboard),
		Href:  b.cfg.PathPrefix,
		Icon:  ui.Icon("home", ui.IconConfig{}),
	}}
	items = append(items, b.navGroups(ctx, count)...)
	if ops := b.opsItems(ctx); len(ops) > 0 {
		items = append(items, ui.SidebarItem{Label: i18nui.T(ctx, i18nui.KeyAdminSystemNav), Children: ops, Open: true})
	}
	return ui.SidebarConfig{
		NavLabel:              i18nui.T(ctx, i18nui.KeyAdminNav),
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

// navGroups are the entity, page and link entries under their nav
// groups. Groups keep the order they first appear in (entities in the
// order the admin exposes them, then pages, then links); entries sort by
// Order, then label. An entity row carries count's figure when the
// entity is counted and count is non-nil.
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
		all = append(all, navItem{group: p.Nav.Group, order: p.Nav.Order, label: p.Title,
			href: b.cfg.PathPrefix + p.Path, icon: p.Nav.Icon})
	}
	for _, l := range b.cfg.Links {
		all = append(all, navItem{group: l.Group, label: l.Label, href: l.Href, icon: l.Icon})
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
		label := i18nui.T(ctx, i18nui.KeyAdminEntities)
		if g != "" {
			label = i18nui.NavGroupLabel(ctx, nil, g, "")
		}
		out = append(out, ui.SidebarItem{Label: label, Children: children, Open: true})
	}
	return out
}

// opsItems are the System group's links, one per page the admin
// has what it needs to draw.
func (b *Battery) opsItems(ctx context.Context) []ui.SidebarItem {
	var out []ui.SidebarItem
	add := func(ok bool, key i18nui.Key, path, icon string) {
		if ok {
			href := b.cfg.PathPrefix + path
			out = append(out, ui.SidebarItem{Label: i18nui.T(ctx, key), Href: href, MatchPath: href, Icon: navIcon(icon)})
		}
	}
	add(b.cfg.Queue != nil, i18nui.KeyAdminQueue, "/queue", "layers")
	add(b.db != nil, i18nui.KeyAdminAudit, "/audit", "activity")
	add(b.cfg.Policy != nil, i18nui.KeyAdminRoles, "/rbac/roles", "shield")
	add(b.cfg.Auth != nil, i18nui.KeyAdminUserRoles, "/rbac/users", "users")
	add(b.cfg.ProcessModules != nil, i18nui.KeyAdminModules, "/modules", "cpu")
	return out
}

// navIcon draws a nav icon, or nothing for an empty name.
func navIcon(name string) render.HTML {
	if name == "" {
		return ""
	}
	return ui.Icon(name, ui.IconConfig{})
}

// navOf returns an entity's nav placement, or nil.
// countsInNav reports whether the entity's nav row shows a count: it is
// in the nav and its Nav does not set HideCount.
func countsInNav(e *entity.Entity) bool {
	n := navOf(e)
	return n == nil || (!n.Hide && !n.HideCount)
}

// navCountArea names the layout area holding the entity's nav count.
func navCountArea(e *entity.Entity) string { return "count-" + e.GetName() }

// navCount is the entity row's count: the records the caller can read,
// under the same elevated read the entity's list draws with. A refused
// or failed count draws nothing.
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

// brand is the sidebar's head: the logo (or the title's initial), the
// title and the "Back office" line.
type brand struct {
	component.ContextOnly
	b *Battery
}

func (s brand) RenderCtx(ctx context.Context) render.HTML {
	return ui.SidebarBrand(ui.SidebarBrandConfig{
		Name: s.b.title(ctx),
		Sub:  i18nui.T(ctx, i18nui.KeyAdminBrandSub),
		Logo: s.b.cfg.Logo,
	})
}

// ----- toolbar -----------------------------------------------------------------

// accountMenu is the signed-in user, their roles, the account page,
// and Sign out.
func (b *Battery) accountMenu(ctx context.Context) render.HTML {
	name := b.displayName(ctx)
	items := []ui.MenuItem{
		{Label: i18nui.TVars(ctx, i18nui.KeyAdminSignedInAs, map[string]string{"name": name}), Disabled: true},
	}
	if roles := callerHeldRoles(ctx); len(roles) > 0 {
		items = append(items, ui.MenuItem{
			Label:    i18nui.TVars(ctx, i18nui.KeyAdminYourRoles, map[string]string{"roles": strings.Join(roles, ", ")}),
			Disabled: true,
		})
	}
	items = append(items, ui.MenuItem{Separator: true}, ui.MenuItem{
		Label: i18nui.T(ctx, i18nui.KeyAdminAccountSettings),
		Href:  b.cfg.PathPrefix + "/account",
	})
	if b.cfg.SignOutPath != "" {
		dest := b.cfg.LoginPath
		if dest == "" {
			dest = "/"
		}
		out := interactive.Post(b.cfg.SignOutPath).OnSuccess(interactive.Navigate(dest))
		items = append(items, ui.MenuItem{Label: i18nui.T(ctx, i18nui.KeySignOut), Do: &out})
	}
	return ui.Menu(ui.MenuConfig{
		Label:    i18nui.T(ctx, i18nui.KeyAdminAccount),
		Avatar:   &ui.AvatarConfig{Name: name},
		Items:    items,
		Position: ui.MenuBottomEnd,
	})
}

// displayName names the signed-in user: their name, else their email,
// else their id.
func (b *Battery) displayName(ctx context.Context) string {
	if handlerUser(ctx) == nil {
		return ""
	}
	return cmp.Or(b.userName(ctx), userEmail(ctx), adminActorID(ctx))
}

// handlerUser is the signed-in user the auth middleware set, or nil.
func handlerUser(ctx context.Context) any {
	u, _ := handler.GetUser(ctx)
	return u
}

// crumbs draws the breadcrumbs for path: the product, then where the
// page sits. A record's crumb is its title, read behind its own gate.
func (b *Battery) crumbs(ctx context.Context, path string) render.HTML {
	items := []ui.Crumb{{Text: b.title(ctx), Href: b.cfg.PathPrefix}}
	items = append(items, b.trail(ctx, path)...)
	items[len(items)-1].Current = true
	items[len(items)-1].Href = ""
	return ui.Breadcrumbs(ui.BreadcrumbsConfig{CompactMobile: true, Ctx: ctx}, items...)
}

// trail is the crumbs after the product for one admin path.
func (b *Battery) trail(ctx context.Context, path string) []ui.Crumb {
	rest := strings.TrimPrefix(path, b.cfg.PathPrefix)
	switch rest {
	case "", "/":
		return []ui.Crumb{{Text: i18nui.T(ctx, i18nui.KeyAdminDashboard)}}
	case "/search":
		return []ui.Crumb{{Text: i18nui.T(ctx, i18nui.KeyAdminSearch)}}
	case "/account":
		return []ui.Crumb{{Text: i18nui.T(ctx, i18nui.KeyAdminAccountSettings)}}
	case "/queue":
		return []ui.Crumb{{Text: i18nui.T(ctx, i18nui.KeyAdminQueue)}}
	case "/audit":
		return []ui.Crumb{{Text: i18nui.T(ctx, i18nui.KeyAdminAudit)}}
	case "/rbac/roles":
		return []ui.Crumb{{Text: i18nui.T(ctx, i18nui.KeyAdminRoles)}}
	case "/rbac/users":
		return []ui.Crumb{{Text: i18nui.T(ctx, i18nui.KeyAdminUserRoles)}}
	case "/modules":
		return []ui.Crumb{{Text: i18nui.T(ctx, i18nui.KeyAdminModules)}}
	}
	for _, p := range b.cfg.Pages {
		if rest == p.Path {
			return []ui.Crumb{{Text: p.Title}}
		}
	}
	if e, id, ok := b.entityPath(rest); ok {
		list := ui.Crumb{Text: b.plural(ctx, e), Href: b.entityBase(e)}
		switch id {
		case "":
			return []ui.Crumb{list}
		case "create":
			return []ui.Crumb{list, {Text: i18nui.TVars(ctx, i18nui.KeyAdminPaletteNew, map[string]string{"entity": b.singular(ctx, e)})}}
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
