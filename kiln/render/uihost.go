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

	opts := []uihost.Option{}
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
			Description: w.App.PWA.Description, StartURL: w.App.PWA.StartURL,
			Scope: w.App.PWA.Scope, Display: uihost.PWADisplay(w.App.PWA.Display),
			ThemeColor: w.App.PWA.ThemeColor, BackgroundColor: w.App.PWA.BackgroundColor,
			DenyPaths: deny,
		}))
	}
	fwApp.Mount(uihost.New(site, opts...))
	return nil
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

func worldTheme(c world.AppConfig) style.Theme {
	t := uitheme.Default()
	for key, value := range c.Theme {
		if value == "" {
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
		if value != "" {
			t.DarkColors[key] = value
		}
	}
	return t
}
