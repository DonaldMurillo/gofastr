package uihost

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/middleware"
	"github.com/DonaldMurillo/gofastr/core/router"
)

var (
	inlineBundleRe = regexp.MustCompile(`(?s)<style data-cui-bundle="([^"]*)">(.*?)</style>`)
	inlineAppRe    = regexp.MustCompile(`(?s)<style id="cui-app-css">(.*?)</style>`)
	inlineSchemeRe = regexp.MustCompile(`(?s)<head><script>(.*?)</script>`)
)

func cspHash(body string) string {
	sum := sha256.Sum256([]byte(body))
	return "'sha256-" + base64.StdEncoding.EncodeToString(sum[:]) + "'"
}

// servePage serves path through the standalone host, which installs the
// framework's default security headers the way framework.App does.
func servePage(t *testing.T, ds *UIHost, path string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	ds.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	return w
}

// TestFirstPaintInlinesHeadCSS pins the first-paint shape of a live page
// whose head fits the budget: the component sheets, app.css and the
// color-scheme bootstrap arrive in the document itself, in the order the
// links had, so first paint waits on no second round trip.
func TestFirstPaintInlinesHeadCSS(t *testing.T) {
	registry.IsolateForTest(t)
	a := registerTestStyle(t, "fp-a")
	b := registerTestStyle(t, "fp-b")
	ds := newTestUIHostForMany(a, b)
	body := servePage(t, ds, "/").Body.String()

	for _, link := range []string{"/__gofastr/comp-bundle.css", `rel="stylesheet" href="/__gofastr/app.css`, `src="/__gofastr/color-scheme.js`} {
		if strings.Contains(body, link) {
			t.Errorf("an inlined head still links %s", link)
		}
	}
	m := inlineBundleRe.FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("no inline component bundle:\n%s", truncate(body, 1500))
	}
	for _, s := range []*registry.Style{a, b} {
		if !strings.Contains(","+m[1]+",", ","+s.Name()+",") {
			t.Errorf("data-cui-bundle=%q does not name %s", m[1], s.Name())
		}
		e, _ := registry.Lookup(s.Name())
		th := ds.activeTheme()
		if !strings.Contains(m[2], e.ServedCSSForHash(th, style.ThemeHash(th))) {
			t.Errorf("inline bundle lacks %s's served sheet", s.Name())
		}
	}
	app := inlineAppRe.FindStringSubmatch(body)
	if app == nil {
		t.Fatal("no inline app.css")
	}
	if want, _ := ds.appCSSCached(); app[1] != want {
		t.Error("inline app.css differs from the served sheet")
	}
	if strings.Index(body, m[0]) > strings.Index(body, app[0]) {
		t.Error("app.css precedes the component sheets; host overrides lose the cascade")
	}
	if inlineSchemeRe.FindStringSubmatch(body) == nil {
		t.Error("the color-scheme bootstrap is not the first thing in <head>")
	}
}

// TestFirstPaintCSPAllowsInlined pins the policy half: every inlined block
// is allowed by its hash in the response's own policy, and nothing else
// inline is. 'unsafe-inline' never appears.
func TestFirstPaintCSPAllowsInlined(t *testing.T) {
	registry.IsolateForTest(t)
	a := registerTestStyle(t, "fp-csp-a")
	b := registerTestStyle(t, "fp-csp-b")
	w := servePage(t, newTestUIHostForMany(a, b), "/")
	body, csp := w.Body.String(), w.Header().Get("Content-Security-Policy")

	if strings.Contains(csp, "unsafe-inline") {
		t.Fatalf("policy allows any inline content: %s", csp)
	}
	directive := func(name string) string {
		for _, d := range strings.Split(csp, ";") {
			if f := strings.Fields(d); len(f) > 0 && f[0] == name {
				return " " + strings.Join(f[1:], " ") + " "
			}
		}
		return ""
	}
	styleSrc, scriptSrc := directive("style-src"), directive("script-src")
	for _, src := range []string{styleSrc, scriptSrc} {
		if !strings.Contains(src, " 'self' ") {
			t.Errorf("an added directive dropped default-src's 'self' (links stop loading): %q", src)
		}
	}
	for _, m := range [][]string{inlineBundleRe.FindStringSubmatch(body), inlineAppRe.FindStringSubmatch(body)} {
		if m == nil {
			t.Fatal("page has no inline sheet to check")
		}
		if h := cspHash(m[len(m)-1]); !strings.Contains(styleSrc, " "+h+" ") {
			t.Errorf("style-src lacks %s: %s", h, csp)
		}
	}
	s := inlineSchemeRe.FindStringSubmatch(body)
	if s == nil {
		t.Fatal("no inline bootstrap")
	}
	if h := cspHash(s[1]); !strings.Contains(scriptSrc, " "+h+" ") {
		t.Errorf("script-src lacks %s: %s", h, csp)
	}
}

