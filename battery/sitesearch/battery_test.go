package sitesearch

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	coreapp "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/app/decide"
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
	shards := shardsFor(m, "validation")
	if len(shards) != 1 {
		t.Fatalf("validation reads %d shards, want 1: %+v", len(shards), m.Keys)
	}
	shard := shards[0]
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
	var names []string
	for n := 0; ; n++ {
		name := pageFile(m, n)
		if _, err := os.Stat(filepath.Join(dir, "__gofastr", "search", name)); err != nil {
			break
		}
		names = append(names, name)
	}
	if len(names) != 2 {
		t.Errorf("exported index holds %d pages, want 2 (home, forms)", len(names))
	}
	for _, name := range names {
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

type roleKey struct{}

// The index is what every visitor searches, so it is built as an
// anonymous visitor: the identity on the context that asked for the
// build never reaches the pages' policies.
func TestIndexIgnoresCallerIdentity(t *testing.T) {
	bat := New(Config{})
	app := newSite(t, bat)
	site := app.Mountables()[0].(*uihost.UIHost).App
	site.RegisterScreen(coreapp.NewScreen("/admin", htmlScreen(`<main><h1>Admin</h1><p>Payroll quokka.</p></main>`)).
		WithPolicy(coreapp.PolicyFunc(func(ctx context.Context) coreapp.Decision {
			if ctx.Value(roleKey{}) == "admin" {
				return decide.Allow()
			}
			return decide.Block(http.StatusForbidden)
		})), nil)
	if err := app.InitPlugins(); err != nil {
		t.Fatal(err)
	}
	admin := context.WithValue(context.Background(), roleKey{}, "admin")
	if err := bat.Refresh(admin); err != nil {
		t.Fatal(err)
	}
	idx, err := bat.Index(admin)
	if err != nil {
		t.Fatal(err)
	}
	if got := idx.Search("quokka", 5); got != nil {
		t.Errorf("an admin-only page entered the shared index: %q", urls(got))
	}
}

func TestExcludeSkipsBeforeRendering(t *testing.T) {
	bat := New(Config{Exclude: []string{"/broken"}})
	app := newSite(t, bat)
	site := app.Mountables()[0].(*uihost.UIHost).App
	site.Register("/broken", panicScreen{}, nil)
	if err := app.InitPlugins(); err != nil {
		t.Fatal(err)
	}
	if _, err := bat.Index(context.Background()); err != nil {
		t.Errorf("an excluded page that cannot render broke the index: %v", err)
	}
}

type panicScreen struct{}

func (panicScreen) Render() render.HTML { panic("broken page") }

func TestExcludeCoversSources(t *testing.T) {
	bat := New(Config{Exclude: []string{"/drafts"}, Sources: []Source{func(context.Context) ([]Page, error) {
		return []Page{{URL: "/drafts/next", Title: "Unannounced zanzibar"}}, nil
	}}})
	app := newSite(t, bat)
	if err := app.InitPlugins(); err != nil {
		t.Fatal(err)
	}
	idx, err := bat.Index(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got := idx.Search("zanzibar", 5); got != nil {
		t.Errorf("an excluded source page was indexed: %q", urls(got))
	}
}

// OnStop honors its deadline while a build is still running.
func TestStopHonorsDeadlineMidBuild(t *testing.T) {
	release, inBuild := make(chan struct{}), make(chan struct{})
	bat := New(Config{Sources: []Source{func(context.Context) ([]Page, error) {
		close(inBuild)
		<-release
		return nil, nil
	}}})
	app := newSite(t, bat)
	if err := app.InitPlugins(); err != nil {
		t.Fatal(err)
	}
	defer close(release)
	if err := bat.OnStart(context.Background()); err != nil {
		t.Fatal(err)
	}
	<-inBuild
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- bat.OnStop(ctx) }()
	select {
	case err := <-done:
		if err == nil {
			t.Error("OnStop returned nil while the build still ran")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("OnStop ignored its deadline")
	}
}

// A query that waits behind a slow first build gives up when its own
// request ends; the build it waited on carries on.
func TestIndexWaiterHonorsContext(t *testing.T) {
	release, inBuild := make(chan struct{}), make(chan struct{})
	bat := New(Config{Sources: []Source{func(context.Context) ([]Page, error) {
		close(inBuild)
		<-release
		return nil, nil
	}}})
	app := newSite(t, bat)
	if err := app.InitPlugins(); err != nil {
		t.Fatal(err)
	}
	defer close(release)
	go func() { _, _ = bat.Index(context.Background()) }()
	<-inBuild
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := bat.Index(ctx)
		done <- err
	}()
	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("waiting Index = %v, want its deadline", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("a waiting Index ignored its context")
	}
}

// A build that failed partway leaves nothing behind for the next one.
func TestExportStartsFresh(t *testing.T) {
	bat := New(Config{})
	if err := bat.ExportStart(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := bat.ExportPage("/a", `<main><h1>Old</h1><p>stale walrus</p></main>`); err != nil {
		t.Fatal(err)
	}
	// The build fails here: ExportFiles never runs. The retry:
	if err := bat.ExportStart(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := bat.ExportPage("/a", `<main><h1>New</h1><p>fresh walrus</p></main>`); err != nil {
		t.Fatal(err)
	}
	files, err := bat.ExportFiles(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for name, body := range files {
		if strings.Contains(string(body), "stale") {
			t.Errorf("%s carries the failed build's page: %s", name, body)
		}
	}
}

// One page that cannot render leaves the rest of the site searchable.
func TestBrokenPageLeavesSearchUp(t *testing.T) {
	bat := New(Config{})
	app := newSite(t, bat)
	site := app.Mountables()[0].(*uihost.UIHost).App
	site.Register("/broken", panicScreen{}, nil)
	if err := app.InitPlugins(); err != nil {
		t.Fatal(err)
	}
	idx, err := bat.Index(context.Background())
	if err != nil {
		t.Fatalf("a page that cannot render took search down: %v", err)
	}
	if got := urls(idx.Search("validation", 5)); got != "/docs/forms" {
		t.Errorf("results = %q, want the forms page", got)
	}
}

// A failed build is not re-run by every query; Refresh retries at once.
func TestFailedBuildWaitsToRetry(t *testing.T) {
	calls := 0
	bat := New(Config{Sources: []Source{func(context.Context) ([]Page, error) {
		calls++
		return nil, errors.New("changelog feed down")
	}}})
	app := newSite(t, bat)
	if err := app.InitPlugins(); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		if _, err := bat.Index(context.Background()); err == nil {
			t.Fatal("a failed source built an index")
		}
	}
	if calls != 1 {
		t.Errorf("three queries ran the failed build %d times, want 1", calls)
	}
	if err := bat.Refresh(context.Background()); err == nil {
		t.Fatal("Refresh with the source still down returned nil")
	}
	if calls != 2 {
		t.Errorf("Refresh ran the build %d times in all, want a second run", calls)
	}
}

// A build cut short by its caller is no failure to wait out.
func TestCancelledBuildIsNotCached(t *testing.T) {
	calls := 0
	bat := New(Config{Sources: []Source{func(ctx context.Context) ([]Page, error) {
		calls++
		if calls == 1 {
			<-ctx.Done()
			return nil, ctx.Err()
		}
		return nil, nil
	}}})
	app := newSite(t, bat)
	if err := app.InitPlugins(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := bat.Index(ctx); err == nil {
		t.Fatal("a cancelled build returned an index")
	}
	if _, err := bat.Index(context.Background()); err != nil {
		t.Errorf("the build after a cancelled one returned %v", err)
	}
}

// cancelScreen cancels the build that renders it, then waits for the
// cancellation to land.
type cancelScreen struct{ cancel context.CancelFunc }

func (s cancelScreen) Render() render.HTML {
	s.cancel()
	time.Sleep(50 * time.Millisecond)
	return render.HTML(`<main><h1>Cancel</h1></main>`)
}

// ctxScreen fails to render once its context is done, the way a page
// whose data comes from a query does.
type ctxScreen string

func (s ctxScreen) Render() render.HTML { return render.HTML(s) }

func (s ctxScreen) RenderCtx(ctx context.Context) render.HTML {
	if ctx.Err() != nil {
		panic(ctx.Err())
	}
	return render.HTML(s)
}

// A build cancelled partway keeps no half-built index.
func TestCancelledWalkKeepsNoIndex(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	bat := New(Config{})
	site := coreapp.NewApp("sitesearch-cancel")
	site.Register("/", cancelScreen{cancel}, nil)
	site.Register("/docs/forms", ctxScreen(`<main><h1>Forms</h1><p>Build a form.</p></main>`), nil)
	app := framework.NewApp(framework.WithoutDefaultMiddleware()).Mount(uihost.New(site))
	if err := app.Batteries.Register(bat); err != nil {
		t.Fatal(err)
	}
	if err := app.InitPlugins(); err != nil {
		t.Fatal(err)
	}
	_, _ = bat.Index(ctx)
	idx, err := bat.Index(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got := urls(idx.Search("form", 5)); got != "/docs/forms" {
		t.Errorf("results = %q after a cancelled build, want the forms page", got)
	}
}
