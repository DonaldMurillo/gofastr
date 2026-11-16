package uihost

import (
	"bytes"
	"context"
	"encoding/json"
	stdimage "image"
	"image/color"
	"image/png"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/core/router"
)

func TestSweepAssetsHostlessOriginRefused(t *testing.T) {
	for _, origin := range []string{"null", "file:///etc/passwd", "%zz"} {
		ds := newTestUIHost()
		req := httptest.NewRequest(http.MethodPost, "/__gofastr/session", nil)
		req.Header.Set("Origin", origin)
		rec := httptest.NewRecorder()
		ds.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Errorf("POST /__gofastr/session with Origin %q = %d, want 403", origin, rec.Code)
		}
	}
}

// A valid token under the other origin mode's cookie name is not read.
func TestSweepAssetsReadsOnlyTheOriginsCookieName(t *testing.T) {
	ds := newTestUIHost()
	sess := ds.CreateSession()
	remote := httptest.NewRequest(http.MethodGet, "http://app.example/", nil)
	remote.AddCookie(&http.Cookie{Name: sessionCookieDevName, Value: sess.Token})
	if got := readSessionCookie(remote); got != "" {
		t.Errorf("a remote origin read the dev cookie: %q", got)
	}
	loop := httptest.NewRequest(http.MethodGet, "http://localhost:8080/", nil)
	loop.AddCookie(&http.Cookie{Name: sessionCookieSecureName, Value: sess.Token})
	if got := readSessionCookie(loop); got != "" {
		t.Errorf("a plaintext loopback origin read the __Host- cookie: %q", got)
	}
}

func TestSweepAssetsGrantDropsTheAmbientCookie(t *testing.T) {
	f := newEmbedFixture(t)
	grant := f.grantFor(t, "reports")
	sess := f.host.CreateSession()
	req := httptest.NewRequest(http.MethodGet, "/__gofastr/actions.js", nil)
	req.Header.Set(embedGrantHeader, grant)
	req.AddCookie(&http.Cookie{Name: sessionCookieSecureName, Value: sess.Token})
	req.Header.Set("Authorization", "Bearer someone-else")
	if !f.host.requireSessionOrEmbedGrant(httptest.NewRecorder(), req) {
		t.Fatal("a valid grant was refused")
	}
	if req.Header.Get("Cookie") != "" || req.Header.Get("Authorization") != "" {
		t.Errorf("a grant-authorised request kept its ambient credentials: %v", req.Header)
	}
}

func TestSweepAssetsMutatingBodyCapIs64KiB(t *testing.T) {
	ds := newTestUIHost()
	body := `{"action":"noop","params":{"payload":"` + strings.Repeat("A", 70<<10) + `"},"componentId":"missing"}`
	req := httptest.NewRequest(http.MethodPost, "/__gofastr/action", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.ContentLength = -1
	rec := httptest.NewRecorder()
	ds.ServeHTTP(rec, req)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("a 70 KiB action body of unknown length = %d, want 413", rec.Code)
	}
}

func TestSweepAssetsCreateSessionRefusesAGrant(t *testing.T) {
	ds := newTestUIHost()
	req := httptest.NewRequest(http.MethodPost, "/__gofastr/session", nil)
	req.Header.Set(embedGrantHeader, "emg_anything")
	rec := httptest.NewRecorder()
	ds.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden || rec.Header().Get("Set-Cookie") != "" {
		t.Fatalf("POST /__gofastr/session with an embed grant = %d, cookie %q; want 403 and no cookie",
			rec.Code, rec.Header().Get("Set-Cookie"))
	}
}

func TestSweepAssetsSessionBodyCarriesOnlyTheID(t *testing.T) {
	ds := newTestUIHost()
	rec := httptest.NewRecorder()
	ds.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/__gofastr/session", nil))
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body) != 1 || !strings.HasPrefix(body["sessionId"], "sess-") || strings.Contains(body["sessionId"], ".") {
		t.Fatalf("session body = %v, want only the bare sessionId", body)
	}
}

