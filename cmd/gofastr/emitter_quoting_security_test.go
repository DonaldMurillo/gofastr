package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework"
	fwentity "github.com/DonaldMurillo/gofastr/framework/entity"
)

// Property: no blueprint-IR-derived string may leave the Go literal,
// identifier position, or comment it is emitted into.
//
// blueprint_emitter_injection_test.go covers 12 IR sites and states the threat
// model this file inherits (an agent authors gofastr.yml from natural-language
// requirements, so every IR string is transcribed text). This file is the
// exhaustive complement: every remaining string-bearing IR field the entity and
// screen emitters read. Same rubric as the sibling: reject at validate, or
// refuse in the emitter, or emit inertly.
//
// The 2026-08-04 sweep found exactly one failing site out of the ~60 below
// (relation.name; see TestRelationNameMustBeAnIdentifier). Keep the loop rather
// than pruning to that one case: its value is that it re-checks every sibling
// site whenever an emitter changes, which is how the one gap was found.

var irBreakers = []string{
	"x`+PWN()+`y",                  // closes a raw (backtick) literal
	`x"+PWN()+"y`,                  // closes an interpreted literal
	"x\nfunc PWN() {}\nvar y = \"", // newline escape out of an interpreted literal
	"x*/ PWN() /*y",                // closes a block comment
	"x // PWN()\ny",                // line-comment escape
}

func TestIRStringsStayInsideTheirLiteral(t *testing.T) {
	for _, payload := range irBreakers {
		label := strings.NewReplacer("\n", "N", "`", "BT", `"`, "Q", "*", "S", "/", "SL", " ", "_").Replace(payload)
		for _, tc := range irQuotingSites(payload) {
			t.Run(tc.site+"/"+label, func(t *testing.T) {
				if err := validateBlueprint(tc.bp); err != nil {
					return // rejected at the boundary
				}
				files, err := renderBlueprintFiles(tc.bp)
				if err != nil {
					return // emitter refused
				}
				for _, f := range files {
					if strings.HasSuffix(f.name, ".go") {
						assertIRStayedData(t, tc.site, f.name, f.content)
					}
				}
			})
		}
	}
}

func assertIRStayedData(t *testing.T, site, name, src string) {
	t.Helper()
	if !strings.Contains(src, "PWN") {
		return
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, name, src, parser.AllErrors)
	if err != nil {
		t.Fatalf("SECURITY: [injection] %s: emitted %s does not parse: %v", site, name, err)
	}
	var escaped bool
	ast.Inspect(file, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok && strings.Contains(id.Name, "PWN") {
			escaped = true
			return false
		}
		return true
	})
	if escaped {
		t.Fatalf("SECURITY: [injection] %s: payload became an identifier in %s", site, name)
	}
}

type irSite struct {
	site string
	bp   Blueprint
}

