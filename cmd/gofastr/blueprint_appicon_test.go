package main

import (
	"strings"
	"testing"
)

func TestBlueprintMainEmitsAppIcon(t *testing.T) {
	bp := Blueprint{
		App: BlueprintApp{
			Name:   "Demo",
			Module: "example.com/demo",
			Theme:  map[string]string{"primary": "#4338CA"},
		},
	}
	got := renderBlueprintMain(bp)
	assertContains(t, got, `uihost.WithAppIcon(appIconPNG())`)
	assertContains(t, got, `func appIconPNG() []byte`)
	assertContains(t, got, `fwimage "github.com/DonaldMurillo/gofastr/framework/image"`)
	// Gradient stops derive from the blueprint's primary theme color.
	assertContains(t, got, `"#4338CA"`)
}

func TestBlueprintMainEmitsRobots(t *testing.T) {
	bp := Blueprint{App: BlueprintApp{Name: "Demo", Module: "example.com/demo"}}
	got := renderBlueprintMain(bp)
	assertContains(t, got, `uihost.WithRobots(uihost.RobotsConfig{Disallow: []string{"/__gofastr/"}})`)
}

func TestBlueprintIconStopsFallBackOnNonHexPrimary(t *testing.T) {
	from, to := blueprintIconStops(Blueprint{
		App: BlueprintApp{Theme: map[string]string{"primary": "oklch(0.82 0.155 78)"}},
	})
	if !strings.HasPrefix(from, "#") || len(from) != 7 {
		t.Errorf("from stop must be #RRGGBB, got %q", from)
	}
	if !strings.HasPrefix(to, "#") || len(to) != 7 {
		t.Errorf("to stop must be #RRGGBB, got %q", to)
	}
}

func TestBlueprintIconStopsDarkenPrimary(t *testing.T) {
	from, to := blueprintIconStops(Blueprint{
		App: BlueprintApp{Theme: map[string]string{"primary": "#4338CA"}},
	})
	if from != "#4338CA" {
		t.Errorf("from stop should be the primary, got %q", from)
	}
	if to == from {
		t.Errorf("to stop should be a darkened variant, got %q", to)
	}
}
