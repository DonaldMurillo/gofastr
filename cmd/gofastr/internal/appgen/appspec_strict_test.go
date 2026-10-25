package appgen

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
	"github.com/DonaldMurillo/gofastr/framework"
)

// Generated apps ship uihost strict mode on: full site-level SEO surface,
// per-screen SEO defaults that pass the checks honestly, and an axe gate
// whose scans feed the coverage manifest strict dev boots verify.

func TestAppSpecMainEmitsStrictDefaults(t *testing.T) {
	bp := appspec.AppSpec{App: appspec.AppSpecApp{Name: "Demo", Module: "example.com/demo"}}
	got := RenderAppSpecMain(bp)
	clitest.AssertContains(t, got, `uihost.WithStrict()`)
	clitest.AssertContains(t, got, `uihost.WithSitemap(uihost.SitemapConfig{BaseURL: appBaseURL()`)
	clitest.AssertContains(t, got, `uihost.WithDescription(`)
	clitest.AssertContains(t, got, `func appBaseURL() string`)
	clitest.AssertContains(t, got, `APP_BASE_URL`)
}

func TestAppSpecSiteDescriptionPrefersAppSpecValue(t *testing.T) {
	bp := appspec.AppSpec{App: appspec.AppSpecApp{
		Name: "Demo", Module: "example.com/demo",
		Description: "Freight quotes in seconds.",
	}}
	got := RenderAppSpecMain(bp)
	clitest.AssertContains(t, got, `uihost.WithDescription("Freight quotes in seconds.")`)
}

func TestAppSpecSiteDescriptionDerivesFromEntities(t *testing.T) {
	bp := appspec.AppSpec{
		App: appspec.AppSpecApp{Name: "Demo", Module: "example.com/demo"},
		Entities: []framework.EntityDeclaration{
			{Name: "product"}, {Name: "order"},
		},
	}
	got := RenderAppSpecMain(bp)
	if !strings.Contains(got, `uihost.WithDescription("Demo: manage products and orders.")`) {
		t.Fatalf("derived description missing; main.go was:\n%s", sectionAround(got, "WithDescription"))
	}
}

func TestAppSpecSitemapAndRobotsExcludeAdmin(t *testing.T) {
	bp := appspec.AppSpec{App: appspec.AppSpecApp{
		Name: "Demo", Module: "example.com/demo",
		Admin: appspec.AppSpecAdmin{Enabled: true},
	}}
	got := RenderAppSpecMain(bp)
	clitest.AssertContains(t, got, `ExcludePaths: []string{"/admin"}`)
	clitest.AssertContains(t, got, `Disallow: []string{"/__gofastr/", "/admin"}`)
}

func TestAppSpecScreenWithoutDescriptionOptsOutViaScreenSEO(t *testing.T) {
	bp := appspec.AppSpec{
		App: appspec.AppSpecApp{Name: "Demo", Module: "example.com/demo"},
		Screens: []appspec.AppSpecScreen{
			{Name: "about", Route: "/about", Title: "About"},
		},
	}
	got := RenderAppSpecScreens(bp)
	clitest.AssertContains(t, got, `func (s *AboutScreen) ScreenSEO(ctx context.Context) uihost.SEO { return uihost.SEO{} }`)
	clitest.AssertContains(t, got, `"github.com/DonaldMurillo/gofastr/framework/uihost"`)
	if strings.Contains(got, `func (s *AboutScreen) ScreenDescription()`) {
		t.Fatal("description-less screen must opt out, not declare an empty description")
	}
}

func TestAppSpecUntitledScreenFallsBackToName(t *testing.T) {
	bp := appspec.AppSpec{
		App: appspec.AppSpecApp{Name: "Demo", Module: "example.com/demo"},
		Screens: []appspec.AppSpecScreen{
			{Name: "about", Route: "/about"},
		},
	}
	got := RenderAppSpecScreens(bp)
	if strings.Contains(got, `ScreenTitle(ctx context.Context) string { return "" }`) {
		t.Fatal("untitled screen must fall back to its name, not an empty title")
	}
}

