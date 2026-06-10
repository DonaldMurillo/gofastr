package widget_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core-ui/widget"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/core/router"
)

// Architecture contract tests — these lock the "minimal-register +
// lazy-fetch + hydrate" pattern in place. Every widget primitive that
// ships in the framework must obey these rules; every change to the
// widget runtime / registry must not break them. Failing any of
// these means the architecture has regressed and apps building on
// the framework will silently break.
//
// Tests live in the widget package so they exercise the canonical
// registry + chrome endpoints directly, without coupling to a
// specific demo screen or example app.
//
// The contract has four guarantees, each tested below:
//
//   1. The /__gofastr/widgets registry is metadata-only. It must
//      never carry rendered chrome HTML, and statePath must be
//      omitted entirely for widgets with no signals.
//   2. Chrome HTML is reachable at the per-widget /chrome endpoint,
//      ready for lazy fetch on first open.
//   3. The /chrome endpoint renders the same HTML the framework's
//      SSR layer would inline — single source of truth.
//   4. Registry metadata exposes deeplink fields (key, value, params)
//      so the runtime can dispatch boot-time deep-link sync without
//      knowing about the widget at build time.

// chromeStub is a tiny Component used so the contract tests are
// independent of preset.Modal/Drawer/etc.
type chromeStub struct{ html string }

func (c chromeStub) Render() render.HTML { return render.HTML(c.html) }

var _ component.Component = chromeStub{}

// CONTRACT 1a: Registry payload contains metadata only — chrome HTML
// must NEVER appear in the /__gofastr/widgets JSON.
func TestArchContract_RegistryHasNoChromeHTML(t *testing.T) {
	def := widget.New("arch-chrome-test").
		Hidden().
		Slot("body", chromeStub{html: `<p class="distinctive-chrome-text">SHOULD-NOT-BE-IN-REGISTRY</p>`}).
		Build()

	r := router.New()
	widget.Mount(r, &def)
	widget.MountRuntime(r)
	srv := httptest.NewServer(r)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/__gofastr/widgets")
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("registry status: err=%v code=%d", err, resp.StatusCode)
	}
	body := readBody(t, resp)
	if strings.Contains(body, "SHOULD-NOT-BE-IN-REGISTRY") {
		t.Error("contract violation: chrome HTML must not be embedded in the widgets registry — fetch via cfg.chromePath")
	}
	if strings.Contains(body, "distinctive-chrome-text") {
		t.Error("contract violation: registry contains chrome markup")
	}
}

// CONTRACT 1b: statePath is omitted when the widget declared no signals.
func TestArchContract_StatePathOmittedWhenNoSignals(t *testing.T) {
	def := widget.New("arch-state-test").
		Hidden().
		Slot("body", chromeStub{html: `<p>x</p>`}).
		Build()

	r := router.New()
	widget.Mount(r, &def)
	widget.MountRuntime(r)
	srv := httptest.NewServer(r)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/__gofastr/widgets")
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("registry status: %v", err)
	}
	var entries []map[string]any
	if err := json.Unmarshal([]byte(readBody(t, resp)), &entries); err != nil {
		t.Fatalf("decode: %v", err)
	}
	cfg := findCfg(t, entries, "arch-state-test")
	if _, present := cfg["statePath"]; present {
		t.Errorf("contract violation: statePath should be omitted for widgets with no signals; got %v", cfg["statePath"])
	}
}

// findCfg looks up a widget by name in the registry payload. Tests
// in this package share the process-global registry, so assertions
// must scope to their own widget name rather than expecting exactly
// one entry.
func findCfg(t *testing.T, entries []map[string]any, name string) map[string]any {
	t.Helper()
	for _, e := range entries {
		cfg, ok := e["cfg"].(map[string]any)
		if !ok {
			continue
		}
		if cfg["name"] == name {
			return cfg
		}
	}
	t.Fatalf("widget %q not found in registry payload", name)
	return nil
}

