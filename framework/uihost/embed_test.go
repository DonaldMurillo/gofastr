package uihost

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/render"
	fembed "github.com/DonaldMurillo/gofastr/framework/embed"
)

// embedSubjectComp renders whoever the request is authenticated as. It is how
// the tests below tell "rendered as the granted subject" apart from "rendered
// anonymously" — which is the difference between an authenticated embed and a
// public one, and the thing a cookie leak or a fail-open resolver would break.
type embedSubjectComp struct{}

func (c *embedSubjectComp) Render() render.HTML { return c.RenderCtx(context.Background()) }

func (c *embedSubjectComp) RenderCtx(ctx context.Context) render.HTML {
	viewer := "anonymous"
	if u, ok := handler.GetUser(ctx); ok {
		if s, ok := u.(string); ok {
			viewer = s
		}
	}
	// Also report whether a cookie was still visible on the request this
	// screen rendered from. That is what "embed routes do not honour cookies"
	// has to mean in practice: not merely that the handler ignores one, but
	// that no code downstream of it can see one either.
	cookie := "none"
	if r := app.RequestFromContext(ctx); r != nil && r.Header.Get("Cookie") != "" {
		cookie = "present"
	}
	// And whether the screen can see the grant that authorised this render.
	// A screen branching on "am I embedded" reads this; if it is absent on the
	// first render but present on the island RPCs that follow, the screen
	// renders one thing and then swaps to another.
	grant := "absent"
	if g, ok := fembed.GrantFromContext(ctx); ok {
		grant = "present/" + g.Surface + "/" + strings.Join(g.Scopes, ",")
	}
	return render.HTML("<p>viewer:" + viewer + "</p><p>cookie:" + cookie + "</p><p>grant:" + grant + "</p>")
}

// Actions makes this an InteractiveComponent, so the host compiles action JS
// for it. Without at least one action-bearing component in the fixture,
// GetActionJS() is empty and any test asserting the frame receives the action
// bundle passes no matter what the frame is served.
func (c *embedSubjectComp) Actions() {
	component.On("embed-probe", func(_ *component.ComponentContext) {})
}

// embedTestScreen is a fembed.Screen for uihost tests that build a fembed.Host
// directly (theme-resolution tests) without registering a real *app.Screen or
// running the boot walk. *app.Screen is what production surfaces carry.
type embedTestScreen struct{ path string }

func (s embedTestScreen) RoutePath() string { return s.path }

type embedFixture struct {
	host    *UIHost
	embed   *fembed.Host
	surface string
}

const (
	embedTestOrigin  = "https://acme.com"
	embedTestOrigin2 = "https://shop.acme.com"
)

func newEmbedFixture(t *testing.T, mutate ...func(*fembed.Config)) embedFixture {
	t.Helper()
	application := app.NewApp("Embed Test")
	application.SetDefaultLayout(app.NewLayout("main").WithHeader(&testHeaderComp{}))
	reportsScreen := app.NewScreen("/reports", &embedSubjectComp{}).WithTitle("Reports")
	otherScreen := app.NewScreen("/other", &embedSubjectComp{}).WithTitle("Other")
	application.RegisterScreen(reportsScreen, nil)
	application.RegisterScreen(otherScreen, nil)

	cfg := fembed.Config{
		Surfaces: []fembed.Surface{
			{
				Name:    "reports",
				Screen:  reportsScreen,
				Origins: []string{embedTestOrigin, embedTestOrigin2},
				Scopes:  []string{"read"},
				Theme:   fembed.ThemeConfig{AllowTokens: []string{"color-primary"}, MaxVariants: 2},
			},
			{Name: "other", Screen: otherScreen, Origins: []string{embedTestOrigin}},
		},
		BurnStore: fembed.NewMemoryBurnStore(),
		Resolve: func(_ context.Context, subject string) (any, error) {
			return subject, nil
		},
	}
	for _, m := range mutate {
		m(&cfg)
	}
	eh, err := fembed.New(cfg)
	if err != nil {
		t.Fatalf("embed.New: %v", err)
	}
	eh.SetKeys([]byte("nonce-key-nonce-key-nonce-key-32"), []byte("grant-key-grant-key-grant-key-32"))

	return embedFixture{host: New(application, WithEmbed(eh)), embed: eh, surface: "reports"}
}