func irQuotingSites(payload string) []irSite {
	base := func() Blueprint {
		return Blueprint{
			App: BlueprintApp{Name: "app"},
			Entities: []framework.EntityDeclaration{{
				Name: "tickets",
				Fields: []framework.FieldDeclaration{
					{Name: "title", Type: "string"},
					{Name: "status", Type: "string", Values: []string{"open", "closed"}},
					{Name: "owner_id", Type: "string"},
					{Name: "due_on", Type: "date"},
				},
			}},
			Screens: []BlueprintScreen{{Name: "home", Route: "/", Type: "page"}},
		}
	}
	var out []irSite
	add := func(site string, mut func(*Blueprint)) {
		bp := base()
		mut(&bp)
		out = append(out, irSite{site, bp})
	}
	ent := func(b *Blueprint) *framework.EntityDeclaration { return &b.Entities[0] }

	// ---- app ----
	add("app.base_url", func(b *Blueprint) { b.App.BaseURL = payload })
	add("app.db_driver", func(b *Blueprint) { b.App.DBDriver = payload })
	add("app.db_url", func(b *Blueprint) { b.App.DBURL = payload })
	add("app.static_dir", func(b *Blueprint) { b.App.StaticDir = payload })
	add("app.api_prefix", func(b *Blueprint) { b.App.APIPrefix = payload })
	add("app.theme_value", func(b *Blueprint) { b.App.Theme = map[string]string{"primary": payload} })
	add("app.auth.base_path", func(b *Blueprint) {
		b.App.Auth = BlueprintAuth{Enabled: true, DevMode: true, BasePath: payload}
	})
	add("app.auth.jwt_secret", func(b *Blueprint) {
		b.App.Auth = BlueprintAuth{Enabled: true, JWTSecret: payload}
	})
	add("app.admin.path", func(b *Blueprint) {
		b.App.Admin = BlueprintAdmin{Enabled: true, Path: payload}
	})
	add("app.admin.role", func(b *Blueprint) {
		b.App.Admin = BlueprintAdmin{Enabled: true, Role: payload}
	})
	add("app.admin.login_path", func(b *Blueprint) {
		b.App.Admin = BlueprintAdmin{Enabled: true, LoginPath: payload}
	})
	add("app.admin.seed_email", func(b *Blueprint) {
		b.App.Admin = BlueprintAdmin{Enabled: true, SeedEmail: payload}
	})
	add("app.admin.seed_password", func(b *Blueprint) {
		b.App.Admin = BlueprintAdmin{Enabled: true, SeedPassword: payload}
	})
	add("app.pwa.name", func(b *Blueprint) { b.App.PWA = BlueprintPWA{Enabled: true, Name: payload} })
	add("app.pwa.short_name", func(b *Blueprint) { b.App.PWA = BlueprintPWA{Enabled: true, ShortName: payload} })
	add("app.pwa.description", func(b *Blueprint) {
		b.App.PWA = BlueprintPWA{Enabled: true, Description: payload}
	})
	add("app.pwa.theme_color", func(b *Blueprint) {
		b.App.PWA = BlueprintPWA{Enabled: true, ThemeColor: payload}
	})
	add("app.pwa.background_color", func(b *Blueprint) {
		b.App.PWA = BlueprintPWA{Enabled: true, BackgroundColor: payload}
	})

	// ---- nav ----
	add("nav.label", func(b *Blueprint) { b.Nav = []BlueprintNavItem{{Label: payload, Href: "/"}} })
	add("nav.href", func(b *Blueprint) { b.Nav = []BlueprintNavItem{{Label: "Home", Href: payload}} })
	add("nav.icon", func(b *Blueprint) { b.Nav = []BlueprintNavItem{{Label: "Home", Href: "/", Icon: payload}} })
	add("nav.role", func(b *Blueprint) { b.Nav = []BlueprintNavItem{{Label: "Home", Href: "/", Role: payload}} })
	add("nav.child.label", func(b *Blueprint) {
		b.Nav = []BlueprintNavItem{{Label: "Top", Items: []BlueprintNavItem{{Label: payload, Href: "/"}}}}
	})
	add("nav.child.href", func(b *Blueprint) {
		b.Nav = []BlueprintNavItem{{Label: "Top", Items: []BlueprintNavItem{{Label: "Kid", Href: payload}}}}
	})
	add("nav.child.role", func(b *Blueprint) {
		b.Nav = []BlueprintNavItem{{Label: "Top", Items: []BlueprintNavItem{{Label: "Kid", Href: "/", Role: payload}}}}
	})

	// ---- entity ----
	add("field.pattern", func(b *Blueprint) { ent(b).Fields[0].Pattern = payload })
	add("field.default", func(b *Blueprint) { ent(b).Fields[0].Default = payload })
	add("scope.tenant_field", func(b *Blueprint) {
		ent(b).Scope = &fwentity.ScopeDeclaration{TenantField: payload}
	})
	add("scope.owner_field", func(b *Blueprint) {
		ent(b).Scope = &fwentity.ScopeDeclaration{OwnerField: payload}
	})
	add("scope.cross_owner_read", func(b *Blueprint) {
		ent(b).Scope = &fwentity.ScopeDeclaration{OwnerField: "owner_id", CrossOwnerRead: payload}
	})
	add("search_fields", func(b *Blueprint) { ent(b).SearchFields = []string{payload} })
	add("pagination.cursor_field", func(b *Blueprint) {
		ent(b).Pagination = &fwentity.PaginationDeclaration{CursorField: payload}
	})
	add("pagination.cursor_fields", func(b *Blueprint) {
		ent(b).Pagination = &fwentity.PaginationDeclaration{CursorFields: []string{payload}}
	})
	add("index.name", func(b *Blueprint) {
		ent(b).Indices = []framework.Index{{Name: payload, Columns: []string{"title"}}}
	})
	add("index.column", func(b *Blueprint) {
		ent(b).Indices = []framework.Index{{Name: "idx", Columns: []string{payload}}}
	})
	add("properties.key", func(b *Blueprint) { ent(b).Properties = map[string]any{payload: "v"} })
	add("properties.value", func(b *Blueprint) { ent(b).Properties = map[string]any{"k": payload} })
	add("access.read", func(b *Blueprint) {
		ent(b).Exposure = &fwentity.ExposureDeclaration{Access: &fwentity.AccessDeclaration{Read: payload}}
	})
	add("access.create", func(b *Blueprint) {
		ent(b).Exposure = &fwentity.ExposureDeclaration{Access: &fwentity.AccessDeclaration{Create: payload}}
	})
	add("relation.name", func(b *Blueprint) {
		ent(b).Relations = []framework.Relation{{
			Type: framework.RelManyToOne, Name: payload, Entity: "tickets", ForeignKey: "parent_id",
		}}
	})
	add("relation.foreign_key", func(b *Blueprint) {
		ent(b).Relations = []framework.Relation{{
			Type: framework.RelManyToOne, Name: "parent", Entity: "tickets", ForeignKey: payload,
		}}
	})
	add("relation.through", func(b *Blueprint) {
		ent(b).Relations = []framework.Relation{{
			Type: framework.RelManyToMany, Name: "tags", Entity: "tickets",
			ForeignKey: "tag_id", Through: payload, LocalKey: "ticket_id",
		}}
	})
	add("relation.local_key", func(b *Blueprint) {
		ent(b).Relations = []framework.Relation{{
			Type: framework.RelManyToMany, Name: "tags", Entity: "tickets",
			ForeignKey: "tag_id", Through: "t_pivot", LocalKey: payload,
		}}
	})
	add("relation.foreign_key_target", func(b *Blueprint) {
		ent(b).Relations = []framework.Relation{{
			Type: framework.RelManyToOne, Name: "parent", Entity: "tickets",
			ForeignKey: "parent_id", ForeignKeyTarget: payload,
		}}
	})

	// ---- seed ----
	add("seed.row_value", func(b *Blueprint) {
		b.Seed = []BlueprintSeedEntity{{Entity: "tickets", Rows: []map[string]any{{"title": payload}}}}
	})
	add("seed.row_key", func(b *Blueprint) {
		b.Seed = []BlueprintSeedEntity{{Entity: "tickets", Rows: []map[string]any{{payload: "x"}}}}
	})
	add("seed.weight_key", func(b *Blueprint) {
		b.Seed = []BlueprintSeedEntity{{
			Entity: "tickets", Count: 2,
			Weights: map[string]map[string]int{payload: {"open": 1}},
		}}
	})

	// ---- screen ----
	// Screen chrome strings the 2026-08-04 sweep never reached: title and
	// description feed the SEO describers and the registrar table, route
	// feeds the router registration AND the e2e/axe test files.
	add("screen.title", func(b *Blueprint) { b.Screens[0].Title = payload })
	add("screen.description", func(b *Blueprint) { b.Screens[0].Description = payload })
	add("screen.route", func(b *Blueprint) { b.Screens[0].Route = payload })
	add("screen.layout", func(b *Blueprint) { b.Screens[0].Layout = payload })
	add("screen.access.role", func(b *Blueprint) {
		b.Screens[0].Access = BlueprintAccess{Auth: true, Role: payload}
	})

	// ---- blocks ----
	block := func(site string, blk BlueprintBlock) {
		add(site, func(b *Blueprint) {
			b.Screens[0].Body = []BlueprintBlock{blk}
			// entity_detail / entity_form(edit) render a specific record and
			// require an {id} route param to clear validateBlueprint; without it
			// the validator (correctly) rejects and these injection sites would
			// be skipped instead of exercised.
			if blk.Kind == "entity_detail" || (blk.Kind == "entity_form" && strings.EqualFold(blk.Mode, "edit")) {
				b.Screens[0].Route = "/tickets/{id}"
			}
		})
	}
	block("block.mode", BlueprintBlock{Kind: "entity_form", Entity: "tickets", Mode: payload})
	block("block.search", BlueprintBlock{Kind: "entity_list", Entity: "tickets", Search: payload})
	block("block.filters", BlueprintBlock{Kind: "entity_list", Entity: "tickets", Filters: []string{payload}})
	block("block.fields", BlueprintBlock{Kind: "entity_list", Entity: "tickets", Fields: []string{payload}})
	block("block.island", BlueprintBlock{Kind: "island", Island: payload})
	block("block.widget", BlueprintBlock{Kind: "widget", Widget: payload})
	block("block.props_value", BlueprintBlock{Kind: "stack", Props: map[string]any{"gap": payload}})
	block("block.props_key", BlueprintBlock{Kind: "stack", Props: map[string]any{payload: "1"}})
	block("block.action_name", BlueprintBlock{
		Kind: "text", Text: "hi",
		Actions: []BlueprintAction{{Name: payload, Event: "click", ClientJS: "void 0"}},
	})
	block("block.action_event", BlueprintBlock{
		Kind: "text", Text: "hi",
		Actions: []BlueprintAction{{Name: "go", Event: payload, ClientJS: "void 0"}},
	})
	block("block.action_client_js", BlueprintBlock{
		Kind: "text", Text: "hi",
		Actions: []BlueprintAction{{Name: "go", Event: "click", ClientJS: payload}},
	})
	block("block.transition_label", BlueprintBlock{
		Kind: "entity_detail", Entity: "tickets",
		Transitions: []BlueprintTransition{{Label: payload, Status: "open"}},
	})
	block("block.transition_status", BlueprintBlock{
		Kind: "entity_detail", Entity: "tickets",
		Transitions: []BlueprintTransition{{Label: "Open it", Status: payload}},
	})
	block("block.transition_variant", BlueprintBlock{
		Kind: "entity_detail", Entity: "tickets",
		Transitions: []BlueprintTransition{{Label: "Open it", Status: "open", Variant: payload}},
	})
	block("block.transition_stamp", BlueprintBlock{
		Kind: "entity_detail", Entity: "tickets",
		Transitions: []BlueprintTransition{{Label: "Open it", Status: "open", Stamp: payload}},
	})
	// Typed block string fields the sweep never reached: they all flow into
	// the uinode Props literal (renderGoLiteral), the catalog emitters, or
	// the empty-state copy of a list block.
	block("block.text", BlueprintBlock{Kind: "text", Text: payload})
	block("block.class", BlueprintBlock{Kind: "div", Class: payload})
	block("block.href", BlueprintBlock{Kind: "text", Text: "hi", Href: payload})
	block("block.empty_text", BlueprintBlock{Kind: "entity_list", Entity: "tickets", Fields: []string{"title"}, EmptyText: payload})

	// ---- endpoints / stubs ----
	add("endpoint.name", func(b *Blueprint) {
		b.Endpoints = []BlueprintEndpoint{{Name: payload, Method: "GET", Path: "/x", Handler: "handleX"}}
	})
	add("endpoint.path", func(b *Blueprint) {
		b.Endpoints = []BlueprintEndpoint{{Name: "x", Method: "GET", Path: payload, Handler: "handleX"}}
	})
	add("endpoint.description", func(b *Blueprint) {
		b.Endpoints = []BlueprintEndpoint{{
			Name: "x", Method: "GET", Path: "/x", Handler: "handleX", Description: payload,
		}}
	})
	add("middleware.description", func(b *Blueprint) {
		b.Middleware = []BlueprintNamedStub{{Name: "audit", Description: payload}}
	})
	add("plugin.description", func(b *Blueprint) {
		b.Plugins = []BlueprintNamedStub{{Name: "metrics", Description: payload}}
	})
	add("helper.description", func(b *Blueprint) {
		b.Helpers = []BlueprintNamedStub{{Name: "slugify", Description: payload}}
	})
	return out
}

