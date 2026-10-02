package framework

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	uiapp "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
	"github.com/DonaldMurillo/gofastr/internal/retired"
	"github.com/DonaldMurillo/gofastr/internal/upgrade"
)

// A registry shaped like the shipped one: a breaking note retiring a
// class family and a data-fui-* attribute, plus a non-breaking note
// whose spellings must not leak in.
const retiredFixtureRegistry = `
through: v9.9.9
releases:
  - version: v9.0.0
    title: retired spellings
    notes:
      - change: "the ui-button class is gone"
        breaking: true
        hits: edit
        find:
          strings:
            classes: [ui-button]
            attrs: [data-fui-signal, data-fui-toggle-]
`

// retiredScreen renders its markup at run time: the classes and
// attribute names reach the page through Sprintf, exactly the shapes a
// source scan cannot see but a rendered response can.
type retiredScreen struct {
	kind  string
	attr  string
	value string
}

func (s retiredScreen) Render() render.HTML {
	if s.kind == "" {
		return render.HTML(`<button class="fui-button" data-fui-comp="ui-sidebar">ok</button>`)
	}
	return render.HTML(fmt.Sprintf(`<div class="ui-%s %s-x" DATA-%s="%s">old</div>`, s.kind, s.kind, s.attr, s.value))
}

func retiredHarness(t *testing.T, s retiredScreen) (*TestApp, *renderFailureTB) {
	t.Helper()
	reg, err := upgrade.Parse(retiredFixtureRegistry)
	if err != nil {
		t.Fatalf("parse fixture registry: %v", err)
	}
	retired.UseForTest(t, retired.FromRegistry(reg))
	site := uiapp.NewApp("retired-check")
	site.Register("/", s, nil)
	app := NewApp(WithoutDefaultMiddleware()).Mount(uihost.New(site))
	reporter := &renderFailureTB{TB: t}
	return TestHarness(reporter, app), reporter
}

func TestHarnessReportsRetiredMarkup(t *testing.T) {
	harness, reporter := retiredHarness(t, retiredScreen{kind: "button", attr: "fui-signal", value: "s"})
	resp := harness.Get("/")
	if resp.Status() != 200 {
		t.Fatalf("retired markup changed the status: %d", resp.Status())
	}
	if !strings.Contains(resp.Body(), `class="ui-button`) {
		t.Fatalf("page does not carry the retired class; test is not exercising the hook: %s", resp.Body())
	}
	want := []string{
		`GET /: retired markup: class "ui-button" (v9.0.0: the ui-button class is gone); run gofastr upgrade`,
		`GET /: retired markup: attr "DATA-fui-signal" (v9.0.0: the ui-button class is gone); run gofastr upgrade`,
	}
	if !equalStringSlices(reporter.messages, want) {
		t.Fatalf("harness reports = %q, want %q", reporter.messages, want)
	}
}

func TestHarnessSilentOnMigratedMarkup(t *testing.T) {
	harness, reporter := retiredHarness(t, retiredScreen{})
	harness.Get("/")
	if len(reporter.messages) != 0 {
		t.Fatalf("migrated markup reported: %q", reporter.messages)
	}
}

// The SPA-nav partial of the same screen is scanned too.
func TestHarnessScansNavPartials(t *testing.T) {
	harness, reporter := retiredHarness(t, retiredScreen{kind: "button", attr: "fui-signal", value: "s"})
	harness.Request("GET", "/", nil).WithHeader("X-Gofastr-Navigate", "1").WithHeader("X-Gofastr-From", "/x").Execute()
	if len(reporter.messages) == 0 {
		t.Fatal("navigation partial was not scanned")
	}
	for _, m := range reporter.messages {
		if !strings.Contains(m, "ui-button") && !strings.Contains(m, "DATA-fui-signal") {
			t.Fatalf("unexpected report: %s", m)
		}
	}
}