func TestSweepAssetsSetSessionKeyEndsRotation(t *testing.T) {
	ds := newTestUIHost()
	old := []byte("old-session-key-0123456789abcdef") // not-a-secret: test key
	ds.SetSessionKey(old)
	tok := ds.CreateSession().Token
	ds.SetSessionKeys([]byte("new-session-key-0123456789abcdef"), old) // not-a-secret: test key
	if _, ok := ds.verifySessionToken(tok); !ok {
		t.Fatal("a previous key's token did not verify inside the rotation window")
	}
	ds.SetSessionKey([]byte("new-session-key-0123456789abcdef")) // not-a-secret: test key
	if _, ok := ds.verifySessionToken(tok); ok {
		t.Fatal("SetSessionKey kept accepting the previous key")
	}
}

type sweepAssetsTopics []string

func (s sweepAssetsTopics) PresenceTopics(context.Context) []string { return s }
func (sweepAssetsTopics) Render() render.HTML                       { return "" }

func TestSweepAssetsDeclaredTopicWithCommaDropped(t *testing.T) {
	got := declaredTopics(context.Background(), sweepAssetsTopics{"doc:1,doc:2", "doc:3"})
	if len(got) != 1 || got[0] != "doc:3" {
		t.Fatalf("declaredTopics = %v, want [doc:3]", got)
	}
}

func TestSweepAssetsPresenceParamBounded(t *testing.T) {
	long := httptest.NewRequest(http.MethodGet, "/?presence="+strings.Repeat("a", maxPresenceParamLen+1), nil)
	if got := boundedPresenceParam(long); got != "" {
		t.Fatalf("an oversize ?presence= was threaded through (%d bytes)", len(got))
	}
	if got := boundedPresenceParam(httptest.NewRequest(http.MethodGet, "/?presence=doc:1", nil)); got != "doc:1" {
		t.Fatalf("boundedPresenceParam = %q, want doc:1", got)
	}
}

func TestSweepAssetsOversizeDeclaredListIgnored(t *testing.T) {
	ds := newTestUIHost()
	list := strings.TrimSuffix(strings.Repeat("doc:1,", maxPresenceParamLen/6+1), ",")
	q := url.Values{"declared": {list}, "dsig": {ds.signDeclaredTopics("u", list, time.Now().Add(time.Hour))}}
	if got := ds.verifiedDeclaredTopics(q, "u", time.Now()); got != nil {
		t.Fatalf("a correctly signed %d-byte list was admitted: %v", len(list), got)
	}
}

func TestSweepAssetsUnbundledPageLinksEachSheet(t *testing.T) {
	registry.IsolateForTest(t)
	a, b := registerTestStyle(t, "sweep-a"), registerTestStyle(t, "sweep-b")
	ds := newTestUIHostForMany(a, b)
	page := string(a.Render(&innerSpan{body: "a"})) + string(b.Render(&innerSpan{body: "b"}))
	tags := ds.componentCSSTags(page, false)
	if strings.Contains(tags, "comp-bundle.css") {
		t.Fatalf("an unbundled page linked the bundle:\n%s", tags)
	}
	for _, s := range []*registry.Style{a, b} {
		if !strings.Contains(tags, `data-cui-style="`+s.Name()+`"`) {
			t.Errorf("no per-component link for %s:\n%s", s.Name(), tags)
		}
	}
}

func TestSweepAssetsSafeComponentName(t *testing.T) {
	for _, n := range []string{"ui-button", "ns:card.v2_x"} {
		if !safeComponentName(n) {
			t.Errorf("safeComponentName(%q) = false", n)
		}
	}
	for _, n := range []string{"", "a/b", "a?b", "a#b", "a b", "a%2Fb"} {
		if safeComponentName(n) {
			t.Errorf("safeComponentName(%q) = true", n)
		}
	}
}

func TestSweepAssetsOnePageWideIndicator(t *testing.T) {
	ds := newTestUIHost()
	if css := ds.AppCSS(); !strings.Contains(css, "fui-nav-progress") || strings.Contains(css, "[data-fui-page-loading]") {
		t.Fatal("the default app.css must carry the progress strip and no page-loading rule")
	}
	WithPageLoading(sweepAssetsTopics{})(ds)
	if css := ds.AppCSS(); strings.Contains(css, "fui-nav-progress") || !strings.Contains(css, "[data-fui-page-loading]") {
		t.Fatal("with WithPageLoading, app.css must carry the page-loading rule and no progress strip")
	}
}

