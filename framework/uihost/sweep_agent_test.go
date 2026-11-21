package uihost

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
)

type failingCardSigner struct{ ed25519.PrivateKey }

func (failingCardSigner) Sign(io.Reader, []byte, crypto.SignerOpts) ([]byte, error) {
	return nil, errors.New("hsm-secret-detail")
}

// A card that cannot be signed at request time answers 500 without the
// signing error, never an unsigned card.
func TestSweepAgentSigningFailureIs500(t *testing.T) {
	ds := newAgentReadyHost(WithAgentReady(AgentReadyConfig{
		BaseURL: "https://agent.example",
		AgentCard: &AgentCardConfig{Name: "Signed", SigningKeys: []AgentCardSigningKey{
			{KeyID: "k1", Signer: failingCardSigner{fixedEd25519(t)}},
		}},
	}))
	rec := doGet(ds, "/.well-known/agent-card.json", "")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status %d, want 500: %s", rec.Code, rec.Body)
	}
	if body := rec.Body.String(); strings.Contains(body, "Signed") || strings.Contains(body, "hsm-secret-detail") {
		t.Errorf("failed signing leaked the card or the error: %s", body)
	}
}

func TestSweepAgentSigningRejectsUnsupportedCurve(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P224(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	pv := signingMountPanic(WithAgentReady(AgentReadyConfig{
		BaseURL:   "https://ok.example",
		AgentCard: &AgentCardConfig{Name: "X", SigningKeys: []AgentCardSigningKey{{KeyID: "k", Signer: key}}},
	}))
	if err, ok := pv.(error); !ok || !strings.Contains(err.Error(), "unsupported EC curve") {
		t.Fatalf("panic = %v, want an unsupported EC curve error", pv)
	}
}

func TestSweepAgentSigningNilSignerNamed(t *testing.T) {
	pv := signingMountPanic(WithAgentReady(AgentReadyConfig{
		BaseURL:   "https://ok.example",
		AgentCard: &AgentCardConfig{Name: "X", SigningKeys: []AgentCardSigningKey{{KeyID: "k"}}},
	}))
	if err, ok := pv.(error); !ok || !strings.Contains(err.Error(), "SigningKeys[0]: Signer is nil") {
		t.Fatalf("panic = %v, want an error naming SigningKeys[0]", pv)
	}
}

func TestSweepAgentSignatureProtectedHeader(t *testing.T) {
	srv, _ := mustSignHost(t, "https://agent.example/", AgentCardSigningKey{KeyID: "k1", Signer: fixedEd25519(t)})
	body, _ := getBody(t, srv.URL+"/.well-known/agent-card.json")
	var card struct {
		Signatures []struct{ Protected string } `json:"signatures"`
	}
	if err := json.Unmarshal([]byte(body), &card); err != nil || len(card.Signatures) != 1 {
		t.Fatalf("signatures: %v %s", err, body)
	}
	raw, err := base64.RawURLEncoding.DecodeString(card.Signatures[0].Protected)
	if err != nil {
		t.Fatal(err)
	}
	var header map[string]string
	if err := json.Unmarshal(raw, &header); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"alg": "EdDSA", "typ": "JOSE", "kid": "k1", "jku": "https://agent.example/.well-known/jwks.json"}
	if len(header) != len(want) {
		t.Errorf("protected header = %v, want %v", header, want)
	}
	for k, v := range want {
		if header[k] != v {
			t.Errorf("protected header %s = %q, want %q", k, header[k], v)
		}
	}
}

// A pinned BaseURL makes the card and the Link header request-independent.
func TestSweepAgentPinnedOriginHasNoVary(t *testing.T) {
	ds := newISOHost(
		WithAgentReady(AgentReadyConfig{BaseURL: "https://pinned.example", Title: "T", AgentCard: &AgentCardConfig{Name: "N", MCPEndpoint: "/mcp"}}),
	)
	for _, p := range []string{"/.well-known/agent-card.json", "/"} {
		rec := doGet(ds, p, "")
		for _, v := range rec.Header().Values("Vary") {
			if strings.Contains(v, "Host") || strings.Contains(v, "X-Forwarded-Proto") {
				t.Errorf("%s: Vary %q with a pinned origin", p, v)
			}
		}
	}
}

