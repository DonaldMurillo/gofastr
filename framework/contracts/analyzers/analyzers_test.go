package analyzers_test

import (
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/contracts"
	"github.com/DonaldMurillo/gofastr/framework/contracts/analyzers"
)

// fixture builds a throwaway module from a file map and runs every
// analyzer over it. Each test states the exact rules it expects and the
// exact rules it must NOT see. False positives are the failure mode
// that kills a linter, so absence is asserted as loudly as presence.
func fixture(t *testing.T, files map[string]string) []contracts.Diagnostic {
	t.Helper()
	dir := t.TempDir()
	if _, ok := files["go.mod"]; !ok {
		files["go.mod"] = "module example.com/app\n\ngo 1.26\n"
	}
	for name, body := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cfg := contracts.DefaultConfig()
	pass, err := contracts.NewPass(dir, cfg)
	if err != nil {
		t.Fatal(err)
	}
	report, err := contracts.Run(pass, contracts.RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Errors) > 0 {
		t.Fatalf("analyzer errors: %v", report.Errors)
	}
	return report.Diagnostics
}

func rules(ds []contracts.Diagnostic) map[string]int {
	out := map[string]int{}
	for _, d := range ds {
		out[d.RuleID]++
	}
	return out
}

func assertHas(t *testing.T, ds []contracts.Diagnostic, ruleID string) contracts.Diagnostic {
	t.Helper()
	for _, d := range ds {
		if d.RuleID == ruleID {
			return d
		}
	}
	t.Fatalf("expected %s; got %v", ruleID, rules(ds))
	return contracts.Diagnostic{}
}

func assertNot(t *testing.T, ds []contracts.Diagnostic, ruleID, why string) {
	t.Helper()
	for _, d := range ds {
		if d.RuleID == ruleID {
			t.Fatalf("false positive %s at %s (%s): %s", ruleID, d.Location(), why, d.Message)
		}
	}
}

// routerFile wraps route registrations in a module that imports the
// router, which is what makes the route analyzer look at the file.
func routerFile(body string) string {
	return `package main

import (
	"net/http"

	"github.com/DonaldMurillo/gofastr/core/router"
)

var _ = http.NotFound

func wire(r *router.Router) {
` + body + `
}
`
}

// ----------------------------------------------------------------------
// Routing
// ----------------------------------------------------------------------

func TestColonPathParameterIsReported(t *testing.T) {
	ds := fixture(t, map[string]string{
		"main.go": routerFile(`	r.Handle("GET", "/users/:id", nil)`),
	})
	d := assertHas(t, ds, contracts.RuleColonPathParam)
	if !strings.Contains(d.Suggestion, "/users/{id}") {
		t.Errorf("suggestion does not show the corrected pattern: %q", d.Suggestion)
	}
}

func TestBracePathParameterIsClean(t *testing.T) {
	ds := fixture(t, map[string]string{
		"main.go": routerFile(`	r.Handle("GET", "/users/{id}", nil)`),
	})
	assertNot(t, ds, contracts.RuleColonPathParam, "brace syntax is the correct spelling")
}

func TestScreenColonParameterIsNotReported(t *testing.T) {
	// core-ui's screen router takes `:id` natively. It even rewrites
	// `{id}` into `:id`. Flagging it there would be telling people to
	// break working code.
	ds := fixture(t, map[string]string{
		"screens.go": `package main

import "github.com/DonaldMurillo/gofastr/core-ui/app"

func wire(site *app.App) {
	site.RegisterScreen(app.NewScreen("/users/:id", nil), nil)
}
`,
	})
	assertNot(t, ds, contracts.RuleColonPathParam, "screens use core-ui's router, not ServeMux")
}

func TestNonUppercaseMethodIsReportedWithAFix(t *testing.T) {
	ds := fixture(t, map[string]string{
		"main.go": routerFile(`	r.Handle("post", "/orders", nil)`),
	})
	d := assertHas(t, ds, contracts.RuleNonUppercaseVerb)
	if d.Fix == nil || len(d.Fix.Edits) != 1 {
		t.Fatalf("expected a mechanical fix, got %+v", d.Fix)
	}
	if d.Fix.Edits[0].New != `"POST"` {
		t.Errorf("fix writes %q", d.Fix.Edits[0].New)
	}
}

func TestDuplicateRouteIsReportedWithinAPackage(t *testing.T) {
	ds := fixture(t, map[string]string{
		"main.go": routerFile("\tr.Handle(\"GET\", \"/a\", nil)\n\tr.Handle(\"GET\", \"/a\", nil)"),
	})
	assertHas(t, ds, contracts.RuleDuplicateRoute)
}

func TestSameRouteInDifferentPackagesIsNotDuplicate(t *testing.T) {
	// Two programs in one repository both serving /healthz do not
	// collide. Scoping duplicates globally made every example app in the
	// tree report against every other.
	ds := fixture(t, map[string]string{
		"appa/main.go": routerFile(`	r.Handle("GET", "/healthz", nil)`),
		"appb/main.go": routerFile(`	r.Handle("GET", "/healthz", nil)`),
	})
	assertNot(t, ds, contracts.RuleDuplicateRoute, "separate packages are separate programs")
}

func TestGroupPrefixIsAppliedAndGuardsRecognised(t *testing.T) {
	ds := fixture(t, map[string]string{
		"main.go": `package main

import "github.com/DonaldMurillo/gofastr/framework"

func wire(app *framework.App) {
	admin := app.Group("/admin", nil)
	admin.Post("/users", nil)
}
`,
	})
	// Registered on a group carrying an option, so it is guarded.
	assertNot(t, ds, contracts.RuleUnguardedMutation, "the group carries a guard")
}

func TestUnguardedMutationIsReported(t *testing.T) {
	ds := fixture(t, map[string]string{
		"main.go": routerFile(`	r.Handle("DELETE", "/users/{id}", nil)`),
	})
	assertHas(t, ds, contracts.RuleUnguardedMutation)
}

// GOFASTR1004's premise, that sorting a table is not navigation, is a
// statement about browsers: scroll position, focus, history entries.
// A headless API has none of those, and `GET /orders/page/{n}` is an
// ordinary REST shape there, so the rule only runs when the module
// actually renders UI. Advisory even then: the evidence is a path
// segment's NAME, and URL-addressable pagination is a legitimate design
// (a blog's page 2 wants a URL).
func TestInPageStateAsRouteFiresOnlyForUIApps(t *testing.T) {
	headless := fixture(t, map[string]string{
		"main.go": routerFile(`	r.Handle("GET", "/orders/page/{n}", nil)`),
	})
	assertNot(t, headless, contracts.RuleStateAsRoute,
		"a module that renders no UI has no in-page state")

	ui := fixture(t, map[string]string{
		"main.go": routerFile(`	r.Handle("GET", "/orders/page/{n}", nil)`),
		"page.go": `package main

import "github.com/DonaldMurillo/gofastr/core-ui/html"

func page() any { return html.Text("orders") }
`,
	})
	d := assertHas(t, ui, contracts.RuleStateAsRoute)
	if d.Severity != contracts.SeverityInfo {
		t.Errorf("severity = %v, want info — a name heuristic must never gate", d.Severity)
	}
}