func TestSweepAssetsInterceptCSSOnlyWhenIntercepting(t *testing.T) {
	overlay := strings.TrimSpace(app.InterceptOverlayCSS())
	if overlay == "" {
		t.Fatal("setup: empty overlay CSS")
	}
	plain := app.NewApp("no-intercept")
	plain.Register("/products", &testHomeComp{}, nil)
	if strings.Contains(New(plain).AppCSS(), overlay) {
		t.Error("an app with no intercepting route shipped the overlay CSS")
	}
	ix := app.NewApp("intercept")
	ix.Register("/products", &testHomeComp{}, nil)
	ix.Register("/products/:id", &docRoomComp{}, nil, app.InterceptFrom("/products", app.ScreenDrawer))
	if !strings.Contains(New(ix).AppCSS(), overlay) {
		t.Error("an app with an intercepting route lacks the overlay CSS")
	}
}

func TestSweepAssetsLateContributeWarns(t *testing.T) {
	style.ResetRegistryForTest()
	t.Cleanup(style.ResetRegistryForTest)
	ds := newTestUIHost()
	ds.appCSSCached()
	var logs bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	defer slog.SetDefault(prev)
	style.Contribute(func(ss *style.StyleSheet) { ss.Rule(".sweep-late").Set("color", "red").End() })
	body, _ := ds.appCSSCached()
	ds.appCSSCached()
	if strings.Contains(body, ".sweep-late") {
		t.Error("app.css was recomposed after the first render")
	}
	if n := strings.Count(logs.String(), "style.Contribute called after the first page render"); n != 1 {
		t.Errorf("want one late-Contribute warning, got %d:\n%s", n, logs.String())
	}
}

func TestSweepAssetsBundleFollowsTheRequestTheme(t *testing.T) {
	registry.IsolateForTest(t)
	ds := hostWithTheme(t, style.DefaultTheme())
	paint := func(th style.Theme) string { return ".x{color:" + th.Colors.Primary.Value + "}" }
	a := registry.RegisterStyle(uniqueName("sweep-bundle-a"), paint)
	b := registry.RegisterStyle(uniqueName("sweep-bundle-b"), paint)
	key := ds.RegisterThemeVariant(brandTheme("#0d9488"))
	rec := httptest.NewRecorder()
	ds.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/__gofastr/comp-bundle.css?names="+a.Name()+","+b.Name()+"&t="+key, nil))
	if rec.Code != http.StatusOK || !strings.Contains(strings.ToLower(rec.Body.String()), "#0d9488") {
		t.Fatalf("the bundle under ?t= did not render the variant's palette (%d):\n%s", rec.Code, rec.Body.String())
	}
}

// A variant equal to the app theme leaves the app theme's component cache warm when released.
func TestSweepAssetsReleaseKeepsAppThemeCacheForSameTheme(t *testing.T) {
	registry.IsolateForTest(t)
	appTheme := brandTheme("#4f46e5")
	ds := hostWithTheme(t, appTheme)
	entry := registry.RegisterStyle(uniqueName("sweep-keep"), func(style.Theme) string { return ".k{color:blue}" }).Entry()
	entry.CSSFor(appTheme)
	ds.ReleaseThemeVariant(ds.RegisterThemeVariant(appTheme))
	if !entry.HasCachedTheme(style.ThemeHash(appTheme)) {
		t.Fatal("releasing a variant equal to the app theme evicted the app theme's component CSS")
	}
}

func TestSweepAssetsAssetURLLeavesUncleanPath(t *testing.T) {
	ds, _, _ := newAssetDirHost(t)
	if got := ds.AssetURL("/./logo.svg"); got != "/./logo.svg" {
		t.Fatalf("AssetURL(%q) = %q, want it unchanged", "/./logo.svg", got)
	}
}

