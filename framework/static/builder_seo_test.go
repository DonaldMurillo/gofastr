package static

import (
	"bytes"
	"context"
	stdimage "image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	coreapp "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

// testIconPNGStatic returns PNG bytes of a solid 512×512 image.
func testIconPNGStatic(t *testing.T) []byte {
	t.Helper()
	img := stdimage.NewNRGBA(stdimage.Rect(0, 0, 512, 512))
	for y := range 512 {
		for x := range 512 {
			img.Set(x, y, color.NRGBA{R: 30, G: 90, B: 200, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode test icon: %v", err)
	}
	return buf.Bytes()
}

func TestBuildWritesSitemapAndRobots(t *testing.T) {
	a := coreapp.NewApp("SSGTest")
	a.Register("/", &homeScreen{}, nil)
	a.Register("/about", &homeScreen{}, nil)
	a.Register("/products/:slug", &productScreen{}, nil)
	a.Register("/admin", &homeScreen{}, nil)
	host := uihost.New(a,
		uihost.WithSitemap(uihost.SitemapConfig{
			BaseURL:      "https://example.com",
			ExcludePaths: []string{"/admin"},
		}),
		uihost.WithRobots(uihost.RobotsConfig{Disallow: []string{"/admin"}}),
	)

	out := t.TempDir()
	res, err := (&Builder{Host: host, OutDir: out}).Build(context.Background())
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	sm, err := os.ReadFile(filepath.Join(out, "sitemap.xml"))
	if err != nil {
		t.Fatalf("missing sitemap.xml: %v", err)
	}
	body := string(sm)
	for _, loc := range []string{
		"<loc>https://example.com/</loc>",
		"<loc>https://example.com/about/</loc>",
		"<loc>https://example.com/products/alpha/</loc>",
		"<loc>https://example.com/products/beta/</loc>",
	} {
		if !strings.Contains(body, loc) {
			t.Errorf("sitemap.xml missing %s, got:\n%s", loc, body)
		}
	}
	if strings.Contains(body, "/admin") {
		t.Errorf("sitemap.xml must honor ExcludePaths, got:\n%s", body)
	}

	rb, err := os.ReadFile(filepath.Join(out, "robots.txt"))
	if err != nil {
		t.Fatalf("missing robots.txt: %v", err)
	}
	rbody := string(rb)
	if !strings.Contains(rbody, "Disallow: /admin") {
		t.Errorf("robots.txt missing disallow rule, got:\n%s", rbody)
	}
	if !strings.Contains(rbody, "Sitemap: https://example.com/sitemap.xml") {
		t.Errorf("robots.txt missing derived Sitemap line, got:\n%s", rbody)
	}

	for _, want := range []string{"/sitemap.xml", "/robots.txt"} {
		found := false
		for _, a := range res.Assets {
			if a == want {
				found = true
			}
		}
		if !found {
			t.Errorf("res.Assets missing %s: %v", want, res.Assets)
		}
	}
}

// TestBuildSitemapRobotsBasePath: the export's base path appears once
// in every sitemap URL, whether the configured BaseURL is the bare
// origin or already names the mount (the docs site's
// "https://donaldmurillo.github.io/gofastr" once listed
// /gofastr/gofastr/... URLs that 404). Page URLs carry the slash the
// export's links do, the form the host serves without a redirect.
func TestBuildSitemapRobotsBasePath(t *testing.T) {
	for _, c := range []struct{ baseURL, site string }{
		{"https://user.github.io", "https://user.github.io/gofastr"},
		{"https://user.github.io/gofastr", "https://user.github.io/gofastr"},
		{"https://user.github.io/gofastr/", "https://user.github.io/gofastr"},
		{"https://user.github.io/mygofastr", "https://user.github.io/mygofastr/gofastr"},
	} {
		t.Run(c.baseURL, func(t *testing.T) {
			a := coreapp.NewApp("SSGTest")
			a.Register("/", &homeScreen{}, nil)
			a.Register("/about", &homeScreen{}, nil)
			host := uihost.New(a,
				uihost.WithSitemap(uihost.SitemapConfig{BaseURL: c.baseURL}),
				uihost.WithRobots(uihost.RobotsConfig{}),
			)

			out := t.TempDir()
			if _, err := (&Builder{Host: host, OutDir: out, BasePath: "/gofastr"}).Build(context.Background()); err != nil {
				t.Fatalf("Build: %v", err)
			}

			sm, err := os.ReadFile(filepath.Join(out, "sitemap.xml"))
			if err != nil {
				t.Fatalf("missing sitemap.xml: %v", err)
			}
			for _, loc := range []string{c.site + "/", c.site + "/about/"} {
				if !strings.Contains(string(sm), "<loc>"+loc+"</loc>") {
					t.Errorf("sitemap missing <loc>%s</loc>, got:\n%s", loc, sm)
				}
			}
			if n := strings.Count(string(sm), "<loc>"); n != 2 {
				t.Errorf("sitemap lists %d URLs, want 2:\n%s", n, sm)
			}

			rb, err := os.ReadFile(filepath.Join(out, "robots.txt"))
			if err != nil {
				t.Fatalf("missing robots.txt: %v", err)
			}
			if !strings.Contains(string(rb), "Sitemap: "+c.site+"/sitemap.xml\n") {
				t.Errorf("robots.txt Sitemap line must be %s/sitemap.xml, got:\n%s", c.site, rb)
			}
		})
	}
}

func TestBuildSkipsSitemapRobotsWhenNotConfigured(t *testing.T) {
	a := coreapp.NewApp("SSGTest")
	a.Register("/", &homeScreen{}, nil)
	host := uihost.New(a)

	out := t.TempDir()
	if _, err := (&Builder{Host: host, OutDir: out}).Build(context.Background()); err != nil {
		t.Fatalf("Build: %v", err)
	}
	for _, f := range []string{"sitemap.xml", "robots.txt"} {
		if _, err := os.Stat(filepath.Join(out, f)); err == nil {
			t.Errorf("%s must not be written when not configured", f)
		}
	}
}

func TestBuildKeepsUserSuppliedRobotsAndSitemap(t *testing.T) {
	staticDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(staticDir, "robots.txt"), []byte("User-agent: *\nDisallow: /mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(staticDir, "sitemap.xml"), []byte("<urlset>mine</urlset>"), 0o644); err != nil {
		t.Fatal(err)
	}

	a := coreapp.NewApp("SSGTest")
	a.Register("/", &homeScreen{}, nil)
	host := uihost.New(a,
		uihost.WithStaticDir(staticDir),
		uihost.WithSitemap(uihost.SitemapConfig{BaseURL: "https://example.com"}),
		uihost.WithRobots(uihost.RobotsConfig{}),
	)

	out := t.TempDir()
	res, err := (&Builder{Host: host, OutDir: out}).Build(context.Background())
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	// The generated file is not written under the user's either.
	for _, p := range []string{"/robots.txt", "/sitemap.xml"} {
		if n := slices.Index(res.Assets, p); n < 0 || slices.Contains(res.Assets[n+1:], p) {
			t.Errorf("assets list %s other than once: %v", p, res.Assets)
		}
	}

	rb, _ := os.ReadFile(filepath.Join(out, "robots.txt"))
	if !strings.Contains(string(rb), "Disallow: /mine") {
		t.Errorf("user-supplied robots.txt must win, got:\n%s", rb)
	}
	sm, _ := os.ReadFile(filepath.Join(out, "sitemap.xml"))
	if string(sm) != "<urlset>mine</urlset>" {
		t.Errorf("user-supplied sitemap.xml must win, got:\n%s", sm)
	}
}

func TestBuildDumpsAppIcons(t *testing.T) {
	a := coreapp.NewApp("SSGTest")
	a.Register("/", &homeScreen{}, nil)
	host := uihost.New(a, uihost.WithAppIcon(testIconPNGStatic(t)))

	out := t.TempDir()
	res, err := (&Builder{Host: host, OutDir: out}).Build(context.Background())
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	for _, rel := range []string{
		"__gofastr/icons/icon-32.png",
		"__gofastr/icons/icon-180.png",
		"__gofastr/icons/icon-192.png",
		"__gofastr/icons/icon-512.png",
		"favicon.ico",
	} {
		if _, err := os.Stat(filepath.Join(out, rel)); err != nil {
			t.Errorf("missing exported icon %s: %v", rel, err)
		}
	}
	found := false
	for _, a := range res.Assets {
		if a == "/__gofastr/icons/icon-192.png" {
			found = true
		}
	}
	if !found {
		t.Errorf("res.Assets missing icon entries: %v", res.Assets)
	}
}

// TestSitemapListsOnlyExportedPages: a page the export skipped (an
// excluded route, one that answers 410) has no file on the static
// host, so the sitemap must not send a crawler to it. A skipped page
// whose path ends in an exported one's (/docs/about, /about) is told
// apart wherever the site URL is mounted.
func TestSitemapListsOnlyExportedPages(t *testing.T) {
	for _, c := range []struct{ baseURL, basePath, site string }{
		{"https://example.com", "", "https://example.com"},
		{"https://example.com", "/sub", "https://example.com/sub"},
		{"https://example.com/sub", "/sub", "https://example.com/sub"},
		{"https://example.com/docs", "", "https://example.com/docs"},
	} {
		t.Run(c.baseURL+c.basePath, func(t *testing.T) {
			a := coreapp.NewApp("SitemapSkips")
			a.Register("/", &homeScreen{}, nil)
			a.Register("/about", &homeScreen{}, nil)
			a.Register("/docs/about", &homeScreen{}, nil)
			a.Register("/broken", &homeScreen{}, nil)
			a.RegisterScreen(coreapp.NewScreen("/gone", nfDeclaredGone{}), nil)
			host := uihost.New(a, uihost.WithSitemap(uihost.SitemapConfig{BaseURL: c.baseURL}))
			out := t.TempDir()
			b := &Builder{Host: host, OutDir: out, BasePath: c.basePath, ExcludeRoutes: []string{"/broken", "/docs"}}
			if _, err := b.Build(context.Background()); err != nil {
				t.Fatalf("Build: %v", err)
			}
			sm := readOut(t, out, "sitemap.xml")
			for _, want := range []string{"<loc>" + c.site + "/</loc>", "<loc>" + c.site + "/about/</loc>"} {
				if !strings.Contains(sm, want) {
					t.Errorf("sitemap lacks %s:\n%s", want, sm)
				}
			}
			for _, gone := range []string{"/broken", "/gone", "/docs/about"} {
				if strings.Contains(sm, c.site+gone) {
					t.Errorf("sitemap lists %s, which the export skipped:\n%s", gone, sm)
				}
			}
			if n := strings.Count(sm, "<url>"); n != 2 {
				t.Errorf("sitemap has %d entries, want 2:\n%s", n, sm)
			}
		})
	}
}

// TestSitemapLocalesListOnlyExported: an app that routes by locale
// prefix lists one entry per locale; a skipped page drops from every
// locale's list.
func TestSitemapLocalesListOnlyExported(t *testing.T) {
	a := coreapp.NewApp("SitemapLocales")
	a.Register("/", &helloScreen{}, nil)
	a.Register("/about", &helloScreen{}, nil)
	a.Register("/broken", &helloScreen{}, nil)
	host := uihost.New(a, uihost.WithSitemap(uihost.SitemapConfig{BaseURL: "https://example.com"}))
	cat := i18n.NewMapCatalog()
	cat.Set("es", exportHello.Key(), i18n.Message{Text: "Hola"})
	tr := i18n.NewTranslator(cat, "en")
	set, err := i18n.NewPathLocales(tr)
	if err != nil {
		t.Fatal(err)
	}
	host.SetPathLocales(set)
	out := t.TempDir()
	b := &Builder{Host: host, OutDir: out, ExcludeRoutes: []string{"/broken"}}
	if _, err := b.Build(i18n.WithTranslator(context.Background(), tr)); err != nil {
		t.Fatalf("Build: %v", err)
	}
	sm := readOut(t, out, "sitemap.xml")
	for _, want := range []string{"<loc>https://example.com/es/about/</loc>", "<loc>https://example.com/about/</loc>"} {
		if !strings.Contains(sm, want) {
			t.Errorf("sitemap lacks %s:\n%s", want, sm)
		}
	}
	if strings.Contains(sm, "broken") {
		t.Errorf("sitemap lists the excluded /broken:\n%s", sm)
	}
}

// flipScreen's StaticPaths answers in the opposite order on each call,
// as a map's keys may.
type flipScreen struct {
	productScreen
	calls *int
}

func (f *flipScreen) StaticPaths(context.Context) []map[string]string {
	*f.calls++
	if *f.calls%2 == 0 {
		return []map[string]string{{"slug": "beta"}, {"slug": "alpha"}}
	}
	return []map[string]string{{"slug": "alpha"}, {"slug": "beta"}}
}

// TestSitemapKeysEntriesByPage: StaticPaths that change order between
// calls must not hand one page's entry another page's verdict. A
// redirected /products/beta used to keep its entry while the exported
// /products/alpha lost its own.
func TestSitemapKeysEntriesByPage(t *testing.T) {
	a := coreapp.NewApp("SitemapFlip")
	a.Register("/", &homeScreen{}, nil)
	calls := 0
	a.Register("/products/:slug", &flipScreen{calls: &calls}, nil)
	a.Redirect("/products/beta", "/")
	host := uihost.New(a, uihost.WithSitemap(uihost.SitemapConfig{BaseURL: "https://example.com"}))
	out := t.TempDir()
	if _, err := (&Builder{Host: host, OutDir: out}).Build(context.Background()); err != nil {
		t.Fatalf("Build: %v", err)
	}
	sm := readOut(t, out, "sitemap.xml")
	if !strings.Contains(sm, "https://example.com/products/alpha") {
		t.Errorf("sitemap dropped the exported /products/alpha:\n%s", sm)
	}
	if strings.Contains(sm, "https://example.com/products/beta") {
		t.Errorf("sitemap lists the redirected /products/beta:\n%s", sm)
	}
}

// growScreen lists a second page on every even call, the way a row
// inserted between two reads would.
type growScreen struct {
	productScreen
	calls *int
}

func (g *growScreen) StaticPaths(context.Context) []map[string]string {
	*g.calls++
	if *g.calls%2 == 0 {
		return []map[string]string{{"slug": "alpha"}, {"slug": "gamma"}}
	}
	return []map[string]string{{"slug": "alpha"}}
}

// When the sitemap's renders disagreed, the export logged a warning and
// kept the whole document: the ExcludeRoutes /admin page and a /gamma
// the export never wrote were both listed. Only written pages are now.
func TestSitemapDropsUnwrittenPages(t *testing.T) {
	a := coreapp.NewApp("SitemapGrow")
	a.Register("/", &homeScreen{}, nil)
	a.Register("/admin", &homeScreen{}, nil)
	calls := 0
	a.Register("/products/:slug", &growScreen{calls: &calls}, nil)
	host := uihost.New(a, uihost.WithSitemap(uihost.SitemapConfig{BaseURL: "https://example.com"}))
	out := t.TempDir()
	if _, err := (&Builder{Host: host, OutDir: out, ExcludeRoutes: []string{"/admin"}}).Build(context.Background()); err != nil {
		t.Fatalf("Build: %v", err)
	}
	sm := readOut(t, out, "sitemap.xml")
	if !strings.Contains(sm, "https://example.com/products/alpha") {
		t.Errorf("sitemap dropped the exported /products/alpha:\n%s", sm)
	}
	for _, gone := range []string{"/admin", "/products/gamma"} {
		if strings.Contains(sm, "https://example.com"+gone) {
			t.Errorf("sitemap lists %s, which the export never wrote:\n%s", gone, sm)
		}
	}
}