// CONTRACT 1c: statePath IS emitted when the widget declared signals.
func TestArchContract_StatePathEmittedWhenSignalsExist(t *testing.T) {
	def := widget.New("arch-state-emitted").
		Hidden().
		Slot("body", chromeStub{html: `<p>x</p>`}).
		Signal("count", widget.SignalFunc(func() (any, error) { return 0, nil })).
		Build()

	r := router.New()
	widget.Mount(r, &def)
	widget.MountRuntime(r)
	srv := httptest.NewServer(r)
	defer srv.Close()

	resp, _ := http.Get(srv.URL + "/__gofastr/widgets")
	body := readBody(t, resp)
	if !strings.Contains(body, `"statePath":"/core-ui/widget/arch-state-emitted/state"`) {
		t.Errorf("contract violation: statePath must be emitted when signals exist; body=%s", body)
	}
}

// CONTRACT 2: Each widget's chromePath is reachable and returns the
// rendered HTML. Apps' runtime fetches this on first open.
func TestArchContract_ChromeEndpointServesRenderedHTML(t *testing.T) {
	const marker = `MARKER-distinct-chrome-content-xyz789`
	def := widget.New("arch-chrome-endpoint").
		Hidden().
		Slot("body", chromeStub{html: `<p class="x">` + marker + `</p>`}).
		Build()

	r := router.New()
	widget.Mount(r, &def)
	srv := httptest.NewServer(r)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/core-ui/widget/arch-chrome-endpoint/chrome")
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("chrome status: err=%v code=%d", err, resp.StatusCode)
	}
	body := readBody(t, resp)
	if !strings.Contains(body, marker) {
		t.Errorf("contract violation: chrome endpoint must serve the rendered chrome; missing marker, body=%s", body)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("chrome content-type should be text/html; got %q", ct)
	}
}

// CONTRACT 3: Registry advertises chromePath for every registered
// widget. Without this, the runtime can't locate the chrome.
func TestArchContract_RegistryExposesChromePath(t *testing.T) {
	def := widget.New("arch-cpath").
		Slot("body", chromeStub{html: `<p>x</p>`}).
		Build()

	r := router.New()
	widget.Mount(r, &def)
	widget.MountRuntime(r)
	srv := httptest.NewServer(r)
	defer srv.Close()

	resp, _ := http.Get(srv.URL + "/__gofastr/widgets")
	body := readBody(t, resp)
	wantPath := `"chromePath":"/core-ui/widget/arch-cpath/chrome"`
	if !strings.Contains(body, wantPath) {
		t.Errorf("contract violation: registry must expose chromePath; missing %q in %s", wantPath, body)
	}
}

// CONTRACT 4: Deeplink metadata (key/value/params) is round-tripped
// through the registry so the runtime can sync URL <-> widget state
// at boot and popstate.
func TestArchContract_RegistryExposesDeepLinkMetadata(t *testing.T) {
	def := widget.New("arch-deeplink").
		Hidden().
		DeepLink("modal", "user-edit").
		DeepLinkParam("user_id").
		DeepLinkParam("tab").
		Slot("body", chromeStub{html: `<p>x</p>`}).
		Build()

	r := router.New()
	widget.Mount(r, &def)
	widget.MountRuntime(r)
	srv := httptest.NewServer(r)
	defer srv.Close()

	resp, _ := http.Get(srv.URL + "/__gofastr/widgets")
	body := readBody(t, resp)
	for _, want := range []string{
		`"deepLinkKey":"modal"`,
		`"deepLinkValue":"user-edit"`,
		`"deepLinkParams":["user_id","tab"]`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("contract violation: registry missing deep-link field %q", want)
		}
	}
}

