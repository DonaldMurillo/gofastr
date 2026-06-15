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
	BlueprintAPIPrefix = "api"
)

// BlueprintBaseCSS is an owned extension point for app-specific base CSS.
// It's empty by default: every generated surface composes framework/ui
// components and core-ui/app layouts that ship their own CSS, so the
// generated app ships no bespoke styling. Add app CSS here or in static/app.css.
func BlueprintBaseCSS() string {
	return ""
}

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

// BlueprintFontCSS holds the @font-face rules for the app's fonts, shared by
// the UI host and the admin battery so every surface loads identical fonts.
const BlueprintFontCSS = ""

// BlueprintSidebarConfig returns the navigation sidebar configuration.
func BlueprintSidebarConfig() ui.SidebarConfig {
	return ui.SidebarConfig{Title: "ShopFront", Items: []ui.SidebarItem{
		{Label: "Home", Href: "/"},
		{Label: "Products", Href: "/products"},
		{Label: "Categories", Href: "/categories"},
		{Label: "Orders", Href: "/orders"},
		{Label: "Reviews", Href: "/reviews"},
	}, Footer: ui.SignOut(ui.SignOutConfig{Next: "/"})}
}

// RegisterGenerated wires blueprint-generated screens, endpoints, middleware, and plugins.
func RegisterGenerated(fwApp *framework.App, site *app.App, db *sql.DB) {
	if site == nil {
		site = app.NewApp("ShopFront")
	}
	blueprintResources["categories"] = ResourceConfig{
		Title: "Categories", Singular: "Category", BasePath: "/categories", APIPath: "/api/categories",
		Crud: fwApp.MustCrudHandler("categories"),
		Fields: []ResField{
			{Key: "name", Label: "Name", Type: "string"},
			{Key: "slug", Label: "Slug", Type: "string"},
			{Key: "description", Label: "Description", Type: "text"},
			{Key: "image", Label: "Image", Type: "image"},
			{Key: "sort_order", Label: "Sort Order", Type: "int"},
			{Key: "active", Label: "Active", Type: "bool"},
		},
		Related: []RelatedList{
			{
				Title: "Products", ForeignKey: "category_id", BasePath: "/products",
				Crud: fwApp.MustCrudHandler("products"),
				Fields: []ResField{
					{Key: "name", Label: "Name", Type: "string"},
					{Key: "slug", Label: "Slug", Type: "string"},
					{Key: "sku", Label: "SKU", Type: "string"},
					{Key: "description", Label: "Description", Type: "text"},
				},
				Relations: map[string]RelSource{
					"category_id": {Crud: fwApp.MustCrudHandler("categories"), Display: "name"},
				},
			},
		},
	}
	blueprintResources["orders"] = ResourceConfig{
		Title: "Orders", Singular: "Order", BasePath: "/orders", APIPath: "/api/orders",
		Crud:    fwApp.MustCrudHandler("orders"),
		CanEdit: true,
		Fields: []ResField{
			{Key: "user_id", Label: "User", Type: "string"},
			{Key: "order_number", Label: "Order Number", Type: "string"},
			{Key: "status", Label: "Status", Type: "enum", Values: []string{"pending", "confirmed", "processing", "shipped", "delivered", "cancelled", "refunded"}},
			{Key: "customer_name", Label: "Customer Name", Type: "string"},
			{Key: "customer_email", Label: "Customer Email", Type: "string"},
			{Key: "customer_phone", Label: "Customer Phone", Type: "string"},
			{Key: "shipping_address", Label: "Shipping Address", Type: "json"},
			{Key: "billing_address", Label: "Billing Address", Type: "json"},
			{Key: "subtotal", Label: "Subtotal", Type: "decimal"},
			{Key: "tax", Label: "Tax", Type: "decimal"},
			{Key: "shipping_cost", Label: "Shipping Cost", Type: "decimal"},
			{Key: "total", Label: "Total", Type: "decimal"},
			{Key: "notes", Label: "Notes", Type: "text"},
			{Key: "shipped_at", Label: "Shipped At", Type: "timestamp"},
			{Key: "delivered_at", Label: "Delivered At", Type: "timestamp"},
		},
		Related: []RelatedList{
			{
				Title: "Order Items", ForeignKey: "order_id", BasePath: "",
				Crud: fwApp.MustCrudHandler("order_items"),
				Fields: []ResField{
					{Key: "user_id", Label: "User", Type: "string"},
					{Key: "product_id", Label: "Product", Type: "relation"},
					{Key: "product_name", Label: "Product Name", Type: "string"},
					{Key: "quantity", Label: "Quantity", Type: "int"},
				},
				Relations: map[string]RelSource{
					"order_id":   {Crud: fwApp.MustCrudHandler("orders"), Display: "user_id"},
					"product_id": {Crud: fwApp.MustCrudHandler("products"), Display: "name"},
				},
			},
		},
	}
	blueprintResources["products"] = ResourceConfig{
		Title: "Products", Singular: "Product", BasePath: "/products", APIPath: "/api/products",
		Crud:    fwApp.MustCrudHandler("products"),
		CanEdit: true,
		Fields: []ResField{
			{Key: "name", Label: "Name", Type: "string"},
			{Key: "slug", Label: "Slug", Type: "string"},
			{Key: "sku", Label: "SKU", Type: "string"},
			{Key: "description", Label: "Description", Type: "text"},
			{Key: "price", Label: "Price", Type: "decimal"},
			{Key: "compare_at_price", Label: "Compare At Price", Type: "decimal"},
			{Key: "stock", Label: "Stock", Type: "int"},
			{Key: "category_id", Label: "Category", Type: "relation"},
			{Key: "status", Label: "Status", Type: "enum", Values: []string{"draft", "active", "archived"}},
			{Key: "featured", Label: "Featured", Type: "bool"},
			{Key: "weight", Label: "Weight", Type: "float"},
			{Key: "image", Label: "Image", Type: "image"},
			{Key: "tags", Label: "Tags", Type: "json"},
		},
		Relations: map[string]RelSource{
			"category_id": {Crud: fwApp.MustCrudHandler("categories"), Display: "name"},
		},
		Related: []RelatedList{
			{
				Title: "Order Items", ForeignKey: "product_id", BasePath: "",
				Crud: fwApp.MustCrudHandler("order_items"),
				Fields: []ResField{
					{Key: "user_id", Label: "User", Type: "string"},
					{Key: "order_id", Label: "Order", Type: "relation"},
					{Key: "product_name", Label: "Product Name", Type: "string"},
					{Key: "quantity", Label: "Quantity", Type: "int"},
				},
				Relations: map[string]RelSource{
					"order_id":   {Crud: fwApp.MustCrudHandler("orders"), Display: "user_id"},
					"product_id": {Crud: fwApp.MustCrudHandler("products"), Display: "name"},
				},
			},
			{
				Title: "Reviews", ForeignKey: "product_id", BasePath: "/reviews",
				Crud: fwApp.MustCrudHandler("reviews"),
				Fields: []ResField{
					{Key: "author_name", Label: "Author Name", Type: "string"},
					{Key: "rating", Label: "Rating", Type: "int"},
					{Key: "title", Label: "Title", Type: "string"},
					{Key: "body", Label: "Body", Type: "text"},
				},
				Relations: map[string]RelSource{
					"product_id": {Crud: fwApp.MustCrudHandler("products"), Display: "name"},
				},
			},
		},
	}
	blueprintResources["reviews"] = ResourceConfig{
		Title: "Reviews", Singular: "Review", BasePath: "/reviews", APIPath: "/api/reviews",
		Crud: fwApp.MustCrudHandler("reviews"),
		Fields: []ResField{
			{Key: "product_id", Label: "Product", Type: "relation"},
			{Key: "author_name", Label: "Author Name", Type: "string"},
			{Key: "rating", Label: "Rating", Type: "int"},
			{Key: "title", Label: "Title", Type: "string"},
			{Key: "body", Label: "Body", Type: "text"},
			{Key: "verified", Label: "Verified", Type: "bool"},
		},
		Relations: map[string]RelSource{
			"product_id": {Crud: fwApp.MustCrudHandler("products"), Display: "name"},
		},
	}
	site.WithTheme(BlueprintTheme())
	sbCfg := BlueprintSidebarConfig()
	sb := ui.Sidebar(sbCfg)
	appLayout := app.NewLayout("app").WithSidebar(sb)
	site.SetDefaultLayout(appLayout)
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
	site.Register("/", &HomeScreen{}, appLayout)
	site.Register("/products", &ProductsScreen{}, appLayout)
	site.Register("/categories", &CategoriesScreen{}, appLayout)
	site.Register("/orders", &OrdersScreen{}, appLayout)
	site.Register("/reviews", &ReviewsScreen{}, appLayout)
	site.Register("/new-product", &ProductNewScreen{}, appLayout)
	site.Register("/product-detail", &ProductDetailScreen{}, appLayout)
	site.Register("/order-detail", &OrderDetailScreen{}, appLayout)
	site.Register("/product-detail/edit", &ProductsEditScreen{}, appLayout)
	site.Register("/order-detail/edit", &OrdersEditScreen{}, appLayout)
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
