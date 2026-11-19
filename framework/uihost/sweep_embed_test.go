package uihost

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/framework/axecov"
	fembed "github.com/DonaldMurillo/gofastr/framework/embed"
)

func TestSweepEmbedAPIPathsOutsideSurfaceSpace(t *testing.T) {
	for _, p := range []string{embedExchangePath, embedRefreshPath, embedLoaderPath, embedRuntimePath} {
		if strings.HasPrefix(p, fembed.SurfacePrefix) {
			t.Errorf("%s sits under the surface prefix %s, so a surface name could shadow it", p, fembed.SurfacePrefix)
		}
	}
}

func TestSweepEmbedResolveThemeRefusesOversizeBeforeDecoding(t *testing.T) {
	f := newEmbedFixture(t)
	s, ok := f.embed.Lookup("reports")
	if !ok {
		t.Fatal("fixture surface missing")
	}
	big := strings.Repeat("A", maxEmbedThemeParam+1)
	allocs := testing.AllocsPerRun(5, func() {
		if _, ok := f.host.resolveEmbedTheme(s, big); ok {
			t.Fatal("an oversize theme parameter resolved")
		}
	})
	if allocs != 0 {
		t.Fatalf("resolveEmbedTheme allocated %.0f times on an oversize parameter; it must refuse before decoding", allocs)
	}
}

func TestSweepEmbedThemeCapEvictsLeastRecentlyUsed(t *testing.T) {
	var state embedThemeState
	for _, p := range []string{"brand-a", "brand-b"} {
		if ok, _, _ := state.reserve("reports", p, 2); !ok {
			t.Fatalf("reserve %s refused under the cap", p)
		}
		state.record("reports", p, "key-"+p)
	}
	if _, ok := state.lookup("reports", "brand-a"); !ok {
		t.Fatal("brand-a did not resolve")
	}
	ok, evicted, _ := state.reserve("reports", "brand-c", 2)
	if !ok || len(evicted) != 1 || evicted[0] != "key-brand-b" {
		t.Fatalf("reserve at the cap: ok=%v evicted=%v, want the least recently used key-brand-b", ok, evicted)
	}
	if _, ok := state.lookup("reports", "brand-a"); !ok {
		t.Fatal("the recently used brand-a was evicted")
	}
}

func TestSweepEmbedFrameAncestorsMatchedAcrossWhitespace(t *testing.T) {
	got := withFrameAncestors("default-src 'self'; frame-ancestors\t'none'", []string{embedTestOrigin})
	if strings.Contains(got, "'none'") || strings.Count(got, "frame-ancestors") != 1 {
		t.Fatalf("a tab after the directive name hid it from the rewrite: %q", got)
	}
}

func TestSweepEmbedSurfacePathOfAVanishedSurfaceIsEmpty(t *testing.T) {
	f := newEmbedFixture(t)
	if got := f.host.embedSurfacePath(fembed.Grant{Surface: "gone"}); got != "" {
		t.Fatalf("a grant for an unknown surface scoped to %q, want empty", got)
	}
	if got := f.host.embedSurfacePath(fembed.Grant{Surface: "reports"}); got != "/reports" {
		t.Fatalf("embedSurfacePath(reports) = %q, want /reports", got)
	}
}

func TestSweepEmbedShellLanguageFollowsTheAppRoute(t *testing.T) {
	f := newEmbedFixture(t)
	f.host.langFunc = func(path string) string {
		if path == "/reports" {
			return "fr"
		}
		return ""
	}
	rec := f.do(t, http.MethodGet, "/__gofastr/embed/reports", "")
	if !strings.Contains(rec.Body.String(), `lang="fr"`) {
		t.Fatalf("the shell's language did not come from the /reports route:\n%s", rec.Body.String())
	}
}

func TestSweepEmbedShellAnchorsComponentSheets(t *testing.T) {
	f := newEmbedFixture(t)
	rec := f.do(t, http.MethodGet, "/__gofastr/embed/reports", "")
	if !strings.Contains(rec.Body.String(), `id="cui-app-css"`) {
		t.Fatalf("the shell's app.css link carries no cui-app-css anchor:\n%s", rec.Body.String())
	}
}