func TestSweepAgentCardSkillsNeverNull(t *testing.T) {
	body := doGet(newAgentReadyHost(WithAgentCard(AgentCardConfig{Name: "X", A2AEndpoint: "/a2a"})), "/.well-known/agent-card.json", "").Body.String()
	if !strings.Contains(body, `"skills": []`) {
		t.Errorf("card without skills does not carry an empty array:\n%s", body)
	}
}

func TestSweepAgentMarkdownAlternateStripsControls(t *testing.T) {
	if got := markdownAlternate("/a\x00b\r\n\tc/"); got != "/ab\tc/llm.md" {
		t.Errorf("markdownAlternate = %q", got)
	}
}

func negotiatedMarkdown(ds *UIHost, path string) string {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("Accept", "text/markdown")
	rec := httptest.NewRecorder()
	ds.ServeHTTP(rec, req)
	return rec.Body.String()
}

// A gated screen's negotiated markdown carries no title or SEO front matter.
func TestSweepAgentNegotiatedMarkdownHidesGatedMetadata(t *testing.T) {
	a := app.NewApp("dyn")
	a.Register("/", &testHomeComp{}, nil)
	deny := app.PolicyFunc(func(context.Context) app.Decision {
		return app.Decision{Kind: app.DecisionBlock, Status: 403}
	})
	scr := app.NewScreen("/secret/{id}", &loadedTitleComp{}).WithPolicy(deny)
	scr.Title = "Project NIGHTFALL"
	a.RegisterScreen(scr, nil)
	a.RegisterScreen(app.NewScreen("/ledger", &ledgerComp{}).WithPolicy(deny), nil)
	ds := New(a, WithPublicLLMMD(), WithMarkdownNegotiation())
	if md := negotiatedMarkdown(ds, "/secret/42"); strings.Contains(md, "NIGHTFALL") || strings.Contains(md, "Thing 42") {
		t.Errorf("gated negotiated markdown leaked metadata:\n%s", md)
	}
	if md := negotiatedMarkdown(ds, "/ledger"); strings.Contains(md, "LEDGER") || strings.HasPrefix(md, "---") {
		t.Errorf("gated negotiated markdown leaked its SEO:\n%s", md)
	}
}

// Negotiated markdown's front matter comes from the loaded instance.
func TestSweepAgentNegotiatedMarkdownLoadedSEO(t *testing.T) {
	a := app.NewApp("dyn")
	a.Register("/", &testHomeComp{}, nil)
	a.Register("/things/{id}", &loadedTitleComp{}, nil)
	ds := New(a, WithPublicLLMMD(), WithMarkdownNegotiation())
	if md := negotiatedMarkdown(ds, "/things/42"); !strings.Contains(md, "All about thing 42") {
		t.Errorf("front matter missing the loaded description:\n%s", md)
	}
}

// A card member with no JSON form answers 500, not an empty 200.
func TestSweepAgentCardEncodeFailureIs500(t *testing.T) {
	ds := newAgentReadyHost(WithAgentCard(AgentCardConfig{
		Name:            "Bad",
		SecuritySchemes: map[string]AgentSecurityScheme{"x": {"f": func() {}}},
	}))
	if rec := doGet(ds, "/.well-known/agent-card.json", ""); rec.Code != http.StatusInternalServerError {
		t.Fatalf("status %d, want 500: %s", rec.Code, rec.Body)
	}
}

// WhenToUse or CLI alone turns /llms.txt on.
func TestSweepAgentLLMsTxtSectionsAloneOptIn(t *testing.T) {
	for name, cfg := range map[string]AgentReadyConfig{
		"when-to-use": {WhenToUse: "Use it for barcodes."},
		"cli":         {CLI: &CLIToolConfig{Install: "brew install svc"}},
	} {
		t.Run(name, func(t *testing.T) {
			if rec := doGet(newISOHost(WithAgentReady(cfg)), "/llms.txt", ""); rec.Code != http.StatusOK {
				t.Fatalf("status %d, want 200", rec.Code)
			}
		})
	}
}

func TestSweepAgentAcceptQZeroRefuses(t *testing.T) {
	for accept, want := range map[string]bool{
		"text/markdown;q=0":             false,
		"text/markdown; q=0.0":          false,
		"text/html, text/markdown;q=0":  false,
		"text/markdown;q=0.1":           true,
		"text/markdown;q=0, text/plain": false,
	} {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Accept", accept)
		if got := acceptsMarkdown(req); got != want {
			t.Errorf("acceptsMarkdown(%q) = %v, want %v", accept, got, want)
		}
	}
	for _, accept := range []string{"*/*", "text/html", "application/json;q=0"} {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Accept", accept)
		if acceptsProblemJSON(req) {
			t.Errorf("acceptsProblemJSON(%q) = true", accept)
		}
	}
}