func TestSweepAssetsAppIconCropsNotStretches(t *testing.T) {
	src := stdimage.NewNRGBA(stdimage.Rect(0, 0, 300, 100))
	for y := range 100 {
		for x := range 300 {
			c := color.NRGBA{B: 255, A: 255}
			if x >= 100 && x < 200 {
				c = color.NRGBA{R: 255, A: 255}
			}
			src.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, src); err != nil {
		t.Fatal(err)
	}
	ds := newAppIconHost(t, WithAppIcon(buf.Bytes()))
	icon, err := png.Decode(bytes.NewReader(ds.AppIconAssets()["/__gofastr/icons/icon-32.png"]))
	if err != nil {
		t.Fatal(err)
	}
	if r, _, b, _ := icon.At(1, 16).RGBA(); r < b {
		t.Fatal("the icon's left edge is not the source's red centre: the source was stretched, not centre-cropped")
	}
}

func TestSweepAssetsAppIconAssetsCarryFavicon(t *testing.T) {
	ds := newAppIconHost(t, WithAppIcon(testIconPNG(t, 64, 64)))
	assets := ds.AppIconAssets()
	if fav := assets["/favicon.ico"]; len(fav) == 0 || !bytes.Equal(fav, assets["/__gofastr/icons/icon-32.png"]) {
		t.Fatal("AppIconAssets lacks a /favicon.ico equal to the 32 px icon")
	}
}

func TestSweepAssetsBundleImmutableOnlyAtItsVersion(t *testing.T) {
	registry.IsolateForTest(t)
	a, b := registerTestStyle(t, "sweep-v-a"), registerTestStyle(t, "sweep-v-b")
	ds := newTestUIHostForMany(a, b)
	theme := ds.activeTheme()
	names := []string{a.Name(), b.Name()}
	sort.Strings(names)
	current := liveSheetURL(names, theme, style.ThemeHash(theme))
	get := func(u string) string {
		rec := httptest.NewRecorder()
		ds.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, u, nil))
		return rec.Header().Get("Cache-Control")
	}
	if cc := get(current); cc != immutableCacheControl {
		t.Fatalf("control: the bundle at its own version is %q, want immutable", cc)
	}
	stale := "/__gofastr/comp-bundle.css?names=" + strings.Join(names, ",") + "&v=0123456789ab"
	if cc := get(stale); cc != "no-cache" {
		t.Fatalf("the bundle at a stale version is %q, want no-cache", cc)
	}
}

func TestSweepAssetsResolvePredicateCoversEveryBranch(t *testing.T) {
	staticDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(staticDir, ".env"), []byte("SECRET=1"), 0o600); err != nil {
		t.Fatal(err)
	}
	a := app.NewApp("resolve-branches")
	a.SetDefaultLayout(chromeTestLayout("main", &testHeaderComp{}, nil, &testFooterComp{}))
	a.RegisterScreen(app.NewScreen("/docs/", &testHomeComp{}).WithTitle("Docs"), nil)
	ds := New(a, WithStaticDir(staticDir))
	ds.SetStaticFS(fstest.MapFS{"js/app.js": {Data: []byte("1")}, "sub/x.txt": {Data: []byte("x")}})
	ds.Mount(router.New())
	for _, p := range []string{"/docs", "/js/app.js", "/sub", "/.env", "/nope"} {
		req := httptest.NewRequest(http.MethodGet, p, nil)
		want := ds.resolvesStaticOrScreen(req)
		rec := httptest.NewRecorder()
		ds.serveOrRender(rec, req)
		if got := rec.Code != http.StatusNotFound; got != want {
			t.Errorf("%s: predicate %v, serveOrRender %d", p, want, rec.Code)
		}
	}
}

func TestSweepAssetsSessionCookieIsHttpOnly(t *testing.T) {
	for _, target := range []string{"https://app.example/", "http://localhost/"} {
		ds := newTestUIHost()
		rec := httptest.NewRecorder()
		ds.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, target+"__gofastr/session", nil))
		if c := rec.Header().Get("Set-Cookie"); !strings.Contains(c, "HttpOnly") {
			t.Errorf("session cookie for %s is not HttpOnly: %q", target, c)
		}
	}
}