// Island RPC answers, widget chrome and widget state are app-router
// routes like any page: an island that renders a
// retired class only after a click is reported, as an HTML
// fragment or as a JSON signal value. WithoutDefaultMiddleware does
// not opt out. (A real widget is not mounted here: core-ui/widget's
// registry is process-global, and every later page in this package
// would SSR its chrome.)
func TestHarnessScansIslandResponses(t *testing.T) {
	reg, err := upgrade.Parse(retiredFixtureRegistry)
	if err != nil {
		t.Fatalf("parse fixture registry: %v", err)
	}
	retired.UseForTest(t, retired.FromRegistry(reg))
	app := NewApp(WithoutDefaultMiddleware())
	app.Router().Post("/api/island", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<ul class="ui-%s"><li>row</li></ul>`, "button")
	}))
	app.Router().Post("/api/signal", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"list":"<li data-%s=\"x\">row</li>"}`, "fui-signal")
	}))
	reporter := &renderFailureTB{TB: t}
	harness := TestHarness(reporter, app)

	cases := []struct {
		method, path, want string
	}{
		{"POST", "/api/island", `POST /api/island: retired markup: class "ui-button"`},
		{"POST", "/api/signal", `POST /api/signal: retired markup: attr "data-fui-signal"`},
	}
	for _, c := range cases {
		reporter.messages = nil
		resp := harness.Request(c.method, c.path, nil).Execute()
		if resp.Status() != 200 {
			t.Fatalf("%s %s: status %d: %s", c.method, c.path, resp.Status(), resp.Body())
		}
		found := false
		for _, m := range reporter.messages {
			found = found || strings.HasPrefix(m, c.want)
		}
		if !found {
			t.Errorf("%s %s: reports = %q, want one starting %q", c.method, c.path, reporter.messages, c.want)
		}
	}
}

// The harness reads the live registry, once, and uihost's own page
// shell (runtime injection, layout markers) carries nothing it retires.
// Whether the kit's components and widget chrome are clean is
// framework/gallery's retired_markup_test.go, which renders all of them.
func TestHarnessUsesLiveRegistry(t *testing.T) {
	retired.ResetForTest(t)
	site := uiapp.NewApp("retired-kit-check")
	site.Register("/", kitChromeScreen{}, nil)
	app := NewApp(WithoutDefaultMiddleware()).Mount(uihost.New(site))
	reporter := &renderFailureTB{TB: t}
	harness := TestHarness(reporter, app)
	resp := harness.Get("/")
	if resp.Status() != 200 {
		t.Fatalf("status = %d: %s", resp.Status(), resp.Body())
	}
	if retired.Loads() != 1 {
		t.Fatalf("registry loads = %d, want 1: the scan must read the live registry", retired.Loads())
	}
	if len(reporter.messages) != 0 {
		t.Fatalf("kit chrome reported retired markup (registry wrong or kit wrong): %q", reporter.messages)
	}
}

// End to end through the real uihost handler: under ProductionScan
// (and with GOFASTR_DEV unset), serving a page that carries retired
// markup loads the registry zero times. The guard is proven by the
// tests above, which load it exactly once for the same page.
func TestProductionScanServesWithoutLoad(t *testing.T) {
	retired.ResetForTest(t)
	t.Setenv("GOFASTR_DEV", "")
	site := uiapp.NewApp("retired-prod")
	site.Register("/", retiredScreen{kind: "button", attr: "fui-signal", value: "s"}, nil)
	app := NewApp(WithoutDefaultMiddleware()).Mount(uihost.New(site))
	handler := retired.ProductionScan(t, app.Router())
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `class="ui-button`) {
		t.Fatalf("production serve changed: %d %s", rec.Code, rec.Body.String())
	}
	if n := retired.Loads(); n != 0 {
		t.Fatalf("production posture loaded the registry %d times", n)
	}
}

// kitChromeScreen composes real kit primitives, not raw markup, so the
// page body is what the design system itself emits.
type kitChromeScreen struct{}

func (kitChromeScreen) Render() render.HTML {
	return html.Div(html.DivConfig{},
		html.Heading(html.HeadingConfig{Level: 1}, render.Text("Kit page")),
		html.Paragraph(html.TextConfig{}, render.Text("composed from core-ui/html")),
	)
}