// CONTRACT 5: Hidden widgets DO appear in the registry — the runtime
// needs to know they exist so it can lazy-fetch them on click.
// Visibility is a render-time decision (uihost SSR-inline path), not
// a registry filter.
func TestArchContract_HiddenWidgetsStillRegistered(t *testing.T) {
	def := widget.New("arch-hidden").
		Hidden().
		Slot("body", chromeStub{html: `<p>x</p>`}).
		Build()

	r := router.New()
	widget.Mount(r, &def)
	widget.MountRuntime(r)
	srv := httptest.NewServer(r)
	defer srv.Close()

	resp, _ := http.Get(srv.URL + "/__gofastr/widgets")
	body := readBody(t, resp)
	if !strings.Contains(body, `"name":"arch-hidden"`) {
		t.Errorf("contract violation: hidden widget must still appear in registry; body=%s", body)
	}
	if !strings.Contains(body, `"hidden":true`) {
		t.Errorf("contract violation: hidden flag must be exposed; body=%s", body)
	}
}

// CONTRACT 6: AllForSSR returns the live registry — used by the SSR
// host to inline visible widgets into page responses.
func TestArchContract_AllForSSRReturnsRegisteredWidgets(t *testing.T) {
	def := widget.New("arch-all-for-ssr").
		Slot("body", chromeStub{html: `<p>x</p>`}).
		Build()

	r := router.New()
	widget.Mount(r, &def)

	found := false
	for _, d := range widget.AllForSSR() {
		if d.Name == "arch-all-for-ssr" {
			found = true
			break
		}
	}
	if !found {
		t.Error("contract violation: widget.AllForSSR() must include every registered widget so the SSR host can inline them")
	}
}

// CONTRACT 8: Per-page scoping — a widget with no Routes declared
// is available on every path; widgets with Routes are visible only
// on paths their matchers accept. Apps relying on .Pages() /
// .PagesPrefix() / .PagesMatch() expect both the catalog endpoint
// and the SSR-inline pass to honour the filter.
func TestArchContract_GlobalWidgetAvailableEverywhere(t *testing.T) {
	def := widget.New("arch-global-scope").
		Hidden().
		Slot("body", chromeStub{html: `<p>x</p>`}).
		Build()
	r := router.New()
	widget.Mount(r, &def)
	widget.MountRuntime(r)
	srv := httptest.NewServer(r)
	defer srv.Close()
	for _, page := range []string{"/", "/anywhere", "/deep/nested"} {
		resp, _ := http.Get(srv.URL + "/__gofastr/widgets?page=" + page)
		body := readBody(t, resp)
		if !strings.Contains(body, `"name":"arch-global-scope"`) {
			t.Errorf("contract violation: global widget (no Routes) must appear on every page; missing on %s", page)
		}
	}
}

func TestArchContract_ExactPageScoping(t *testing.T) {
	def := widget.New("arch-exact-scope").
		Hidden().
		Pages("/foo").
		Slot("body", chromeStub{html: `<p>x</p>`}).
		Build()
	r := router.New()
	widget.Mount(r, &def)
	widget.MountRuntime(r)
	srv := httptest.NewServer(r)
	defer srv.Close()

	resp, _ := http.Get(srv.URL + "/__gofastr/widgets?page=/foo")
	if !strings.Contains(readBody(t, resp), `"name":"arch-exact-scope"`) {
		t.Error("contract violation: widget with .Pages(\"/foo\") must appear on /foo")
	}
	resp, _ = http.Get(srv.URL + "/__gofastr/widgets?page=/bar")
	if strings.Contains(readBody(t, resp), `"name":"arch-exact-scope"`) {
		t.Error("contract violation: widget with .Pages(\"/foo\") must NOT appear on /bar")
	}
}

func TestArchContract_PrefixPageScoping(t *testing.T) {
	def := widget.New("arch-prefix-scope").
		Hidden().
		PagesPrefix("/components/").
		Slot("body", chromeStub{html: `<p>x</p>`}).
		Build()
	r := router.New()
	widget.Mount(r, &def)
	widget.MountRuntime(r)
	srv := httptest.NewServer(r)
	defer srv.Close()

	for _, page := range []string{"/components/modal", "/components/drawer", "/components/anything"} {
		resp, _ := http.Get(srv.URL + "/__gofastr/widgets?page=" + page)
		if !strings.Contains(readBody(t, resp), `"name":"arch-prefix-scope"`) {
			t.Errorf("contract violation: PagesPrefix(\"/components/\") must match %s", page)
		}
	}
	for _, page := range []string{"/about", "/docs/", "/components"} {
		// "/components" (no trailing slash) shouldn't prefix-match
		// "/components/".
		resp, _ := http.Get(srv.URL + "/__gofastr/widgets?page=" + page)
		if strings.Contains(readBody(t, resp), `"name":"arch-prefix-scope"`) {
			t.Errorf("contract violation: PagesPrefix(\"/components/\") must NOT match %s", page)
		}
	}
}

