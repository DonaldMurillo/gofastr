// Command backoffice is a minimal example of the battery/admin entity CRUD
// admin rendered through a UI host: a few entities, a (demo-grade) login, and
// admin.New(...) generating the whole back-office with defaults.
//
// The admin screens hydrate with runtime.js — the list is a DataTable island
// (paginate without a reload), delete is a data-fui-confirm button, and forms
// are server-rendered. There is no bespoke JavaScript anywhere in this app.
//
// The auth here is a deliberately tiny demo stand-in (a signed-cookie-free
// session) so the example stays focused on the admin. Real apps wire
// battery/auth and pass admin.Config.Authorize a role check.
package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/DonaldMurillo/gofastr/battery/admin"
	appui "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/ui"
	"github.com/DonaldMurillo/gofastr/framework/uihost"

	_ "github.com/mattn/go-sqlite3"
)

const sessionCookie = "bo_session"

func main() {
	app := setupApp(":memory:")
	addr := ":8086"
	if p := os.Getenv("PORT"); p != "" {
		addr = ":" + p
	}
	log.Printf("backoffice on %s — visit /login (any email), then /admin", addr)
	if err := http.ListenAndServe(addr, app.Router()); err != nil {
		log.Fatal(err)
	}
}

// setupApp builds the whole app ready to serve: entities migrated, seed data
// loaded, the admin battery initialized. Returns a *framework.App whose
// Router() can be served directly (main) or via httptest (e2e).
func setupApp(dsn string) *framework.App {
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		log.Fatal(err)
	}
	db.SetMaxOpenConns(1) // in-memory SQLite: single shared connection

	site := appui.NewApp("Backoffice")
	site.WithTheme(createTheme())
	// The public pages share the centered-container layout shell — the
	// design system's editorial column — so the hero and auth card sit in
	// a comfortable measure without any page CSS.
	public := appui.NewLayout("public").WithContainer()
	site.Register("/", &homeScreen{}, public)
	site.Register("/login", &loginScreen{}, public) // GET sign-in (themed); POST → loginSubmit

	// Zero bespoke CSS: the uihost ships the base typography floor, the
	// admin battery ships the whole back-office shell, and the public
	// screens below compose framework/ui (Hero, AuthCard, Form) — all
	// recolored by the theme tokens set in createTheme.
	host := uihost.New(site)
	app := framework.NewUIHostApp(host,
		framework.WithDB(db),
		framework.WithConfig(framework.AppConfig{Name: "backoffice"}),
	)

	// Demo session: a cookie names the signed-in user. Applied to every route
	// (including the host-served admin screens) so the admin's authorize gate
	// sees a non-nil user.
	app.Use(demoSession)

	registerEntities(app)

	// Expose every CRUD entity as an admin screen — the explicit
	// whole-back-office opt-in (an empty Entities list exposes nothing).
	app.RegisterBattery(admin.New(admin.Config{Title: "Backoffice", EntityListLimit: 8, AllEntities: true}))

	// GET /login is the themed host screen registered above. The form posts to a
	// DISTINCT path (/login/submit) so an explicit route doesn't shadow /login
	// and force a 405 on the host-served GET screen.
	app.Router().Post("/login/submit", http.HandlerFunc(loginSubmit))
	app.Router().Get("/logout", http.HandlerFunc(logout))

	// Bring the app up without binding a port: migrate, init the battery, seed.
	if err := framework.AutoMigrate(db, app.Registry); err != nil {
		log.Fatal(err)
	}
	if err := app.InitPlugins(); err != nil {
		log.Fatal(err)
	}
	seed(db)
	return app
}

func registerEntities(app *framework.App) {
	// suppliers is the target of a BelongsTo on products, so the product form
	// renders a supplier relationship picker (a <select> of suppliers).
	app.Entity("suppliers", entity.EntityConfig{
		Fields: []schema.Field{
			{Name: "name", Type: schema.String, Required: true, Max: f64(120)},
		},
	})
	app.Entity("products", entity.EntityConfig{
		Fields: []schema.Field{
			{Name: "name", Type: schema.String, Required: true, Max: f64(120)},
			{Name: "price", Type: schema.Float},
			{Name: "in_stock", Type: schema.Bool},
			{Name: "category", Type: schema.Enum, Values: []string{"tools", "parts", "accessories"}, Default: "tools"},
			{Name: "description", Type: schema.Text},
			{Name: "photo", Type: schema.Image},        // shows a thumbnail in list/detail
			{Name: "specs", Type: schema.JSON},         // shows a code block in detail
			{Name: "launched_on", Type: schema.Date},   // shows a formatted date
			{Name: "supplier_id", Type: schema.String}, // FK → suppliers (optional)
		},
		Relations: []entity.Relation{
			entity.BelongsTo("supplier", "suppliers", "supplier_id"),
		},
	})
	app.Entity("customers", entity.EntityConfig{
		Fields: []schema.Field{
			{Name: "name", Type: schema.String, Required: true, Max: f64(120)},
			{Name: "email", Type: schema.String, Required: true, Max: f64(200)},
			{Name: "vip", Type: schema.Bool},
		},
	})
}