// Property: no generator-config-derived string may escape the comment it is
// emitted into.
//
// `gofastr generate sdk` reads name / sdk_version / module from --flags or from
// the project's own gofastr.codegen.yml (applySDKConfigDefaults) and
// concatenates them into a stamp comment at the top of every emitted artifact.
// Only client.go had a gate behind it (format.Source in renderSDKGoFiles); the
// JS/TS clients and both READMEs had none, and client.js is what
// framework/sdkdocs serves to browsers.
//
// Fixed at the choke point: sdkSpec.Header scrubs each interpolated value
// through sdkCommentSafe, which covers all four comment syntaxes at once
// rather than per emitter. The escape payload below therefore has to survive
// nothing; it must come out inert everywhere.
func TestSDKHeaderCannotEscapeComment(t *testing.T) {
	spec := func() sdkSpec {
		decl := framework.EntityDeclaration{
			Name:   "tickets",
			Fields: []framework.FieldDeclaration{{Name: "title", Type: "string"}},
		}
		return sdkSpec{
			App: "app", SDKVersion: "0.0.0", GofastrVersion: "dev", Module: "local/app-sdk",
			Decls:    []framework.EntityDeclaration{decl},
			Entities: []cliEntity{buildEntityModel(decl, cliVerbs)},
		}
	}
	// One escape shape, because one thing ends a `//` comment: a newline.
	//
	// sdkCommentSafe also defuses `-->` and `*/`. Those are NOT asserted here,
	// deliberately: the only artifact wrapping the stamp in an HTML comment is
	// the README, which no route serves (see assertStampStayedInert), so a test
	// over them could not fail and would be noise. They stay in the production
	// helper as defense in depth for the READMEs and for any future emitter that
	// puts the stamp in a block comment.
	escapes := map[string]string{
		"newline": "app\nPWN()\nvoid 0 //",
	}
	sites := []struct {
		site string
		mut  func(*sdkSpec, string)
	}{
		{"sdk.name", func(s *sdkSpec, v string) { s.App = v }},
		{"sdk.sdk_version", func(s *sdkSpec, v string) { s.SDKVersion = v }},
		{"sdk.gofastr_version", func(s *sdkSpec, v string) { s.GofastrVersion = v }},
	}
	for label, escape := range escapes {
		for _, tc := range sites {
			t.Run(tc.site+"/"+label, func(t *testing.T) {
				sp := spec()
				tc.mut(&sp, escape)
				files := renderSDKJSFiles(sp)
				goFiles, err := renderSDKGoFiles(sp)
				if err != nil {
					t.Fatalf("renderSDKGoFiles: %v", err)
				}
				files = append(files, goFiles...)
				for _, f := range files {
					assertStampStayedInert(t, tc.site, f)
				}
			})
		}
	}
}