func TestArchContract_MatchFnPageScoping(t *testing.T) {
	def := widget.New("arch-match-scope").
		Hidden().
		PagesMatch(func(p string) bool { return strings.HasSuffix(p, ".admin") }).
		Slot("body", chromeStub{html: `<p>x</p>`}).
		Build()
	r := router.New()
	widget.Mount(r, &def)
	widget.MountRuntime(r)
	srv := httptest.NewServer(r)
	defer srv.Close()

	resp, _ := http.Get(srv.URL + "/__gofastr/widgets?page=/users.admin")
	if !strings.Contains(readBody(t, resp), `"name":"arch-match-scope"`) {
		t.Error("PagesMatch must accept /users.admin")
	}
	resp, _ = http.Get(srv.URL + "/__gofastr/widgets?page=/users")
	if strings.Contains(readBody(t, resp), `"name":"arch-match-scope"`) {
		t.Error("PagesMatch must reject /users")
	}
}

func TestArchContract_AvailableOnHelper(t *testing.T) {
	scoped := widget.New("arch-helper-scoped").
		Pages("/yes").
		Slot("body", chromeStub{html: `<p>x</p>`}).
		Build()
	global := widget.New("arch-helper-global").
		Slot("body", chromeStub{html: `<p>x</p>`}).
		Build()
	r := router.New()
	widget.Mount(r, &scoped)
	widget.Mount(r, &global)

	yes := widget.AvailableOn("/yes")
	no := widget.AvailableOn("/no")

	gotScopedOnYes := contains(yes, "arch-helper-scoped")
	gotScopedOnNo := contains(no, "arch-helper-scoped")
	gotGlobalOnYes := contains(yes, "arch-helper-global")
	gotGlobalOnNo := contains(no, "arch-helper-global")

	if !gotScopedOnYes {
		t.Error("AvailableOn(/yes) missing scoped widget that matches")
	}
	if gotScopedOnNo {
		t.Error("AvailableOn(/no) leaked scoped widget that should be filtered out")
	}
	if !gotGlobalOnYes || !gotGlobalOnNo {
		t.Error("global widget must appear on every path via AvailableOn")
	}
}

func contains(defs []*widget.Definition, name string) bool {
	for _, d := range defs {
		if d.Name == name {
			return true
		}
	}
	return false
}

// CONTRACT 7: RenderChrome produces identical HTML to what the
// chrome endpoint serves — single source of truth, so SSR-inlined
// and lazy-fetched widgets are byte-for-byte the same.
func TestArchContract_RenderChromeMatchesChromeEndpoint(t *testing.T) {
	def := widget.New("arch-render-eq").
		Slot("body", chromeStub{html: `<p class="x">eq-test</p>`}).
		Build()

	r := router.New()
	widget.Mount(r, &def)
	srv := httptest.NewServer(r)
	defer srv.Close()

	resp, _ := http.Get(srv.URL + "/core-ui/widget/arch-render-eq/chrome")
	via := readBody(t, resp)
	direct := widget.RenderChrome(&def)
	if via != direct {
		t.Errorf("contract violation: RenderChrome and /chrome endpoint must produce identical HTML.\nvia endpoint:\n%s\nvia RenderChrome:\n%s", via, direct)
	}
}

func readBody(t *testing.T, resp *http.Response) string {
	t.Helper()
	defer resp.Body.Close()
	buf := make([]byte, 0, 4096)
	tmp := make([]byte, 4096)
	for {
		n, err := resp.Body.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)
		}
		if err != nil {
			break
		}
	}
	return string(buf)
}
