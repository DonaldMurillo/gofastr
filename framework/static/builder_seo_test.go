package static

import (
	"bytes"
	"context"
	stdimage "image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	coreapp "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

// testIconPNGStatic returns PNG bytes of a solid 512×512 image.
func testIconPNGStatic(t *testing.T) []byte {
	t.Helper()
	img := stdimage.NewNRGBA(stdimage.Rect(0, 0, 512, 512))
	for y := 0; y < 512; y++ {
		for x := 0; x < 512; x++ {
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
		"<loc>https://example.com/about</loc>",
		"<loc>https://example.com/products/alpha</loc>",
		"<loc>https://example.com/products/beta</loc>",
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

func TestBuildSitemapRobotsBasePath(t *testing.T) {
	a := coreapp.NewApp("SSGTest")
	a.Register("/", &homeScreen{}, nil)
	a.Register("/about", &homeScreen{}, nil)
	host := uihost.New(a,
		uihost.WithSitemap(uihost.SitemapConfig{BaseURL: "https://user.github.io"}),
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
	if !strings.Contains(string(sm), "<loc>https://user.github.io/gofastr/about</loc>") {
		t.Errorf("sitemap locs must include the base path, got:\n%s", sm)
	}
	if strings.Contains(string(sm), "github.io/about") {
		t.Errorf("sitemap contains un-prefixed loc:\n%s", sm)
	}

	rb, err := os.ReadFile(filepath.Join(out, "robots.txt"))
	if err != nil {
		t.Fatalf("missing robots.txt: %v", err)
	}
	if !strings.Contains(string(rb), "Sitemap: https://user.github.io/gofastr/sitemap.xml") {
		t.Errorf("robots.txt Sitemap line must include base path, got:\n%s", rb)
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
	if _, err := (&Builder{Host: host, OutDir: out}).Build(context.Background()); err != nil {
		t.Fatalf("Build: %v", err)
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
