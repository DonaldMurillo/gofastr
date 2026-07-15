package uihost

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
)

type robotsComp struct{}

func (robotsComp) Render() render.HTML  { return html.Div(html.DivConfig{}, render.Text("hi")) }
func (robotsComp) ScreenRobots() string { return "noindex,nofollow" }

func TestScreenRobotsEmitsMeta(t *testing.T) {
	a := app.NewApp("x")
	a.Register("/draft", &robotsComp{}, nil)
	ds := New(a)
	w := httptest.NewRecorder()
	ds.ServeHTTP(w, httptest.NewRequest("GET", "/draft", nil))
	want := `<meta name="robots" content="noindex,nofollow">`
	if !strings.Contains(w.Body.String(), want) {
		t.Errorf("expected %s in head, got:\n%s", want, w.Body.String())
	}
}

type robotsBundleComp struct{}

func (robotsBundleComp) Render() render.HTML  { return html.Div(html.DivConfig{}, render.Text("hi")) }
func (robotsBundleComp) ScreenRobots() string { return "from-interface" }
func (robotsBundleComp) ScreenSEO() SEO       { return SEO{Robots: "noindex"} }

func TestSEOBundleRobotsBeatsScreenRobots(t *testing.T) {
	a := app.NewApp("x")
	a.Register("/", &robotsBundleComp{}, nil)
	ds := New(a)
	w := httptest.NewRecorder()
	ds.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	body := w.Body.String()
	if !strings.Contains(body, `<meta name="robots" content="noindex">`) {
		t.Errorf("expected bundle robots to win, got:\n%s", body)
	}
	if strings.Contains(body, "from-interface") {
		t.Errorf("per-concern interface must not fire when bundle sets Robots:\n%s", body)
	}
}

func TestWithRobotsMetaGlobal(t *testing.T) {
	a := app.NewApp("x")
	a.Register("/", &plainComp{}, nil)
	ds := New(a, WithRobotsMeta("noindex"))
	w := httptest.NewRecorder()
	ds.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	want := `<meta name="robots" content="noindex">`
	if !strings.Contains(w.Body.String(), want) {
		t.Errorf("expected sitewide robots meta, got:\n%s", w.Body.String())
	}
}