func sweepEmbedContentStatus(t *testing.T, comp component.Component) int {
	t.Helper()
	application := app.NewApp("embed render failure")
	application.SetDefaultLayout(bareLayout("main"))
	scr := app.NewScreen("/boom", comp).WithTitle("Boom")
	application.RegisterScreen(scr, nil)
	eh, err := fembed.New(fembed.Config{
		Surfaces:  []fembed.Surface{{Name: "boom", Screen: scr, Origins: []string{embedTestOrigin}}},
		BurnStore: fembed.NewMemoryBurnStore(),
	})
	if err != nil {
		t.Fatalf("embed.New: %v", err)
	}
	eh.SetKeys([]byte("nonce-key-nonce-key-nonce-key-32"), []byte("grant-key-grant-key-grant-key-32"))
	f := embedFixture{host: New(application, WithEmbed(eh)), embed: eh, surface: "boom"}
	grant := f.grantFor(t, "boom")
	rec := f.do(t, http.MethodGet, "/__gofastr/embed/boom/content", "", func(r *http.Request) {
		r.Header.Set(embedGrantHeader, grant)
	})
	return rec.Code
}

func TestSweepEmbedContentPanicIs500(t *testing.T) {
	if got := sweepEmbedContentStatus(t, &loadBoom{}); got != http.StatusInternalServerError {
		t.Fatalf("a panicking embed screen answered %d, want 500", got)
	}
}

func TestSweepEmbedContentRenderErrorIs404(t *testing.T) {
	if got := sweepEmbedContentStatus(t, &loadErr{}); got != http.StatusNotFound {
		t.Fatalf("an embed screen whose Load fails answered %d, want 404", got)
	}
}