func TestSweepAgentOriginIgnoresForwardedHost(t *testing.T) {
	ds := newISOHost(WithAgentCard(AgentCardConfig{Name: "t", MCPEndpoint: "/mcp"}), WithAgentLinkHeaders(), WithTrustedProxies("192.0.2.1"))
	for _, p := range []string{"/.well-known/agent-card.json", "/"} {
		req := httptest.NewRequest(http.MethodGet, p, nil)
		req.Host = "real.example"
		req.Header.Set("X-Forwarded-Host", "evil.example")
		rec := httptest.NewRecorder()
		ds.ServeHTTP(rec, req)
		if strings.Contains(rec.Body.String()+rec.Header().Get("Link"), "evil.example") {
			t.Errorf("%s reflected X-Forwarded-Host", p)
		}
	}
}

// A Link header an earlier layer wrote survives the discovery links.
func TestSweepAgentLinkHeaderAppends(t *testing.T) {
	ds := newAgentReadyHost(WithAgentReady(AgentReadyConfig{BaseURL: "https://ex.com", Title: "T"}))
	rec := httptest.NewRecorder()
	rec.Header().Set("Link", `</style.css>; rel="preload"`)
	ds.writeAgentLinkHeaders(context.Background(), rec, httptest.NewRequest(http.MethodGet, "/", nil))
	link := rec.Header().Get("Link")
	if !strings.HasPrefix(link, `</style.css>; rel="preload", `) || !strings.Contains(link, `rel="llms-txt"`) {
		t.Errorf("Link = %q", link)
	}
}

func TestSweepAgentPWADenyMatchesWholeSegments(t *testing.T) {
	deny := New(app.NewApp("x"), WithPWA(PWAConfig{})).pwaDenyPaths(PWAConfig{})
	for p, want := range map[string]bool{
		"/__gofastr/actions.js":  true,
		"/apiary.png":            true,
		"/__gofastr/action":      false,
		"/__gofastr/action/save": false,
		"/api":                   false,
	} {
		if got := pwaAllowedPrecache(p, deny); got != want {
			t.Errorf("pwaAllowedPrecache(%q) = %v, want %v", p, got, want)
		}
	}
}

func TestSweepAgentPWABuiltInDenyList(t *testing.T) {
	deny := New(app.NewApp("x"), WithPWA(PWAConfig{})).pwaDenyPaths(PWAConfig{})
	for _, p := range []string{
		"/__gofastr/sse", "/__gofastr/session", "/__gofastr/action", "/__gofastr/widgets",
		"/__gofastr/widget/card.js", "/core-ui/widget/x/state", "/api/me", "/auth/login",
	} {
		if pwaAllowedPrecache(p, deny) {
			t.Errorf("built-in deny list admits %s", p)
		}
	}
}

// A DenyPaths "/" or a relative entry does not deny the whole app.
func TestSweepAgentPWADenyPathsIgnoreRoot(t *testing.T) {
	a := app.NewApp("x")
	a.Register("/", &plainComp{}, nil)
	ds := New(a, WithPWA(PWAConfig{DenyPaths: []string{"/", " ", "static"}, Precache: []string{"/static/ok.png"}}))
	sw, err := ds.PWAServiceWorkerJS("")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, p := range pwaSWPrecache(t, sw) {
		found = found || p == "/static/ok.png"
	}
	if !found {
		t.Errorf("a root or relative DenyPaths entry dropped /static/ok.png:\n%s", sw)
	}
}

// The live worker stores responses only at install and fetches
// navigations from the network first.
func TestSweepAgentLiveWorkerNeverCachesAtRuntime(t *testing.T) {
	sw := pwaLiveWorker(t, "Acme")
	if n := strings.Count(sw, "cache.put("); n != 1 {
		t.Errorf("cache.put appears %d times, want once (install)", n)
	}
	_, nav, _ := strings.Cut(sw, `if (req.mode === "navigate") {`)
	nav, _, _ = strings.Cut(nav, "return;")
	if !strings.Contains(nav, "event.respondWith(fetch(req).catch(") || strings.Contains(nav, "caches.match(req") {
		t.Errorf("navigations are not network-first:\n%s", nav)
	}
}

