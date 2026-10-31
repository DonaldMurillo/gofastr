package uihost

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/core/router"
)

// manyItems lists n item pages, and counts how often the sitemap asks.
type manyItems struct {
	n     int
	calls int
}

func (*manyItems) SetParams(map[string]string) {}
func (*manyItems) Render() render.HTML         { return "<p>item</p>" }
func (m *manyItems) StaticPaths(context.Context) []map[string]string {
	m.calls++
	out := make([]map[string]string, m.n)
	for i := range out {
		out[i] = map[string]string{"id": strconv.Itoa(i)}
	}
	return out
}

func sitemapServer(t *testing.T, items *manyItems) http.Handler {
	t.Helper()
	a := app.NewApp("x")
	a.Register("/items/:id", items, nil)
	ds := New(a, WithSitemap(SitemapConfig{BaseURL: "https://x.test"}))
	r := router.New()
	ds.Mount(r)
	return r
}

func getSitemap(t *testing.T, h http.Handler, path string) (int, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec.Code, rec.Body.String()
}

// One urlset may hold 50,000 URLs: past that a search engine rejects the
// whole file, so /sitemap.xml becomes an index naming its parts.
func TestSitemapSplitsPastURLCap(t *testing.T) {
	h := sitemapServer(t, &manyItems{n: sitemapMaxURLs + 1})
	code, index := getSitemap(t, h, "/sitemap.xml")
	if code != http.StatusOK || !strings.Contains(index, "<sitemapindex") || strings.Contains(index, "<urlset") {
		t.Fatalf("/sitemap.xml = %d, want a sitemap index:\n%.400s", code, index)
	}
	for _, want := range []string{"<loc>https://x.test/sitemap/1.xml</loc>", "<loc>https://x.test/sitemap/2.xml</loc>"} {
		if !strings.Contains(index, want) {
			t.Errorf("index lacks %s:\n%s", want, index)
		}
	}
	if strings.Contains(index, "/sitemap/3.xml") {
		t.Errorf("index names a third part:\n%s", index)
	}
	total := 0
	for i, want := range []int{sitemapMaxURLs, 1} {
		code, part := getSitemap(t, h, "/sitemap/"+strconv.Itoa(i+1)+".xml")
		if code != http.StatusOK || !strings.HasPrefix(part, `<?xml`) || !strings.Contains(part, "<urlset") {
			t.Fatalf("part %d = %d:\n%.400s", i+1, code, part)
		}
		if got := strings.Count(part, "<url>"); got != want {
			t.Errorf("part %d holds %d URLs, want %d", i+1, got, want)
		}
		total += strings.Count(part, "<url>")
	}
	if total != sitemapMaxURLs+1 {
		t.Errorf("parts hold %d URLs, want %d", total, sitemapMaxURLs+1)
	}
	if code, _ := getSitemap(t, h, "/sitemap/3.xml"); code != http.StatusNotFound {
		t.Errorf("/sitemap/3.xml = %d, want 404", code)
	}
}

