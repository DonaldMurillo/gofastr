package main

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework"
)

// Generated apps ship uihost strict mode on: full site-level SEO surface,
// per-screen SEO defaults that pass the checks honestly, and an axe gate
// whose scans feed the coverage manifest strict dev boots verify.

func TestBlueprintMainEmitsStrictDefaults(t *testing.T) {
	bp := Blueprint{App: BlueprintApp{Name: "Demo", Module: "example.com/demo"}}
	got := renderBlueprintMain(bp)
	assertContains(t, got, `uihost.WithStrict()`)
	assertContains(t, got, `uihost.WithSitemap(uihost.SitemapConfig{BaseURL: appBaseURL()`)
	assertContains(t, got, `uihost.WithDescription(`)
	assertContains(t, got, `func appBaseURL() string`)
	assertContains(t, got, `APP_BASE_URL`)
}

func TestBlueprintSiteDescriptionPrefersBlueprintValue(t *testing.T) {
	bp := Blueprint{App: BlueprintApp{
		Name: "Demo", Module: "example.com/demo",
		Description: "Freight quotes in seconds.",
	}}
	got := renderBlueprintMain(bp)
	assertContains(t, got, `uihost.WithDescription("Freight quotes in seconds.")`)
}

func TestBlueprintSiteDescriptionDerivesFromEntities(t *testing.T) {
	bp := Blueprint{
		App: BlueprintApp{Name: "Demo", Module: "example.com/demo"},
		Entities: []framework.EntityDeclaration{
			{Name: "product"}, {Name: "order"},
		},
	}
	got := renderBlueprintMain(bp)
	if !strings.Contains(got, `uihost.WithDescription("Demo — manage products and orders.")`) {
		t.Fatalf("derived description missing; main.go was:\n%s", sectionAround(got, "WithDescription"))
	}
}

func TestBlueprintSitemapAndRobotsExcludeAdmin(t *testing.T) {
	bp := Blueprint{App: BlueprintApp{
		Name: "Demo", Module: "example.com/demo",
		Admin: BlueprintAdmin{Enabled: true},
	}}
	got := renderBlueprintMain(bp)
	assertContains(t, got, `ExcludePaths: []string{"/admin"}`)
	assertContains(t, got, `Disallow: []string{"/__gofastr/", "/admin"}`)
}

func TestBlueprintScreenWithoutDescriptionOptsOutViaScreenSEO(t *testing.T) {
	bp := Blueprint{
		App: BlueprintApp{Name: "Demo", Module: "example.com/demo"},
		Screens: []BlueprintScreen{
			{Name: "about", Route: "/about", Title: "About"},
		},
	}
	got := renderBlueprintScreens(bp)
	assertContains(t, got, `func (s *AboutScreen) ScreenSEO() uihost.SEO { return uihost.SEO{} }`)
	assertContains(t, got, `"github.com/DonaldMurillo/gofastr/framework/uihost"`)
	if strings.Contains(got, `func (s *AboutScreen) ScreenDescription()`) {
		t.Fatal("description-less screen must opt out, not declare an empty description")
	}
}

func TestBlueprintScreenWithDescriptionKeepsDescriber(t *testing.T) {
	bp := Blueprint{
		App: BlueprintApp{Name: "Demo", Module: "example.com/demo"},
		Screens: []BlueprintScreen{
			{Name: "about", Route: "/about", Title: "About", Description: "Who we are."},
		},
	}
	got := renderBlueprintScreens(bp)
	assertContains(t, got, `func (s *AboutScreen) ScreenDescription() string { return "Who we are." }`)
	if strings.Contains(got, `func (s *AboutScreen) ScreenSEO()`) {
		t.Fatal("described screen must not also emit the opt-out")
	}
}

func TestBlueprintUntitledScreenFallsBackToName(t *testing.T) {
	bp := Blueprint{
		App: BlueprintApp{Name: "Demo", Module: "example.com/demo"},
		Screens: []BlueprintScreen{
			{Name: "about", Route: "/about"},
		},
	}
	got := renderBlueprintScreens(bp)
	if strings.Contains(got, `ScreenTitle() string { return "" }`) {
		t.Fatal("untitled screen must fall back to its name, not an empty title")
	}
}

