package main

import (
	"maps"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// marketingLinksFixtureYAML is a marketing-layout blueprint that registers only
// the routes its screens declare: /, /pricing and (auth variant) /login. No
// /about, /terms or /privacy screen exists, so any chrome href to those routes
// points at a route the generated app never mounts (#312).
//
// meridian.yml cannot observe this bug: it is the one shipped blueprint that
// declares all four chrome routes.
func marketingLinksFixtureYAML(withAuth bool) string {
	var sb strings.Builder
	sb.WriteString(`
app:
  name: Chroma
  module: github.com/example/chroma
`)
	if withAuth {
		sb.WriteString(`  auth:
    enabled: true
    dev_mode: true
`)
	}
	sb.WriteString(`screens:
  - name: home
    route: /
    layout: marketing
    title: Chroma
    body:
      - kind: hero
        props:
          title: Ship it
  - name: pricing
    route: /pricing
    layout: marketing
    title: Pricing
    body:
      - kind: hero
        props:
          title: Pricing
`)
	if withAuth {
		sb.WriteString(`  - name: login
    route: /login
    layout: marketing
    title: Sign in
    body:
      - kind: login_form
`)
	}
	return sb.String()
}

// generatedFuncBody returns the source of func <name> in src, brace-matched to
// its closing brace. Empty when the function is absent.
func generatedFuncBody(src, name string) string {
	start := strings.Index(src, "func "+name+"(")
	if start < 0 {
		return ""
	}
	depth := 0
	for i := start; i < len(src); i++ {
		switch src[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return src[start : i+1]
			}
		}
	}
	return ""
}

// Nothing compared the marketing chrome's hrefs against the routes the same
// generation registers. A blueprint with one marketing screen and no /terms
// screen generated a footer linking a 404. Both header branches (plain and
// auth-aware) share the literals, so both are exercised.
func TestMarketingChromeLinksMatchRegisteredScreens(t *testing.T) {
	for _, tc := range []struct {
		name     string
		withAuth bool
	}{
		{name: "plain", withAuth: false},
		{name: "auth_header", withAuth: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "gofastr.yml")
			writeTestFile(t, path, marketingLinksFixtureYAML(tc.withAuth))
			bp, err := loadBlueprint(path)
			if err != nil {
				t.Fatalf("loadBlueprint: %v", err)
			}
			files := mustRenderBlueprintFiles(t, bp)

			// The route table: every screen mount the same generation emits.
			registered := map[string]bool{}
			registerStmt := regexp.MustCompile(`\bsite\.Register\("([^"]+)"`)
			registerScreenStmt := regexp.MustCompile(`\bsite\.RegisterScreen\(app\.NewScreen\("([^"]+)"`)
			for _, f := range files {
				for _, m := range registerStmt.FindAllStringSubmatch(f.content, -1) {
					registered[m[1]] = true
				}
				for _, m := range registerScreenStmt.FindAllStringSubmatch(f.content, -1) {
					registered[m[1]] = true
				}
			}
			for _, mustExist := range []string{"/", "/pricing"} {
				if !registered[mustExist] {
					t.Fatalf("fixture route %s not registered; registered=%v — test fixture is broken, not the generator", mustExist, slices.Sorted(maps.Keys(registered)))
				}
			}

			var appGo string
			for _, f := range files {
				if f.name == "app.go" {
					appGo = f.content
				}
			}
			if appGo == "" {
				t.Fatal("generation produced no app.go")
			}

			// Every href the marketing chrome emits, per surface...
			hrefRe := regexp.MustCompile(`Href: "([^"]+)"`)
			bodies := map[string]string{}
			var hrefs []string
			for _, fn := range []string{"marketingHeader", "marketingFooter"} {
				body := generatedFuncBody(appGo, fn)
				if body == "" {
					t.Fatalf("app.go has no %s function:\n%s", fn, appGo)
				}
				found := hrefRe.FindAllStringSubmatch(body, -1)
				if len(found) == 0 {
					t.Fatalf("%s emits no Href at all; the chrome assertions below would pass vacuously", fn)
				}
				bodies[fn] = body
				for _, m := range found {
					hrefs = append(hrefs, m[1])
				}
			}

			// ...must resolve to a route the same generation registers.
			for _, href := range hrefs {
				if !registered[href] {
					t.Errorf("marketing chrome links to %q, but no screen in the same generation registers that route (registered=%v)", href, slices.Sorted(maps.Keys(registered)))
				}
			}
			// The fixture registers no /about, /terms or /privacy screen, so
			// those links must not be emitted at all.
			for _, absent := range []string{"/about", "/terms", "/privacy"} {
				if slices.Contains(hrefs, absent) {
					t.Errorf("chrome still links %q though no screen registers it", absent)
				}
			}

			// The converse, asserted PER SURFACE: /pricing is registered, so
			// the header must carry its nav link and the footer its Product
			// column. Against the pooled hrefs this guard is blind — a
			// mutation that empties the header nav survives because the
			// footer still supplies /pricing, and one that drops the whole
			// link table passes the subset checks vacuously.
			if !strings.Contains(bodies["marketingHeader"], `Href: "/pricing"`) {
				t.Errorf("marketingHeader omits the registered /pricing nav link:\n%s", bodies["marketingHeader"])
			}
			if !strings.Contains(bodies["marketingFooter"], `Title: "Product"`) ||
				!strings.Contains(bodies["marketingFooter"], `{Label: "Pricing", Href: "/pricing"}`) {
				t.Errorf("marketingFooter omits the registered /pricing link's Product column:\n%s", bodies["marketingFooter"])
			}
		})
	}
}