// The byte cap splits too: 50 MiB uncompressed per file.
func TestSitemapSplitsPastByteCap(t *testing.T) {
	entry := func(name string) string { return name + strings.Repeat("_", 40-len(name)) }
	entries := []string{entry("ENTRY-A"), entry("ENTRY-B"), entry("ENTRY-C"), entry("ENTRY-D")}
	open := "<urlset>\n"
	// Room for the head, the open and close tags, and two entries.
	limit := len(sitemapXMLHead) + len(open) + len(sitemapURLSetClose) + 80
	files := splitSitemap("https://x.test", "2026-10-30", open, entries, 10, limit)
	if len(files) != 3 {
		t.Fatalf("got %d files, want an index and two parts", len(files))
	}
	for _, f := range files[1:] {
		if len(f.Body) > limit {
			t.Errorf("%s is %d bytes, over the %d cap", f.Path, len(f.Body), limit)
		}
	}
	if !strings.Contains(files[1].Body, "ENTRY-B") || !strings.Contains(files[2].Body, "ENTRY-D") || strings.Contains(files[1].Body, "ENTRY-C") {
		t.Errorf("parts split at the wrong entry:\n%s\n%s", files[1].Body, files[2].Body)
	}
	// Exactly at the cap still fits in one file.
	one := splitSitemap("https://x.test", "2026-10-30", open, entries[:2], 10, limit)
	if len(one) != 1 || one[0].Path != "/sitemap.xml" {
		t.Errorf("two entries at the cap split into %d files", len(one))
	}
	// An entry larger than the cap alone stays one file, with no empty
	// part in front of it.
	if big := splitSitemap("https://x.test", "2026-10-30", open, entries[:1], 10, limit-80+10); len(big) != 1 {
		t.Errorf("one oversize entry split into %d files", len(big))
	}
}

// keep is asked about each locale's page by its prefixed routing path:
// the export skipped /es/about, so its entry and every alternate naming
// it go, while /about stays.
func TestSitemapFilesKeepPerLocale(t *testing.T) {
	a := app.NewApp("x")
	a.Register("/about", &manyItems{}, nil)
	ds := New(a, WithSitemap(SitemapConfig{BaseURL: "https://x.test"}))
	ds.SetPathLocales(i18n.PathLocales{Default: "en", Prefixed: []string{"es"}})
	var asked []string
	files, _ := ds.SitemapFiles("", func(p string) bool {
		asked = append(asked, p)
		return p != "/es/about"
	})
	doc := files[0].Body
	if !strings.Contains(doc, "<loc>https://x.test/about</loc>") {
		t.Errorf("sitemap dropped /about:\n%s", doc)
	}
	if strings.Contains(doc, "https://x.test/es/about") {
		t.Errorf("sitemap names the skipped /es/about:\n%s", doc)
	}
	if !strings.Contains(doc, `hreflang="x-default" href="https://x.test/about"`) {
		t.Errorf("sitemap dropped the x-default alternate:\n%s", doc)
	}
	if !slices.Contains(asked, "/es/about") || !slices.Contains(asked, "/about") {
		t.Errorf("keep was asked about %v, want /about and /es/about", asked)
	}
}

// Under `gofastr dev` a page added while the server runs shows at once.
func TestSitemapDevRendersEachTime(t *testing.T) {
	t.Setenv("GOFASTR_ENV", "")
	t.Setenv("GOFASTR_DEV", "1")
	items := &manyItems{n: 1}
	h := sitemapServer(t, items)
	getSitemap(t, h, "/sitemap.xml")
	getSitemap(t, h, "/sitemap.xml")
	if items.calls != 2 {
		t.Errorf("StaticPaths ran %d times for two dev requests, want 2", items.calls)
	}
}

// Every anonymous GET re-ran StaticPaths and the page policy for every
// path. The live document is kept for sitemapTTL.
func TestSitemapRenderIsMemoised(t *testing.T) {
	items := &manyItems{n: 2}
	a := app.NewApp("x")
	a.Register("/items/:id", items, nil)
	ds := New(a, WithSitemap(SitemapConfig{BaseURL: "https://x.test"}))
	r := router.New()
	ds.Mount(r)
	getSitemap(t, r, "/sitemap.xml")
	getSitemap(t, r, "/sitemap.xml")
	if items.calls != 1 {
		t.Fatalf("StaticPaths ran %d times for two requests, want 1", items.calls)
	}
	ds.sitemapMemo.at = time.Now().Add(-sitemapTTL - time.Second)
	items.n = 3
	_, doc := getSitemap(t, r, "/sitemap.xml")
	if items.calls != 2 || !strings.Contains(doc, "https://x.test/items/2") {
		t.Errorf("an expired render was served again (calls %d):\n%s", items.calls, doc)
	}
}
