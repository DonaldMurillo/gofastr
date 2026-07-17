package main

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework"
)

// websitesBlueprint exercises the "usable website" generator features:
// /api prefix, ordered+coerced seed, nested entity blocks, enum options,
// relation selects, dynamic detail routes, and the kiln-free node renderer.
func websitesBlueprint() Blueprint {
	return Blueprint{
		App: BlueprintApp{
			Name: "Shop", Module: "example.com/shop", StaticDir: "static",
			APIPrefix: "api",
		},
		Entities: []framework.EntityDeclaration{
			{Name: "categories", Fields: []framework.FieldDeclaration{{Name: "name", Type: "string"}}},
			{Name: "items", Fields: []framework.FieldDeclaration{
				{Name: "name", Type: "string", Required: true},
				{Name: "status", Type: "enum", Values: []string{"draft", "published"}},
				{Name: "category_id", Type: "relation", To: "categories"},
				{Name: "price", Type: "decimal"},
			}},
		},
		Screens: []BlueprintScreen{
			{Name: "dashboard", Route: "/", Body: []BlueprintBlock{
				{Kind: "section", Props: map[string]any{"class": "wrap"}, Children: []BlueprintBlock{
					{Kind: "entity_list", Entity: "items", Fields: []string{"name", "status"}, Limit: 5},
				}},
				{Kind: "entity_form", Entity: "items", Mode: "create"},
			}},
			{Name: "item_detail", Route: "/items/{id}", Body: []BlueprintBlock{
				{Kind: "entity_detail", Entity: "items"},
			}},
		},
		Seed: []BlueprintSeedEntity{
			{Entity: "categories", Rows: []map[string]any{{"name": "Tools"}}},
			{Entity: "items", Rows: []map[string]any{{"name": "Hammer", "price": 9.99}}},
		},
	}
}

func TestBlueprint_APIPrefixAndSeedWiring(t *testing.T) {
	bp := websitesBlueprint()
	main := renderBlueprintMain(bp)
	if !strings.Contains(main, "APIPrefix: apiPrefix") {
		t.Error("main.go does not pass the API prefix into AppConfig")
	}
	if !strings.Contains(main, "fwApp.WithSeed(") || !strings.Contains(main, "seedData()") {
		t.Error("main.go does not wire the seed hook")
	}
	if !strings.Contains(main, "CountAll(ctx, framework.ListOptions{})") {
		t.Error("seed hook is not idempotent (no CountAll gate)")
	}
	if !strings.Contains(main, "appBaseCSS()") {
		t.Error("main.go does not mount appBaseCSS")
	}
}

func TestBlueprint_AppConstsAndRoutes(t *testing.T) {
	app := renderBlueprintApp(websitesBlueprint())
	if !strings.Contains(app, `apiPrefix = "api"`) {
		t.Error("app.go missing apiPrefix const")
	}
	// Screen routes ({id}->:id conversion) now live in per-screen mount funcs,
	// not app.go — check the screen files. app.go names no screen.
	screens := allScreenContent(mustRenderBlueprintFiles(t, websitesBlueprint()))
	if !strings.Contains(screens, `site.Register("/items/:id"`) {
		t.Errorf("detail screen route not converted {id}->:id for the screen router:\n%s", screens)
	}
	if !strings.Contains(app, "func appBaseCSS() string") {
		t.Error("app.go missing appBaseCSS function")
	}
	// The generator ships ZERO bespoke CSS — every surface composes framework/ui
	// components + core-ui/app layouts that own their styling.
	if strings.Contains(app, ".gofastr-entity") || strings.Contains(app, ".gofastr-auth") {
		t.Error("appBaseCSS must ship no bespoke CSS — components own their styling")
	}
}

func TestBlueprint_ScreensAreKilnFree(t *testing.T) {
	screens := renderBlueprintScreens(websitesBlueprint())
	if strings.Contains(screens, "gofastr/kiln") {
		t.Errorf("generated screens import the kiln namespace:\n%s", screens)
	}
	// Any node rendering uses the first-party core-ui packages, never kiln.
	// (Most surfaces now compose framework/ui components and use no node
	// renderer at all — that's fine; the invariant is "never kiln".)
	if strings.Contains(screens, "noderender") && !strings.Contains(screens, "core-ui/noderender") {
		t.Error("screens use a non-first-party noderender")
	}
}