// TestFirstPaintOverBudgetLinks: a head too heavy for the first round trip
// keeps its cacheable links, and the policy stays untouched.
func TestFirstPaintOverBudgetLinks(t *testing.T) {
	registry.IsolateForTest(t)
	name := uniqueName("fp-heavy")
	heavy := registry.RegisterStyle(name, func(theme style.Theme) string {
		sh := style.NewComponentSheet(name, theme)
		buf := make([]byte, 8)
		for range 3000 {
			_, _ = rand.Read(buf)
			sh = sh.Rule(".r"+hex.EncodeToString(buf)).Set("color", "#"+hex.EncodeToString(buf[:3])).End()
		}
		return sh.MustBuild()
	})
	w := servePage(t, newTestUIHostForMany(heavy, registerTestStyle(t, "fp-light")), "/")
	body := w.Body.String()
	if inlineBundleRe.MatchString(body) || inlineAppRe.MatchString(body) {
		t.Fatal("an over-budget head was inlined")
	}
	if !strings.Contains(body, "/__gofastr/comp-bundle.css?names=") {
		t.Error("an over-budget head lost its bundle link")
	}
	if strings.Contains(w.Header().Get("Content-Security-Policy"), "sha256-") {
		t.Error("the policy gained hashes for a page that inlined nothing")
	}
}

// TestFirstPaintOptOutLinks: WithNoInlineFirstPaint keeps every head
// resource a link, for a host whose policy is set where the page's hashes
// cannot reach (a proxy, a CDN).
func TestFirstPaintOptOutLinks(t *testing.T) {
	registry.IsolateForTest(t)
	a := registerTestStyle(t, "fp-off-a")
	b := registerTestStyle(t, "fp-off-b")
	ds := newTestUIHostForMany(a, b)
	WithNoInlineFirstPaint()(ds)
	w := servePage(t, ds, "/")
	body := w.Body.String()
	if inlineBundleRe.MatchString(body) || inlineAppRe.MatchString(body) || inlineSchemeRe.MatchString(body) {
		t.Fatal("the opt-out page inlined its head")
	}
	for _, link := range []string{"/__gofastr/comp-bundle.css?names=", "/__gofastr/app.css?v=", "/__gofastr/color-scheme.js?v="} {
		if !strings.Contains(body, link) {
			t.Errorf("opt-out page lacks %s", link)
		}
	}
	if strings.Contains(w.Header().Get("Content-Security-Policy"), "sha256-") {
		t.Error("the opt-out page's policy gained hashes")
	}
}

// TestFirstPaintRefusedPolicyLinks: a policy that cannot take a hash
// ('none' for styles) keeps the links rather than shipping blocks the
// browser would drop.
func TestFirstPaintRefusedPolicyLinks(t *testing.T) {
	registry.IsolateForTest(t)
	ds := newTestUIHostForMany(registerTestStyle(t, "fp-none-a"), registerTestStyle(t, "fp-none-b"))
	h := router.New()
	h.Use(middleware.SecurityHeaders(middleware.SecurityHeadersConfig{ContentSecurityPolicy: "default-src 'self'; style-src 'none'"}))
	ds.Mount(h)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if inlineBundleRe.MatchString(w.Body.String()) {
		t.Fatal("inlined under a policy that refuses inline styles")
	}
	if got := w.Header().Get("Content-Security-Policy"); got != "default-src 'self'; style-src 'none'" {
		t.Errorf("policy rewritten to %q", got)
	}
}

