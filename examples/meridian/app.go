package main

import (
	"context"
	"database/sql"
	"log"
	"net/url"
	"os"

	"github.com/DonaldMurillo/gofastr/battery/auth"
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/app/decide"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/interactive"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core-ui/widget"
	"github.com/DonaldMurillo/gofastr/core-ui/widget/preset"
	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/framework"
	fwimage "github.com/DonaldMurillo/gofastr/framework/image"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

const (
	appName   = "Meridian"
	appModule = "github.com/DonaldMurillo/gofastr/examples/meridian"
	dbDriver  = "sqlite"
	dbURL     = "file:meridian.db"
	staticDir = "static"
	apiPrefix = "api"
)

// appBaseCSS is an owned extension point for app-specific base CSS.
// It's empty by default: every generated surface composes framework/ui
// components and core-ui/app layouts that ship their own CSS, so the
// generated app ships no bespoke styling. Add app CSS here or in static/app.css.
func appBaseCSS() string {
	return ""
}

// authPolicy gates a screen: redirect anonymous GETs to the login
// page (with ?next=) and 403 a signed-in user missing the required role.
func authPolicy(loginPath, role string) app.Policy {
	return app.PolicyFunc(func(ctx context.Context) app.Decision {
		u, ok := handler.GetUser(ctx)
		if !ok || u == nil {
			next := "/"
			if r := app.RequestFromContext(ctx); r != nil {
				next = r.URL.Path
			}
			return decide.Redirect(loginPath + "?next=" + url.QueryEscape(next))
		}
		if role != "" {
			if rh, ok := u.(interface{ GetRoles() []string }); ok {
				for _, r := range rh.GetRoles() {
					if r == role {
						return decide.Allow()
					}
				}
			}
			return decide.Block(403, "Forbidden")
		}
		return decide.Allow()
	})
}

// guestPolicy gates a guest-only screen (login / signup): a
// signed-in visitor is redirected to the app instead of seeing a sign-in
// form they're already past.
func guestPolicy(appHome string) app.Policy {
	return app.PolicyFunc(func(ctx context.Context) app.Decision {
		if u, ok := handler.GetUser(ctx); ok && u != nil {
			return decide.Redirect(appHome)
		}
		return decide.Allow()
	})
}

// marketingHeader / Footer wrap the public marketing layout.
func marketingHeader(ctx context.Context) render.HTML {
	nav := []ui.SiteHeaderLink{{Label: "Pricing", Href: "/pricing"}, {Label: "About", Href: "/about"}}
	var actions render.HTML
	if u, ok := handler.GetUser(ctx); ok && u != nil {
		nav = append(nav, ui.SiteHeaderLink{Label: "Dashboard", Href: "/app"})
		actions = ui.Cluster(ui.ClusterConfig{Gap: ui.GapSM, Align: ui.AlignCenter, NoWrap: true}, ui.SignOut(ui.SignOutConfig{Next: "/"}), ui.ThemeToggle(ui.ThemeToggleConfig{Variant: ui.ThemeToggleIcon}))
	} else {
		actions = ui.Cluster(ui.ClusterConfig{Gap: ui.GapSM, Align: ui.AlignCenter, NoWrap: true}, ui.LinkButton(ui.LinkButtonConfig{Label: "Sign in", Href: "/login", Variant: ui.ButtonSecondary, Size: ui.ButtonSizeSmall}), ui.ThemeToggle(ui.ThemeToggleConfig{Variant: ui.ThemeToggleIcon}))
	}
	return ui.SiteHeader(ui.SiteHeaderConfig{
		Brand:    ui.Link(ui.LinkConfig{Href: "/", Text: appName}),
		NavItems: nav,
		Drawer:   ui.SiteHeaderDrawerSheet,
		Actions:  actions,
	})
}

func marketingFooter() render.HTML {
	return ui.SiteFooter(ui.SiteFooterConfig{
		Lead: ui.Link(ui.LinkConfig{Href: "/", Text: appName}),
		Columns: []ui.SiteFooterColumn{
			{Title: "Product", Links: []ui.SiteFooterLink{{Label: "Pricing", Href: "/pricing"}}},
			{Title: "Company", Links: []ui.SiteFooterLink{{Label: "About", Href: "/about"}}},
			{Title: "Legal", Links: []ui.SiteFooterLink{{Label: "Terms", Href: "/terms"}, {Label: "Privacy", Href: "/privacy"}}},
		},
	})
}

// appIconPNG generates Meridian's app-icon source at startup — a diagonal
// indigo→teal gradient in the brand's primary/secondary hues.
// uihost.WithAppIcon derives /favicon.ico, the sized PNGs, and the head
// links from this one image, so no binary icon assets live in the repo.
func appIconPNG() []byte {
	img, err := fwimage.NewGradient(512, 512, "#4338CA", "#0E7C86")
	if err != nil {
		return nil // WithAppIcon warns and skips on undecodable input
	}
	b, err := img.PNG().Bytes()
	if err != nil {
		return nil
	}
	return b
}

func appTheme() style.Theme {
	theme := style.DefaultTheme()
	theme.Colors.Accent.Value = "#4338CA"
	theme.Colors.Background.Value = "#F8F7F4"
	theme.Colors.Border.Value = "#E7E5DF"
	theme.Colors.BorderStrong.Value = "#33334A"
	theme.Colors.Danger.Value = "#B91C1C" // 5.2:1 on its 15% tinted chip — was #B42318 (fails AA on badges)
	theme.Colors.Info.Value = "#1D4ED8"
	theme.Colors.Primary.Value = "#4338CA"
	theme.Colors.PrimaryFg.Value = "#FFFFFF"
	theme.Colors.Secondary.Value = "#0E7C86"
	theme.Colors.Success.Value = "#166534" // 5.6:1 on its 15% tinted chip — was #15803D (4.10:1, fails AA on badges)
	theme.Colors.Surface.Value = "#FFFFFF"
	theme.Colors.SurfaceSoft.Value = "#F2F1EC"
	theme.Colors.Text.Value = "#1B1B2A"
	theme.Colors.TextMuted.Value = "#65657A"
	theme.Colors.TextSubtle.Value = "#6A6A72" // 4.7:1 on surface-soft — was #9A9AAB (2.3:1, fails AA on eyebrows/footer titles)
	theme.Colors.Warning.Value = "#854D0E"    // 5.4:1 on its 15% tinted chip — was #B45309 (fails AA on badges)
	theme.Fonts.Body.Value = "'Hanken Grotesk', ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif"
	theme.Fonts.Heading.Value = "'Bricolage Grotesque', 'Hanken Grotesk', ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif"
	theme.DarkColors = map[string]string{
		"accent":        "#8B80F2",
		"background":    "#15141B",
		"border":        "#322E3D",
		"border-strong": "#494457",
		"danger":        "#F87171",
		"info":          "#60A5FA",
		"primary":       "#8B80F2",
		"primary-fg":    "#15141B",
		"secondary":     "#2DD4BF",
		"success":       "#4ADE80",
		"surface":       "#1F1D27",
		"surface-soft":  "#29262F",
		"text":          "#ECEAF3",
		"text-muted":    "#A29FB0",
		"text-subtle":   "#9498AC", // 5.9:1 on dark surface — was #726F80 (3.8:1, fails AA on eyebrows/footer)
		"warning":       "#FBBF24",
	}
	return theme
}

// inkTheme is the app palette inverted to its dark values — registered as
// a section-level theme override (ui.Themed) so a marketing band renders
// as dark "ink" in BOTH color schemes. The override re-declares every
// token under a .fui-theme-<hash> class in app.css; components inside the
// wrapped subtree dereference var(--color-*) against it via the cascade.
func inkTheme() style.Theme {
	t := appTheme()
	for field, value := range map[*style.Color]string{
		&t.Colors.Accent:       "#8B80F2",
		&t.Colors.Background:   "#15141B",
		&t.Colors.Border:       "#322E3D",
		&t.Colors.BorderStrong: "#494457",
		&t.Colors.Danger:       "#F87171",
		&t.Colors.Info:         "#60A5FA",
		&t.Colors.Primary:      "#8B80F2",
		&t.Colors.PrimaryFg:    "#15141B",
		&t.Colors.Secondary:    "#2DD4BF",
		&t.Colors.Success:      "#4ADE80",
		&t.Colors.Surface:      "#1F1D27",
		&t.Colors.SurfaceSoft:  "#29262F",
		&t.Colors.Text:         "#ECEAF3",
		&t.Colors.TextMuted:    "#A29FB0",
		&t.Colors.TextSubtle:   "#9498AC",
		&t.Colors.Warning:      "#FBBF24",
	} {
		field.Value = value
	}
	return t
}

// inkBand is the registered handle screens wrap dark marketing bands with.
var inkBand = style.RegisterThemeOverride(inkTheme())

// customersList is the one configured Customers list. The screen and the
// island endpoint share it so a sort/page RPC returns exactly the table
// the initial SSR painted. Page size 8 keeps the island's pagination
// exercised by the seed data alone.
func customersList() ResourceConfig {
	return appResources["customers"].
		WithColumns("name", "email", "company", "status", "mrr").
		WithSearch("name").
		WithFilters(ResFilter{Key: "status", Label: "Status", Type: "enum", Values: []string{"trialing", "active", "past_due", "canceled"}}).
		WithLimit(8).
		WithCreate().
		WithHeading("Customers").
		WithEmpty("No customers yet — add your first to get started.").
		WithIsland("/api/tables/customers").
		WithActions(interactive.OpenOnClick(ui.Button(ui.ButtonConfig{Label: "Quick add", Variant: ui.ButtonSecondary}), "customer-quick-add"))
}

// quickAddCustomerModal is a plain preset.Modal: the centered slot paints
// the default panel surface (background, border, radius, padding), so the
// body ships zero chrome of its own — just a heading and a ui.Form. The
// form RPCs the auto-CRUD endpoint; on success it closes the modal, resets
// the fields, and SPA-navigates to the list so the new row appears.
func quickAddCustomerModal() widget.Definition {
	heading := html.Heading(html.HeadingConfig{Level: 2, ID: "customer-quick-add-title"}, render.Text("New customer"))
	form := ui.Form(ui.FormConfig{Action: "/api/customers", Method: "POST", SubmitLabel: "Add customer", ExtraAttrs: interactive.Post("/api/customers").
		OnSuccess(interactive.CloseWidget(), interactive.ResetForm(), interactive.Navigate("/app/customers")).Attrs()},
		ui.FormField(ui.FormFieldConfig{Label: "Name", For: "qa-name", Required: true, Input: html.Input(html.InputConfig{Type: "text", Name: "name", ID: "qa-name", ExtraAttrs: html.Attrs{"required": "required"}})}),
		ui.FormField(ui.FormFieldConfig{Label: "Email", For: "qa-email", Required: true, Input: html.Input(html.InputConfig{Type: "email", Name: "email", ID: "qa-email", ExtraAttrs: html.Attrs{"required": "required"}})}),
		ui.FormField(ui.FormFieldConfig{Label: "Company", For: "qa-company", Input: html.Input(html.InputConfig{Type: "text", Name: "company", ID: "qa-company"})}),
	)
	return preset.Modal("customer-quick-add").
		Hidden().
		LabelledBy("customer-quick-add-title").
		Pages("/app/customers").
		Slot("body", app.NewStaticComponent(render.Join(heading, form))).
		Build()
}

// fontFaceCSS holds the @font-face rules for the app's fonts, shared by
// the UI host and the admin battery so every surface loads identical fonts.
const fontFaceCSS = "@font-face { font-family: 'Bricolage Grotesque'; font-style: normal; font-weight: 400 700; font-display: swap; src: url('/fonts/bricolage-grotesque.woff2') format('woff2'); }\n@font-face { font-family: 'Hanken Grotesk'; font-style: normal; font-weight: 400 700; font-display: swap; src: url('/fonts/hanken-grotesk.woff2') format('woff2'); }\n"

// sidebarConfig returns the navigation sidebar configuration.
func sidebarConfig() ui.SidebarConfig {
	return ui.SidebarConfig{Title: "Meridian", Items: []ui.SidebarItem{
		{Label: "Overview", Href: "/app"},
		{Label: "Customers", Href: "/app/customers"},
		{Label: "Subscriptions", Href: "/app/subscriptions"},
		{Label: "Invoices", Href: "/app/invoices"},
		{Label: "Admin", Href: "/admin", Roles: []string{"admin"}},
	}, Footer: ui.Stack(ui.StackConfig{Gap: ui.GapSM, Align: ui.AlignStart}, ui.ThemeToggle(ui.ThemeToggleConfig{Variant: ui.ThemeToggleLabel}), ui.SignOut(ui.SignOutConfig{Next: "/"}))}
}

// appLayout / marketingLayout are package-level so the per-screen mount funcs
// (screen_<name>.go) can reference them; RegisterGenerated assigns them.
var (
	appLayout       *app.Layout
	marketingLayout *app.Layout
)

// RegisterGenerated wires blueprint-generated screens, endpoints, middleware, and plugins.
func RegisterGenerated(fwApp *framework.App, site *app.App, db *sql.DB) {
	if site == nil {
		site = app.NewApp("Meridian")
	}
	site.WithTheme(appTheme())
	sbCfg := sidebarConfig()
	sb := ui.Sidebar(sbCfg)
	appLayout = app.NewLayout("app").WithSidebar(sb)
	site.SetDefaultLayout(appLayout)
	ui.MountSidebar(routerMounter{fwApp.Router()}, sbCfg)
	marketingLayout = app.NewLayout("marketing").
		WithContainer().
		WithHeader(app.NewContextComponent(marketingHeader)).
		WithFooter(app.NewStaticComponent(marketingFooter()))
	// mountGenerated populates appResources (per-entity crud files) and mounts
	// every screen. It runs early so hand-written endpoints below that capture
	// a resource config (e.g. the customers island at /api/tables/customers)
	// see a populated map.
	mountGenerated(fwApp, site, db)
	{
		stack := preset.ToastStack("blueprint-toasts").Build()
		widget.Mount(fwApp.Router(), &stack)
	}
	{
		modal := quickAddCustomerModal()
		widget.Mount(fwApp.Router(), &modal)
	}
	// Island endpoint for the Customers table: sort/page RPCs from the list
	// screen hit this and the runtime swaps just the table island.
	fwApp.Router().HandleFunc("GET", "/api/tables/customers", customersList().TableHandler())
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
		// Scoped API tokens (PATs): logged-in users mint them at
		// POST /auth/tokens (session-only — a leaked token can't mint
		// siblings) and the generated CLI under cli/ sends them as
		// Authorization: Bearer gfsk_… on every request.
		apiTokens, err := auth.NewSQLAPITokenStore(db)
		if err != nil {
			log.Fatalf("api token store: %v", err)
		}
		serviceAccounts, err := auth.NewSQLServiceAccountStore(db)
		if err != nil {
			log.Fatalf("service account store: %v", err)
		}
		authMgr.Use(auth.NewTokensPlugin(apiTokens))
		authMgr.Init(fwApp)
		auth.SetDefaultLoginErrorPath("/login")
		// Bootstrap admin account so the back-office is reachable on a
		// fresh database. Created only when absent (idempotent). The
		// password comes from ADMIN_SEED_PASSWORD (see the generated
		// .env — gitignored, so a deploy must export the variable
		// itself), never from committed source; without it no admin
		// is seeded and the skip is logged loudly.
		if seedPw := os.Getenv("ADMIN_SEED_PASSWORD"); seedPw != "" {
			if _, _, err := authCfg.UserStore.FindByEmail(context.Background(), "admin@meridian.dev"); err != nil {
				if h, herr := auth.HashPassword(seedPw); herr == nil {
					authCfg.UserStore.CreateUser(context.Background(), "admin@meridian.dev", h, []string{"admin", "user"})
				}
			}
		} else {
			log.Printf("WARN: ADMIN_SEED_PASSWORD is not set — admin %q was NOT seeded; on a fresh database the back-office login will fail", "admin@meridian.dev")
		}
		// Resolve the session cookie to a user on every request so
		// owner/access-scoped CRUD sees the logged-in user. Without
		// this, authorized requests fail closed (401) just like
		// anonymous ones.
		fwApp.Use(auth.SessionMiddleware(authMgr))
		// TokenMiddleware rides alongside: it only touches
		// Authorization: Bearer gfsk_… credentials, so session and JWT
		// requests pass through untouched. RequireAPIScopes then makes
		// the minted scopes real — a ["customers:*"] token is 403'd off
		// every other /api resource; sessions stay unscoped.
		fwApp.Use(auth.TokenMiddleware(authCfg.UserStore, serviceAccounts, apiTokens))
		fwApp.Use(auth.RequireAPIScopes("/api"))
		ui.SetRolesExtractor(func(ctx context.Context) []string {
			if u, ok := handler.GetUser(ctx); ok && u != nil {
				if rh, ok := u.(interface{ GetRoles() []string }); ok {
					return rh.GetRoles()
				}
			}
			return nil
		})
		// auth.CSRF is intentionally NOT mounted: this generated surface
		// is JSON-first (REST CRUD + /mcp), and the CSRF middleware 403s
		// any unsafe-method request that doesn't echo the csrf cookie as
		// an X-CSRF-Token header — which plain JSON/MCP clients don't.
		// Session cookies are SameSite=Strict, so cross-site form posts
		// don't carry the session in modern browsers. If you add browser
		// HTML forms, mount auth.CSRF — see `gofastr docs blueprints`
		// (Auth section) and `gofastr docs auth`.
	}
	_ = routerMounter{}
}

// routerMounter adapts framework's *router.Router to ui.WidgetMounter.
type routerMounter struct{ r *router.Router }

func (m routerMounter) MountWidget(def *widget.Definition) {
	widget.Mount(m.r, def)
}
