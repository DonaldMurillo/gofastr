package appgen

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
)

// The generated e2e asserted GET / is 200 for every app, so a valid spec
// with no screen at / shipped a TestE2E that failed on a 404 nothing
// was meant to serve. It asserts the home page only when a screen sits
// there.
func TestE2EHomeCheckNeedsHomeScreen(t *testing.T) {
	const home = `e2eDo(t, e2eClient, "GET", base+"/", "")`
	screen := func(name, route string) appspec.AppSpecScreen {
		return appspec.AppSpecScreen{Name: name, Route: route, Title: name, Body: []appspec.AppSpecBlock{{Type: "heading", Text: name}}}
	}
	bp := appspec.AppSpec{App: appspec.AppSpecApp{Name: "Desk", Module: "example.com/desk", DBDriver: "sqlite", DBURL: "desk.db"}}
	bp.Screens = []appspec.AppSpecScreen{screen("tickets", "/tickets"), screen("stats", "/stats")}
	if e2e := renderAppSpecE2ETest(bp); strings.Contains(e2e, home) {
		t.Errorf("e2e asserts GET / with no screen at /:\n%s", e2e)
	}
	bp.Screens = append(bp.Screens, screen("home", "/"))
	if e2e := renderAppSpecE2ETest(bp); !strings.Contains(e2e, home) {
		t.Errorf("e2e skips GET / with a screen at /:\n%s", e2e)
	}
}
