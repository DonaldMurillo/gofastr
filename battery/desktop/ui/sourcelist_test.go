package desktopui_test

import (
	"strings"
	"testing"

	desktopui "github.com/DonaldMurillo/gofastr/battery/desktop/ui"
	"github.com/DonaldMurillo/gofastr/core/render"
)

func sampleList() desktopui.SourceListConfig {
	return desktopui.SourceListConfig{
		Label: "Tasks",
		Sections: []desktopui.SourceSection{
			{
				Title: "Views",
				Items: []desktopui.SourceItem{
					{Label: "Today", Href: "/"},
					{Label: "History", Href: "/history"},
				},
			},
			{
				Items: []desktopui.SourceItem{
					{Label: "Settings", Href: "/settings"},
				},
			},
		},
		CurrentPath: "/history",
	}
}

// SourceList renders the marker, labelled sections, links with the
// pill class, and aria-current from CurrentPath for first paint. No
// inline style.
func TestSourceListMarkup(t *testing.T) {
	out := string(desktopui.SourceList(sampleList()))
	for _, w := range []string{
		`data-fui-comp="desktopui-sourcelist"`,
		`class="desktopui-sourcelist__header"`,
		`>Views</h2>`,
		`href="/history"`,
		`>History</span></a>`,
		`href="/settings"`,
	} {
		if !strings.Contains(out, w) {
			t.Errorf("source list missing %q:\n%s", w, out)
		}
	}
	if strings.Contains(out, "style=") {
		t.Errorf("source list must not emit an inline style:\n%s", out)
	}
	// Exactly one current item: the /history row, not / or /settings.
	if n := strings.Count(out, `aria-current="page"`); n != 1 {
		t.Errorf("want exactly 1 aria-current, got %d:\n%s", n, out)
	}
	// An unlabeled section emits no header element.
	if n := strings.Count(out, "desktopui-sourcelist__header"); n != 1 {
		t.Errorf("want exactly 1 section header, got %d:\n%s", n, out)
	}
}

// Labels are HTML-escaped; a javascript: href degrades to # the same
// way ui.Sidebar's links do.
func TestSourceListEscapesAndSanitizes(t *testing.T) {
	cfg := desktopui.SourceListConfig{
		Label: "Evil",
		Sections: []desktopui.SourceSection{{
			Items: []desktopui.SourceItem{
				{Label: `<b>&"x"`, Href: "javascript:alert(1)"},
			},
		}},
	}
	out := string(desktopui.SourceList(cfg))
	if strings.Contains(out, "javascript:") {
		t.Errorf("javascript: href survived:\n%s", out)
	}
	if !strings.Contains(out, `href="#"`) {
		t.Errorf("rejected href must degrade to #:\n%s", out)
	}
	if strings.Contains(out, "<b>") {
		t.Errorf("label was not escaped:\n%s", out)
	}
}

// The source list's CSS: transparent background (the native sidebar
// material shows through), capsule selection on Accent, HIG caption
// size section headers, a keyboard focus ring, and a grayed selection
// when the window is inactive.
func TestSourceListCSS(t *testing.T) {
	css := componentCSS(t, "desktopui-sourcelist")
	for _, w := range []string{
		"background: transparent",
		"border-radius: var(--radii-full",
		"background: var(--color-accent",
		"font-size: var(--text-xs",
		"outline:",
		"var(--color-accent",
		"html.desktop-inactive",
	} {
		if !strings.Contains(css, w) {
			t.Errorf("source list CSS missing %q:\n%s", w, css)
		}
	}
}

var _ render.HTML = ""