func TestAppSpecEmitsAxeTest(t *testing.T) {
	bp := appspec.AppSpec{
		App: appspec.AppSpecApp{Name: "Demo", Module: "example.com/demo"},
		Screens: []appspec.AppSpecScreen{
			{Name: "home", Route: "/", Title: "Home"},
		},
	}
	got := renderAppSpecAxeTest(bp)
	clitest.AssertContains(t, got, `func TestAxeEveryScreen(`)
	clitest.AssertContains(t, got, `framework/testkit/axetest`)
	clitest.AssertContains(t, got, `/sitemap.xml`)
	clitest.AssertContains(t, got, `allow-skip`)
	clitest.AssertContains(t, got, `axetest.Schemes`)
	// The redirect assertion keeps a bounced navigation from silently
	// covering the wrong screen.
	clitest.AssertContains(t, got, `redirected to`)
	// Owned seam for concrete dynamic-route URLs.
	clitest.AssertContains(t, got, `var axeExtraPages []string`)
}

func TestAppSpecAxeTestScansGatedWithSeparateAuthedBrowser(t *testing.T) {
	bp := appspec.AppSpec{
		App: appspec.AppSpecApp{
			Name: "Demo", Module: "example.com/demo",
			Auth:  appspec.AppSpecAuth{Enabled: true},
			Admin: appspec.AppSpecAdmin{Enabled: true, SeedEmail: "a@x.io", SeedPassword: "pw123456"}, // not-a-secret: test fixture exercising the seeded-admin template branch
		},
		Screens: []appspec.AppSpecScreen{
			{Name: "home", Route: "/", Title: "Home"},
			{Name: "board", Route: "/board", Title: "Board", Access: appspec.AppSpecAccess{Auth: true}},
		},
	}
	got := renderAppSpecAxeTest(bp)
	clitest.AssertContains(t, got, `"/board"`)                     // baked gated list
	clitest.AssertContains(t, got, `authed := axetest.NewBrowser`) // separate context
	clitest.AssertContains(t, got, `func axeLogin(`)
	if !strings.Contains(got, "gated[page]") {
		t.Fatal("anonymous pass must skip gated pages")
	}
}

func TestAppSpecAxeTestFailsLoudlyOnGatedWithoutCreds(t *testing.T) {
	bp := appspec.AppSpec{
		App: appspec.AppSpecApp{
			Name: "Demo", Module: "example.com/demo",
			Auth: appspec.AppSpecAuth{Enabled: true},
		},
		Screens: []appspec.AppSpecScreen{
			{Name: "home", Route: "/", Title: "Home"},
			{Name: "board", Route: "/board", Title: "Board", Access: appspec.AppSpecAccess{Auth: true}},
		},
	}
	got := renderAppSpecAxeTest(bp)
	clitest.AssertContains(t, got, `cannot be scanned: no seeded admin`)
	if strings.Contains(got, "func axeLogin(") {
		t.Fatal("no-creds template must not emit the login helper")
	}
}

func TestAppSpecMainTrimsAdminPathTrailingSlash(t *testing.T) {
	bp := appspec.AppSpec{App: appspec.AppSpecApp{
		Name: "Demo", Module: "example.com/demo",
		Admin: appspec.AppSpecAdmin{Enabled: true, Path: "/admin/"},
	}}
	got := RenderAppSpecMain(bp)
	clitest.AssertContains(t, got, `ExcludePaths: []string{"/admin"}`)
	clitest.AssertContains(t, got, `Disallow: []string{"/__gofastr/", "/admin"}`)
}

func TestAppSpecGitignoreCoversManifestDir(t *testing.T) {
	if !strings.Contains(appSpecGitignore, ".gofastr/") {
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