func TestBlueprint_NestedEntityListRenders(t *testing.T) {
	screens := renderBlueprintScreens(websitesBlueprint())
	// The nested entity_list is server-rendered via the resource engine
	// (ui.DataTable), never a raw uinode.Node{Kind:"entity_list"} or a
	// client-fetch island.
	if strings.Contains(screens, `Kind: "entity_list"`) {
		t.Error("nested entity_list left as an unrendered node kind")
	}
	if !strings.Contains(screens, `appResources["items"]`) {
		t.Errorf("nested entity_list did not render via the resource engine:\n%s", screens)
	}
}

func TestBlueprint_FormEnumAndRelationFields(t *testing.T) {
	screens := renderBlueprintScreens(websitesBlueprint())
	// Form submits to the prefixed API via data-fui-rpc.
	if !strings.Contains(screens, `"data-fui-rpc": "/api/items"`) {
		t.Error("entity_form does not submit to the /api endpoint via data-fui-rpc")
	}
	// Enum field renders <option> elements for its declared values.
	if !strings.Contains(screens, `value=\"draft\"`) || !strings.Contains(screens, `value=\"published\"`) {
		t.Error("enum field did not render option elements")
	}
	// Relation field renders a select bound to its target entity.
	if !strings.Contains(screens, `data-rel-entity=\"categories\"`) {
		t.Error("relation field did not render a target-bound select")
	}
}

// TestBlueprint_AppCRUDScreensSynthesized verifies that an entity_list flagged
// `create: true` and an entity_detail produce writable app screens: a /new
// create form, a /{id}/edit form, a List "New" affordance, and a CanEdit detail
// (Edit + Delete) — all server-rendered through the resource engine's Form.
func TestBlueprint_AppCRUDScreensSynthesized(t *testing.T) {
	bp := Blueprint{
		App: BlueprintApp{Name: "Shop", Module: "example.com/shop", APIPrefix: "api"},
		Entities: []framework.EntityDeclaration{
			{Name: "widgets", Fields: []framework.FieldDeclaration{
				{Name: "name", Type: "string", Required: true},
				{Name: "status", Type: "enum", Values: []string{"draft", "live"}},
			}},
		},
		Nav: []BlueprintNavItem{{Label: "Widgets", Href: "/app/widgets"}},
		Screens: []BlueprintScreen{
			{Name: "widgets", Route: "/app/widgets", Layout: "app", Body: []BlueprintBlock{
				{Kind: "entity_list", Entity: "widgets", Fields: []string{"name", "status"}, Create: true},
			}},
			{Name: "widget_detail", Route: "/app/widgets/{id}", Layout: "app", Body: []BlueprintBlock{
				{Kind: "entity_detail", Entity: "widgets"},
			}},
		},
	}
	files, err := renderBlueprintFiles(bp)
	if err != nil {
		t.Fatalf("renderBlueprintFiles: %v", err)
	}
	screens := allScreenContent(files)
	crudFile := fileContent(files, "screen_widgets_crud.go")
	if crudFile == "" {
		t.Fatalf("missing screen_widgets_crud.go; files=%v", sortedFileNames(files))
	}

	// Create + edit form screens render via the resource engine's Form.
	assertContains(t, screens, `appResources["widgets"].Form(ctx, "")`)
	assertContains(t, screens, `appResources["widgets"].Form(ctx, s.id)`)
	// List shows "New"; detail shows Edit/Delete (CanEdit) and posts to the API.
	assertContains(t, screens, ".WithCreate().List(ctx)")
	assertContains(t, crudFile, "CanEdit: true")
	assertContains(t, crudFile, `APIPath: "/api/widgets"`)
	// The /new and /{id}/edit routes are registered (in the crud mount funcs).
	assertContains(t, screens, `"/app/widgets/new"`)
	assertContains(t, screens, `"/app/widgets/:id/edit"`)
	// app.go no longer owns any of this — it calls mountGenerated instead.
	if strings.Contains(fileContent(files, "app.go"), `appResources["widgets"]`) {
		t.Error("app.go must not carry the widgets appResources entry")
	}
}

