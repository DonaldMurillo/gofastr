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
					{Label: "Today", Href: "/", Count: "3"},
					{Label: "History", Href: "/history", Count: "128"},
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
// row class, a right-aligned count, and aria-current from CurrentPath
// for first paint. No inline style.
func TestSourceListMarkup(t *testing.T) {
	out := string(desktopui.SourceList(sampleList()))
	for _, w := range []string{
		`data-fui-comp="desktopui-sourcelist"`,
		`class="desktopui-sourcelist__header"`,
		`>Views</h2>`,
		`href="/history"`,
		`>History</span>`,
		`class="desktopui-sourcelist__count">128</span>`,
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
	// A row without a Count renders no count span (Settings).
	if n := strings.Count(out, "desktopui-sourcelist__count"); n != 2 {
		t.Errorf("want exactly 2 count spans, got %d:\n%s", n, out)
	}
}

// The current row also carries the runtime's own active class, the
// marker the activelink module reconciles: it clears stale
// aria-current only from links that carry the class it stamps, so a
// server-rendered marker without it survives the module's first sweep
// and two rows read as current after a navigation that lands before
// the module idle-loads.
func TestSourceListStampsRuntimeActiveClass(t *testing.T) {
	out := string(desktopui.SourceList(sampleList()))
	if !strings.Contains(out, `class="desktopui-sourcelist__item active"`) || !strings.Contains(out, `aria-current="page"`) {
		t.Errorf("the current row must carry the runtime active class beside aria-current:\n%s", out)
	}
	if n := strings.Count(out, "__item active"); n != 1 {
		t.Errorf("want exactly 1 row carrying the active class, got %d:\n%s", n, out)
	}
	// The non-current rows keep the plain class.
	if n := strings.Count(out, `class="desktopui-sourcelist__item"`); n != 2 {
		t.Errorf("want exactly 2 plain rows, got %d:\n%s", n, out)
	}
}

// The source list's CSS, against the native capture metrics: rows at
// the measured native height with the theme's body token (the desktop
// theme resolves it to the HIG 13px size), a soft selection whose text keeps the
// normal color (measured on the Notes capture: fill #EFEFEF over a
// #F9F9F9 sidebar in light, #2F2F2F over #212121 in dark, both within
// a couple of points of a 5% mix of the text color), the accent
// reserved for the keyboard focus ring, and a grayed selection when
// the window is inactive.
func TestSourceListCSS(t *testing.T) {
	css := componentCSS(t, "desktopui-sourcelist")
	for _, w := range []string{
		"background: transparent",
		"--desktop-sourcelist-row: 28px",
		"min-height: var(--desktop-sourcelist-row, 28px)",
		"font-size: var(--text-base, 1rem)",
		"background: color-mix(in srgb, var(--color-text, #18181B) 5%, transparent)",
		"font-size: var(--text-xs",
		"outline:",
		"var(--color-accent",
		"html.desktop-inactive",
	} {
		if !strings.Contains(css, w) {
			t.Errorf("source list CSS missing %q:\n%s", w, css)
		}
	}
	// The selection is a soft rounded rect, not a primary button: no
	// accent fill, no white text, no capsule.
	for _, banned := range []string{
		"background: var(--color-accent",
		"var(--radii-full",
		"color: var(--color-primary-fg",
	} {
		if strings.Contains(css, banned) {
			t.Errorf("source list CSS still ships the web-nav selection (%q):\n%s", banned, css)
		}
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

var _ render.HTML = ""