// assertStampStayedInert requires every PWN-bearing line to still be inside the
// comment the stamp was written into.
//
// Scope note: this checks the client artifacts only, client.js and client.d.ts
// (served from the app's own origin at <base>/sdk/client.js as
// application/javascript, so an escape there is same-origin script execution)
// and client.go (compiled by the consumer). The READMEs are deliberately NOT
// swept: they interpolate spec.App into heading and prose positions on purpose,
// no route serves them (sdkdocs registers /sdk/client.js, /sdk/client.d.ts and
// the zip, never the README), and the value there is the developer's own app
// name in a file in their own tree. Asserting on prose interpolation would be a
// wrong-layer test. The stamp comment inside them is covered because Header()
// is the single choke point all four artifacts share.
func assertStampStayedInert(t *testing.T, site string, f generatedFile) {
	t.Helper()
	if !strings.HasPrefix(f.name, "client.") {
		return
	}
	for n, line := range strings.Split(f.content, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.Contains(trimmed, "PWN") {
			continue
		}
		if strings.HasPrefix(trimmed, "//") {
			continue
		}
		t.Errorf("SECURITY: [injection] %s: %s:%d escaped the comment: %q", site, f.name, n+1, line)
	}
}

// Property: the SDK's go.mod contains exactly the module directive the
// generator meant to write.
//
// renderSDKGoFiles builds it as fmt.Sprintf("module %s\n\ngo 1.21\n", …) with
// nothing between the value and the next directive, so a newline in --module
// (or in gofastr.codegen.yml's `module:`) appends directives of the attacker's
// choosing. `replace` is the sharp one: it redirects a dependency to a path in
// the tree, and every downstream repo that builds the SDK compiles whatever is
// there. Rejected at buildSDKSpec so no target can emit it.
func TestSDKModulePathRejectsDirectives(t *testing.T) {
	for _, module := range []string{
		"local/app-sdk\n\nreplace golang.org/x/net => ./evil",
		"local/app-sdk\nrequire evil.test v1.0.0",
		"local/app-sdk // ",
		"local/app-sdk\r",
		"-local/app-sdk",
	} {
		if err := validateSDKModulePath(module); err == nil {
			t.Errorf("SECURITY: [injection] module %q accepted; it lands in go.mod as a bare directive line", module)
		}
	}
	for _, ok := range []string{"local/app-sdk", "example.com/team/app-sdk", "app_sdk", "a.b~c/d-e"} {
		if err := validateSDKModulePath(ok); err != nil {
			t.Errorf("legitimate module path %q rejected: %v", ok, err)
		}
	}
}

