package static

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	coreapp "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

var indexRowLink = regexp.MustCompile(`^\| \[[^\]]*\]\(([^)]*)\)`)

// goneScreen answers 404, so the export writes neither page nor doc.
type goneScreen struct{}

func (*goneScreen) ScreenStatusCode() int { return 404 }
func (*goneScreen) Render() render.HTML   { return "<p>gone</p>" }

func TestExpandLiteralColonRoute(t *testing.T) {
	const route = "/a:b"
	ctx := context.Background()
	a := coreapp.NewApp("literal-colon")
	a.Register(route, &homeScreen{}, nil)
	if _, params, ok := a.Router.Resolve(route); !ok || len(params) != 0 {
		t.Fatalf("router resolved %q = %t with params %v, want a literal route", route, ok, params)
	}
	paths, err := expandRoute(ctx, a, route)
	if err != nil || !slices.Equal(paths, []string{route}) {
		t.Fatalf("expandRoute(%q) = %v, %v, want [%s]", route, paths, err, route)
	}
}

func TestBuildRefusesNonPortableRouteBeforeWriting(t *testing.T) {
	for _, tc := range []struct {
		name, route, slug, locale string
		redirect                  bool
	}{
		{name: "colon", route: "/a:b"},
		{name: "nested reserved name", route: "/posts/CoN.txt"},
		{name: "trailing dot", route: "/posts/ending."},
		{name: "reserved punctuation", route: "/has*star"},
		{name: "question mark", route: "/posts/:slug", slug: "a?b"},
		{name: "expanded path", route: "/posts/:slug", slug: "chapter/a:b"},
		{name: "redirect", route: "/old/AUX", redirect: true},
		{name: "locale prefix", route: "/", locale: "aux"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := coreapp.NewApp("portable-path")
			a.Register("/", &homeScreen{}, nil)
			switch {
			case tc.redirect:
				a.Redirect(tc.route, "/")
			case tc.slug != "":
				a.Register(tc.route, &keyedScreen{paths: []map[string]string{{"slug": tc.slug}}}, nil)
			case tc.route != "/":
				a.Register(tc.route, &homeScreen{}, nil)
			}
			host := uihost.New(a)
			if tc.locale != "" {
				host.SetPathLocales(i18n.PathLocales{Default: "en", Prefixed: []string{tc.locale}})
			}
			root := t.TempDir()
			out := filepath.Join(root, "site")
			_, err := (&Builder{Host: host, OutDir: out}).Build(context.Background())
			if err == nil || !strings.Contains(err.Error(), tc.route) || !strings.Contains(err.Error(), "portable filename") {
				t.Errorf("Build = %v, want a portable filename error naming %q", err, tc.route)
			}
			entries, err := os.ReadDir(root)
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 0 {
				t.Errorf("export touched the output before rejecting %q: %v", tc.route, entries)
			}
		})
	}
}

// The exported page index links docs the export wrote: a StaticPaths
// route linked /products/:slug/llm.md, a file no export writes, and
// never its exported pages. The Spanish tree's index did the same.
func TestExportIndexLinksWrittenDocs(t *testing.T) {
	a := coreapp.NewApp("t")
	a.Register("/", &homeScreen{}, nil)
	a.Register("/products/:slug", &productScreen{}, nil)
	a.Register("/gone", &goneScreen{}, nil) // answers 404: no page, no doc
	host := uihost.New(a)
	host.SetPathLocales(i18n.PathLocales{Default: "en", Prefixed: []string{"es"}})
	out := t.TempDir()
	if _, err := (&Builder{Host: host, OutDir: out}).Build(context.Background()); err != nil {
		t.Fatalf("Build: %v", err)
	}
	for _, index := range []string{"llm-pages.md", "es/llm-pages.md"} {
		md := readOut(t, out, index)
		pre := strings.TrimSuffix(strings.TrimSuffix(index, "llm-pages.md"), "/")
		if pre != "" {
			pre = "/" + pre
		}
		for _, want := range []string{pre + "/products/alpha/llm.md", pre + "/products/beta/llm.md"} {
			if !strings.Contains(md, "("+want+")") {
				t.Errorf("%s does not link %s:\n%s", index, want, md)
			}
		}
		for _, line := range strings.Split(md, "\n") {
			m := indexRowLink.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			if _, err := os.Stat(filepath.Join(out, filepath.FromSlash(strings.TrimPrefix(m[1], "/")))); err != nil {
				t.Errorf("%s links %s, which the export did not write", index, m[1])
			}
		}
	}
}

