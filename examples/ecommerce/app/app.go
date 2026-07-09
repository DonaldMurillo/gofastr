package main

import (
	"database/sql"
	"net/http"
	"os"

	"github.com/DonaldMurillo/gofastr/battery/auth"
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core-ui/widget"
	"github.com/DonaldMurillo/gofastr/core-ui/widget/preset"
	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

const (
	appName   = "ShopFront"
	appModule = "github.com/DonaldMurillo/gofastr/examples/ecommerce"
	dbDriver  = "sqlite"
	dbURL     = "file:shop.db"
	staticDir = ""
	apiPrefix = "api"
)

// appBaseCSS is an owned extension point for app-specific base CSS.
// It's empty by default: every generated surface composes framework/ui
// components and core-ui/app layouts that ship their own CSS, so the
// generated app ships no bespoke styling. Add app CSS here or in static/app.css.
func appBaseCSS() string {
	return ""
}

func appTheme() style.Theme {
	theme := style.DefaultTheme()
	theme.Colors.Background.Value = "#F8FAFC"
	theme.Colors.Border.Value = "#E2E8F0"
	theme.Colors.Danger.Value = "#EF4444"
	theme.Colors.Primary.Value = "#2563EB"
	theme.Colors.PrimaryFg.Value = "#FFFFFF"
	theme.Colors.Secondary.Value = "#F59E0B"
	theme.Colors.Success.Value = "#10B981"
	theme.Colors.Surface.Value = "#FFFFFF"
	theme.Colors.Text.Value = "#0F172A"
	theme.Colors.TextMuted.Value = "#64748B"
	theme.Colors.Warning.Value = "#F59E0B"
	return theme
}

// fontFaceCSS holds the @font-face rules for the app's fonts, shared by
// the UI host and the admin battery so every surface loads identical fonts.
const fontFaceCSS = ""

// sidebarConfig returns the navigation sidebar configuration.
func sidebarConfig() ui.SidebarConfig {
	return ui.SidebarConfig{Title: "ShopFront", Items: []ui.SidebarItem{
		{Label: "Home", Href: "/"},
		{Label: "Products", Href: "/products"},
		{Label: "Categories", Href: "/categories"},
		{Label: "Orders", Href: "/orders"},
		{Label: "Reviews", Href: "/reviews"},
	}, Footer: ui.SignOut(ui.SignOutConfig{Next: "/"})}
}

var (
	appLayout *app.Layout
)

// RegisterGenerated wires blueprint-generated screens, endpoints, middleware, and plugins.
func RegisterGenerated(fwApp *framework.App, site *app.App, db *sql.DB) {
	if site == nil {
		site = app.NewApp("ShopFront")
	}
	site.WithTheme(appTheme())
	sbCfg := sidebarConfig()
	sb := ui.Sidebar(sbCfg)
	appLayout = app.NewLayout("app").WithSidebar(sb)
	site.SetDefaultLayout(appLayout)
	ui.MountSidebar(routerMounter{fwApp.Router()}, sbCfg)
	{
		stack := preset.ToastStack("blueprint-toasts").Build()
		widget.Mount(fwApp.Router(), &stack)
	}
	{
		// WARNING: auth runs in DEV MODE — HTTP-friendly cookies (no
		// Secure flag, plain session_id name) and a per-process JWT
		// secret minted at startup. Do NOT deploy like this: set
		// `dev_mode: false` and `jwt_secret` under app.auth in the
		// blueprint, serve over HTTPS, then regenerate.
		authCfg := auth.AuthConfig{DevMode: true, JWTSecret: os.Getenv("JWT_SECRET")}
		authCfg.UserStore = auth.NewEntityUserStore(db, "auth_users")
		authCfg.SessionStore = auth.NewEntitySessionStore(db, "auth_sessions")
		authMgr := auth.New(authCfg)
		authMgr.Use(auth.NewCorePlugin())
		authMgr.Init(fwApp)
		auth.SetDefaultLoginErrorPath("/login")
		// Resolve the session cookie to a user on every request so
		// owner/access-scoped CRUD sees the logged-in user. Without
		// this, authorized requests fail closed (401) just like
		// anonymous ones.
		fwApp.Use(auth.SessionMiddleware(authMgr))
		// auth.CSRF is intentionally NOT mounted: this generated surface
		// is JSON-first (REST CRUD + /mcp), and the CSRF middleware 403s
		// any unsafe-method request that doesn't echo the csrf cookie as
		// an X-CSRF-Token header — which plain JSON/MCP clients don't.
		// Session cookies are SameSite=Strict, so cross-site form posts
		// don't carry the session in modern browsers. If you add browser
		// HTML forms, mount auth.CSRF — see `gofastr docs blueprints`
		// (Auth section) and `gofastr docs auth`.
	}
	{
		_, b := ui.ConfirmAction(ui.ConfirmActionConfig{Name: "delete-products", TriggerLabel: "Delete", Title: "Delete this Products?", Body: "This action will soft-delete the record. It can be restored later.", RPCPath: "/products/{id}"})
		d := b.Build()
		widget.Mount(fwApp.Router(), &d)
	}
	mountGenerated(fwApp, site, db)
	fwApp.Router().Handle("POST", "/orders/{id}/confirm", http.HandlerFunc(ConfirmOrder))
	fwApp.Router().Handle("POST", "/orders/{id}/ship", http.HandlerFunc(ShipOrder))
	fwApp.Use(RequestLoggerMiddleware)
	fwApp.RegisterPlugin(AnalyticsPlugin{})
	_ = routerMounter{}
}

// routerMounter adapts framework's *router.Router to ui.WidgetMounter.
type routerMounter struct{ r *router.Router }

func (m routerMounter) MountWidget(def *widget.Definition) {
	widget.Mount(m.r, def)
}