// Property: an entity relation name is constrained the same way every other
// name in the IR is.
//
// validateBlueprint runs isGoIdentifier(toCamelCase(x)) over entity names, field
// names, screen names, endpoint handlers, middleware, plugins and helpers.
// Relation names got NO such check, yet renderEntityModel emits
// toCamelCase(rel.Name) in struct-field-identifier position and
// toCamelJSON(rel.Name) inside a BACKTICK struct tag (which has no escape
// mechanism at all), and generate_typed.go emits toCamelCase(rel.Name) in
// const-identifier position.
//
// Severity, honestly: what this reliably produces is generated Go that does not
// compile, NOT injected code. The depth pass tried to build a payload yielding
// valid Go and could not: the two identifier sites land in the same file and
// demand contradictory syntax (a struct field vs. a spec inside `const (` that
// is prefixed with "TicketsIncl"), so satisfying one breaks the other. That is
// an argument, not a proof of impossibility, which is why the fix is the guard
// every sibling name already has rather than a bet on that argument holding.
// assertBlueprintGoParses in blueprint.go is the backstop for the next such gap.
func TestRelationNameMustBeAnIdentifier(t *testing.T) {
	bp := func(name string) Blueprint {
		return Blueprint{
			App: BlueprintApp{Name: "app"},
			Entities: []framework.EntityDeclaration{{
				Name:   "tickets",
				Fields: []framework.FieldDeclaration{{Name: "title", Type: "string"}},
				Relations: []framework.Relation{{
					Type: framework.RelManyToOne, Name: name, Entity: "tickets", ForeignKey: "parent_id",
				}},
			}},
			Screens: []BlueprintScreen{{Name: "home", Route: "/", Type: "page"}},
		}
	}
	for _, name := range []string{
		"parent`+PWN()+`",
		"parent\tstring\n}\n\nfunc\tPWN(){}\n\ntype\tJunk\tstruct{\n\tZ",
	} {
		if err := validateBlueprint(bp(name)); err != nil {
			continue // the fix: reject at the boundary like every sibling name
		}
		files, err := renderBlueprintFiles(bp(name))
		if err != nil {
			continue
		}
		for _, f := range files {
			if strings.HasSuffix(f.name, ".go") {
				assertIRStayedData(t, "relation.name", f.name, f.content)
			}
		}
	}
}

