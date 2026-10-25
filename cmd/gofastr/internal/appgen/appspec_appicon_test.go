package appgen

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

func TestAppSpecMainEmitsAppIcon(t *testing.T) {
	bp := appspec.AppSpec{
		App: appspec.AppSpecApp{
			Name:   "Demo",
			Module: "example.com/demo",
			Theme:  map[string]string{"primary": "#4338CA"},
		},
	}
	got := RenderAppSpecMain(bp)
	clitest.AssertContains(t, got, `uihost.WithAppIcon(appIconPNG())`)
	clitest.AssertContains(t, got, `func appIconPNG() []byte`)
	clitest.AssertContains(t, got, `fwimage "github.com/DonaldMurillo/gofastr/framework/image"`)
	// Gradient stops derive from the app spec's primary theme color.
	clitest.AssertContains(t, got, `"#4338CA"`)
}

func TestAppSpecMainEmitsRobots(t *testing.T) {
	bp := appspec.AppSpec{App: appspec.AppSpecApp{Name: "Demo", Module: "example.com/demo"}}
	got := RenderAppSpecMain(bp)
	clitest.AssertContains(t, got, `uihost.WithRobots(uihost.RobotsConfig{Disallow: []string{"/__gofastr/"}})`)
}

func TestAppSpecIconStopsFallBackOnNonHexPrimary(t *testing.T) {
	from, to := appSpecIconStops(appspec.AppSpec{
		App: appspec.AppSpecApp{Theme: map[string]string{"primary": "oklch(0.82 0.155 78)"}},
	})
	if !strings.HasPrefix(from, "#") || len(from) != 7 {
		t.Errorf("from stop must be #RRGGBB, got %q", from)
	}
	if !strings.HasPrefix(to, "#") || len(to) != 7 {
		t.Errorf("to stop must be #RRGGBB, got %q", to)
	}
}

func TestAppSpecIconStopsDarkenPrimary(t *testing.T) {
	from, to := appSpecIconStops(appspec.AppSpec{
		App: appspec.AppSpecApp{Theme: map[string]string{"primary": "#4338CA"}},
	})
	if from != "#4338CA" {
		t.Errorf("from stop should be the primary, got %q", from)
	}
	if to == from {
		t.Errorf("to stop should be a darkened variant, got %q", to)
	}
}
