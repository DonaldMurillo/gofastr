package render

import (
	"sort"
	"strings"

	coreapp "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core-ui/widget"
	corerender "github.com/DonaldMurillo/gofastr/core/render"
	corerouter "github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/ui"
	uitheme "github.com/DonaldMurillo/gofastr/framework/ui/theme"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
	"github.com/DonaldMurillo/gofastr/kiln/world"
)

// applyUIHostPages mounts the same SSR + hydration host used by current
// generated apps. It must run after CRUD, middleware, hooks, and explicit
// routes because UIHost is the router fallback.
func applyUIHostPages(fwApp *framework.App, w *world.World) error {
	name := w.App.Name
	if name == "" {
		name = "Kiln"
	}
	site := coreapp.NewApp(name).WithTheme(worldTheme(w.App))
	site.NoLLMMD = !w.App.LLMMD

	layouts := map[string]*coreapp.Layout{}
	defaultLayout := coreapp.NewLayout("app").WithContainer()
	if len(w.Nav) > 0 {
		sidebarCfg := ui.SidebarConfig{Title: name, Items: sidebarItems(w.Nav)}
		defaultLayout = coreapp.NewLayout("app").WithSidebar(ui.Sidebar(sidebarCfg))
		ui.MountSidebar(routerMounter{fwApp.Router()}, sidebarCfg)
	}
	layouts["app"] = defaultLayout
	layouts["marketing"] = coreapp.NewLayout("marketing").WithContainer()
	site.SetDefaultLayout(defaultLayout)

	paths := make([]string, 0, len(w.Pages))
	for path := range w.Pages {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		page := w.Pages[path]
		if page == nil {
			continue
		}
		layout := defaultLayout
		if page.Layout != nil && page.Layout.Name != "" {
			var ok bool
			layout, ok = layouts[page.Layout.Name]
			if !ok {
				layout = coreapp.NewLayout(page.Layout.Name).WithContainer()
				layouts[page.Layout.Name] = layout
			}
		}
		site.RegisterScreen(&coreapp.Screen{
			Path: path, Name: page.Name, Title: page.Title,
			Description: page.Description, Type: screenType(page.Type),
			Component: &worldScreen{page: page}, Layout: layout,
		}, layout)
	}

	// Build-mode reload client: page-structure world edits force an SPA
	// refresh of the current page (kiln/live/reload.go). Dev-mode SSE,
	// same exception class as framework/dev livereload.
	opts := []uihost.Option{uihost.WithExtraScripts("/.kiln/reload.js")}
	if w.App.StaticDir != "" {
		opts = append(opts, uihost.WithStaticDir(w.App.StaticDir))
	}
	if w.App.LLMMD {
		opts = append(opts, uihost.WithPublicLLMMD())
	}
	if w.App.PWA.Enabled {
		deny := []string{"/kiln", "/mcp"}
		if prefix := strings.Trim(w.App.APIPrefix, "/"); prefix != "" && prefix != "api" {
			deny = append(deny, "/"+prefix)
		}
		if authPath := strings.TrimSpace(w.App.Auth.BasePath); authPath != "" && authPath != "/auth" {
			deny = append(deny, authPath)
		}
		opts = append(opts, uihost.WithPWA(uihost.PWAConfig{
			Name: w.App.PWA.Name, ShortName: w.App.PWA.ShortName,
			Description: w.App.PWA.Description, StartURL: sameOriginPWAPath(w.App.PWA.StartURL),
			Scope: sameOriginPWAPath(w.App.PWA.Scope), Display: uihost.PWADisplay(w.App.PWA.Display),
			ThemeColor: w.App.PWA.ThemeColor, BackgroundColor: w.App.PWA.BackgroundColor,
			DenyPaths: deny,
		}))
	}
	fwApp.Mount(uihost.New(site, opts...))
	return nil
}

// sameOriginPWAPath coerces an agent-authored start_url/scope to a
// same-origin path. The manifest is what the operator's browser
// installs: a scheme-relative "//evil.example/pwa" (or the backslash
// spelling, which browsers normalize to it) launches the installed PWA
// on the attacker's origin under the kiln app's name, so anything that
// is not a plain root-absolute path falls back to the same default
// uihost uses. Coerced, not refused: the preview keeps its PWA chrome,
// and freeze refuses the value outright at graduation.
func sameOriginPWAPath(v string) string {
	normalized := strings.ReplaceAll(strings.ToLower(v), "\\", "/")
	if v == "" ||
		(strings.HasPrefix(v, "/") && !strings.HasPrefix(normalized, "//") && !strings.Contains(normalized, "://")) {
		return v
	}
	return "/"
}