func (f embedFixture) do(t *testing.T, method, path string, body string, mutate ...func(*http.Request)) *httptest.ResponseRecorder {
	t.Helper()
	var req *http.Request
	if body != "" {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	for _, m := range mutate {
		m(req)
	}
	rec := httptest.NewRecorder()
	f.host.ServeHTTP(rec, req)
	return rec
}

// grantFor runs the whole handshake and returns the frame's credential.
func (f embedFixture) grantFor(t *testing.T, surface string) string {
	t.Helper()
	nonce, err := f.embed.MintNonce(context.Background(), surface, "user-7", embedTestOrigin, nil)
	if err != nil {
		t.Fatalf("MintNonce: %v", err)
	}
	body, _ := json.Marshal(map[string]string{"token": nonce, "origin": embedTestOrigin})
	rec := f.do(t, http.MethodPost, "/__gofastr/embed-exchange", string(body))
	if rec.Code != http.StatusOK {
		t.Fatalf("exchange: status %d, body %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Grant       string `json:"grant"`
		ExpiresInMS int64  `json:"expires_in_ms"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode grant: %v", err)
	}
	if out.Grant == "" || out.ExpiresInMS <= 0 {
		t.Fatalf("exchange returned an unusable grant: %+v", out)
	}
	return out.Grant
}

// ---------------------------------------------------------------------------
// Framing
// ---------------------------------------------------------------------------

// The shell must list EVERY allowed origin, not the one that framed it: no
// Origin header is sent on a navigation GET, so the server cannot know who the
// framer is when the header is written.
func TestEmbedShellFramingHeaders(t *testing.T) {
	f := newEmbedFixture(t)
	rec := f.do(t, http.MethodGet, "/__gofastr/embed/reports", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}

	csp := rec.Header().Get("Content-Security-Policy")
	ancestors := frameAncestorsOf(t, csp)
	for _, o := range []string{embedTestOrigin, embedTestOrigin2} {
		if !strings.Contains(ancestors, o) {
			t.Errorf("frame-ancestors %q omits allowed origin %q", ancestors, o)
		}
	}
	// Exact origins only — no wildcard, and not the app's own 'none' default.
	if strings.Contains(ancestors, "'none'") || strings.Contains(ancestors, "*") {
		t.Errorf("frame-ancestors must name exact origins, got %q", ancestors)
	}
	if xfo := rec.Header().Get("X-Frame-Options"); xfo != "" {
		t.Errorf("X-Frame-Options = %q — it has no allowlist mode, so it must be removed or it overrides the CSP", xfo)
	}
	if corp := rec.Header().Get("Cross-Origin-Resource-Policy"); corp != "cross-origin" {
		t.Errorf("Cross-Origin-Resource-Policy = %q, want cross-origin", corp)
	}
}

// A surface's own origins must not leak into another surface's policy.
func TestEmbedShellFramingIsPerSurface(t *testing.T) {
	f := newEmbedFixture(t)
	rec := f.do(t, http.MethodGet, "/__gofastr/embed/other", "")
	csp := rec.Header().Get("Content-Security-Policy")
	if strings.Contains(csp, embedTestOrigin2) {
		t.Fatalf("surface \"other\" allows only %q but its CSP names %q: %s", embedTestOrigin, embedTestOrigin2, csp)
	}
}

func TestEmbedShellCarriesRuntimeAndConfig(t *testing.T) {
	f := newEmbedFixture(t)
	body := f.do(t, http.MethodGet, "/__gofastr/embed/reports", "").Body.String()

	for _, want := range []string{
		`<meta name="gofastr-embed"`,
		`id="gofastr-embed-root"`,
		embedRuntimePath,
		`content="noindex, nofollow"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("shell is missing %q:\n%s", want, body)
		}
	}
	// The shell must NOT ship the full runtime: nav lives there, and SPA
	// navigation inside a frame is supposed to be impossible.
	if strings.Contains(body, `"/__gofastr/runtime.js"`) {
		t.Error("the shell loads the FULL runtime — the embed composition exists precisely to omit nav")
	}
	// And it must be content-free. The surface's content is fetched with the
	// grant; anything server-rendered here would be rendered anonymously.
	if strings.Contains(body, "viewer:") {
		t.Error("the shell server-rendered the surface content — it is fetched by a credential-less navigation, so that content would be anonymous")
	}
}

// The shell must carry the component catalog. Without it the kernel's CSS
// scanner cannot resolve a data-fui-comp marker to a stylesheet URL, so the
// frame renders every component's MARKUP with none of its CSS — cards lose
// their surface, grids collapse to one column, stat cards become bare
// paragraphs. Every element is present, so a DOM assertion sees nothing wrong;
// this was caught in a screenshot.
func TestEmbedShellCarriesTheComponentCatalog(t *testing.T) {
	f := newEmbedFixture(t)
	body := f.do(t, http.MethodGet, "/__gofastr/embed/reports", "").Body.String()
	if !strings.Contains(body, `id="gofastr-catalog"`) {
		t.Fatalf("the shell ships no component catalog — components inside the frame would render unstyled:\n%s", body)
	}
	// The runtime module manifest rides along for the same reason: without it
	// loadModule has no cache-busting version for a demand-loaded module.
	if !strings.Contains(body, `id="gofastr-runtime-modules"`) {
		t.Errorf("the shell ships no runtime module manifest:\n%s", body)
	}
}

func TestEmbedUnknownSurfaceIs404(t *testing.T) {
	f := newEmbedFixture(t)
	for _, path := range []string{
		"/__gofastr/embed/nope",
		"/__gofastr/embed/nope/content",
	} {
		if rec := f.do(t, http.MethodGet, path, ""); rec.Code != http.StatusNotFound {
			t.Errorf("%s: status %d, want 404", path, rec.Code)
		}
	}
}

// An app that hands out no pieces of itself must serve no embed surface at all
// — not even a 404 that confirms the feature exists in this build.
func TestEmbedRoutesAbsentWithoutConfig(t *testing.T) {
	application := app.NewApp("No Embed")
	application.RegisterScreen(app.NewScreen("/", &embedSubjectComp{}).WithTitle("Home"), nil)
	ds := New(application)

	for _, path := range []string{
		"/__gofastr/embed.js",
		"/__gofastr/embed-runtime.js",
		"/__gofastr/embed/reports",
	} {
		rec := httptest.NewRecorder()
		ds.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code == http.StatusOK {
			t.Errorf("%s answered 200 on an app with no embed host", path)
		}
	}
}

// ---------------------------------------------------------------------------
// Cookies
// ---------------------------------------------------------------------------

// The whole design assumes identity can only arrive explicitly. Inside a
// cross-site frame no cookie is sent, but an app framed by its OWN site
// (app.acme.com inside www.acme.com) is same-site and a Strict cookie does ride
// along. Honouring it there would hand a signed-in user's full session to a
// third party's frame, so every embed route discards cookies before reading
// anything.
func TestEmbedIgnoresSessionCookie(t *testing.T) {
	f := newEmbedFixture(t)

	// A real, valid session for this host.
	sess := f.host.CreateSession()
	withCookie := func(req *http.Request) {
		req.AddCookie(&http.Cookie{Name: sessionCookieSecureName, Value: sess.Token})
		req.AddCookie(&http.Cookie{Name: sessionCookieDevName, Value: sess.Token})
	}

	// The content route is the one that carries identity. With a session
	// cookie and NO grant it must still refuse.
	rec := f.do(t, http.MethodGet, "/__gofastr/embed/reports/content", "", withCookie)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("content with a valid session cookie but no grant: status %d, want 401 — the cookie was honoured", rec.Code)
	}

	// And with a grant, the rendered identity must come from the GRANT, not
	// from the cookie's session.
	grant := f.grantFor(t, "reports")
	rec = f.do(t, http.MethodGet, "/__gofastr/embed/reports/content", "", withCookie, func(req *http.Request) {
		req.Header.Set(embedGrantHeader, grant)
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("content with grant: status %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "viewer:user-7") {
		t.Fatalf("content did not render as the granted subject:\n%s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "cookie:none") {
		t.Fatalf("the screen still saw a Cookie header — an embed route must DISCARD the credential, not merely decline to read it:\n%s", rec.Body.String())
	}

	// No embed route may mint or refresh a session.
	for _, path := range []string{"/__gofastr/embed/reports", "/__gofastr/embed/reports/content"} {
		rec := f.do(t, http.MethodGet, path, "", withCookie, func(req *http.Request) {
			req.Header.Set(embedGrantHeader, grant)
		})
		if sc := rec.Header().Get("Set-Cookie"); sc != "" {
			t.Errorf("%s set a cookie on an embed route: %q", path, sc)
		}
	}
}

// ---------------------------------------------------------------------------
// Exchange
// ---------------------------------------------------------------------------

// The exchange spends a single-use nonce, so it must be unreachable by anything
// that fires on navigation, prefetch, or an <img> — all of which are GETs.
func TestEmbedExchangeRejectsGET(t *testing.T) {
	f := newEmbedFixture(t)
	for _, path := range []string{"/__gofastr/embed-exchange", "/__gofastr/embed-refresh"} {
		rec := f.do(t, http.MethodGet, path, "")
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("GET %s: status %d, want 405", path, rec.Code)
		}
	}
}

// A prefetched iframe, a double-mounted loader and a page refresh all fire the
// exchange twice. If the second attempt failed, the feature would surface as
// "the embed randomly doesn't load".
func TestEmbedExchangeIsIdempotent(t *testing.T) {
	f := newEmbedFixture(t)
	nonce, err := f.embed.MintNonce(context.Background(), "reports", "user-7", embedTestOrigin, nil)
	if err != nil {
		t.Fatalf("MintNonce: %v", err)
	}
	body, _ := json.Marshal(map[string]string{"token": nonce, "origin": embedTestOrigin})

	first := f.do(t, http.MethodPost, "/__gofastr/embed-exchange", string(body))
	second := f.do(t, http.MethodPost, "/__gofastr/embed-exchange", string(body))
	if first.Code != http.StatusOK || second.Code != http.StatusOK {
		t.Fatalf("statuses %d / %d, want 200 / 200", first.Code, second.Code)
	}
	// Compare the GRANT, not the whole body. The body carries expires_in_ms,
	// derived from the wall clock at write time, so two sequential exchanges
	// straddling a millisecond differ by one and this failed roughly once in a
	// thousand runs — reporting "one nonce bought two identities" for two
	// byte-identical grants. TestExchangeIsIdempotentOnTheGrant covers the
	// replay property itself, across a second boundary so it can actually fail.
	grantOf := func(rec *httptest.ResponseRecorder) string {
		t.Helper()
		var out struct {
			Grant string `json:"grant"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("decode: %v", err)
		}
		return out.Grant
	}
	if grantOf(first) != grantOf(second) {
		t.Fatalf("the second exchange returned a DIFFERENT grant — one nonce bought two identities:\n%s\n%s",
			first.Body.String(), second.Body.String())
	}
	if cc := first.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store — a grant is a bearer credential for one viewer", cc)
	}
}

// Which check failed is an oracle: it tells a caller probing with a captured
// nonce exactly how far they got. Every rejection answers identically.
func TestEmbedExchangeFailuresAreIndistinguishable(t *testing.T) {
	f := newEmbedFixture(t)
	good, err := f.embed.MintNonce(context.Background(), "reports", "user-7", embedTestOrigin, nil)
	if err != nil {
		t.Fatalf("MintNonce: %v", err)
	}
	// A nonce that is already spent AND past its grant window — the rejection
	// class the oracle matters most for, and the one this test used to set up
	// and then never exercise. GrantTTL is milliseconds here so the window
	// really closes.
	spentFixture := newEmbedFixture(t, func(c *fembed.Config) { c.GrantTTL = 20 * time.Millisecond })
	spent, err := spentFixture.embed.MintNonce(context.Background(), "reports", "user-7", embedTestOrigin, nil)
	if err != nil {
		t.Fatalf("MintNonce: %v", err)
	}
	if _, err := spentFixture.embed.Exchange(context.Background(), spent, embedTestOrigin); err != nil {
		t.Fatalf("pre-spend: %v", err)
	}
	time.Sleep(50 * time.Millisecond)

	cases := map[string]string{
		"garbage":             "not-a-token",
		"wrong prefix":        "emg_" + strings.TrimPrefix(good, "emb_"),
		"tampered":            good[:len(good)-2] + "xy",
		"wrong framed origin": good,
		"already used":        spent,
	}
	var bodies []string
	for name, tok := range cases {
		origin := embedTestOrigin
		if name == "wrong framed origin" {
			origin = "https://evil.example"
		}
		target := f
		if name == "already used" {
			target = spentFixture
		}
		body, _ := json.Marshal(map[string]string{"token": tok, "origin": origin})
		rec := target.do(t, http.MethodPost, "/__gofastr/embed-exchange", string(body))
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s: status %d, want 401", name, rec.Code)
		}
		bodies = append(bodies, rec.Body.String())
	}
	for i := 1; i < len(bodies); i++ {
		if bodies[i] != bodies[0] {
			t.Fatalf("rejection bodies differ — the response distinguishes WHICH check failed:\n%q\n%q", bodies[0], bodies[i])
		}
	}
}

// ---------------------------------------------------------------------------
// Content
// ---------------------------------------------------------------------------

func TestEmbedContentRequiresAGrant(t *testing.T) {
	f := newEmbedFixture(t)

	if rec := f.do(t, http.MethodGet, "/__gofastr/embed/reports/content", ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("no grant: status %d, want 401", rec.Code)
	}

	// A nonce is not a grant. Presenting one must fail even though it verifies
	// under the OTHER key.
	nonce, err := f.embed.MintNonce(context.Background(), "reports", "user-7", embedTestOrigin, nil)
	if err != nil {
		t.Fatalf("MintNonce: %v", err)
	}
	rec := f.do(t, http.MethodGet, "/__gofastr/embed/reports/content", "", func(req *http.Request) {
		req.Header.Set(embedGrantHeader, nonce)
	})
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("nonce presented as a grant: status %d, want 401", rec.Code)
	}
}

// A grant names its surface and so does the URL. If they can disagree, one
// surface's credential reads another's screen.
func TestEmbedContentRejectsCrossSurfaceGrant(t *testing.T) {
	f := newEmbedFixture(t)
	grant := f.grantFor(t, "reports")

	rec := f.do(t, http.MethodGet, "/__gofastr/embed/other/content", "", func(req *http.Request) {
		req.Header.Set(embedGrantHeader, grant)
	})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("a \"reports\" grant read the \"other\" surface: status %d, want 404", rec.Code)
	}
	// And it must be indistinguishable from a surface that does not exist, or
	// a grant holder can enumerate which names are real.
	unknown := f.do(t, http.MethodGet, "/__gofastr/embed/no-such-thing/content", "", func(req *http.Request) {
		req.Header.Set(embedGrantHeader, grant)
	})
	if unknown.Code != rec.Code || unknown.Body.String() != rec.Body.String() {
		t.Fatalf("a wrong-surface grant (%d %q) answers differently from an unknown surface (%d %q)",
			rec.Code, rec.Body.String(), unknown.Code, unknown.Body.String())
	}
}

func TestEmbedContentIsChromeLessAndUncacheable(t *testing.T) {
	f := newEmbedFixture(t)
	grant := f.grantFor(t, "reports")
	rec := f.do(t, http.MethodGet, "/__gofastr/embed/reports/content", "", func(req *http.Request) {
		req.Header.Set(embedGrantHeader, grant)
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()

	if strings.Contains(body, "Test Header") {
		t.Errorf("embedded content carries the site header — the embed layout is supposed to be chrome-less:\n%s", body)
	}
	if n := strings.Count(body, "<main"); n != 1 {
		t.Errorf("content has %d <main> landmarks, want exactly 1 (a nested main is an axe violation):\n%s", n, body)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store — the content is rendered for one subject", cc)
	}
}

// A resolver that cannot find the subject means the identity is gone (deleted,
// disabled, store outage). Rendering anonymously there would silently downgrade
// an authenticated embed into a public one.
func TestEmbedContentFailsClosedOnResolverError(t *testing.T) {
	f := newEmbedFixture(t, func(c *fembed.Config) {
		c.Resolve = func(context.Context, string) (any, error) {
			return nil, context.DeadlineExceeded
		}
	})
	grant := f.grantFor(t, "reports")
	rec := f.do(t, http.MethodGet, "/__gofastr/embed/reports/content", "", func(req *http.Request) {
		req.Header.Set(embedGrantHeader, grant)
	})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status %d, want 403 — an unresolvable subject must not render anonymously", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "viewer:") {
		t.Fatal("content rendered despite the resolver failing")
	}
}

// ---------------------------------------------------------------------------
// Refresh
// ---------------------------------------------------------------------------

func TestEmbedRefreshRollsTheGrantForward(t *testing.T) {
	f := newEmbedFixture(t)
	grant := f.grantFor(t, "reports")

	rec := f.do(t, http.MethodPost, "/__gofastr/embed-refresh", "", func(req *http.Request) {
		req.Header.Set(embedGrantHeader, grant)
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("refresh: status %d: %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Grant       string `json:"grant"`
		ExpiresInMS int64  `json:"expires_in_ms"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.Grant == "" || out.ExpiresInMS <= 0 {
		t.Fatalf("unusable refreshed grant: %+v", out)
	}
	// The refreshed grant must actually work.
	content := f.do(t, http.MethodGet, "/__gofastr/embed/reports/content", "", func(req *http.Request) {
		req.Header.Set(embedGrantHeader, out.Grant)
	})
	if content.Code != http.StatusOK {
		t.Fatalf("refreshed grant did not authenticate: status %d", content.Code)
	}

	// A nonce cannot be refreshed into a grant.
	nonce, err := f.embed.MintNonce(context.Background(), "reports", "user-7", embedTestOrigin, nil)
	if err != nil {
		t.Fatalf("MintNonce: %v", err)
	}
	bad := f.do(t, http.MethodPost, "/__gofastr/embed-refresh", "", func(req *http.Request) {
		req.Header.Set(embedGrantHeader, nonce)
	})
	if bad.Code != http.StatusUnauthorized {
		t.Fatalf("refreshing a nonce: status %d, want 401", bad.Code)
	}
}

// ---------------------------------------------------------------------------
// Theme
// ---------------------------------------------------------------------------

func embedThemeParam(t *testing.T, tokens map[string]string) string {
	t.Helper()
	raw, err := json.Marshal(tokens)
	if err != nil {
		t.Fatalf("marshal theme: %v", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}

func TestEmbedThemeAppliesOnlyAllowedTokens(t *testing.T) {
	f := newEmbedFixture(t)

	plain := f.do(t, http.MethodGet, "/__gofastr/embed/reports", "").Body.String()
	if strings.Contains(plain, "app.css?t=") {
		t.Fatalf("an unthemed shell linked a theme variant:\n%s", plain)
	}

	themed := f.do(t, http.MethodGet,
		"/__gofastr/embed/reports?theme="+embedThemeParam(t, map[string]string{"color-primary": "#123456"}), "").Body.String()
	if !strings.Contains(themed, "app.css?t=") {
		t.Fatalf("an allowed token did not produce a theme variant:\n%s", themed)
	}

	// A token the surface does not allow must not move a pixel.
	notAllowed := f.do(t, http.MethodGet,
		"/__gofastr/embed/reports?theme="+embedThemeParam(t, map[string]string{"color-danger": "#ff0000"}), "").Body.String()
	if strings.Contains(notAllowed, "app.css?t=") {
		t.Fatalf("a token outside AllowTokens produced a theme variant:\n%s", notAllowed)
	}

	// A surface with no AllowTokens is not re-themable at all.
	other := f.do(t, http.MethodGet,
		"/__gofastr/embed/other?theme="+embedThemeParam(t, map[string]string{"color-primary": "#123456"}), "").Body.String()
	if strings.Contains(other, "app.css?t=") {
		t.Fatalf("a surface with an empty AllowTokens was re-themed:\n%s", other)
	}
}

// Every distinct theme is a fresh render plus a component-CSS cache miss, so an
// uncapped registry is cheap amplification. The cap bounds how many a surface
// HOLDS, not how many it will ever serve: the shell route is unauthenticated by
// necessity (a frame is fetched by a navigation), so refusing at the cap let any
// stranger fill the slots and lock the real customer out of their own branding
// for the life of the process. It evicts the least recently used instead.
func TestEmbedThemeVariantsAreCapped(t *testing.T) {
	f := newEmbedFixture(t) // reports declares MaxVariants: 2

	for _, hex := range []string{"#111111", "#222222", "#333333", "#444444"} {
		body := f.do(t, http.MethodGet,
			"/__gofastr/embed/reports?theme="+embedThemeParam(t, map[string]string{"color-primary": hex}), "").Body.String()
		if !strings.Contains(body, "app.css?t=") {
			t.Fatalf("theme %s was refused — the cap must evict, not lock the surface out:\n%s", hex, body)
		}
	}
	if got := f.host.ThemeVariantCount(); got > 2 {
		t.Fatalf("the registry holds %d variants after 4 distinct themes against a cap of 2 — eviction did not release the underlying registration", got)
	}
}

// The lockout the eviction policy exists to prevent: a stranger presenting the
// cap's worth of themes must not stop the customer's own theme from being
// served on the next page load.
func TestEmbedThemeCapDoesNotLockOutTheCustomer(t *testing.T) {
	f := newEmbedFixture(t) // MaxVariants: 2
	for i := 0; i < 32; i++ {
		f.do(t, http.MethodGet, "/__gofastr/embed/reports?theme="+
			embedThemeParam(t, map[string]string{"color-primary": fmt.Sprintf("#%06x", i+1)}), "")
	}
	body := f.do(t, http.MethodGet, "/__gofastr/embed/reports?theme="+
		embedThemeParam(t, map[string]string{"color-primary": "#ff6600"}), "").Body.String()
	if !strings.Contains(body, "app.css?t=") {
		t.Fatal("after 32 anonymous themes the customer's own theme was refused")
	}
	if got := f.host.ThemeVariantCount(); got > 2 {
		t.Fatalf("the registry grew to %d despite the cap of 2", got)
	}
}

func TestEmbedThemeIgnoresMalformedInput(t *testing.T) {
	f := newEmbedFixture(t)
	for _, param := range []string{
		"not-base64!!",
		base64.RawURLEncoding.EncodeToString([]byte("{not json")),
		embedThemeParam(t, map[string]string{"color-primary": "red; --x:}"}),
		embedThemeParam(t, map[string]string{}),
	} {
		rec := f.do(t, http.MethodGet, "/__gofastr/embed/reports?theme="+param, "")
		if rec.Code != http.StatusOK {
			t.Errorf("theme=%q: status %d, want 200 — a bad brand config should degrade to the app theme, not break the embed", param, rec.Code)
		}
		if strings.Contains(rec.Body.String(), "app.css?t=") {
			t.Errorf("theme=%q produced a variant", param)
		}
	}
}

// The token values reach CSS. Whatever ApplyTokens accepts must not be able to
// escape its declaration.
func TestEmbedThemeVariantCSSIsWellFormed(t *testing.T) {
	f := newEmbedFixture(t)
	body := f.do(t, http.MethodGet,
		"/__gofastr/embed/reports?theme="+embedThemeParam(t, map[string]string{"color-primary": "#abcdef"}), "").Body.String()

	idx := strings.Index(body, "app.css?t=")
	if idx < 0 {
		t.Fatalf("no variant link:\n%s", body)
	}
	rest := body[idx+len("app.css?t="):]
	key := rest[:strings.IndexByte(rest, '"')]

	rec := getAppCSS(t, f.host, "t="+key)
	css := rec.Body.String()
	if !strings.Contains(css, "#abcdef") {
		t.Fatalf("the variant CSS does not carry the customer's colour:\n%s", css[:min(600, len(css))])
	}
	if strings.Count(css, "{") != strings.Count(css, "}") {
		t.Fatal("the variant CSS has unbalanced braces — a value escaped its declaration")
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// ---------------------------------------------------------------------------
// Assets
// ---------------------------------------------------------------------------

// The loader is fetched BY a customer's page, so it is a cross-origin
// subresource: the app's default CORP would block it.
func TestEmbedLoaderIsCrossOriginReadable(t *testing.T) {
	f := newEmbedFixture(t)
	rec := f.do(t, http.MethodGet, "/__gofastr/embed.js", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	if got := rec.Header().Get("Cross-Origin-Resource-Policy"); got != "cross-origin" {
		t.Errorf("Cross-Origin-Resource-Policy = %q, want cross-origin", got)
	}
	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want nosniff", got)
	}
	if !strings.Contains(rec.Body.String(), "gofastr-embed/1") {
		t.Error("the loader does not carry the handshake protocol marker")
	}
}

func TestEmbedRuntimeIsServed(t *testing.T) {
	f := newEmbedFixture(t)
	rec := f.do(t, http.MethodGet, "/__gofastr/embed-runtime.js", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "gofastr-embed/1") {
		t.Error("the embed runtime does not contain boot-embed")
	}
}

// A host with no signing keys cannot verify anything. It must say so rather
// than behave as though every token were fine.
func TestEmbedWithoutKeysRefuses(t *testing.T) {
	application := app.NewApp("Keyless")
	reportsScreen := app.NewScreen("/reports", &embedSubjectComp{}).WithTitle("Reports")
	application.RegisterScreen(reportsScreen, nil)
	eh, err := fembed.New(fembed.Config{
		Surfaces:  []fembed.Surface{{Name: "reports", Screen: reportsScreen, Origins: []string{embedTestOrigin}}},
		BurnStore: fembed.NewMemoryBurnStore(),
	})
	if err != nil {
		t.Fatalf("embed.New: %v", err)
	}
	ds := New(application, WithEmbed(eh))

	for _, tc := range []struct{ method, path string }{
		{http.MethodPost, "/__gofastr/embed-exchange"},
		{http.MethodPost, "/__gofastr/embed-refresh"},
		{http.MethodGet, "/__gofastr/embed/reports/content"},
		{http.MethodGet, "/__gofastr/embed/reports"},
	} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(`{}`))
		req.Header.Set("Content-Type", "application/json")
		ds.ServeHTTP(rec, req)
		if rec.Code != http.StatusServiceUnavailable {
			t.Errorf("%s %s: status %d, want 503", tc.method, tc.path, rec.Code)
		}
	}
}

var _ = style.DefaultTheme

// Session middleware runs BEFORE this handler, on the app's own router, and
// this package does not control that ordering — so stripCookies inside the
// handler is too late to stop a middleware from having already installed a user
// from a cookie. That happens for real in the same-site framing case
// (app.acme.com inside www.acme.com), where a Strict cookie really is sent.
// The grant must be the only identity an embedded surface can have.
func TestEmbedContentIgnoresAMiddlewareInstalledUser(t *testing.T) {
	f := newEmbedFixture(t, func(c *fembed.Config) {
		// No resolver: the grant carries a subject but nothing maps it to a
		// user, so the render path never overwrites the context. This is the
		// exact shape in which a leaked middleware identity would survive.
		c.Resolve = nil
	})
	grant := f.grantFor(t, "reports")

	// Stand in for the app's session middleware.
	impersonated := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := handler.SetUser(r.Context(), "attacker-from-cookie")
		f.host.ServeHTTP(w, r.WithContext(ctx))
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/__gofastr/embed/reports/content", nil)
	req.Header.Set(embedGrantHeader, grant)
	impersonated.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "attacker-from-cookie") {
		t.Fatalf("the surface rendered as the middleware's user — the grant is supposed to be the only identity here:\n%s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "viewer:anonymous") {
		t.Fatalf("expected an anonymous render with no resolver:\n%s", rec.Body.String())
	}
}

// The cap has to be consulted BEFORE registering, not before serving.
// RegisterThemeVariant stores unconditionally, so capping only the response
// would let a customer varying a token per page view grow the registry without
// bound while every response fell back to the app theme.
func TestEmbedThemeCapBoundsTheVariantRegistry(t *testing.T) {
	f := newEmbedFixture(t) // reports declares MaxVariants: 2

	for _, hex := range []string{"#111111", "#222222", "#333333", "#444444", "#555555", "#666666"} {
		f.do(t, http.MethodGet, "/__gofastr/embed/reports?theme="+embedThemeParam(t, map[string]string{"color-primary": hex}), "")
	}

	if registered := f.host.ThemeVariantCount(); registered > 2 {
		t.Fatalf("the theme registry holds %d variants after 6 distinct themes against a cap of 2 — the cap gates the response, not the registration", registered)
	}
}

// The cap has to survive a burst, not just a steady state.
//
// Checking it and registering cannot be one atomic step — registering renders
// CSS, far too slow to hold a lock across — so the slot is RESERVED before the
// render. Without the reservation, N concurrent requests carrying N distinct
// themes each read "under the cap" and each register, and a burst is how
// amplification arrives.
//
// In a simultaneous burst every slot is in flight, so there is nothing evictable
// and the cap refuses — which is also the branch that keeps eviction from
// stealing a reservation out from under a request that is mid-render.
//
// This drives embedThemeState directly rather than the HTTP handler, with an
// explicit gap between reserve and record. Going through the handler does not
// reliably produce the interleaving: the work between the two calls is
// microseconds, so the goroutines serialise and the test passes with the bug
// present. Making the window explicit is the difference between testing the
// invariant and hoping for a schedule.
func TestEmbedThemeCapHoldsUnderConcurrency(t *testing.T) {
	const (
		racers = 40
		max    = 2
	)
	var state embedThemeState
	var admitted int32
	var wg sync.WaitGroup
	start := make(chan struct{})

	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			param := fmt.Sprintf("theme-%d", i)
			<-start
			ok, _, _ := state.reserve("reports", param, max)
			if !ok {
				return
			}
			atomic.AddInt32(&admitted, 1)
			// Stand in for the CSS render that happens between reserving a slot
			// and knowing the variant key.
			time.Sleep(2 * time.Millisecond)
			state.record("reports", param, "key-"+param)
		}(i)
	}
	close(start)
	wg.Wait()

	if got := atomic.LoadInt32(&admitted); got != max {
		t.Fatalf("%d of %d concurrent distinct themes were admitted against a cap of %d — the cap is checked but not reserved", got, racers, max)
	}
	state.mu.Lock()
	held := len(state.resolved["reports"])
	state.mu.Unlock()
	if held != max {
		t.Fatalf("the resolution cache holds %d entries, want %d", held, max)
	}
}

// A rejected theme parameter must leave nothing behind. The parameter is
// attacker-chosen, so caching every rejection would replace one unbounded map
// with another — and consuming a slot per rejection would let malformed input
// lock a customer out of their own branding.
func TestEmbedThemeRejectionsConsumeNothing(t *testing.T) {
	f := newEmbedFixture(t) // MaxVariants: 2

	for i := 0; i < 50; i++ {
		bad := base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf("{not json %d", i)))
		f.do(t, http.MethodGet, "/__gofastr/embed/reports?theme="+bad, "")
	}

	// BOTH maps. reserve writes the attacker-supplied parameter into resolved
	// and into used; asserting on only one of them meant a leak in the other was
	// invisible here, and the whole property depends on neither growing.
	f.host.embedThemes.mu.Lock()
	cached := len(f.host.embedThemes.resolved["reports"])
	stamped := len(f.host.embedThemes.used["reports"])
	f.host.embedThemes.mu.Unlock()
	if cached != 0 || stamped != 0 {
		t.Errorf("50 rejected theme parameters left resolved=%d used=%d entries — "+
			"the bookkeeping is attacker-growable", cached, stamped)
	}

	// And the surface must still accept its full quota of real themes.
	good := 0
	for _, hex := range []string{"#111111", "#222222"} {
		body := f.do(t, http.MethodGet,
			"/__gofastr/embed/reports?theme="+embedThemeParam(t, map[string]string{"color-primary": hex}), "").Body.String()
		if strings.Contains(body, "app.css?t=") {
			good++
		}
	}
	if good != 2 {
		t.Fatalf("only %d of 2 valid themes were served after 50 rejections — rejections consumed the cap", good)
	}
}

// Relaxing the framing rule must not drop the rest of the policy. An embed
// document loads scripts and styles like any other page; it needs
// frame-ancestors widened and default-src / script-src / img-src left exactly
// as the app set them. Replacing the whole header would be a downgrade dressed
// up as a relaxation.
func TestEmbedFramingKeepsTheRestOfTheCSP(t *testing.T) {
	f := newEmbedFixture(t)
	rec := f.do(t, http.MethodGet, "/__gofastr/embed/reports", "")
	csp := rec.Header().Get("Content-Security-Policy")

	for _, directive := range []string{"default-src", "img-src", "object-src", "form-action", "base-uri"} {
		if !strings.Contains(csp, directive) {
			t.Errorf("the embed CSP lost the %s directive: %q", directive, csp)
		}
	}
	if strings.Count(csp, "frame-ancestors") != 1 {
		t.Errorf("expected exactly one frame-ancestors directive: %q", csp)
	}
	if strings.Contains(csp, "frame-ancestors 'none'") {
		t.Errorf("the app's frame-ancestors 'none' survived: %q", csp)
	}
}

// frameAncestorsOf returns just the frame-ancestors directive of a policy, so
// an assertion about framing cannot accidentally match another directive's
// value (object-src 'none' is not a framing statement).
func frameAncestorsOf(t *testing.T, policy string) string {
	t.Helper()
	for _, d := range strings.Split(policy, ";") {
		d = strings.TrimSpace(d)
		if strings.HasPrefix(d, "frame-ancestors") {
			return d
		}
	}
	t.Fatalf("no frame-ancestors directive in %q", policy)
	return ""
}

func TestWithFrameAncestors(t *testing.T) {
	origins := []string{"https://a.example", "https://b.example"}
	cases := []struct{ in, want string }{
		{"", "frame-ancestors https://a.example https://b.example"},
		{"default-src 'self'; frame-ancestors 'none'; img-src 'self' data:",
			"default-src 'self'; frame-ancestors https://a.example https://b.example; img-src 'self' data:"},
		{"default-src 'self'",
			"default-src 'self'; frame-ancestors https://a.example https://b.example"},
		// Directive names are case-insensitive; a policy spelling it
		// differently must still be replaced, not duplicated.
		{"default-src 'self'; Frame-Ancestors 'none'",
			"default-src 'self'; frame-ancestors https://a.example https://b.example"},
	}
	for _, c := range cases {
		if got := withFrameAncestors(c.in, origins); got != c.want {
			t.Errorf("withFrameAncestors(%q) =\n  %q\nwant\n  %q", c.in, got, c.want)
		}
	}
}

// None of the embed URLs is content-addressed, so none may carry a long
// max-age: a cached loader pins a customer's page to an old build — including
// past a security fix — and a cached shell demand-loads component CSS at hashes
// the new build no longer serves. Same policy as /__gofastr/runtime.js.
func TestEmbedAssetsAreNotLongCached(t *testing.T) {
	f := newEmbedFixture(t)
	for _, path := range []string{
		"/__gofastr/embed.js",
		"/__gofastr/embed-runtime.js",
		"/__gofastr/embed/reports",
	} {
		cc := f.do(t, http.MethodGet, path, "").Header().Get("Cache-Control")
		if cc != "no-cache" {
			t.Errorf("%s Cache-Control = %q, want no-cache — the URL is not content-addressed", path, cc)
		}
	}
	// Credentialed responses go further: never stored at all.
	grant := f.grantFor(t, "reports")
	content := f.do(t, http.MethodGet, "/__gofastr/embed/reports/content", "", func(req *http.Request) {
		req.Header.Set(embedGrantHeader, grant)
	})
	if cc := content.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("content Cache-Control = %q, want no-store", cc)
	}
}

// Infrastructure endpoints that gate on the session cookie have to accept an
// embed grant too. A frame never sends a cookie, so a cookie-only gate meant
// every widget inside an embed silently did nothing — the catalog 401'd and a
// modal trigger became dead DOM. And in a same-site framing an ambient cookie
// COULD satisfy it, so whether an embed's widgets worked depended on the
// viewer's unrelated app session.
func TestEmbedGrantOpensTheWidgetCatalog(t *testing.T) {
	f := newEmbedFixture(t)

	if rec := f.do(t, http.MethodGet, "/__gofastr/widgets?page=/reports", ""); rec.Code == http.StatusOK {
		t.Fatal("the widget catalog answered an anonymous request")
	}

	grant := f.grantFor(t, "reports")
	rec := f.do(t, http.MethodGet, "/__gofastr/widgets?page=/reports", "", func(req *http.Request) {
		req.Header.Set(embedGrantHeader, grant)
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("the widget catalog refused a valid grant: status %d", rec.Code)
	}

	// A forged grant must not open it.
	bad := f.do(t, http.MethodGet, "/__gofastr/widgets?page=/reports", "", func(req *http.Request) {
		req.Header.Set(embedGrantHeader, "emg_forged.forged")
	})
	if bad.Code == http.StatusOK {
		t.Fatal("the widget catalog accepted a forged grant")
	}
}

// The frame has to announce the SURFACE's app route. A widget scoped with
// .Pages("/reports") is not scoped to /__gofastr/embed/reports, so discovery
// keyed on the shell's own URL would exclude exactly the widgets the surface
// declared.
func TestEmbedShellAnnouncesTheSurfacePath(t *testing.T) {
	f := newEmbedFixture(t)
	body := f.do(t, http.MethodGet, "/__gofastr/embed/reports", "").Body.String()
	if !strings.Contains(body, "&#34;path&#34;:&#34;/reports&#34;") {
		t.Fatalf("the shell config does not carry the surface's app route:\n%s", body)
	}
}