// The word list must only match state VERBS, never resource nouns, and
// must stay away from continuous client-owned state, where a URL is the
// deliberate, shareable artifact.
func TestStateRouteWordListSparesResourcesAndClientState(t *testing.T) {
	ui := func(route string) []contracts.Diagnostic {
		return fixture(t, map[string]string{
			"main.go": routerFile(route),
			"page.go": `package main

import "github.com/DonaldMurillo/gofastr/core-ui/html"

func page() any { return html.Text("x") }
`,
		})
	}
	// "order" the noun: a resource route in any shop, not sort state.
	assertNot(t, ui(`	r.Handle("GET", "/order/{id}", nil)`),
		contracts.RuleStateAsRoute, "a singular resource route is not in-page state")
	// A wildcard NAMED "page" is a resource identifier, a CMS page slug,
	// not pagination. Only literal segments carry the rule's evidence;
	// stripping the braces before matching erased that distinction.
	assertNot(t, ui(`	r.Handle("GET", "/docs/{page}", nil)`),
		contracts.RuleStateAsRoute, "a parameter named page is a slug, not pagination")
	// A shareable map viewport and an animated chart range: continuous,
	// client-owned state whose URL form is the point. Neither routes nor
	// islands, and not this rule's business.
	assertNot(t, ui(`	r.Handle("GET", "/map/{lat}/{lng}/{zoom}", nil)`),
		contracts.RuleStateAsRoute, "a map viewport URL is a shareable artifact")
	assertNot(t, ui(`	r.Handle("GET", "/metrics/chart/{range}", nil)`),
		contracts.RuleStateAsRoute, "a chart range URL is a shareable artifact")
}

// ----------------------------------------------------------------------
// Security
// ----------------------------------------------------------------------

func TestSQLConcatWithRequestDataIsReported(t *testing.T) {
	ds := fixture(t, map[string]string{
		"db.go": `package main

import "database/sql"

func f(db *sql.DB, req struct{ Email string }) {
	rows, _ := db.Query("SELECT * FROM users WHERE email = '" + req.Email + "'")
	_ = rows
}
`,
	})
	assertHas(t, ds, contracts.RuleSQLStringConcat)
}

func TestSQLPlaceholderIsClean(t *testing.T) {
	ds := fixture(t, map[string]string{
		"db.go": `package main

import "database/sql"

func f(db *sql.DB, email string) {
	rows, _ := db.Query("SELECT * FROM users WHERE email = $1", email)
	_ = rows
}
`,
	})
	assertNot(t, ds, contracts.RuleSQLStringConcat, "placeholders are the correct form")
}

func TestSQLConcatRespectsLegacyAnnotation(t *testing.T) {
	ds := fixture(t, map[string]string{
		"db.go": `package main

import "database/sql"

func f(db *sql.DB, req struct{ Email string }) {
	// safe-sql: the column set is a fixed allow-list checked above
	rows, _ := db.Query("SELECT * FROM users WHERE email = '" + req.Email + "'")
	_ = rows
}
`,
	})
	assertNot(t, ds, contracts.RuleSQLStringConcat, "the pre-contracts annotation still says why")
}

func TestInsecureCookieIsReported(t *testing.T) {
	ds := fixture(t, map[string]string{
		"h.go": `package main

import "net/http"

func f(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{Name: "sid", Value: token})
}
`,
	})
	d := assertHas(t, ds, contracts.RuleInsecureCookie)
	for _, want := range []string{"HttpOnly", "Secure", "SameSite"} {
		if !strings.Contains(d.Message, want) {
			t.Errorf("message does not name %s: %q", want, d.Message)
		}
	}
}

