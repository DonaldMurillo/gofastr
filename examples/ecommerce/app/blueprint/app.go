package blueprint

import (
	"database/sql"
	"net/http"

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
	BlueprintAppName   = "ShopFront"
	BlueprintModule    = "github.com/DonaldMurillo/gofastr/examples/ecommerce"
	BlueprintDBDriver  = "sqlite"
	BlueprintDBURL     = "file:shop.db"
	BlueprintStaticDir = ""
)

func BlueprintTheme() style.Theme {
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

// BlueprintSidebarConfig returns the navigation sidebar configuration.
func BlueprintSidebarConfig() ui.SidebarConfig {
	return ui.SidebarConfig{Title: "ShopFront", Items: []ui.SidebarItem{
		{Label: "Home", Href: "/"},
		{Label: "Products", Href: "/products"},
		{Label: "Categories", Href: "/categories"},
		{Label: "Orders", Href: "/orders"},
		{Label: "Reviews", Href: "/reviews"},
	}}
}

// RegisterGenerated wires blueprint-generated screens, endpoints, middleware, and plugins.
func RegisterGenerated(fwApp *framework.App, site *app.App, db *sql.DB) {
	if site == nil {
		site = app.NewApp("ShopFront")
	}
	site.WithTheme(BlueprintTheme())
	sbCfg := BlueprintSidebarConfig()
	sb := ui.Sidebar(sbCfg)
	layout := app.NewLayout("blueprint").WithSidebar(sb)
	site.SetDefaultLayout(layout)
	ui.MountSidebar(blueprintRouterMounter{fwApp.Router()}, sbCfg)
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
		authCfg := auth.AuthConfig{DevMode: true}
		authCfg.UserStore = auth.NewEntityUserStore(db, "auth_users")
		authCfg.SessionStore = auth.NewEntitySessionStore(db, "auth_sessions")
		authMgr := auth.New(authCfg)
		authMgr.Use(auth.NewCorePlugin())
		// Auto-create auth tables if they don't exist.
		db.Exec(`CREATE TABLE IF NOT EXISTS auth_users (id TEXT PRIMARY KEY, email TEXT UNIQUE NOT NULL, password_hash TEXT NOT NULL DEFAULT '', roles TEXT NOT NULL DEFAULT '[]', password_set INTEGER NOT NULL DEFAULT 0)`)
		db.Exec(`CREATE TABLE IF NOT EXISTS auth_sessions (id TEXT NOT NULL, token TEXT UNIQUE NOT NULL, user_id TEXT NOT NULL, created_at DATETIME NOT NULL, expires_at DATETIME NOT NULL, two_factor_verified INTEGER NOT NULL DEFAULT 0, pending_two_factor INTEGER NOT NULL DEFAULT 0)`)
		authMgr.Init(fwApp)
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
	site.Register("/", &HomeScreen{}, nil)
	site.Register("/products", &ProductsScreen{}, nil)
	site.Register("/categories", &CategoriesScreen{}, nil)
	site.Register("/orders", &OrdersScreen{}, nil)
	site.Register("/reviews", &ReviewsScreen{}, nil)
	site.Register("/new-product", &ProductNewScreen{}, nil)
	site.Register("/product-detail", &ProductDetailScreen{}, nil)
	site.Register("/order-detail", &OrderDetailScreen{}, nil)
	fwApp.Router().Handle("POST", "/orders/{id}/confirm", http.HandlerFunc(ConfirmOrder))
	fwApp.Router().Handle("POST", "/orders/{id}/ship", http.HandlerFunc(ShipOrder))
	fwApp.Use(RequestLoggerMiddleware)
	fwApp.RegisterPlugin(AnalyticsPlugin{})
	_ = blueprintRouterMounter{}
}

// blueprintRouterMounter adapts framework's *router.Router to ui.WidgetMounter.
type blueprintRouterMounter struct{ r *router.Router }

func (m blueprintRouterMounter) MountWidget(def *widget.Definition) {
	widget.Mount(m.r, def)
}