func TestBlueprintEmitsAxeTest(t *testing.T) {
	bp := Blueprint{
		App: BlueprintApp{Name: "Demo", Module: "example.com/demo"},
		Screens: []BlueprintScreen{
			{Name: "home", Route: "/", Title: "Home"},
		},
	}
	got := renderBlueprintAxeTest(bp)
	assertContains(t, got, `func TestAxeEveryScreen(`)
	assertContains(t, got, `framework/testkit/axetest`)
	assertContains(t, got, `/sitemap.xml`)
	assertContains(t, got, `allow-skip`)
	assertContains(t, got, `axetest.Schemes`)
	// The redirect assertion keeps a bounced navigation from silently
	// covering the wrong screen.
	assertContains(t, got, `redirected to`)
	// Owned seam for concrete dynamic-route URLs.
	assertContains(t, got, `var axeExtraPages []string`)
}

func TestBlueprintAxeTestScansGatedWithSeparateAuthedBrowser(t *testing.T) {
	bp := Blueprint{
		App: BlueprintApp{
			Name: "Demo", Module: "example.com/demo",
			Auth:  BlueprintAuth{Enabled: true},
			Admin: BlueprintAdmin{Enabled: true, SeedEmail: "a@x.io", SeedPassword: "pw123456"}, // not-a-secret: test fixture exercising the seeded-admin template branch
		},
		Screens: []BlueprintScreen{
			{Name: "home", Route: "/", Title: "Home"},
			{Name: "board", Route: "/board", Title: "Board", Access: BlueprintAccess{Auth: true}},
		},
	}
	got := renderBlueprintAxeTest(bp)
	assertContains(t, got, `"/board"`)                     // baked gated list
	assertContains(t, got, `authed := axetest.NewBrowser`) // separate context
	assertContains(t, got, `func axeLogin(`)
	if !strings.Contains(got, "gated[page]") {
		t.Fatal("anonymous pass must skip gated pages")
	}
}

func TestBlueprintAxeTestFailsLoudlyOnGatedWithoutCreds(t *testing.T) {
	bp := Blueprint{
		App: BlueprintApp{
			Name: "Demo", Module: "example.com/demo",
			Auth: BlueprintAuth{Enabled: true},
		},
		Screens: []BlueprintScreen{
			{Name: "home", Route: "/", Title: "Home"},
			{Name: "board", Route: "/board", Title: "Board", Access: BlueprintAccess{Auth: true}},
		},
	}
	got := renderBlueprintAxeTest(bp)
	assertContains(t, got, `cannot be scanned: no seeded admin`)
	if strings.Contains(got, "func axeLogin(") {
		t.Fatal("no-creds template must not emit the login helper")
	}
}

func TestBlueprintMainTrimsAdminPathTrailingSlash(t *testing.T) {
	bp := Blueprint{App: BlueprintApp{
		Name: "Demo", Module: "example.com/demo",
		Admin: BlueprintAdmin{Enabled: true, Path: "/admin/"},
	}}
	got := renderBlueprintMain(bp)
	assertContains(t, got, `ExcludePaths: []string{"/admin"}`)
	assertContains(t, got, `Disallow: []string{"/__gofastr/", "/admin"}`)
}

func TestBlueprintGitignoreCoversManifestDir(t *testing.T) {
	if !strings.Contains(blueprintGitignore, ".gofastr/") {
		t.Fatal("generated .gitignore must ignore .gofastr/ (axe-coverage manifest and friends)")
	}
}

// sectionAround trims a large generated source to the lines mentioning
// needle, for readable failures.
func sectionAround(src, needle string) string {
	var out []string
	for _, line := range strings.Split(src, "\n") {
		if strings.Contains(line, needle) {
			out = append(out, line)
		}
	}
	if len(out) == 0 {
		return "(no line mentions " + needle + ")"
	}
	return strings.Join(out, "\n")
}