func TestBlueprint_HandlesCamelCaseKeys(t *testing.T) {
	// CrudHandler.ListAll/GetOne serialize columns in camelCase, but blueprint
	// fields are snake_case. Lists/details are now server-rendered by the
	// resource engine, which maps snake -> camel server-side (resCamel/resGet)
	// so a field like generic_name isn't read as an undefined row["generic_name"].
	if !strings.Contains(blueprintResourceGo, "func resCamel") || !strings.Contains(blueprintResourceGo, "func resGet") {
		t.Error("resource engine missing the snake_case -> camelCase key mapping (resCamel/resGet)")
	}
}

func TestBlueprint_LoginScreenAndAdminWiring(t *testing.T) {
	bp := websitesBlueprint()
	bp.App.Auth = BlueprintAuth{Enabled: true, DevMode: true}
	bp.App.Admin = BlueprintAdmin{
		Enabled: true, Path: "/admin", Role: "admin", LoginPath: "/login",
		SeedEmail: "admin@example.com", SeedPassword: "secret-123",
	}
	bp.Screens = append(bp.Screens, BlueprintScreen{
		Name: "login", Route: "/login", Body: []BlueprintBlock{
			{Kind: "login_form", Props: map[string]any{"action": "/auth/login", "next": "/admin"}},
		},
	})

	// Login form composes ui.AuthCard + ui.Form posting to the auth battery.
	screens := renderBlueprintScreens(bp)
	if !strings.Contains(screens, "ui.AuthCard(") || !strings.Contains(screens, `Action: "/auth/login"`) {
		t.Errorf("login_form did not render a ui.AuthCard form posting to /auth/login:\n%s", screens)
	}
	if !strings.Contains(screens, `name=\"email\"`) || !strings.Contains(screens, `name=\"password\"`) {
		t.Error("login form missing email/password inputs")
	}

	// main.go registers the admin battery + login redirect.
	main := renderBlueprintMain(bp)
	if !strings.Contains(main, "battery/admin") || !strings.Contains(main, "admin.New(admin.Config{") {
		t.Error("main.go does not register the admin battery")
	}
	if !strings.Contains(main, `LoginPath: "/login"`) || !strings.Contains(main, `AdminRole: "admin"`) {
		t.Error("admin battery not configured with role + login redirect")
	}

	// app.go registers the bootstrap admin through the post-migrate seed hook.
	app := renderBlueprintApp(bp)
	if !strings.Contains(app, "fwApp.WithSeed(func(ctx context.Context) error") ||
		!strings.Contains(app, "auth.HashPassword(") ||
		!strings.Contains(app, `CreateUser(ctx, "admin@example.com"`) {
		t.Errorf("admin user not seeded through WithSeed:\n%s", app)
	}
	if !strings.Contains(app, `[]string{"admin", "user"}`) {
		t.Error("seeded admin user missing admin role")
	}
}

func TestBlueprint_AdminSeedAfterMigrate(t *testing.T) {
	bp := websitesBlueprint()
	bp.App.Auth = BlueprintAuth{Enabled: true, DevMode: true}
	bp.App.Admin = BlueprintAdmin{
		Enabled: true, Role: "admin", LoginPath: "/login",
		SeedEmail: "admin@example.com", SeedPassword: "secret-123",
	}
	app := renderBlueprintApp(bp)
	if !strings.Contains(app, "fwApp.WithSeed(func(ctx context.Context) error") {
		t.Fatalf("admin seed must run through the post-migrate seed lifecycle:\n%s", app)
	}
	if strings.Contains(app, "FindByEmail(context.Background()") {
		t.Fatalf("admin seed still runs during RegisterGenerated wiring:\n%s", app)
	}
}

func TestBlueprint_SeedOrderedAndDecimalCoerced(t *testing.T) {
	stubs := renderBlueprintStubs(websitesBlueprint())
	if !strings.Contains(stubs, "[]seedEntity{") {
		t.Error("seed data is not an ordered slice")
	}
	// categories must precede items so the relation target exists first.
	ci := strings.Index(stubs, `Entity: "categories"`)
	ii := strings.Index(stubs, `Entity: "items"`)
	if ci < 0 || ii < 0 || ci > ii {
		t.Errorf("seed order not preserved (categories=%d items=%d)", ci, ii)
	}
	// Decimal seed value coerced to a string literal for the validator.
	if !strings.Contains(stubs, `"price": "9.99"`) {
		t.Errorf("decimal seed value not coerced to string:\n%s", stubs)
	}
}