func TestSweepEmbedContentCarriesNoSiteChrome(t *testing.T) {
	f := newEmbedFixture(t)
	grant := f.grantFor(t, "reports")
	rec := f.do(t, http.MethodGet, "/__gofastr/embed/reports/content", "", func(r *http.Request) {
		r.Header.Set(embedGrantHeader, grant)
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	if body := rec.Body.String(); strings.Contains(body, "Test Site") || strings.Contains(body, "<header") {
		t.Fatalf("embedded content carries the app layout's header:\n%s", body)
	}
}

func TestSweepEmbedContentBlockKeepsItsStatus(t *testing.T) {
	policy := app.PolicyFunc(func(context.Context) app.Decision {
		return app.Decision{Kind: app.DecisionBlock, Status: http.StatusUnavailableForLegalReasons}
	})
	f := policyEmbedFixture(t, policy)
	grant := f.grantFor(t, "reports")
	rec := f.do(t, http.MethodGet, "/__gofastr/embed/reports/content", "", func(r *http.Request) {
		r.Header.Set(embedGrantHeader, grant)
	})
	if rec.Code != http.StatusUnavailableForLegalReasons {
		t.Fatalf("a policy block with status 451 answered %d", rec.Code)
	}
}

func sweepEmbedGateHost(t *testing.T, root component.Component) *UIHost {
	t.Helper()
	application, eh := buildEmbedHostWithScreen(t, root, "/reports")
	application.RegisterScreen(app.NewScreen("/child-action", &childActionComp{}), nil)
	return New(application, WithEmbed(eh))
}

type sweepValueChildComp struct{ kid childActionComp }

func (c *sweepValueChildComp) Render() render.HTML { return c.kid.Render() }

type sweepMapValueComp struct {
	kids map[string]component.Component
}

func (c *sweepMapValueComp) Render() render.HTML { return render.HTML("<p>map</p>") }

func TestSweepEmbedGateFollowsValueAndMapChildren(t *testing.T) {
	for name, root := range map[string]component.Component{
		"value field":   &sweepValueChildComp{},
		"map value":     &sweepMapValueComp{kids: map[string]component.Component{"k": &childActionComp{}}},
		"slice element": &sweepSliceComp{kids: []component.Component{&childActionComp{}}},
	} {
		t.Run(name, func(t *testing.T) {
			if got := panicFromMount(t, sweepEmbedGateHost(t, root)); got == "" {
				t.Fatal("Mount accepted a surface whose child, held as a " + name + ", ships a server action")
			}
		})
	}
}

type sweepSliceComp struct {
	kids []component.Component
}

func (c *sweepSliceComp) Render() render.HTML { return render.HTML("<p>slice</p>") }

type sweepAppRefComp struct{ app *app.App }

func (*sweepAppRefComp) Render() render.HTML { return render.HTML("<p>app</p>") }

type sweepHostRefComp struct{ host *UIHost }

func (*sweepHostRefComp) Render() render.HTML { return render.HTML("<p>host</p>") }

func TestSweepEmbedGateStopsAtHostBackReferences(t *testing.T) {
	t.Run("app", func(t *testing.T) {
		root := &sweepAppRefComp{}
		ds := sweepEmbedGateHost(t, root)
		root.app = ds.App
		if got := panicFromMount(t, ds); got != "" {
			t.Fatalf("a back-reference to the App made every screen's action reachable:\n%s", got)
		}
	})
	t.Run("uihost", func(t *testing.T) {
		root := &sweepHostRefComp{}
		ds := sweepEmbedGateHost(t, root)
		root.host = ds
		if got := panicFromMount(t, ds); got != "" {
			t.Fatalf("a back-reference to the UIHost made every compiled action reachable:\n%s", got)
		}
	})
}

type sweepFanComp struct {
	l, r *sweepFanComp
	kid  *childActionComp
}

func (*sweepFanComp) Render() render.HTML { return render.HTML("<p>fan</p>") }

func TestSweepEmbedWalkVisitsEachPointerOnce(t *testing.T) {
	trap := &sweepFanComp{}
	trap.l, trap.r = trap, trap
	root := &sweepFanComp{l: trap, r: trap, kid: &childActionComp{}}
	if !reachableComponentTypes(root)[reflect.TypeFor[childActionComp]()] {
		t.Fatal("revisiting a self-referencing pointer spent the walk budget before the child")
	}
}

type sweepBadge string

func (b sweepBadge) Render() render.HTML { return render.HTML(string(b)) }

type sweepBadgeHolder struct{ badge sweepBadge }

func (*sweepBadgeHolder) Render() render.HTML { return render.HTML("<p>badge</p>") }

func TestSweepEmbedWalkRecordsNonStructComponents(t *testing.T) {
	if !reachableComponentTypes(&sweepBadgeHolder{badge: "new"})[reflect.TypeFor[sweepBadge]()] {
		t.Fatal("a component whose underlying type is a string was not recorded")
	}
}

func TestSweepEmbedOffendersAreSorted(t *testing.T) {
	application := app.NewApp("offender order")
	application.RegisterScreen(app.NewScreen("/b-save", &serverActionScreenComp{}), nil)
	application.RegisterScreen(app.NewScreen("/a-save", &childActionComp{}), nil)
	ds := New(application)
	ds.Mount(router.New())
	got := ds.serverActionOffenders()
	if len(got) < 2 {
		t.Fatalf("want two offenders, got %d", len(got))
	}
	if !sort.SliceIsSorted(got, func(i, j int) bool { return got[i].componentID < got[j].componentID }) {
		t.Fatalf("offenders are not ordered by component id: %+v", got)
	}
}

func TestSweepEmbedStrictManifestMissingOffIsSilent(t *testing.T) {
	t.Setenv("GOFASTR_AXE_COVERAGE_DIR", "")
	t.Chdir(t.TempDir())
	t.Setenv("GOFASTR_DEV", "1")
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	a := app.NewApp("demo")
	a.Register("/", &describedScreen{}, nil)
	ds := New(a, strictSiteOptions()...)
	applyOption(ds, WithStrict(StrictConfig{AxeManifestMissing: StrictAbsenceOff}))
	if msg := mountPanic(t, ds); msg != "" {
		t.Fatalf("StrictAbsenceOff failed boot:\n%s", msg)
	}
	if strings.Contains(buf.String(), "axe coverage unverified") {
		t.Fatalf("StrictAbsenceOff still logged the missing manifest:\n%s", buf.String())
	}
}

func TestSweepEmbedStrictExemptMatching(t *testing.T) {
	c := StrictConfig{ExemptScreens: []string{"/machine/feed", "/internal/*"}}
	for route, want := range map[string]bool{
		"/machine/feed":        true,
		"/machine/feedx":       false,
		"/machine/feed/2":      false,
		"/internal":            true,
		"/internal/tools":      true,
		"/internalx":           false,
		"/internalx/tools":     false,
		"/other/internal/page": false,
	} {
		if got := c.exempt(route); got != want {
			t.Errorf("exempt(%q) = %v, want %v", route, got, want)
		}
	}
}

func TestSweepEmbedStrictAppIconSatisfiesTheIconCheck(t *testing.T) {
	a := app.NewApp("demo")
	a.Register("/", &describedScreen{}, nil)
	ds := New(a,
		WithStrict(),
		WithDescription("A demo app."),
		WithSitemap(SitemapConfig{BaseURL: "https://example.com"}),
		WithRobots(RobotsConfig{}),
	)
	ds.appIcons = map[string][]byte{"/icon-192.png": {0x89}}
	if msg := mountPanic(t, ds); msg != "" {
		t.Fatalf("an app icon without a favicon failed the icon check:\n%s", msg)
	}
}

func TestSweepEmbedStrictAxeCoverageLevelIsHonoured(t *testing.T) {
	t.Setenv("GOFASTR_AXE_COVERAGE_DIR", "")
	t.Chdir(t.TempDir())
	t.Setenv("GOFASTR_DEV", "1")
	if err := axecov.Record(".", "/", "dark"); err != nil {
		t.Fatal(err)
	}
	for _, level := range []StrictLevel{StrictWarn, StrictOff} {
		a := app.NewApp("demo")
		a.Register("/", &describedScreen{}, nil)
		a.Register("/pricing", &describedScreen{}, nil)
		ds := New(a, strictSiteOptions()...)
		applyOption(ds, WithStrict(StrictConfig{AxeCoverage: level}))
		if msg := mountPanic(t, ds); msg != "" {
			t.Fatalf("AxeCoverage level %d still failed boot on a gap:\n%s", level, msg)
		}
	}
}

func TestSweepEmbedStrictAxeCoverageOffSkipsTheManifest(t *testing.T) {
	t.Setenv("GOFASTR_AXE_COVERAGE_DIR", "")
	t.Chdir(t.TempDir())
	t.Setenv("GOFASTR_DEV", "1")
	a := app.NewApp("demo")
	a.Register("/", &describedScreen{}, nil)
	ds := New(a, strictSiteOptions()...)
	applyOption(ds, WithStrict(StrictConfig{AxeCoverage: StrictOff, AxeManifestMissing: StrictAbsenceEnforce}))
	if msg := mountPanic(t, ds); msg != "" {
		t.Fatalf("AxeCoverage off still demanded a manifest:\n%s", msg)
	}
}

func TestSweepEmbedStrictScreenlessAppNeedsNoManifest(t *testing.T) {
	t.Setenv("GOFASTR_AXE_COVERAGE_DIR", "")
	t.Chdir(t.TempDir())
	t.Setenv("GOFASTR_DEV", "1")
	a := app.NewApp("demo")
	a.RegisterScreen(app.NewDialog("/confirm", &bareScreen{}), nil)
	ds := New(a, strictSiteOptions()...)
	applyOption(ds, WithStrict(StrictConfig{AxeManifestMissing: StrictAbsenceEnforce}))
	if msg := mountPanic(t, ds); msg != "" {
		t.Fatalf("an app with no page screens was asked for an axe manifest:\n%s", msg)
	}
}

func TestSweepEmbedGrantExpiryIsRelative(t *testing.T) {
	f := newEmbedFixture(t)
	nonce, err := f.embed.MintNonce(context.Background(), "reports", "user-7", embedTestOrigin, nil)
	if err != nil {
		t.Fatalf("MintNonce: %v", err)
	}
	body, _ := json.Marshal(map[string]string{"token": nonce, "origin": embedTestOrigin})
	rec := f.do(t, http.MethodPost, "/__gofastr/embed-exchange", string(body))
	var out struct {
		ExpiresInMS int64 `json:"expires_in_ms"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.ExpiresInMS <= 0 || out.ExpiresInMS > (7*24*time.Hour).Milliseconds() {
		t.Fatalf("expires_in_ms = %d, want milliseconds from now", out.ExpiresInMS)
	}
}

func TestSweepEmbedStrictAxeCoverageSkippedOutsideDev(t *testing.T) {
	t.Setenv("GOFASTR_AXE_COVERAGE_DIR", "")
	t.Chdir(t.TempDir())
	t.Setenv("GOFASTR_DEV", "")
	a := app.NewApp("demo")
	a.Register("/", &describedScreen{}, nil)
	ds := New(a, strictSiteOptions()...)
	applyOption(ds, WithStrict(StrictConfig{AxeManifestMissing: StrictAbsenceEnforce}))
	if msg := mountPanic(t, ds); msg != "" {
		t.Fatalf("a production boot ran the axe check:\n%s", msg)
	}
}

func TestSweepEmbedExchangeBodyIsCapped(t *testing.T) {
	f := newEmbedFixture(t)
	body, _ := json.Marshal(map[string]string{"token": strings.Repeat("x", 16<<10), "origin": embedTestOrigin})
	rec := f.do(t, http.MethodPost, "/__gofastr/embed-exchange", string(body))
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("a 16 KiB exchange body answered %d, want 413 from the body cap", rec.Code)
	}
}

// A malformed exchange body answers the invalid-JSON-body phrase every
// other uihost JSON endpoint answers, not the bare "bad request".
func TestSweepEmbedExchangeMalformedBodyIsInvalidJSON(t *testing.T) {
	f := newEmbedFixture(t)
	rec := f.do(t, http.MethodPost, "/__gofastr/embed-exchange", `{"token":`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("a malformed exchange body answered %d, want 400", rec.Code)
	}
	if got := strings.TrimSpace(rec.Body.String()); got != "invalid JSON body" {
		t.Fatalf("body %q, want the invalid_json_body phrase", got)
	}
}

func TestSweepEmbedReplayedExchangeWarns(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	f := newEmbedFixture(t)
	nonce, err := f.embed.MintNonce(context.Background(), "reports", "user-7", embedTestOrigin, nil)
	if err != nil {
		t.Fatalf("MintNonce: %v", err)
	}
	body, _ := json.Marshal(map[string]string{"token": nonce, "origin": embedTestOrigin})
	if rec := f.do(t, http.MethodPost, "/__gofastr/embed-exchange", string(body)); rec.Code != http.StatusOK {
		t.Fatalf("first exchange: %d", rec.Code)
	}
	if strings.Contains(buf.String(), "exchanged more than once") {
		t.Fatalf("the first exchange already warned of a replay:\n%s", buf.String())
	}
	if rec := f.do(t, http.MethodPost, "/__gofastr/embed-exchange", string(body)); rec.Code != http.StatusOK {
		t.Fatalf("replayed exchange: %d", rec.Code)
	}
	if !strings.Contains(buf.String(), "exchanged more than once") {
		t.Fatalf("a replayed exchange logged no warning:\n%s", buf.String())
	}
}

func TestSweepEmbedStripMatchesMiddleware(t *testing.T) {
	f := newEmbedFixture(t)
	grant := f.grantFor(t, "reports")
	names := []string{"Cookie", "Authorization", "X-API-Key", "X-Other"}
	set := func(r *http.Request) {
		r.Header.Set(embedGrantHeader, grant)
		for _, n := range names {
			r.Header.Set(n, "v")
		}
	}

	var seen http.Header
	mw := f.embed.Middleware()(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		seen = r.Header.Clone()
	}))
	viaMiddleware := httptest.NewRequest(http.MethodGet, "/reports", nil)
	set(viaMiddleware)
	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, viaMiddleware)
	if seen == nil {
		t.Fatalf("middleware refused the grant: %d %s", rec.Code, rec.Body.String())
	}

	viaRoute := httptest.NewRequest(http.MethodGet, "/__gofastr/embed/reports/content", nil)
	set(viaRoute)
	stripAmbientCredentials(viaRoute)

	for _, n := range names {
		if (seen.Get(n) != "") != (viaRoute.Header.Get(n) != "") {
			t.Errorf("%s: middleware kept=%v, embed routes kept=%v", n, seen.Get(n) != "", viaRoute.Header.Get(n) != "")
		}
	}
}
