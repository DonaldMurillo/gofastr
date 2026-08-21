package main

import (
	"context"
	"database/sql"
	"os"

	"github.com/DonaldMurillo/gofastr/battery/auth"
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/widget"
	"github.com/DonaldMurillo/gofastr/core-ui/widget/preset"
	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/access"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

const (
	appName   = "UpgradeFixture"
	appModule = "example.com/upgrade-fixture"
	dbDriver  = "sqlite"
	dbURL     = "file:fixture.db"
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

// fontFaceCSS holds the @font-face rules for the app's fonts, shared by
// the UI host and the admin battery so every surface loads identical fonts.
const fontFaceCSS = ""

// sidebarConfig returns the navigation sidebar configuration.
func sidebarConfig() ui.SidebarConfig {
	return ui.SidebarConfig{Title: "UpgradeFixture", Items: []ui.SidebarItem{
		{Label: "Home", Href: "/"},
		{Label: "Tasks", Href: "/tasks"},
		{Label: "Tags", Href: "/tags"},
	}, Footer: ui.SignOut(ui.SignOutConfig{Next: "/"})}
}

var (
	appLayout *app.Layout
)

var (
	// authMgr is the app's auth manager, set by RegisterGenerated when
	// auth is enabled. Read it from a new file (e.g. to wire admin.Config.Auth).
	authMgr *auth.AuthManager
	// rolePolicy is the app's RBAC policy, set by RegisterGenerated when an
	// entity declares access: permissions. Read it from a new file (e.g. to
	// wire admin.Config.Policy or append finer-grained grants).
	rolePolicy *access.RolePolicy
)

// RegisterGenerated wires blueprint-generated screens, endpoints, middleware, and plugins.
func RegisterGenerated(fwApp *framework.App, site *app.App, db *sql.DB) {
	if site == nil {
		site = app.NewApp("UpgradeFixture")
	}
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
		// WARNING: auth runs in DEV MODE: HTTP-friendly cookies (no
		// Secure flag, plain session_id name) and a per-process JWT
		// secret minted at startup. Do NOT deploy like this: set
		// `dev_mode: false` and `jwt_secret` under app.auth in the
		// blueprint, serve over HTTPS, then regenerate.
		authCfg := auth.AuthConfig{DevMode: true, JWTSecret: os.Getenv("JWT_SECRET")}
		authCfg.UserStore = auth.NewEntityUserStore(db, "auth_users")
		authCfg.SessionStore = auth.NewEntitySessionStore(db, "auth_sessions")
		authMgr = auth.New(authCfg)
		authMgr.Use(auth.NewCorePlugin())
		authMgr.Init(fwApp)
		auth.SetDefaultLoginErrorPath("/login")
		// Resolve the session cookie to a user on every request so
		// owner/access-scoped CRUD sees the logged-in user. Without
		// this, authorized requests fail closed (401) just like
		// anonymous ones.
		fwApp.Use(auth.SessionMiddleware(authMgr))
		// Entities declare `access:` permissions; install a RolePolicy so the
		// signed-in user's roles resolve to those permissions on the gated
		// CRUD API. The admin role holds the wildcard (full access, the same
		// surface the back-office manages). Add finer per-role Grants here,
		// OR from a new file that appends to rolePolicy, both are additive
		// now that rolePolicy is package-level. Without this, every write 403s.
		rolePolicy = access.NewRolePolicy()
		rolePolicy.Grant("admin", access.Wildcard)
		fwApp.Use(access.Middleware(rolePolicy, func(ctx context.Context) []string {
			if u, ok := handler.GetUser(ctx); ok && u != nil {
				if rh, ok := u.(interface{ GetRoles() []string }); ok {
					return rh.GetRoles()
				}
			}
			return nil
		}))
		// auth.CSRF is intentionally NOT mounted: this generated surface
		// is JSON-first (REST CRUD + /mcp), and the CSRF middleware 403s
		// any unsafe-method request that doesn't echo the csrf cookie as
		// an X-CSRF-Token header, which plain JSON/MCP clients don't.
		// Session cookies are SameSite=Strict, so cross-site form posts
		// don't carry the session in modern browsers. If you add browser
		// HTML forms, mount auth.CSRF. See `gofastr docs blueprints`
		// (Auth section) and `gofastr docs auth`.
	}
	mountGenerated(fwApp, site, db)
	_ = routerMounter{}
}

// routerMounter adapts framework's *router.Router to ui.WidgetMounter.
type routerMounter struct{ r *router.Router }

func (m routerMounter) MountWidget(def *widget.Definition) {
	widget.Mount(m.r, def)
}