// Both workers leave a denied path to the network before any strategy runs.
func TestSweepAgentWorkersSkipDeniedPaths(t *testing.T) {
	a := app.NewApp("x")
	a.Register("/", &plainComp{}, nil)
	static, err := New(a, WithPWA(PWAConfig{})).PWAStaticServiceWorkerJS("", PWAStaticExport{Pages: []string{"/"}, ContentHash: "h"})
	if err != nil {
		t.Fatal(err)
	}
	for name, sw := range map[string]string{"live": pwaLiveWorker(t, "Acme"), "static": static} {
		_, fetch, _ := strings.Cut(sw, `self.addEventListener("fetch"`)
		guard := strings.Index(fetch, "if (denied(url.pathname)) return;")
		if guard < 0 || guard > strings.Index(fetch, "respondWith") {
			t.Errorf("%s worker does not skip denied paths first:\n%s", name, fetch)
		}
	}
}

// Activate keeps the worker's own cache, and the static worker retries a
// navigation under the path's other trailing-slash form.
func TestSweepAgentWorkerCacheLookups(t *testing.T) {
	a := app.NewApp("x")
	a.Register("/", &plainComp{}, nil)
	static, err := New(a, WithPWA(PWAConfig{})).PWAStaticServiceWorkerJS("", PWAStaticExport{Pages: []string{"/"}, ContentHash: "h"})
	if err != nil {
		t.Fatal(err)
	}
	for name, sw := range map[string]string{"live": pwaLiveWorker(t, "Acme"), "static": static} {
		keep := strings.Index(sw, "if (n === CACHE_NAME) return false;")
		if keep < 0 || keep > strings.Index(sw, "if (n.indexOf(CACHE_PREFIX) === 0) return true;") {
			t.Errorf("%s worker's activate can delete its own cache", name)
		}
	}
	if !strings.Contains(static, "return caches.match(alt, { cacheName: CACHE_NAME });") {
		t.Errorf("static worker does not retry the trailing-slash form:\n%s", static)
	}
}

// The static precache holds every component stylesheet under the ?v= URL pages request.
func TestSweepAgentStaticPrecachesVersionedComponentCSS(t *testing.T) {
	registry.IsolateForTest(t)
	newPWAStyles(t, 1)
	a := app.NewApp("x")
	a.Register("/", &plainComp{}, nil)
	ds := New(a, WithPWA(PWAConfig{}))
	sw, err := ds.PWAStaticServiceWorkerJS("", PWAStaticExport{Pages: []string{"/"}, ContentHash: "h"})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, p := range pwaSWPrecache(t, sw) {
		got[p] = true
	}
	entries := registry.All()
	if len(entries) == 0 {
		t.Fatal("setup: no registered styles")
	}
	for _, e := range entries {
		if u := "/__gofastr/comp/" + e.Name + ".css?v=" + e.VersionFor(ds.activeTheme()); !got[u] {
			t.Errorf("static precache is missing %s", u)
		}
	}
}

// A static export lists none of the live server's versioned shell URLs.
func TestSweepAgentStaticPrecacheOmitsLiveURLs(t *testing.T) {
	a := app.NewApp("x")
	a.Register("/", &plainComp{}, nil)
	sw, err := New(a, WithPWA(PWAConfig{})).PWAStaticServiceWorkerJS("", PWAStaticExport{Pages: []string{"/"}, ContentHash: "h"})
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range pwaSWPrecache(t, sw) {
		if strings.HasPrefix(p, "/__gofastr/runtime.js?v=") || strings.HasPrefix(p, "/__gofastr/manifest.js") || strings.HasPrefix(p, "/__gofastr/color-scheme.js?v=") {
			t.Errorf("static precache lists live-only %s", p)
		}
	}
}

func TestSweepAgentPWAThemeColorEscaped(t *testing.T) {
	a := app.NewApp("x")
	a.Register("/", &plainComp{}, nil)
	body := pwaGet(t, New(a, WithPWA(PWAConfig{ThemeColor: `"><script>x()</script>`})), "/").Body.String()
	if strings.Contains(body, "<script>x()") || !strings.Contains(body, `content="&#34;&gt;&lt;script&gt;`) {
		t.Errorf("theme color not escaped:\n%s", body)
	}
}
