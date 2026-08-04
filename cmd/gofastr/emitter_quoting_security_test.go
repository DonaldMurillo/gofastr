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
// screen emitters read. Same rubric as the sibling — reject at validate, or
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
	add("screen.layout", func(b *Blueprint) { b.Screens[0].Layout = payload })
	add("screen.access.role", func(b *Blueprint) {
		b.Screens[0].Access = BlueprintAccess{Auth: true, Role: payload}
	})

	// ---- blocks ----
	block := func(site string, blk BlueprintBlock) {
		add(site, func(b *Blueprint) { b.Screens[0].Body = []BlueprintBlock{blk} })
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
// nothing — it must come out inert everywhere.
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
// Scope note: this checks the client artifacts only — client.js and client.d.ts
// (served from the app's own origin at <base>/sdk/client.js as
// application/javascript, so an escape there is same-origin script execution)
// and client.go (compiled by the consumer). The READMEs are deliberately NOT
// swept: they interpolate spec.App into heading and prose positions on purpose,
// no route serves them (sdkdocs registers /sdk/client.js, /sdk/client.d.ts and
// the zip — never the README), and the value there is the developer's own app
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
// valid Go and could not — the two identifier sites land in the same file and
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