func TestCSPAllowInline(t *testing.T) {
	const S, J = "'sha256-S'", "'sha256-J'"
	cases := []struct {
		name, in, want string
		ok             bool
	}{
		{"no policy", "", "", true},
		{"default only", "default-src 'self'; img-src 'self' data:",
			"default-src 'self'; img-src 'self' data:; style-src 'self' " + S + "; script-src 'self' " + J, true},
		{"own directives", "default-src 'self'; style-src 'self' https://cdn; script-src 'self'",
			"default-src 'self'; style-src 'self' https://cdn " + S + "; script-src 'self' " + J, true},
		{"elem directive wins", "default-src 'self'; style-src-elem 'self'; style-src 'self'; script-src-elem 'self'",
			"default-src 'self'; style-src-elem 'self' " + S + "; style-src 'self'; script-src-elem 'self' " + J, true},
		{"unsafe-inline kept", "default-src 'self'; style-src 'self' 'unsafe-inline'",
			"default-src 'self'; style-src 'self' 'unsafe-inline'; script-src 'self' " + J, true},
		{"nonce disables unsafe-inline", "default-src 'self'; style-src 'self' 'unsafe-inline' 'nonce-n'",
			"default-src 'self'; style-src 'self' 'unsafe-inline' 'nonce-n' " + S + "; script-src 'self' " + J, true},
		{"unrestricted", "img-src 'self'", "img-src 'self'", true},
		{"case folded", "DEFAULT-SRC 'self'", "DEFAULT-SRC 'self'; style-src 'self' " + S + "; script-src 'self' " + J, true},
		{"style none", "default-src 'self'; style-src 'none'", "", false},
		{"default none", "default-src 'none'", "", false},
		{"two policies", "default-src 'self', style-src 'self'",
			"default-src 'self'; style-src 'self' " + S + "; script-src 'self' " + J + ", style-src 'self' " + S, true},
	}
	for _, c := range cases {
		got, ok := cspAllowInline(c.in, []string{S}, []string{J})
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("%s:\n got %q %v\nwant %q %v", c.name, got, ok, c.want, c.ok)
		}
	}
}

// TestInlineBlockRefusesCloser: content that would close its own element
// early is never inlined; the page links it instead.
func TestInlineBlockRefusesCloser(t *testing.T) {
	for _, body := range []string{`a{content:"</style>"}`, `a{content:"</STYLE "}`, `x="</script>"`, `<!--`} {
		if _, ok := newInlineBlock(body); ok {
			t.Errorf("inlined %q", body)
		}
	}
	if _, ok := newInlineBlock(`a{color:red}`); !ok {
		t.Error("refused a plain sheet")
	}
}

// TestFirstPaintReportOnlyGetsHashes: a report-only policy is patched too,
// so trying a stricter policy does not flood the report endpoint with
// the framework's own blocks.
func TestFirstPaintReportOnlyGetsHashes(t *testing.T) {
	registry.IsolateForTest(t)
	ds := newTestUIHostForMany(registerTestStyle(t, "fp-ro-a"), registerTestStyle(t, "fp-ro-b"))
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy-Report-Only", "default-src 'self'")
		ds.ServeHTTP(w, r)
	})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	m := inlineBundleRe.FindStringSubmatch(w.Body.String())
	if m == nil {
		t.Fatal("no inline bundle")
	}
	if ro := w.Header().Get("Content-Security-Policy-Report-Only"); !strings.Contains(ro, cspHash(m[2])) {
		t.Errorf("report-only policy lacks the bundle hash: %s", ro)
	}
}