// Property: `gofastr generate sdk` reads the same hand-written entities/*.go
// packReadEntities serves `gofastr generate cli` (the boundary the tests in
// generate_cli_injection_security_test.go defend), but buildSDKSpec applies
// NONE of the guards buildCLIEntity applies: no isGoIdentifier on the
// derived struct name, no literal-safety check on the table. The table lands
// raw inside "/%s" path literals in renderClientEntity — the same sink the
// CLI guards — and a table like x"+PWN()+"y yields
//
//	path := "/x"+PWN()+"y"
//
// which PARSES, so the format.Source gate renderSDKGoFiles leans on waves it
// through: the injected call compiles into the downloadable, auto-built Go
// SDK (dist/sdk-go.zip, consumed via `go build` in downstream repos). The
// refusal belongs at buildSDKSpec so both targets (go + js) are covered
// before any file is rendered.
func TestSDKSpecRefusesHostileDeclarations(t *testing.T) {
	hostile := map[string]framework.EntityDeclaration{
		"table closes the path literal": {
			Name: "events", Table: `x"+PWN()+"y`,
			Fields: []framework.FieldDeclaration{{Name: "title", Type: "string"}},
		},
		"table backslash escapes the closing quote": {
			Name: "events", Table: `x\`,
			Fields: []framework.FieldDeclaration{{Name: "title", Type: "string"}},
		},
		"table with newline": {
			Name: "events", Table: "x\ny",
			Fields: []framework.FieldDeclaration{{Name: "title", Type: "string"}},
		},
		"entity name is not an identifier": {
			Name: `po"sts`, Table: "posts",
			Fields: []framework.FieldDeclaration{{Name: "title", Type: "string"}},
		},
	}
	for label, decl := range hostile {
		opts := sdkOptions{name: "app"}
		spec, err := buildSDKSpec([]framework.EntityDeclaration{decl}, &opts)
		if err != nil {
			continue // refused at the boundary, like generate cli
		}
		// Not refused at spec build: the artifact must at least refuse to
		// render (the parse gate) rather than carry the payload.
		if _, err := renderSDKGoFiles(spec); err != nil {
			continue
		}
		// Neither layer refused: prove the payload reached expression
		// position in the emitted, parsing client.go.
		files, _ := renderSDKGoFiles(spec)
		for _, f := range files {
			assertIRStayedData(t, "sdk."+label, f.name, f.content)
		}
	}
	// Control: the plain declarations real apps generate still pass both
	// layers.
	opts := sdkOptions{name: "app"}
	spec, err := buildSDKSpec([]framework.EntityDeclaration{{
		Name: "events", Table: "blog_posts",
		Fields: []framework.FieldDeclaration{{Name: "title", Type: "string"}},
	}}, &opts)
	if err != nil {
		t.Fatalf("plain declaration rejected at spec build: %v", err)
	}
	if _, err := renderSDKGoFiles(spec); err != nil {
		t.Fatalf("plain declaration rejected at render: %v", err)
	}
}