func seed(db *sql.DB) {
	// A small amber square as a self-contained (CSP-safe data URI) demo photo so
	// the Image field shows a real thumbnail without a storage backend.
	const photo = "data:image/svg+xml,%3Csvg%20xmlns='http://www.w3.org/2000/svg'%20width='80'%20height='80'%3E%3Crect%20width='80'%20height='80'%20fill='%23f0b429'/%3E%3C/svg%3E"
	products := []struct {
		name, cat, desc string
		price           float64
		stock           int
	}{
		{"Cordless Drill", "tools", "18V brushless drill/driver with two batteries.", 129.0, 1},
		{"Hex Bit Set", "parts", "40-piece S2 steel bit set in a magnetic case.", 24.5, 1},
		{"Safety Goggles", "accessories", "Anti-fog, scratch-resistant, ANSI Z87.1.", 12.0, 0},
		{"Impact Driver", "tools", "1/4\" hex, 1500 in-lbs torque.", 149.0, 1},
		{"Circular Saw", "tools", "7-1/4\" blade, 5800 RPM.", 99.0, 1},
		{"Tape Measure", "tools", "25 ft, magnetic hook.", 18.0, 1},
		{"Work Gloves", "accessories", "Cut-resistant, touchscreen index finger.", 15.5, 1},
		{"Socket Set", "parts", "46-piece metric + SAE.", 64.0, 0},
		{"Stud Finder", "tools", "Deep-scan, edge + center detection.", 32.0, 1},
		{"Utility Knife", "tools", "Retractable, quick-change blade.", 9.5, 1},
		{"Ear Protection", "accessories", "26 dB NRR over-ear.", 21.0, 1},
		{"Drill Bit Set", "parts", "29-piece titanium-coated.", 38.0, 0},
		{"Level", "tools", "24\" box-beam, 3 vials.", 27.0, 1},
		{"Pliers Kit", "parts", "5-piece: needle-nose, lineman, cutters.", 44.0, 1},
	}
	for _, p := range products {
		specs := fmt.Sprintf(`{"category":%q,"price":%g,"in_stock":%t}`, p.cat, p.price, p.stock == 1)
		db.Exec(`INSERT INTO products (id, name, price, in_stock, category, description, photo, specs, launched_on)
			VALUES (lower(hex(randomblob(16))), ?, ?, ?, ?, ?, ?, ?, ?)`,
			p.name, p.price, p.stock, p.cat, p.desc, photo, specs, "2026-01-15")
	}
	db.Exec(`INSERT INTO suppliers (id, name) VALUES (lower(hex(randomblob(16))), 'Acme Supply')`)
	db.Exec(`INSERT INTO suppliers (id, name) VALUES (lower(hex(randomblob(16))), 'Globex Parts')`)
	db.Exec(`INSERT INTO customers (id, name, email, vip) VALUES (lower(hex(randomblob(16))), 'Ada Lovelace', 'ada@example.com', 1)`)
	db.Exec(`INSERT INTO customers (id, name, email, vip) VALUES (lower(hex(randomblob(16))), 'Alan Turing', 'alan@example.com', 0)`)
}

func f64(v float64) *float64 { return &v }

// ----- demo auth ------------------------------------------------------------

type demoUser struct{ email string }

func (u *demoUser) GetID() string { return u.email }

// GetRoles makes the demo user an admin so it clears the admin battery's
// default role gate (admin.Config.AdminRole, default "admin"). A real app
// would derive roles from its user store; this demo treats anyone who signs
// in as the admin. Alternatively, set admin.Config.Authorize for a custom
// predicate.
func (u *demoUser) GetRoles() []string { return []string{"admin"} }

// demoSession reads the session cookie and puts a (non-nil) user on the
// request context. NOT production auth — see the package doc.
func demoSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if c, err := r.Cookie(sessionCookie); err == nil && c.Value != "" {
			r = r.WithContext(handler.SetUser(r.Context(), &demoUser{email: c.Value}))
		}
		next.ServeHTTP(w, r)
	})
}

func loginSubmit(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	email := r.PostFormValue("email")
	if email == "" {
		email = "admin@example.com"
	}
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: email, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, "/admin/e/products", http.StatusSeeOther)
}

func logout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", MaxAge: -1})
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

// ----- public screens (home + sign-in) — composed entirely from
// framework/ui so they share the admin's tokens with zero bespoke CSS ------

type homeScreen struct{ component.ContextOnly }

func (homeScreen) ScreenTitle() string { return "Backoffice" }

func (homeScreen) RenderCtx(ctx context.Context) render.HTML {
	href, label := "/login", "Sign in"
	if u, ok := handler.GetUser(ctx); ok && u != nil {
		href, label = "/admin/e/products", "Open the admin"
	}
	return ui.Hero(ui.HeroConfig{
		Title:    "Backoffice",
		Subtitle: "An entity admin generated by battery/admin, rendered through the UI host. Products, suppliers, and customers are editable at /admin/e/<entity>.",
		Actions:  []render.HTML{ui.LinkButton(ui.LinkButtonConfig{Label: label, Href: href})},
	})
}

// loginScreen is the demo sign-in, composed from ui.AuthCard + ui.Form +
// ui.FormField — the same shapes a real battery/auth login screen uses.
// POST still goes to the raw loginSubmit handler.
type loginScreen struct{ component.ContextOnly }

func (loginScreen) ScreenTitle() string { return "Sign in · Backoffice" }

func (loginScreen) RenderCtx(context.Context) render.HTML {
	return ui.AuthCard(ui.AuthCardConfig{
		Title: "Backoffice",
		Body: ui.Form(ui.FormConfig{Action: "/login/submit", Method: "POST", SubmitLabel: "Sign in"},
			ui.FormField(ui.FormFieldConfig{Label: "Email", For: "f-email", Required: true,
				Input: html.Input(html.InputConfig{ID: "f-email", Name: "email", Type: "email", Value: "admin@example.com",
					ExtraAttrs: html.Attrs{"autocomplete": "email", "required": ""}})}),
			ui.FormField(ui.FormFieldConfig{Label: "Password", For: "f-password",
				Input: html.Input(html.InputConfig{ID: "f-password", Name: "password", Type: "password", Value: "demo",
					ExtraAttrs: html.Attrs{"autocomplete": "current-password"}})}),
		),
		Footer: ui.Muted(render.Text("Demo sign-in — any email works.")),
	})
}