// applyFixes runs the analyzers over a one-file module, applies every
// mechanical fix, and returns the rewritten source, the round trip the
// `--fix` flag performs.
func applyFixes(t *testing.T, source string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range map[string]string{
		"go.mod": "module example.com/app\n\ngo 1.26\n",
		"h.go":   source,
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	pass, err := contracts.NewPass(dir, contracts.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	report, err := contracts.Run(pass, contracts.RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := report.Apply(); err != nil {
		t.Fatalf("apply: %v", err)
	}
	out, err := os.ReadFile(filepath.Join(dir, "h.go"))
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

func TestCookieFixSingleLineLiteral(t *testing.T) {
	got := applyFixes(t, `package main

import "net/http"

func f(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{Name: "sid", Value: token})
}
`)
	for _, want := range []string{"HttpOnly: true", "Secure:   true", "SameSite: http.SameSiteLaxMode"} {
		if !strings.Contains(got, want) {
			t.Errorf("fixed source is missing %q:\n%s", want, got)
		}
	}
	// Apply gofmt-s the result, so the output must be canonical Go. That
	// is what lets the edit ignore surrounding indentation.
	if formatted, err := format.Source([]byte(got)); err != nil {
		t.Fatalf("fixed source does not parse: %v\n%s", err, got)
	} else if string(formatted) != got {
		t.Errorf("fixed source is not gofmt-clean:\n%s", got)
	}
}

func TestCookieFixMultiLineLiteralWithTrailingComma(t *testing.T) {
	got := applyFixes(t, `package main

import "net/http"

func f(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:  "sid",
		Value: token,
		Path:  "/",
	})
}
`)
	if !strings.Contains(got, "SameSite: http.SameSiteLaxMode") {
		t.Fatalf("multi-line literal not fixed:\n%s", got)
	}
	if formatted, err := format.Source([]byte(got)); err != nil {
		t.Fatalf("fixed source does not parse: %v\n%s", err, got)
	} else if string(formatted) != got {
		t.Errorf("fixed source is not gofmt-clean:\n%s", got)
	}
	// Re-verifying the fixed source must now be clean, or `--fix` would
	// loop reporting what it just repaired.
	dir := t.TempDir()
	for name, body := range map[string]string{
		"go.mod": "module example.com/app\n\ngo 1.26\n", "h.go": got,
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	pass, err := contracts.NewPass(dir, contracts.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	report, err := contracts.Run(pass, contracts.RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	assertNot(t, report.Diagnostics, contracts.RuleInsecureCookie, "the fix resolved it")
}

func TestCookieFixPreservesAPartialLiteral(t *testing.T) {
	// Only the genuinely missing attributes are added; an explicit
	// SameSiteStrictMode must survive.
	got := applyFixes(t, `package main

import "net/http"

func f(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{Name: "sid", Value: token, SameSite: http.SameSiteStrictMode})
}
`)
	if !strings.Contains(got, "http.SameSiteStrictMode") {
		t.Errorf("the existing SameSite was overwritten:\n%s", got)
	}
	if strings.Contains(got, "SameSiteLaxMode") {
		t.Errorf("a second SameSite was added:\n%s", got)
	}
	if !strings.Contains(got, "HttpOnly: true") || !strings.Contains(got, "Secure:") {
		t.Errorf("the missing attributes were not added:\n%s", got)
	}
}

func TestSecureCookieIsClean(t *testing.T) {
	ds := fixture(t, map[string]string{
		"h.go": `package main

import "net/http"

func f(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name: "sid", Value: token,
		HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode,
	})
}
`,
	})
	assertNot(t, ds, contracts.RuleInsecureCookie, "every attribute is set")
}

func TestCookieDeletionIsNotReported(t *testing.T) {
	// Clearing a cookie carries no secret; reporting it is pure noise.
	ds := fixture(t, map[string]string{
		"h.go": `package main

import "net/http"

func f(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: "sid", Value: "", Path: "/", MaxAge: -1})
}
`,
	})
	assertNot(t, ds, contracts.RuleInsecureCookie, "an empty value with a negative MaxAge is a deletion")
}

func TestHardcodedSecretShapeIsReported(t *testing.T) {
	ds := fixture(t, map[string]string{
		"cfg.go": "package main\n\nvar apiKey = \"sk-live-9f3c2a1b8e7d6c5f4a3b2c1d\"\n",
	})
	d := assertHas(t, ds, contracts.RuleHardcodedSecret)
	if strings.Contains(d.Snippet, "9f3c2a1b") {
		t.Error("the report printed the secret back out")
	}
}

func TestSecretNamedFieldsHoldingIdentifiersAreClean(t *testing.T) {
	// The false-positive class that made a name-only heuristic useless: a
	// variable called PasswordHash almost always holds a column name.
	ds := fixture(t, map[string]string{
		"cols.go": `package main

type cols struct{ PasswordHash, TokenKind, APIKeyHeader string }

var c = cols{
	PasswordHash: "password_hash",
	TokenKind:    "TokenExpired",
	APIKeyHeader: "X-Api-Key",
}

const secretKeyEnv = "STRIPE_API_KEY"
const i18nPasswordLabel = "ui.auth.password"
`,
	})
	assertNot(t, ds, contracts.RuleHardcodedSecret, "identifiers, enums, and env-var names are not credentials")
}

// ----------------------------------------------------------------------
// Data
// ----------------------------------------------------------------------

func TestIgnoredExecIsReported(t *testing.T) {
	ds := fixture(t, map[string]string{
		"db.go": `package main

import "database/sql"

func f(db *sql.DB, id string) {
	_, _ = db.Exec("DELETE FROM sessions WHERE id = $1", id)
}
`,
	})
	assertHas(t, ds, contracts.RuleIgnoredExec)
}

func TestIgnoredExecRespectsBestEffortAnnotation(t *testing.T) {
	ds := fixture(t, map[string]string{
		"db.go": `package main

import "database/sql"

func f(db *sql.DB, id string) {
	// best-effort: cleanup on a already-closing connection; the row expires anyway
	_, _ = db.Exec("DELETE FROM sessions WHERE id = $1", id)
}
`,
	})
	assertNot(t, ds, contracts.RuleIgnoredExec, "the annotation states the intent")
}

// ----------------------------------------------------------------------
// Performance
// ----------------------------------------------------------------------

func TestRegexpCompiledInFunctionIsReported(t *testing.T) {
	ds := fixture(t, map[string]string{
		"s.go": "package main\n\nimport \"regexp\"\n\nfunc slugify(s string) string {\n\tre := regexp.MustCompile(`[^a-z]+`)\n\treturn re.ReplaceAllString(s, \"-\")\n}\n",
	})
	assertHas(t, ds, contracts.RuleRegexpCompilePerCall)
}

func TestPackageLevelRegexpIsClean(t *testing.T) {
	ds := fixture(t, map[string]string{
		"s.go": "package main\n\nimport \"regexp\"\n\nvar reSlug = regexp.MustCompile(`[^a-z]+`)\n\nfunc slugify(s string) string { return reSlug.ReplaceAllString(s, \"-\") }\n",
	})
	assertNot(t, ds, contracts.RuleRegexpCompilePerCall, "package level compiles once")
}

func TestQueryInLoopParameterisedByTheItemIsReported(t *testing.T) {
	ds := fixture(t, map[string]string{
		"n1.go": `package main

import "database/sql"

func f(db *sql.DB, ids []string) {
	for _, id := range ids {
		row := db.QueryRow("SELECT name FROM users WHERE id = $1", id)
		_ = row
	}
}
`,
	})
	assertHas(t, ds, contracts.RuleQueryInLoop)
}

func TestBatchStatementLoopIsClean(t *testing.T) {
	// The discriminator: here the loop variable IS the statement, which
	// is how you are supposed to run a batch of DDL at startup. An earlier
	// version reported 44 of these across the repository.
	ds := fixture(t, map[string]string{
		"ddl.go": `package main

import "database/sql"

func migrate(db *sql.DB, statements []string) error {
	for _, ddl := range statements {
		if _, err := db.Exec(ddl); err != nil {
			return err
		}
	}
	return nil
}
`,
	})
	assertNot(t, ds, contracts.RuleQueryInLoop, "the loop variable is the statement, not a parameter")
}

// ----------------------------------------------------------------------
// Rendering
// ----------------------------------------------------------------------

func TestBespokeCSSIsReported(t *testing.T) {
	ds := fixture(t, map[string]string{
		"page.go": "package main\n\nconst css = `.card { border-radius: 8px; padding: 16px; }`\n",
	})
	assertHas(t, ds, contracts.RuleBespokeCSS)
}

func TestGoCompositeLiteralIsNotCSS(t *testing.T) {
	// Go struct fields collide with CSS property names constantly. An
	// earlier pattern flagged the generator's own emitted Go source.
	ds := fixture(t, map[string]string{
		"gen.go": `package main

type file struct{ name, content string }

func f(data []byte) []file {
	return []file{{name: "icon.png", content: string(data)}}
}
`,
	})
	assertNot(t, ds, contracts.RuleBespokeCSS, "a Go composite literal is not a stylesheet")
}

func TestCSSInACommentIsNotReported(t *testing.T) {
	ds := fixture(t, map[string]string{
		"doc.go": "package main\n\n// fontFaceCSS holds the @font-face rules for the theme's fonts.\nvar fontFaceCSS string\n",
	})
	assertNot(t, ds, contracts.RuleBespokeCSS, "prose about CSS is not CSS")
}

func TestShortVarDeclarationIsNotCSS(t *testing.T) {
	// Issue #220: `fill` and `stroke` are CSS property names AND legal Go
	// identifiers. In a short variable declaration the `:` of `:=`
	// satisfied the property-colon and the token reference satisfied the
	// value shape, so assigning a design-system token to a local read as
	// bespoke CSS — the exact thing this rule exists to encourage,
	// reported as the thing to stop doing.
	ds := fixture(t, map[string]string{
		"x.go": `package app

func f() string {
	fill := "var(--color-surface)"
	stroke := "var(--color-border)"
	_ = stroke
	return fill
}
`,
	})
	assertNot(t, ds, contracts.RuleBespokeCSS, "a Go short variable declaration assigning a token reference is not CSS")
}

func TestCSSVarValueInGoStringIsReported(t *testing.T) {
	// The := rejection must not widen into "any single-word property is
	// fine": a real declaration in a Go string, with no := anywhere near
	// the colon, still fires. This one only matches through the
	// CSS-shaped-value path (padding + var(--…)), not the hyphen rule.
	ds := fixture(t, map[string]string{
		"page.go": "package main\n\nconst css = `.my-card { padding: var(--spacing-md); }`\n",
	})
	assertHas(t, ds, contracts.RuleBespokeCSS)
}

func TestStructLiteralWithCSSValueIsReported(t *testing.T) {
	// A struct literal with a lowercase field name and a CSS-shaped
	// value has no := inside the match, so the := narrowing must leave
	// it caught. (A QUOTED map key, `{"color": "#fff"}`, does not match
	// at all — the quote between key and colon breaks property-colon
	// contiguity — and that is fine: a quoted key is data, not CSS.)
	ds := fixture(t, map[string]string{
		"m.go": "package main\n\ntype palette struct{ color string }\n\nvar p = palette{color: \"#fff\"}\n",
	})
	assertHas(t, ds, contracts.RuleBespokeCSS)
}

func TestBespokeCSSMessageNamesTheTrigger(t *testing.T) {
	ds := fixture(t, map[string]string{
		"page.go": "package main\n\nconst css = `.my-card { border-radius: 8px; }`\n",
	})
	d := assertHas(t, ds, contracts.RuleBespokeCSS)
	if !strings.Contains(d.Message, "matched") || !strings.Contains(d.Message, "border-radius:") {
		t.Errorf("message does not name the trigger that matched: %q", d.Message)
	}
}

func TestHardNavigationIsReported(t *testing.T) {
	ds := fixture(t, map[string]string{
		"js.go": "package main\n\nconst script = `document.getElementById('x').onclick = () => { location.href = '/orders' }`\n",
	})
	assertHas(t, ds, contracts.RuleHardNavigation)
}

func TestBespokeEventSourceIsReported(t *testing.T) {
	ds := fixture(t, map[string]string{
		"js.go": "package main\n\nconst script = `const es = new EventSource('/my-feed')`\n",
	})
	assertHas(t, ds, contracts.RuleBespokeEventSource)
}

// ----------------------------------------------------------------------
// Entities & permissions
// ----------------------------------------------------------------------

const entityImports = `package main

import (
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

var _ = schema.String
var _ = entity.EntityConfig{}
`

func TestUnscopedPIIEntityIsReported(t *testing.T) {
	ds := fixture(t, map[string]string{
		"e.go": entityImports + `
func wire(app *framework.App) {
	app.Entity("profiles", entity.EntityConfig{
		Fields: []schema.Field{
			{Name: "email", Type: schema.String},
			{Name: "phone", Type: schema.String},
		},
	})
}
`,
	})
	d := assertHas(t, ds, contracts.RuleUnscopedPII)
	if !strings.Contains(d.Message, "email") {
		t.Errorf("message does not name the field: %q", d.Message)
	}
}

func TestOwnerScopedEntityIsClean(t *testing.T) {
	ds := fixture(t, map[string]string{
		"e.go": entityImports + `
func wire(app *framework.App) {
	app.Entity("profiles", entity.EntityConfig{
		Fields: []schema.Field{{Name: "email", Type: schema.String}},
		Scope:  &entity.ScopeConfig{OwnerField: "user_id"},
	})
}
`,
	})
	assertNot(t, ds, contracts.RuleUnscopedPII, "OwnerField scopes rows to their owner")
}

func TestEntityWithBlankAccessBlockIsStillUnscoped(t *testing.T) {
	// An access block of empty strings gates nothing. Counting it as
	// scoping is the difference between "reviewed and declared open" and
	// "reviewed and forgot".
	ds := fixture(t, map[string]string{
		"e.go": entityImports + `
func wire(app *framework.App) {
	app.Entity("profiles", entity.EntityConfig{
		Fields:   []schema.Field{{Name: "email", Type: schema.String}},
		Exposure: &entity.ExposureConfig{Access: entity.AccessControl{Read: ""}},
	})
}
`,
	})
	assertHas(t, ds, contracts.RuleUnscopedPII)
}

func TestPublicEntityIsReported(t *testing.T) {
	ds := fixture(t, map[string]string{
		"e.go": entityImports + `
func wire(app *framework.App) {
	app.Entity("posts", entity.EntityConfig{
		Fields:   []schema.Field{{Name: "title", Type: schema.String}},
		Exposure: &entity.ExposureConfig{Public: true},
	})
}
`,
	})
	assertHas(t, ds, contracts.RulePublicEntity)
}

func TestEntityWithUnreadableFieldsIsNotGuessedAt(t *testing.T) {
	// Fields supplied by a helper are not statically resolvable. Reporting
	// "no PII found" would be a false all-clear, so the rule stays quiet.
	ds := fixture(t, map[string]string{
		"e.go": entityImports + `
func userFields() []schema.Field { return nil }

func wire(app *framework.App) {
	app.Entity("profiles", entity.EntityConfig{Fields: userFields()})
}
`,
	})
	assertNot(t, ds, contracts.RuleUnscopedPII, "the field list could not be read, so nothing is claimed")
}

// GOFASTR1703: CRUD entity exposed with no auth wired. The secure-by-
// default session requirement makes every operation 401 when no auth
// battery is present, so the whole surface reads as broken on first
// contact. Fires when no auth.New and no reader (SessionMiddleware /
// RequireAuth / BFF) appear anywhere.
func TestCrudEntityWithoutAuthIsReported(t *testing.T) {
	ds := fixture(t, map[string]string{
		"e.go": entityImports + `
func wire(app *framework.App) {
	app.Entity("posts", entity.EntityConfig{
		Fields: []schema.Field{{Name: "title", Type: schema.String}},
	})
}
`,
	})
	d := assertHas(t, ds, contracts.RuleCrudWithoutAuth)
	if !strings.Contains(d.Message, "posts") {
		t.Errorf("message does not name the entity: %q", d.Message)
	}
}

// Wiring auth (auth.New + a reader) clears the rule even though the entity
// is CRUD-exposed and not Public: authenticated callers can now reach it.
func TestCrudEntityWithAuthWiredIsClean(t *testing.T) {
	ds := fixture(t, map[string]string{
		"e.go": `package main

import (
	"github.com/DonaldMurillo/gofastr/battery/auth"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

func wire(app *framework.App) {
	mgr := auth.New(auth.AuthConfig{})
	app.Use(auth.SessionMiddleware(mgr))
	app.Entity("posts", entity.EntityConfig{
		Fields: []schema.Field{{Name: "title", Type: schema.String}},
	})
}
`,
	})
	assertNot(t, ds, contracts.RuleCrudWithoutAuth, "auth.New + SessionMiddleware is wired")
}

// A Public entity opts out of the session requirement by intent, so the
// no-auth posture is not a finding (GOFASTR1702 covers the Public choice).
func TestPublicEntityIsCleanOfCrudWithoutAuth(t *testing.T) {
	ds := fixture(t, map[string]string{
		"e.go": entityImports + `
func wire(app *framework.App) {
	app.Entity("posts", entity.EntityConfig{
		Fields:   []schema.Field{{Name: "title", Type: schema.String}},
		Exposure: &entity.ExposureConfig{Public: true},
	})
}
`,
	})
	assertNot(t, ds, contracts.RuleCrudWithoutAuth, "a Public entity is not flagged for lacking auth")
}

const eventSource = `package main

import (
	"context"

	"github.com/DonaldMurillo/gofastr/framework/event"
)

func wire(bus *event.EventBus) {
	bus.On("order.placed", func(ctx context.Context, e event.Event) error { return nil })
	bus.Subscribe("order.refunded", func(ctx context.Context, e event.Event) error { return nil })
}
`

func TestUnexercisedEventSubscriberIsReported(t *testing.T) {
	ds := hookFixture(t, eventSource,
		`{"version":1,"routes":{},"entities":{},"hooks":{},"events":["order.placed"]}`)
	d := assertHas(t, ds, contracts.RuleEventNotEmitted)
	if !strings.Contains(d.Message, "order.refunded") {
		t.Errorf("message does not name the event: %q", d.Message)
	}
	// The covered subscription must not also be reported.
	for _, other := range ds {
		if other.RuleID == contracts.RuleEventNotEmitted && strings.Contains(other.Message, "order.placed") {
			t.Error("a subscriber whose event fired was reported")
		}
	}
}

func TestSubscribeOnANonEventTypeIsIgnored(t *testing.T) {
	// `On` and `Subscribe` are ordinary method names. Without the import
	// guard, a cache or a signal store would be read as an event bus.
	ds := hookFixture(t, `package main

type store struct{}

func (s *store) On(key string, fn func()) {}

func wire(s *store) {
	s.On("some.key", func() {})
}
`, `{"version":1,"routes":{},"entities":{},"hooks":{},"events":[]}`)
	assertNot(t, ds, contracts.RuleEventNotEmitted, "this is not an event bus")
}

func TestTypedHookRegistrationsAreDiscovered(t *testing.T) {
	// The generic constructors appear in source both with an explicit
	// type argument and with it inferred; both have to be readable or the
	// hook-coverage rule silently checks nothing.
	dir := t.TempDir()
	files := map[string]string{
		"go.mod": "module example.com/app\n\ngo 1.26\n",
		"hooks.go": `package main

import (
	"context"

	"github.com/DonaldMurillo/gofastr/framework"
)

type Post struct{ Title string }

func wire(app *framework.App) {
	framework.OnBeforeCreate[Post](app, "posts", func(ctx context.Context, p *Post) error { return nil })
	framework.OnAfterDelete(app, "comments", func(ctx context.Context, id string) error { return nil })
}
`,
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	pass, err := contracts.NewPass(dir, contracts.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	hooks := analyzers.Hooks(pass)
	got := map[string]string{}
	for _, h := range hooks {
		got[h.Entity] = h.Type
	}
	if got["posts"] != "beforecreate" {
		t.Errorf("explicit type argument not read: %v", got)
	}
	if got["comments"] != "afterdelete" {
		t.Errorf("inferred type argument not read: %v", got)
	}
}

// hookFixture writes a module plus a hand-authored coverage manifest, so
// the testing rules can be exercised without running a real suite.
func hookFixture(t *testing.T, source, manifest string) []contracts.Diagnostic {
	t.Helper()
	dir := t.TempDir()
	write := func(name, body string) {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module example.com/app\n\ngo 1.26\n")
	write("main.go", source)
	write(".gofastr/semantic-coverage.json", manifest)

	pass, err := contracts.NewPass(dir, contracts.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	report, err := contracts.Run(pass, contracts.RunOptions{Analyzers: []string{"testing"}})
	if err != nil {
		t.Fatal(err)
	}
	return report.Diagnostics
}

const hookSource = `package main

import (
	"context"

	"github.com/DonaldMurillo/gofastr/framework"
)

type Post struct{ Title, Slug string }

func wire(app *framework.App) {
	framework.OnBeforeCreate[Post](app, "posts", func(ctx context.Context, p *Post) error {
		p.Slug = p.Title
		return nil
	})
}
`

func TestUnfiredHookIsReported(t *testing.T) {
	// The guard that decides whether there is any surface worth checking
	// has to count hooks. A package that registers hooks but wires its
	// routes elsewhere used to skip this check entirely.
	ds := hookFixture(t, hookSource, `{"version":1,"routes":{},"entities":{},"hooks":{}}`)
	d := assertHas(t, ds, contracts.RuleHookNotFired)
	if !strings.Contains(d.Message, "beforecreate") || !strings.Contains(d.Message, "posts") {
		t.Errorf("message does not name the hook and entity: %q", d.Message)
	}
}

func TestFiredHookIsNotReported(t *testing.T) {
	ds := hookFixture(t, hookSource,
		`{"version":1,"routes":{},"entities":{},"hooks":{"posts":["beforecreate"]}}`)
	assertNot(t, ds, contracts.RuleHookNotFired, "the manifest records this hook firing")
}

// ----------------------------------------------------------------------
// Suppression end-to-end
// ----------------------------------------------------------------------

func TestInlineSuppressionSilencesAFinding(t *testing.T) {
	body := `package main

import "net/http"

func f(w http.ResponseWriter, token string) {
	//gofastr:allow(GOFASTR1404) short-lived CSRF cookie, readable by the double-submit script
	http.SetCookie(w, &http.Cookie{Name: "csrf", Value: token})
}
`
	ds := fixture(t, map[string]string{"h.go": body})
	assertNot(t, ds, contracts.RuleInsecureCookie, "suppressed with a stated reason")
}

func TestStaleSuppressionIsReported(t *testing.T) {
	ds := fixture(t, map[string]string{
		"clean.go": "package main\n\n//gofastr:allow(GOFASTR1404) nothing here any more\nfunc f() {}\n",
	})
	assertHas(t, ds, contracts.RuleSuppressionStale)
}

// ----------------------------------------------------------------------
// Determinism
// ----------------------------------------------------------------------

func TestReportIsDeterministic(t *testing.T) {
	files := map[string]string{
		"main.go": routerFile("\tr.Handle(\"get\", \"/a/:id\", nil)\n\tr.Handle(\"POST\", \"/b\", nil)"),
		"db.go":   "package main\n\nimport \"regexp\"\n\nfunc f() { _ = regexp.MustCompile(`x`) }\n",
	}
	var first string
	for i := 0; i < 3; i++ {
		ds := fixture(t, files)
		var b strings.Builder
		for _, d := range ds {
			fmt.Fprintf(&b, "%s|%s|%d\n", d.RuleID, d.File, d.Line)
		}
		if i == 0 {
			first = b.String()
			continue
		}
		if b.String() != first {
			t.Fatalf("run %d differs:\n%s\nvs\n%s", i, b.String(), first)
		}
	}
}

const roleSource = `package main

import "github.com/DonaldMurillo/gofastr/framework/access"

func wire(policy *access.RolePolicy) {
	policy.Grant("editor", "posts:write")
	policy.Grant("support", "orders:read", "orders:refund")
}
`

func TestUnexercisedRoleIsReported(t *testing.T) {
	ds := hookFixture(t, roleSource,
		`{"version":1,"routes":{},"entities":{},"hooks":{},"roles":["editor"]}`)
	d := assertHas(t, ds, contracts.RuleRoleNotExercised)
	if !strings.Contains(d.Message, "support") {
		t.Errorf("message does not name the role: %q", d.Message)
	}
	for _, other := range ds {
		if other.RuleID == contracts.RuleRoleNotExercised && strings.Contains(other.Message, "editor") {
			t.Error("a role a test authenticated as was reported")
		}
	}
}

func TestGrantOnANonPolicyTypeIsIgnored(t *testing.T) {
	// `Grant` appears on OAuth clients and ledgers. Both the import guard
	// and the permission-shaped argument check have to hold, or those
	// become phantom roles.
	ds := hookFixture(t, `package main

type ledger struct{}

func (l *ledger) Grant(account string, amount int) {}

func wire(l *ledger) {
	l.Grant("acct-1", 500)
}
`, `{"version":1,"routes":{},"entities":{},"hooks":{},"roles":[]}`)
	assertNot(t, ds, contracts.RuleRoleNotExercised, "this is not a RolePolicy")
}

// ----------------------------------------------------------------------
// Architecture
// ----------------------------------------------------------------------

// archFixture builds a two-package module with a declared layering.
func archFixture(t *testing.T, config string, files map[string]string) []contracts.Diagnostic {
	t.Helper()
	dir := t.TempDir()
	files["go.mod"] = "module example.com/app\n\ngo 1.26\n"
	files["gofastr.contracts.yml"] = config
	for name, body := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cfg, err := contracts.LoadConfig(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	pass, err := contracts.NewPass(dir, cfg)
	if err != nil {
		t.Fatal(err)
	}
	report, err := contracts.Run(pass, contracts.RunOptions{Analyzers: []string{"architecture"}})
	if err != nil {
		t.Fatal(err)
	}
	return report.Diagnostics
}

const layerConfig = `
contracts:
  architecture:
    layers:
      - name: app
        packages: ["app/**"]
      - name: core
        packages: ["core/**"]
`

func TestUpwardImportIsReported(t *testing.T) {
	ds := archFixture(t, layerConfig, map[string]string{
		"app/app.go":   "package app\n\nfunc Run() {}\n",
		"core/core.go": "package core\n\nimport _ \"example.com/app/app\"\n",
	})
	d := assertHas(t, ds, contracts.RuleLayerViolation)
	if !strings.Contains(d.Message, "core") || !strings.Contains(d.Message, "app") {
		t.Errorf("message does not name both layers: %q", d.Message)
	}
}

func TestDownwardImportIsClean(t *testing.T) {
	ds := archFixture(t, layerConfig, map[string]string{
		"app/app.go":   "package app\n\nimport _ \"example.com/app/core\"\n",
		"core/core.go": "package core\n\nfunc Helper() {}\n",
	})
	assertNot(t, ds, contracts.RuleLayerViolation, "app may import core — that is the declared direction")
}

func TestSameLayerImportIsClean(t *testing.T) {
	ds := archFixture(t, `
contracts:
  architecture:
    layers:
      - name: core
        packages: ["core/**"]
`, map[string]string{
		"core/a/a.go": "package a\n\nimport _ \"example.com/app/core/b\"\n",
		"core/b/b.go": "package b\n\nfunc F() {}\n",
	})
	assertNot(t, ds, contracts.RuleLayerViolation, "a package may import its own layer")
}

func TestForbiddenImportIsReportedWithItsReason(t *testing.T) {
	ds := archFixture(t, `
contracts:
  architecture:
    forbid:
      - from: "core/**"
        to: "heavy/**"
        reason: "core must stay linkable without the decoders"
`, map[string]string{
		"core/core.go":   "package core\n\nimport _ \"example.com/app/heavy\"\n",
		"heavy/heavy.go": "package heavy\n\nfunc F() {}\n",
	})
	d := assertHas(t, ds, contracts.RuleForbiddenImport)
	// The reason is the whole point. A ban nobody can explain gets
	// deleted the first time it is inconvenient.
	if !strings.Contains(d.Suggestion, "linkable without the decoders") {
		t.Errorf("the forbid reason did not reach the finding: %q", d.Suggestion)
	}
}

func TestArchitectureIsSilentWhenUnconfigured(t *testing.T) {
	// Inventing a layering for someone else's tree would be wrong more
	// often than right, and a wrong architecture rule trains people to
	// ignore the analyzer.
	ds := archFixture(t, "contracts:\n  exempt: []\n", map[string]string{
		"core/core.go": "package core\n\nimport _ \"example.com/app/app\"\n",
		"app/app.go":   "package app\n\nfunc Run() {}\n",
	})
	assertNot(t, ds, contracts.RuleLayerViolation, "no layering was declared")
}

// ----------------------------------------------------------------------
// AI guidance
// ----------------------------------------------------------------------

func TestHandrolledCRUDIsReported(t *testing.T) {
	ds := fixture(t, map[string]string{
		"main.go": routerFile("\tr.Handle(\"GET\", \"/posts\", nil)\n" +
			"\tr.Handle(\"POST\", \"/posts\", nil)\n" +
			"\tr.Handle(\"DELETE\", \"/posts/{id}\", nil)"),
	})
	d := assertHas(t, ds, contracts.RuleHandrolledCRUD)
	if !strings.Contains(d.Suggestion, "app.Entity") {
		t.Errorf("suggestion does not name the replacement: %q", d.Suggestion)
	}
}

func TestTwoRoutesOnAResourceIsNotHandrolledCRUD(t *testing.T) {
	// Two endpoints on a resource is a normal amount of custom behaviour;
	// only three or more of the five CRUD verbs reads as reimplementation.
	ds := fixture(t, map[string]string{
		"main.go": routerFile("\tr.Handle(\"GET\", \"/posts\", nil)\n" +
			"\tr.Handle(\"POST\", \"/posts/{id}/publish\", nil)"),
	})
	assertNot(t, ds, contracts.RuleHandrolledCRUD, "two endpoints is custom behaviour, not a CRUD surface")
}

func TestDeclaredEntitySuppressesHandrolledCRUD(t *testing.T) {
	// Hand-written routes beside a declared entity are the custom
	// operations, not a reimplementation of it.
	ds := fixture(t, map[string]string{
		"main.go": routerFile("\tr.Handle(\"GET\", \"/posts\", nil)\n" +
			"\tr.Handle(\"POST\", \"/posts\", nil)\n" +
			"\tr.Handle(\"DELETE\", \"/posts/{id}\", nil)"),
		"entities.go": entityImports + `
func declare(app *framework.App) {
	app.Entity("posts", entity.EntityConfig{
		Fields: []schema.Field{{Name: "title", Type: schema.String}},
	})
}
`,
	})
	assertNot(t, ds, contracts.RuleHandrolledCRUD, "the entity is declared; these are its custom operations")
}

func TestFrameworkInternalRoutesAreNotResources(t *testing.T) {
	ds := fixture(t, map[string]string{
		"main.go": routerFile("\tr.Handle(\"GET\", \"/healthz\", nil)\n" +
			"\tr.Handle(\"POST\", \"/mcp\", nil)\n" +
			"\tr.Handle(\"DELETE\", \"/mcp\", nil)\n" +
			"\tr.Handle(\"PUT\", \"/mcp\", nil)"),
	})
	assertNot(t, ds, contracts.RuleHandrolledCRUD, "/mcp is a framework surface, not the user's resource")
}

func TestHandrolledBatteryIsReported(t *testing.T) {
	ds := fixture(t, map[string]string{
		"mail.go": `package main

import "net/smtp"

func send(addr string, msg []byte) error {
	return smtp.SendMail(addr, nil, "from@example.com", []string{"to@example.com"}, msg)
}
`,
	})
	d := assertHas(t, ds, contracts.RuleHandrolledBattery)
	if !strings.Contains(d.Message, "battery/email") {
		t.Errorf("message does not name the battery: %q", d.Message)
	}
}

func TestRawSQLAgainstADeclaredEntityIsReported(t *testing.T) {
	ds := fixture(t, map[string]string{
		"entities.go": entityImports + `
func declare(app *framework.App) {
	app.Entity("posts", entity.EntityConfig{
		Fields: []schema.Field{{Name: "title", Type: schema.String}},
	})
}
`,
		"report.go": `package main

import "database/sql"

func count(db *sql.DB) int {
	var n int
	_ = db.QueryRow("SELECT COUNT(*) FROM posts").Scan(&n)
	return n
}
`,
	})
	d := assertHas(t, ds, contracts.RuleRawSQLOverRepo)
	if !strings.Contains(d.Message, "posts") {
		t.Errorf("message does not name the table: %q", d.Message)
	}
}

func TestRawSQLAgainstAnUndeclaredTableIsClean(t *testing.T) {
	// The rule is about bypassing an entity's scoping. A table with no
	// entity has no scoping to bypass.
	ds := fixture(t, map[string]string{
		"report.go": `package main

import "database/sql"

func count(db *sql.DB) int {
	var n int
	_ = db.QueryRow("SELECT COUNT(*) FROM legacy_metrics").Scan(&n)
	return n
}
`,
	})
	assertNot(t, ds, contracts.RuleRawSQLOverRepo, "no entity declares this table")
}

func TestLayerGlobDoesNotCollideWithTheModuleName(t *testing.T) {
	// Regression: matching every path *suffix* meant a module named
	// `example.com/acme/core` had the suffix `core` match the glob
	// `core/**`, putting every package in the module into the core layer.
	// The rule then found nothing, silently, forever.
	dir := t.TempDir()
	files := map[string]string{
		"go.mod":                "module example.com/acme/core\n\ngo 1.26\n",
		"gofastr.contracts.yml": layerConfig,
		"app/app.go":            "package app\n\nfunc Run() {}\n",
		"core/core.go":          "package core\n\nimport _ \"example.com/acme/core/app\"\n",
	}
	for name, body := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cfg, err := contracts.LoadConfig(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	pass, err := contracts.NewPass(dir, cfg)
	if err != nil {
		t.Fatal(err)
	}
	report, err := contracts.Run(pass, contracts.RunOptions{Analyzers: []string{"architecture"}})
	if err != nil {
		t.Fatal(err)
	}
	assertHas(t, report.Diagnostics, contracts.RuleLayerViolation)
}

func TestUnparsableFileDoesNotStopTheRest(t *testing.T) {
	// Verify has to be useful mid-edit. That is the whole reason the
	// analyzers are AST-based rather than type-checked, since a
	// go/packages load fails outright on a tree that does not compile.
	// A half-typed file must be skipped, not abort the run.
	ds := fixture(t, map[string]string{
		"half.go": "package main\n\nfunc broken( {\n",
		"ok.go": `package main

import "database/sql"

func f(db *sql.DB, id string) {
	_, _ = db.Exec("DELETE FROM sessions WHERE id = $1", id)
}
`,
	})
	// The valid file is still analysed.
	assertHas(t, ds, contracts.RuleIgnoredExec)
	// And nothing is reported against the file that could not be read.
	for _, d := range ds {
		if d.File == "half.go" {
			t.Errorf("reported %s against an unparsable file: %s", d.RuleID, d.Message)
		}
	}
}

func TestCRLFFileIsAnalysedAndKeepsItsLineEndings(t *testing.T) {
	// The framework builds for GOOS=windows, so CRLF working trees are a
	// real case. Two things must hold: findings are detected with correct
	// lines and clean snippets, and --fix must not rewrite the file's line
	// endings. gofmt always emits LF, so without restoring the original
	// convention a one-line fix produces a diff touching every line,
	// which is how people learn to distrust an autofixer.
	dir := t.TempDir()
	crlf := func(lines ...string) string { return strings.Join(lines, "\r\n") + "\r\n" }
	source := crlf(
		"package main",
		"",
		`import "net/http"`,
		"",
		"func f(w http.ResponseWriter, id string) {",
		`	http.SetCookie(w, &http.Cookie{Name: "sid", Value: id})`,
		"}",
	)
	for name, body := range map[string]string{
		"go.mod": "module example.com/app\n\ngo 1.26\n",
		"h.go":   source,
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	pass, err := contracts.NewPass(dir, contracts.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	report, err := contracts.Run(pass, contracts.RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	d := assertHas(t, report.Diagnostics, contracts.RuleInsecureCookie)
	if d.Line != 6 {
		t.Errorf("line = %d, want 6 — CRLF shifted the line count", d.Line)
	}
	if strings.Contains(d.Snippet, "\r") {
		t.Errorf("a carriage return leaked into the snippet: %q", d.Snippet)
	}

	if _, err := report.Apply(); err != nil {
		t.Fatal(err)
	}
	fixed, err := os.ReadFile(filepath.Join(dir, "h.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(fixed), "SameSite") {
		t.Fatalf("the fix was not applied:\n%s", fixed)
	}
	crlfCount := strings.Count(string(fixed), "\r\n")
	bareLF := strings.Count(string(fixed), "\n") - crlfCount
	if bareLF != 0 {
		t.Errorf("--fix left %d bare LF in a CRLF file — the whole file was rewritten", bareLF)
	}
	if crlfCount == 0 {
		t.Error("--fix stripped every CRLF")
	}
}

func TestLFFileStaysLFThroughFix(t *testing.T) {
	got := applyFixes(t, `package main

import "net/http"

func f(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{Name: "sid", Value: token})
}
`)
	if strings.Contains(got, "\r") {
		t.Errorf("--fix introduced carriage returns into an LF file:\n%q", got)
	}
	if !strings.Contains(got, "SameSite") {
		t.Error("the fix was not applied")
	}
}

func TestFixDoesNotOverrideASuppression(t *testing.T) {
	// A suppression is a written decision with a stated reason. An
	// autofixer that "fixes" it anyway silently reverses that decision,
	// and for GOFASTR1404 specifically, adding HttpOnly to a
	// double-submit CSRF cookie would break the mechanism it protects.
	source := `package main

import "net/http"

func f(w http.ResponseWriter, token string) {
	//gofastr:allow(GOFASTR1404) double-submit CSRF cookie must stay JS-readable
	http.SetCookie(w, &http.Cookie{Name: "csrf", Value: token})
}
`
	if got := applyFixes(t, source); got != source {
		t.Errorf("--fix rewrote a deliberately suppressed finding:\n--- before ---\n%s\n--- after ---\n%s", source, got)
	}
}

func TestSuppressionOnACRLFFile(t *testing.T) {
	// Directives are found through the AST's comment ranges; a stray
	// carriage return must not stop the rule name or the reason parsing.
	dir := t.TempDir()
	crlf := strings.Join([]string{
		"package main",
		"",
		`import "database/sql"`,
		"",
		"func f(db *sql.DB, id string) {",
		"\t//gofastr:allow(GOFASTR1601) fire-and-forget cleanup on a closing conn",
		`	_, _ = db.Exec("DELETE FROM sessions WHERE id = $1", id)`,
		"}",
	}, "\r\n") + "\r\n"
	for name, body := range map[string]string{
		"go.mod": "module example.com/app\n\ngo 1.26\n",
		"db.go":  crlf,
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	pass, err := contracts.NewPass(dir, contracts.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	report, err := contracts.Run(pass, contracts.RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	assertNot(t, report.Diagnostics, contracts.RuleIgnoredExec, "the directive suppresses it")
	if report.Suppressed != 1 {
		t.Errorf("Suppressed = %d, want 1 — the CRLF directive was not counted", report.Suppressed)
	}
}

// authSource builds a module that configures auth, with an optional line
// that mounts a credential reader.
func authSource(mount string) string {
	return `package main

import (
	"github.com/DonaldMurillo/gofastr/battery/auth"
	"github.com/DonaldMurillo/gofastr/framework"
)

func wire(fwApp *framework.App) {
	authMgr := auth.New(auth.AuthConfig{})
	_ = authMgr
	` + mount + `
}
`
}

func TestAuthConfiguredButNeverMounted(t *testing.T) {
	// A real shipped bug: the blueprint generator enabled the auth
	// battery and never mounted the session middleware, so every
	// authenticated request got 401 exactly like an anonymous one.
	ds := fixture(t, map[string]string{"main.go": authSource("")})
	d := assertHas(t, ds, contracts.RuleAuthNotWired)
	if !strings.Contains(d.Message, "reads the credential") {
		t.Errorf("message does not explain the consequence: %q", d.Message)
	}
}

func TestAuthMountedInEveryRecognisedForm(t *testing.T) {
	// Middleware is routinely passed as a VALUE rather than invoked at the
	// mount site, so matching only call expressions missed
	// `Group("/x", auth.RequireAuth)`, a real wiring form.
	for _, mount := range []string{
		"fwApp.Use(auth.SessionMiddleware(authMgr))",
		"_ = auth.RequireAuth",
		"auth.BFF(authMgr)",
	} {
		t.Run(mount, func(t *testing.T) {
			ds := fixture(t, map[string]string{"main.go": authSource(mount)})
			assertNot(t, ds, contracts.RuleAuthNotWired, "this form mounts a credential reader")
		})
	}
}

func TestAuthRuleIgnoresAModuleWithoutAuth(t *testing.T) {
	ds := fixture(t, map[string]string{
		"main.go": "package main\n\nfunc main() {}\n",
	})
	assertNot(t, ds, contracts.RuleAuthNotWired, "no auth is configured at all")
}

// A mount only covers a configure the compiler could link it with. With
// one module-global Mounted flag, the first binary that wired auth
// correctly silenced the rule for every OTHER binary in the module. And
// "configured but never read" is exactly the 401-for-everyone bug the
// rule exists to catch.
func TestAuthMountInOneAppDoesNotCoverAnother(t *testing.T) {
	ds := fixture(t, map[string]string{
		"cmd/appa/main.go": authSource("fwApp.Use(auth.SessionMiddleware(authMgr))"),
		"cmd/appb/main.go": authSource(""),
	})
	d := assertHas(t, ds, contracts.RuleAuthNotWired)
	if !strings.Contains(d.File, "appb") {
		t.Errorf("the finding points at %q, want the app that never mounts", d.File)
	}
	for _, other := range ds {
		if other.RuleID == contracts.RuleAuthNotWired && strings.Contains(other.File, "appa") {
			t.Error("the correctly wired app was reported too")
		}
	}
}

// Without a module path, files get RELATIVE-directory package names
// while the import matcher rejects every path: a graph with nodes and
// no edges, where a mount can never cover a configure in another
// directory. The promised fallback is the opposite: no go.mod means
// packages cannot be told apart, so everything collapses to one node
// and attribution is module-global, exactly the pre-graph behaviour.
func TestAuthAttributionWithoutAGoModule(t *testing.T) {
	ds := fixture(t, map[string]string{
		"go.mod": "", // present but empty: no module path to derive packages from
		"main.go": `package main

import (
	"example.com/app/setup"
	"example.com/app/web"
)

var _ = setup.Manager
var _ = web.Reader
`,
		"setup/setup.go": `package setup

import "github.com/DonaldMurillo/gofastr/battery/auth"

var Manager = auth.New(auth.AuthConfig{})
`,
		"web/web.go": `package web

import "github.com/DonaldMurillo/gofastr/battery/auth"

var Reader = auth.RequireAuth
`,
	})
	assertNot(t, ds, contracts.RuleAuthNotWired,
		"without a go.mod the packages are indistinguishable — the mount must count")
}

// A dot import puts the auth package's names in the file scope, so the
// mount is a bare `RequireAuth`, no selector for the walk to see. The
// import is still on the file, and missing it turns a correctly wired
// app into an error-severity false positive.
func TestAuthMountThroughADotImportIsSeen(t *testing.T) {
	ds := fixture(t, map[string]string{
		"main.go": `package main

import "github.com/DonaldMurillo/gofastr/battery/auth"

func wire() {
	mgr := auth.New(auth.AuthConfig{})
	_ = mgr
}
`,
		"routes.go": `package main

import . "github.com/DonaldMurillo/gofastr/battery/auth"

var guard = RequireAuth
`,
	})
	assertNot(t, ds, contracts.RuleAuthNotWired, "the dot-imported reader is a real mount")
}

// Exclusion has to be by NAME, not by declaration site: a use of a
// shadowed name is a different ident node from its declaration, so a
// pointer-keyed exclusion let the app's own `Opts{RequireAuth: true}`
// field key, or a use of a local `RequireAuth := 42`, count as a
// mount, silently disabling the rule for a manager nothing reads.
func TestAuthShadowedReaderNameIsNotAMount(t *testing.T) {
	const configure = `package main

import "github.com/DonaldMurillo/gofastr/battery/auth"

func wire() {
	mgr := auth.New(auth.AuthConfig{})
	_ = mgr
}
`
	ds := fixture(t, map[string]string{
		"main.go": configure,
		"svc.go": `package main

import . "github.com/DonaldMurillo/gofastr/battery/auth"

var _ = New

type Opts struct{ RequireAuth bool }

var o = Opts{RequireAuth: true}
`,
	})
	assertHas(t, ds, contracts.RuleAuthNotWired)

	ds = fixture(t, map[string]string{
		"main.go": configure,
		"svc.go": `package main

import . "github.com/DonaldMurillo/gofastr/battery/auth"

var _ = New

func local() {
	RequireAuth := 42
	println(RequireAuth)
}
`,
	})
	assertHas(t, ds, contracts.RuleAuthNotWired)
}

// The bare-identifier walk must count USES, not declarations: a method
// the app defines with the same name as a reader (legal Go: method
// names do not collide with file-block dot imports) is not a mount, and
// counting it silenced the rule for a manager nothing reads.
func TestAuthDeclarationNamedLikeAReaderIsNotAMount(t *testing.T) {
	ds := fixture(t, map[string]string{
		"main.go": `package main

import "github.com/DonaldMurillo/gofastr/battery/auth"

func wire() {
	mgr := auth.New(auth.AuthConfig{})
	_ = mgr
}
`,
		"svc.go": `package main

import . "github.com/DonaldMurillo/gofastr/battery/auth"

var _ = New

type Svc struct{}

func (s *Svc) RequireAuth() {}
`,
	})
	assertHas(t, ds, contracts.RuleAuthNotWired)
}

// The two legitimate cross-package shapes must stay covered: main
// configures and an imported package mounts, and the reverse, a setup
// package configures while the importing main mounts. Attribution is by
// import reachability, not by package identity.
func TestAuthCrossPackageWiringIsStillCovered(t *testing.T) {
	const mountsPkg = `package web

import "github.com/DonaldMurillo/gofastr/battery/auth"

var Reader = auth.RequireAuth
`
	ds := fixture(t, map[string]string{
		"main.go": `package main

import (
	"github.com/DonaldMurillo/gofastr/battery/auth"
	_ "example.com/app/web"
)

func wire() {
	mgr := auth.New(auth.AuthConfig{})
	_ = mgr
}
`,
		"web/web.go": mountsPkg,
	})
	assertNot(t, ds, contracts.RuleAuthNotWired, "the mount lives in a package main imports")

	ds = fixture(t, map[string]string{
		"main.go": `package main

import (
	"github.com/DonaldMurillo/gofastr/battery/auth"
	"example.com/app/setup"
)

func wire() {
	_ = setup.Manager
	_ = auth.RequireAuth
}
`,
		"setup/setup.go": `package setup

import "github.com/DonaldMurillo/gofastr/battery/auth"

var Manager = auth.New(auth.AuthConfig{})
`,
	})
	assertNot(t, ds, contracts.RuleAuthNotWired, "the importing main mounts the reader")
}
