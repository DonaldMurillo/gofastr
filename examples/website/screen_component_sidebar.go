package main

import (
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

// SidebarScreen documents framework/ui.Sidebar.
type SidebarScreen struct{}

func (s *SidebarScreen) ScreenTitle() string        { return "Sidebar" }
func (s *SidebarScreen) ScreenDescription() string  { return "Responsive nav column: inline ≥ md, hamburger + drawer < md." }
func (s *SidebarScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *SidebarScreen) Render() render.HTML {
	// Same SidebarConfig powers BOTH the inline column AND the drawer
	// registered globally in component_demos.go's MountSidebar call.
	// One source of truth across breakpoints.
	cfg := ui.SidebarConfig{
		Title:       "Workspace",
		CurrentPath: "/components/sidebar",
		// Keep the auto hamburger so resizing past 768px still shows
		// the responsive transition inside this demo. The explicit
		// "Open sidebar as drawer" button below works at any viewport
		// and hits the same widget.
		Items: []ui.SidebarItem{
			{Label: "Dashboard", Href: "/components/sidebar?section=dashboard"},
			{Label: "Components", MatchPath: "/components"},
			{Label: "Customers", Href: "/customers"},
			{Label: "Settings", Children: []ui.SidebarItem{
				{Label: "Profile", Href: "/components/sidebar?section=profile"},
				{Label: "Team", Href: "/components/sidebar?section=team"},
				{Label: "Billing", Href: "/components/sidebar?section=billing"},
			}},
		},
		Footer: render.Tag("small", nil, render.Text("v0.4.0")),
	}
	sidebar := ui.Sidebar(cfg).Render()

	// The app-shell mockup: sidebar on the left, fake page content on
	// the right. The shell is its own scrollable region so users can
	// see how the sidebar sits relative to actual page content
	// without resizing the whole browser.
	appShell := render.Tag("div", map[string]string{"class": "demo-app-shell"},
		render.Tag("div", map[string]string{"class": "demo-app-shell__sidebar"},
			sidebar,
		),
		render.Tag("div", map[string]string{"class": "demo-app-shell__main"},
			render.Tag("div", map[string]string{"class": "demo-app-shell__main-header"},
				html.Heading(html.HeadingConfig{Level: 3}, render.Text("Dashboard")),
				render.Tag("p", nil, render.Text("Welcome to your workspace. The sidebar to the left is the framework/ui.Sidebar component, with active-route highlighting on the current entry.")),
			),
			render.Tag("div", map[string]string{"class": "demo-app-shell__cards"},
				demoStatCard("Active users", "1,284", "+8.2%"),
				demoStatCard("Open issues", "42", "−3"),
				demoStatCard("Revenue", "$28.4k", "+12%"),
			),
			render.Tag("p", map[string]string{"class": "demo-meta"}, render.Text(
				"The sidebar and main pane are independent scroll containers — long nav stays accessible while content scrolls.")),
		),
	)

	// Drawer trigger — works on any viewport, opens the same content
	// tree via the SSR-registered ui-sidebar-drawer widget.
	drawerTrigger := render.Tag("button", map[string]string{
		"class":         "cta-button",
		"data-fui-open": "ui-sidebar-drawer",
	}, render.Text("☰  Open sidebar as drawer"))

	// Variant cards.
	variantCards := render.Tag("div", map[string]string{"class": "demo-variant-cards"},
		variantCard("Persistent", "default", "Inline column ≥ md, drawer under. The most common app-shell pattern."),
		variantCard("Collapsible", "soon", "Adds a chevron toggle for a compact rail; state persists in localStorage."),
		variantCard("OffCanvas", "soon", "Always opens via hamburger; no inline column. For dense data UIs."),
	)

	src := `// 1. App start — register the drawer companion once:
ui.MountSidebar(routerAdapter{r}, sidebarCfg)

// 2. Render the inline sidebar wherever your layout expects it:
ui.Sidebar(ui.SidebarConfig{
    Title:       "Workspace",
    CurrentPath: r.URL.Path,
    Variant:     ui.SidebarPersistent,
    Items: []ui.SidebarItem{
        {Label: "Dashboard", Href: "/"},
        {Label: "Components", MatchPath: "/components"},
        {Label: "Customers", Href: "/customers"},
        {Label: "Settings", Children: []ui.SidebarItem{
            {Label: "Profile", Href: "/settings/profile"},
            {Label: "Team",    Href: "/settings/team"},
            {Label: "Billing", Href: "/settings/billing"},
        }},
    },
})

// The hamburger trigger (auto-rendered) carries data-fui-open=
// "ui-sidebar-drawer" so a single click on narrow viewports opens
// the same content tree inside the drawer registered by MountSidebar.`

	return render.Tag("div", nil,
		render.Tag("a", map[string]string{"href": "/components/", "class": "doc-back"},
			render.Text("← Components")),

		html.Heading(html.HeadingConfig{Level: 1}, render.Text("Sidebar")),
		render.Tag("p", map[string]string{"class": "lede"}, render.Text(
			"A responsive primary-nav surface. At ≥ md viewports it renders as an inline column; at < md a hamburger button opens the same content tree inside a Drawer. Active-route highlighting from the current URL, nested groups built on <details data-fui-disclosure> auto-open when a descendant matches.")),

		// --- Live app-shell demo ---
		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Live — sidebar inside a real layout")),
		render.Tag("p", nil, render.Text(
			"The sidebar in its native habitat: a two-column app shell with content beside it. The 'Components' item is highlighted because MatchPath: '/components' covers this page; the 'Settings' group auto-opens when one of its children matches. Resize your browser past 768 px to see the inline column give way to a hamburger.")),
		render.Tag("div", map[string]string{"class": "demo-frame demo-frame--wide"},
			render.Tag("div", map[string]string{"class": "demo-live"},
				render.Tag("div", map[string]string{"class": "demo-label"}, render.Text("Live")),
				appShell,
			),
			render.Tag("div", map[string]string{"class": "demo-source"},
				render.Tag("div", map[string]string{"class": "demo-label"}, render.Text("Source")),
				ui.CodeBlock(ui.CodeBlockConfig{Code: src, Language: "go"}),
			),
		),

		// --- Drawer companion ---
		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Sidebar + Drawer, together")),
		render.Tag("p", nil, render.Text(
			"The sidebar's < md hamburger trigger opens a drawer that mirrors the same nav tree. Apps register the drawer once via MountSidebar so the same SidebarConfig powers both surfaces — no duplicate content tree, no drift between desktop and mobile views.")),
		render.Tag("p", nil, render.Text(
			"You can also trigger the drawer manually from anywhere — the button below works at any viewport. Useful for keyboard shortcuts, command palettes, or a 'Browse all sections' link on dense pages.")),
		render.Tag("div", map[string]string{"class": "demo-frame"},
			render.Tag("div", map[string]string{"class": "demo-live"},
				render.Tag("div", map[string]string{"class": "demo-label"}, render.Text("Live")),
				drawerTrigger,
			),
			render.Tag("div", map[string]string{"class": "demo-source"},
				render.Tag("div", map[string]string{"class": "demo-label"}, render.Text("Source")),
				ui.CodeBlock(ui.CodeBlockConfig{Language: "go", Code: `<button data-fui-open="ui-sidebar-drawer">☰  Open sidebar as drawer</button>

// Server-side, registered once at app start:
ui.MountSidebar(routerAdapter{r}, sidebarCfg)
// MountSidebar internally calls preset.Drawer("ui-sidebar-drawer")
// with sidebarCfg as its content tree.`}),
			),
		),

		// --- Variants ---
		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Variants")),
		render.Tag("p", nil, render.Text(
			"Three shipped variants cover the common app-shell patterns. Variant on SidebarConfig defaults to SidebarPersistent.")),
		variantCards,

		// --- Active-route highlighting ---
		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Active-route highlighting")),
		render.Tag("ul", nil,
			render.Tag("li", nil, render.Text("Exact-href match: "), render.Tag("code", nil, render.Text(`Href == r.URL.Path`)), render.Text(" → aria-current=\"page\".")),
			render.Tag("li", nil, render.Text("Prefix match: "), render.Tag("code", nil, render.Text(`MatchPath: "/customers"`)), render.Text(" highlights on every "), render.Tag("code", nil, render.Text("/customers/...")), render.Text(" route.")),
			render.Tag("li", nil, render.Text("Manual: "), render.Tag("code", nil, render.Text(`Active: true`)), render.Text(" forces highlight when the route doesn't map 1:1 to a URL.")),
			render.Tag("li", nil, render.Text("Nested groups auto-open when any descendant is active — so navigating to /settings/billing lands on a Settings group that's already expanded.")),
		),
	)
}

// demoStatCard is a tiny stat tile used in the sidebar demo's main pane.
func demoStatCard(label, value, delta string) render.HTML {
	return render.Tag("div", map[string]string{"class": "demo-stat-card"},
		render.Tag("div", map[string]string{"class": "demo-stat-card__label"}, render.Text(label)),
		render.Tag("div", map[string]string{"class": "demo-stat-card__value"}, render.Text(value)),
		render.Tag("div", map[string]string{"class": "demo-stat-card__delta"}, render.Text(delta)),
	)
}

func variantCard(name, status, body string) render.HTML {
	return render.Tag("div", map[string]string{"class": "demo-variant-card"},
		render.Tag("div", map[string]string{"class": "demo-variant-card__header"},
			html.Strong(html.TextConfig{}, render.Text(name)),
			render.Tag("span", map[string]string{"class": "demo-variant-card__status"}, render.Text(status)),
		),
		render.Tag("p", nil, render.Text(body)),
	)
}