func TestExportIndexLinksUseBasePath(t *testing.T) {
	const base = "/repo"
	a := coreapp.NewApp("t")
	a.Register("/", &homeScreen{}, nil)
	a.Register("/about", &homeScreen{}, nil)
	a.Register("/products/:slug", &productScreen{}, nil)
	overlay := coreapp.NewScreen("/details", &homeScreen{})
	overlay.Type = coreapp.ScreenDrawer
	a.RegisterScreen(overlay, nil)
	host := uihost.New(a)
	host.SetPathLocales(i18n.PathLocales{Default: "en", Prefixed: []string{"es"}})
	out := t.TempDir()
	if _, err := (&Builder{Host: host, OutDir: out, BasePath: base}).Build(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, locale := range []string{"", "/es"} {
		index := strings.TrimPrefix(locale+"/llm-pages.md", "/")
		md := readOut(t, out, index)
		for _, path := range []string{"/llm.md", "/about/llm.md", "/products/alpha/llm.md", "/details/llm.md"} {
			rel := locale + path
			readOut(t, out, strings.TrimPrefix(rel, "/"))
			link := base + rel
			if !strings.Contains(md, "]("+link+")") {
				t.Errorf("%s does not link %s", index, link)
			}
		}
	}
}

type invalidDocStatus struct{ code int }

func (s invalidDocStatus) ScreenStatusCode() int { return s.code }
func (invalidDocStatus) Render() render.HTML     { return "<p>Exported page</p>" }

func TestExportIgnoresInvalidDocStatus(t *testing.T) {
	a := coreapp.NewApp("status")
	a.Register("/below", invalidDocStatus{code: 99}, nil)
	a.Register("/above", invalidDocStatus{code: 1000}, nil)
	out := t.TempDir()
	if _, err := (&Builder{Host: uihost.New(a), OutDir: out}).Build(context.Background()); err != nil {
		t.Fatal(err)
	}
	index := readOut(t, out, "llm-pages.md")
	for _, route := range []string{"below", "above"} {
		t.Run(route, func(t *testing.T) {
			readOut(t, out, route+"/index.html")
			doc := route + "/llm.md"
			if _, err := os.Stat(filepath.Join(out, filepath.FromSlash(doc))); err != nil {
				t.Errorf("exported HTML lacks its Markdown document %s: %v", doc, err)
			}
			if !strings.Contains(index, "](/"+doc+")") {
				t.Errorf("exported index does not link /%s", doc)
			}
		})
	}
}

type linkingScreen struct{}

func (linkingScreen) Render() render.HTML {
	return `<p><a href="/about">About</a> <a href="/repo/intro">Intro</a> <img src="/__gofastr/icons/icon.png" alt="logo"> <a href="//cdn.example/x">CDN</a></p>`
}

func TestExportDocBodyLinksUseBasePath(t *testing.T) {
	const base = "/repo"
	a := coreapp.NewApp("t")
	a.Register("/", linkingScreen{}, nil)
	a.Register("/about", &homeScreen{}, nil)
	a.Register("/repo/intro", &homeScreen{}, nil)
	host := uihost.New(a)
	host.SetPathLocales(i18n.PathLocales{Default: "en", Prefixed: []string{"es"}})
	out := t.TempDir()
	if _, err := (&Builder{Host: host, OutDir: out, BasePath: base}).Build(context.Background()); err != nil {
		t.Fatal(err)
	}
	for doc, wants := range map[string][]string{
		"llm.md":    {base + "/about", base + "/repo/intro", base + "/__gofastr/icons/icon.png", "//cdn.example/x"},
		"es/llm.md": {base + "/es/about", base + "/es/repo/intro", base + "/__gofastr/icons/icon.png", "//cdn.example/x"},
	} {
		md := readOut(t, out, doc)
		for _, want := range wants {
			if !strings.Contains(md, "]("+want+")") {
				t.Errorf("%s does not link %s:\n%s", doc, want, md)
			}
		}
	}
}