type worldScreen struct{ page *world.Page }

func (s worldScreen) Render() corerender.HTML {
	// Kiln's build-mode node renderer is the sole trusted source for these
	// attributes; it allow-lists props and strips inline handlers/raw HTML.
	// The marker keeps the legacy data-kiln-tool delegation scoped to this
	// rendered world instead of trusting the entire uihost document.
	return corerender.Tag("div", map[string]string{"data-fui-trusted": ""}, RenderNode(s.page.Tree))
}

func screenType(value string) coreapp.ScreenType {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "drawer":
		return coreapp.ScreenDrawer
	case "sheet":
		return coreapp.ScreenSheet
	case "dialog":
		return coreapp.ScreenDialog
	default:
		return coreapp.ScreenPage
	}
}

func sidebarItems(items []world.NavItem) []ui.SidebarItem {
	out := make([]ui.SidebarItem, 0, len(items))
	for _, item := range items {
		var roles []string
		if item.Role != "" {
			roles = []string{item.Role}
		}
		out = append(out, ui.SidebarItem{
			Label: item.Label, Href: item.Href, Roles: roles,
			Children: sidebarItems(item.Items),
		})
	}
	return out
}

type routerMounter struct{ r *corerouter.Router }

func (m routerMounter) MountWidget(def *widget.Definition) { widget.Mount(m.r, def) }

// safeThemeValue reports whether an agent-authored theme token value can be
// interpolated into the app stylesheet.
//
// core-ui/style emits `--color-<name>: <value>;` (tokens.go darkSchemeCSS,
// CSSCustomPropertiesOf) with NO escaping on either side, and the result is
// served as the app stylesheet. An unfiltered value closes its declaration
// and appends arbitrary rules, which is a privilege escalation relative to
// the node renderer, whose whole job is to strip `class` and inline styles
// from this same untrusted IR. Escaping belongs upstream in core-ui/style;
// this is the ingestion-side guard for the one caller that feeds it
// request-authored input.
//
// The allowed shape is a CSS color or font stack: alphanumerics plus the
// punctuation those need. Everything that could start a new declaration,
// rule, or at-rule (`;{}@\<>`), a comment (`/*`), or a network fetch
// (`url(`, `image(`) is rejected outright.
func safeThemeValue(v string) bool {
	if v == "" || len(v) > 128 {
		return false
	}
	for _, r := range v {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case strings.ContainsRune(" #%.,-+()/'\"_", r):
		default:
			return false
		}
	}
	lower := strings.ToLower(v)
	for _, bad := range []string{"url(", "image(", "expression(", "/*", "*/", "//"} {
		if strings.Contains(lower, bad) {
			return false
		}
	}
	return true
}

// safeThemeKey bounds an agent-authored DARK token name. Unlike the light
// palette (a closed switch below), DarkColors is a free map whose key lands
// directly in `--color-<key>:`, so a key of `x: y; } :root { --color-primary`
// injects a rule of its own.
func safeThemeKey(k string) bool {
	if k == "" || len(k) > 64 {
		return false
	}
	for i, r := range k {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
		case r == '-' && i > 0:
		default:
			return false
		}
	}
	return true
}

func worldTheme(c world.AppConfig) style.Theme {
	t := uitheme.Default()
	for key, value := range c.Theme {
		if !safeThemeValue(value) {
			continue
		}
		switch key {
		case "primary":
			t.Colors.Primary.Value = value
		case "primary-fg":
			t.Colors.PrimaryFg.Value = value
		case "secondary":
			t.Colors.Secondary.Value = value
		case "background":
			t.Colors.Background.Value = value
		case "surface":
			t.Colors.Surface.Value = value
		case "surface-soft":
			t.Colors.SurfaceSoft.Value = value
		case "text":
			t.Colors.Text.Value = value
		case "text-muted":
			t.Colors.TextMuted.Value = value
		case "text-subtle":
			t.Colors.TextSubtle.Value = value
		case "border":
			t.Colors.Border.Value = value
		case "border-strong":
			t.Colors.BorderStrong.Value = value
		case "accent":
			t.Colors.Accent.Value = value
		case "success":
			t.Colors.Success.Value = value
		case "warning":
			t.Colors.Warning.Value = value
		case "danger":
			t.Colors.Danger.Value = value
		case "info":
			t.Colors.Info.Value = value
		case "font_body":
			t.Fonts.Body.Value = value
		case "font_heading", "font_display":
			t.Fonts.Heading.Value = value
		}
	}
	for key, value := range c.ThemeDark {
		if safeThemeKey(key) && safeThemeValue(value) {
			t.DarkColors[key] = value
		}
	}
	return t
}
