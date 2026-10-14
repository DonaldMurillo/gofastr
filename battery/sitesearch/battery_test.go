package sitesearch

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	coreapp "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

type htmlScreen string

func (s htmlScreen) Render() render.HTML { return render.HTML(s) }

// newSite mounts three pages: a home, a docs page with headed
// sections, and a page that opts out with noindex. The policy-gated
// screen path is the static walker's, covered in framework/static.
func newSite(t *testing.T, bat *Battery) *framework.App {
	t.Helper()
	site := coreapp.NewApp("sitesearch-test")
	site.Register("/", htmlScreen(`<main><h1>Home</h1><p>Welcome to the demo.</p></main>`), nil)
	site.Register("/docs/forms", htmlScreen(`<main><nav>sidebar formsnav</nav><h1>Forms</h1>
		<p>Build a form from fields.</p>
		<h2 id="validation">Validation</h2><p>Errors render beside each field.</p></main>`), nil)
	site.Register("/private", htmlScreen(`<meta name="robots" content="noindex"><main><h1>Validation secrets</h1></main>`), nil)
	app := framework.NewApp(framework.WithoutDefaultMiddleware()).Mount(uihost.New(site))
	if err := app.Batteries.Register(bat); err != nil {
		t.Fatal(err)
	}
	return app
}

func get(t *testing.T, h http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func TestLiveQueryFindsSection(t *testing.T) {
	bat := New(Config{Groups: []Group{{Prefix: "/docs/", Label: "Docs"}}})
	app := newSite(t, bat)
	if err := app.InitPlugins(); err != nil {
		t.Fatal(err)
	}
	rec := get(t, app.Router(), Root+"query?q=valid")
	if rec.Code != http.StatusOK {
		t.Fatalf("query = %d %s", rec.Code, rec.Body)
	}
	var body struct{ Results []Result }
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Results) != 1 {
		t.Fatalf("results = %+v, want only /docs/forms (/private is noindex)", body.Results)
	}
	r := body.Results[0]
	if r.URL != "/docs/forms" || r.Group != "Docs" || len(r.Sections) != 1 || r.Sections[0].ID != "validation" {
		t.Errorf("result = %+v", r)
	}
	if got := get(t, app.Router(), Root+"query?q=formsnav"); !strings.Contains(got.Body.String(), `"results":[]`) {
		t.Errorf("sidebar text was indexed: %s", got.Body)
	}
}

func TestLiveQueryBoundsParams(t *testing.T) {
	app := newSite(t, New(Config{}))
	if err := app.InitPlugins(); err != nil {
		t.Fatal(err)
	}
	if rec := get(t, app.Router(), Root+"query?q="+strings.Repeat("a", maxQueryBytes+1)); rec.Code != http.StatusRequestURITooLong {
		t.Errorf("long query = %d", rec.Code)
	}
	if rec := get(t, app.Router(), Root+"query?q=home&limit=0"); rec.Code != http.StatusBadRequest {
		t.Errorf("limit=0 = %d", rec.Code)
	}
}

func TestLiveServesIndexFiles(t *testing.T) {
	app := newSite(t, New(Config{}))
	if err := app.InitPlugins(); err != nil {
		t.Fatal(err)
	}
	rec := get(t, app.Router(), Root+"manifest.json")
	if rec.Code != http.StatusOK || rec.Header().Get("Cache-Control") != "no-cache" {
		t.Fatalf("manifest = %d %q", rec.Code, rec.Header().Get("Cache-Control"))
	}
	var m manifest
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatal(err)
	}
	shard, ok := m.Shards["va"]
	if !ok {
		t.Fatalf("no shard for va: %+v", m.Shards)
	}
	if rec := get(t, app.Router(), Root+shard); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"validation"`) {
		t.Errorf("shard %s = %d %s", shard, rec.Code, rec.Body)
	}
	if rec := get(t, app.Router(), Root+"nope.json"); rec.Code != http.StatusNotFound {
		t.Errorf("unknown file = %d", rec.Code)
	}
}

func TestExportWritesIndex(t *testing.T) {
	app := newSite(t, New(Config{}))
	dir := t.TempDir()
	if err := app.ExportStatic(context.Background(), dir, "/base"); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "__gofastr", "search", "manifest.json"))
	if err != nil {
		t.Fatalf("export has no search manifest: %v", err)
	}
	var m manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	if len(m.Pages) != 2 {
		t.Errorf("exported index holds %d pages, want 2 (home, forms)", len(m.Pages))
	}
	for _, name := range m.Pages {
		frag, err := os.ReadFile(filepath.Join(dir, "__gofastr", "search", name))
		if err != nil {
			t.Fatal(err)
		}
		// Page URLs stay root-relative: the client adds the base.
		if strings.Contains(string(frag), "/base") {
			t.Errorf("fragment %s carries the base path: %s", name, frag)
		}
	}
}

func TestSourcesAddPages(t *testing.T) {
	bat := New(Config{Sources: []Source{func(context.Context) ([]Page, error) {
		return []Page{{URL: "/records/7", Title: "Zanzibar shipment"}}, nil
	}}})
	app := newSite(t, bat)
	if err := app.InitPlugins(); err != nil {
		t.Fatal(err)
	}
	idx, err := bat.Index(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got := urls(idx.Search("zanzibar", 5)); got != "/records/7" {
		t.Errorf("zanzibar = %q", got)
	}
}

func TestExcludeMatchesPaths(t *testing.T) {
	bat := New(Config{Exclude: []string{"/docs"}})
	app := newSite(t, bat)
	if err := app.InitPlugins(); err != nil {
		t.Fatal(err)
	}
	idx, err := bat.Index(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got := idx.Search("validation", 5); got != nil {
		t.Errorf("an excluded path was indexed: %q", urls(got))
	}
	if got := urls(idx.Search("welcome", 5)); got != "/" {
		t.Errorf("welcome = %q, want /", got)
	}
}

func TestGroupsMatchWholeSegments(t *testing.T) {
	b := New(Config{Groups: []Group{
		{Prefix: "/", Label: "Site"},
		{Prefix: "/doc", Label: "Doc"},
		{Prefix: "/docs/", Label: "Docs"},
	}})
	for path, want := range map[string]string{
		"/":          "Site",
		"/doc":       "Doc",
		"/doc/intro": "Doc",
		"/docs/":     "Docs",
		"/docs/x":    "Docs",
		"/docsx":     "Site",
		"/documents": "Site",
	} {
		if got := b.group(path); got != want {
			t.Errorf("group(%q) = %q, want %q", path, got, want)
		}
	}
}

// TestStartWarmsIndex: the index builds when the app starts, so the
// first reader to search does not wait for every page to render.
func TestStartWarmsIndex(t *testing.T) {
	bat := New(Config{})
	app := newSite(t, bat)
	if err := app.InitPlugins(); err != nil {
		t.Fatal(err)
	}
	if err := bat.OnStart(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := bat.OnStop(context.Background()); err != nil { // waits for the build
		t.Fatal(err)
	}
	bat.mu.Lock()
	built := bat.idx != nil
	bat.mu.Unlock()
	if !built {
		t.Error("OnStart did not build the index")
	}
}
