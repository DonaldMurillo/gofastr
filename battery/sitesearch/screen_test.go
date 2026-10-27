package sitesearch

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

// searchSite is newSite with the search page mounted, plus n pages
// that all say "walrus", for pagination.
func searchSite(t *testing.T, n int) (*framework.App, *Battery) {
	t.Helper()
	bat := New(Config{Groups: []Group{{Prefix: "/docs/", Label: "Docs"}}})
	app := newSite(t, bat)
	site := app.Mountables()[0].(*uihost.UIHost).App
	site.Register("/search", Screen(bat), nil)
	for i := range n {
		site.Register(fmt.Sprintf("/walrus/%d", i), htmlScreen(fmt.Sprintf(`<main><h1>Walrus %d</h1><p>walrus</p></main>`, i)), nil)
	}
	if err := app.InitPlugins(); err != nil {
		t.Fatal(err)
	}
	return app, bat
}

// The page answers a query as links, with no script involved.
func TestScreenRendersResults(t *testing.T) {
	app, _ := searchSite(t, 0)
	rec := get(t, app.Router(), "/search?q=valid")
	if rec.Code != 200 {
		t.Fatalf("GET /search?q=valid = %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{`href="/docs/forms#validation"`, `1 result for “valid”`, `value="valid"`, `noindex`} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q", want)
		}
	}
}

// A failed build says the search is unavailable, never "no results".
func TestScreenSaysIndexUnavailable(t *testing.T) {
	bat := New(Config{Sources: []Source{func(context.Context) ([]Page, error) {
		return nil, fmt.Errorf("source down")
	}}})
	app := newSite(t, bat)
	site := app.Mountables()[0].(*uihost.UIHost).App
	site.Register("/search", Screen(bat), nil)
	if err := app.InitPlugins(); err != nil {
		t.Fatal(err)
	}
	body := get(t, app.Router(), "/search?q=walrus").Body.String()
	if !strings.Contains(body, "search index unavailable") || strings.Contains(body, "source down") {
		t.Fatalf("failed build: no unavailable notice, or the error leaked:\n%s", body)
	}
}

// The index build renders the search page too, with no query: it
// must not wait on the build rendering it, nor index itself.
func TestScreenDoesNotBlockItsBuild(t *testing.T) {
	_, bat := searchSite(t, 0)
	done := make(chan error, 1)
	go func() { _, err := bat.Index(context.Background()); done <- err }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the index build waited on itself through the search page")
	}
	idx, _ := bat.Index(context.Background())
	for _, p := range idx.pages {
		if p.URL == "/search" {
			t.Error("the search page indexed itself")
		}
	}
}

// Results page through ?page=, and an out-of-range page shows none.
func TestScreenPages(t *testing.T) {
	app, _ := searchSite(t, 25)
	first := get(t, app.Router(), "/search?q=walrus").Body.String()
	second := get(t, app.Router(), "/search?q=walrus&page=2").Body.String()
	if n := strings.Count(first, `class="fui-search-results__item"`); n != 20 {
		t.Errorf("page 1 shows %d results, want 20", n)
	}
	if n := strings.Count(second, `class="fui-search-results__item"`); n != 5 {
		t.Errorf("page 2 shows %d results, want 5", n)
	}
	if !strings.Contains(first, "25 results for “walrus”") || !strings.Contains(first, "page=2") {
		t.Error("page 1 lacks the count or the link to page 2")
	}
	// A page that does not parse is the first; one past the end shows
	// none, however large, and never wraps round to the first.
	for p, want := range map[string]int{"0": 20, "-3": 20, "x": 20, "99999999999999999999": 20, "3": 0, "9223372036854775807": 0} {
		rec := get(t, app.Router(), "/search?q=walrus&page="+p)
		if n := strings.Count(rec.Body.String(), `class="fui-search-results__item"`); rec.Code != 200 || n != want {
			t.Errorf("page=%s: %d with %d results, want 200 with %d", p, rec.Code, n, want)
		}
	}
}

// A query past the endpoint's limit is dropped, not searched.
func TestScreenDropsLongQuery(t *testing.T) {
	app, _ := searchSite(t, 0)
	body := get(t, app.Router(), "/search?q="+strings.Repeat("valid+", 60)).Body.String()
	if strings.Contains(body, `class="fui-search-results__item"`) {
		t.Error("a query over 256 bytes was searched")
	}
}

func TestScreenPathRefusesRelative(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("ScreenPath accepted a relative path")
		}
	}()
	ScreenPath("search")
}