// Property: distinct entities must derive distinct client identifiers.
// jsResourceProp is toCamelJSON(table), which collapses blog_posts,
// blog-posts and "blog posts" onto blogPosts. Two such entities emit
//
//	export const blogPostsFields = Object.freeze({...})  // entity A
//	export const blogPostsFields = Object.freeze({...})  // entity B — SyntaxError
//	this["blogPosts"] = new Resource(this, "blog_posts") // entity A
//	this["blogPosts"] = new Resource(this, "blogPosts")  // entity B — last wins
//
// into the SAME client.js — the artifact sdkdocs serves to browsers at
// /docs/api/sdk/client.js. The duplicate const is a hard SyntaxError (the
// whole SDK stops parsing), and the duplicate binding silently points
// client.blogPosts at the second table. The Go target has the same disease
// past its own gate: duplicate `type BlogPosts struct` PARSES (format.Source
// passes) and only fails when the consumer builds the zip. buildSDKSpec must
// refuse the collision before anything renders; the FileSet collision guard
// cannot see inside one file.
func TestSDKDerivedNamesMustNotCollide(t *testing.T) {
	for label, tables := range map[string][2]string{
		"underscore vs camel": {"blog_posts", "blogPosts"},
		"underscore vs dash":  {"blog_posts", "blog-posts"},
		"underscore vs space": {"blog_posts", "blog posts"},
	} {
		decls := []framework.EntityDeclaration{
			{Name: tables[0], Table: tables[0], Fields: []framework.FieldDeclaration{{Name: "title", Type: "string"}}},
			{Name: tables[1], Table: tables[1], Fields: []framework.FieldDeclaration{{Name: "title", Type: "string"}}},
		}
		opts := sdkOptions{name: "app"}
		spec, err := buildSDKSpec(decls, &opts)
		if err != nil {
			continue // refused at the boundary: the fix
		}
		// Surface: the served client.js must declare each resource once.
		jsFiles := renderSDKJSFiles(spec)
		js, dts := jsFiles[0].content, jsFiles[1].content
		for _, needle := range []string{"export const blogPostsFields", `this["blogPosts"]`} {
			if n := strings.Count(js, needle); n > 1 {
				t.Errorf("SECURITY: [derived-collision] %s: client.js emits %q %d times — duplicate const is a SyntaxError in the served artifact and the second this[...] binding silently re-points client.blogPosts at the other table", label, needle, n)
			}
		}
		// Surface: the d.ts must not declare the property twice.
		if n := strings.Count(dts, "readonly blogPosts:"); n > 1 {
			t.Errorf("SECURITY: [derived-collision] %s: client.d.ts declares readonly blogPosts %d times", label, n)
		}
		// Surface: the Go SDK must not declare the same type twice.
		goFiles, rerr := renderSDKGoFiles(spec)
		if rerr != nil {
			t.Errorf("%s: go render failed: %v", label, rerr)
			continue
		}
		if n := strings.Count(goFiles[0].content, "type BlogPosts struct"); n > 1 {
			t.Errorf("SECURITY: [derived-collision] %s: client.go declares type BlogPosts %d times — parses (format.Source passes) and only fails in the consumer's go build", label, n)
		}
	}

	// The same property at the CLI emitter: two entity NAMES whose
	// toCamelCase forms collide (events / Events) derive the same struct
	// prefix, so runEventsList is DEFINED TWICE across two distinct,
	// individually-parsing files. No layer catches it — the FileSet guard
	// only sees paths, and each file parses — and the operator's first
	// signal is a confusing compile error in code they were told is theirs.
	t.Run("cli struct names collide", func(t *testing.T) {
		decls := []framework.EntityDeclaration{
			{Name: "events", Table: "events", Fields: []framework.FieldDeclaration{{Name: "title", Type: "string"}}},
			{Name: "Events", Table: "event_log", Fields: []framework.FieldDeclaration{{Name: "title", Type: "string"}}},
		}
		spec, err := buildCLISpec(decls, defaultCLIOptions(), "example.com/app/entities/client")
		if err != nil {
			return // refused at the boundary: the fix
		}
		files := renderCLIFiles(spec)
		if _, err := fileSetFromGeneratedFiles(files, "cli"); err != nil {
			return // the write layer refused: also the fix
		}
		defs := 0
		for _, f := range files {
			defs += strings.Count(f.content, "func runEventsList(")
		}
		if defs > 1 {
			t.Errorf("SECURITY: [derived-collision] events/Events both derive struct Events: runEventsList defined %d times across files that pass every generation gate — the generated CLI cannot compile and nothing refused it", defs)
		}
	})
}
