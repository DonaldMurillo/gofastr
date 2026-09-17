package uihost

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	// The blank import is load-bearing: framework/ui's init registers
	// the component-options compiler these tests prove. Importing only
	// framework/ui/theme would leave the compiler unregistered and
	// every --fui-* variable unset.
	_ "github.com/DonaldMurillo/gofastr/framework/ui"
	"github.com/DonaldMurillo/gofastr/framework/ui/theme"
	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
)

// The two browser proofs of the theme-options layer, served the way a
// real host serves it: a uihost with framework/ui imported (its init
// registers the component-options compiler), app.css composed at first
// render, scoped themes registered before that.
//
//  1. A scoped theme's dark palette follows the DOCUMENT's scheme
//     (data-color-scheme on <html>): the scope re-declares its own dark
//     tokens under [data-color-scheme="dark"] .fui-theme-<hash>, and
//     the element inside the wrapper resolves --color-background to
//     the SCOPE's dark value, not the root theme's.
//  2. Option variables nest: two complete scoped themes A → B → A, the
//     innermost boundary wins by proximity, and the innermost A resets
//     to A's values.

// themeOptServer boots a uihost on the given theme and serves the
// given body at "/".
func themeOptServer(t *testing.T, th style.Theme, body string) *httptest.Server {
	t.Helper()
	application := app.NewApp("themeopt")
	application.WithTheme(th)
	application.RegisterScreen(
		app.NewScreen("/", &rawHTMLComp{html: body}).WithTitle("theme options"),
		nil,
	)
	ds := New(application)
	mux := http.NewServeMux()
	mux.Handle("/", ds)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func themeOptContext(t *testing.T) context.Context {
	t.Helper()
	return chromedptest.Context(t, chromedptest.AllocatorOptions(chromedp.ExecPath(browserExecutable(t))))
}

// TestScopedDarkModeFollowsTheDocument is the browser half of the
// scoped dark-mode contract. Byte-level emission is pinned in
// core-ui/style; this proves the browser actually resolves a scoped
// element's tokens against the scope's dark palette when <html> flips,
// and against the root's elsewhere.
func TestScopedDarkModeFollowsTheDocument(t *testing.T) {
	if testing.Short() {
		t.Skip("browser e2e: needs a real Chrome")
	}
	root := theme.Default() // complete dark palette; dark background #111113
	scoped := theme.Default()
	// The scope is visually its own: a primary distinct from the root
	// theme's (#4F46E5). That also keeps the process-wide override
	// registry from re-declaring the app default's primary inside this
	// test's scope block, which themevariant_test's leak assertion
	// (correctly) treats as foreign in a variant's app.css.
	scoped.Colors.Primary = style.Color{Name: "primary", Value: "#7C3AED"}
	scoped.Colors.Background = style.Color{Name: "background", Value: "#F0FDF4"}
	scoped.DarkColors["background"] = "#052E16" // distinct from the root's #111113
	ref := style.RegisterThemeOverride(scoped)

	body := fmt.Sprintf(
		`<div class="%s"><p id="in-scope">scoped</p></div>`+
			`<p id="out-of-scope">root</p>`, ref.Class())
	srv := themeOptServer(t, root, body)
	chCtx := themeOptContext(t)
	readVars := map[string]string{}
	err := chromedp.Run(chCtx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.WaitVisible(`#in-scope`, chromedp.ByID),
		// Light mode: force the explicit attribute, the strongest
		// signal — neither dark block matches while it says "light",
		// whatever the OS preference reports.
		chromedp.Evaluate(`document.documentElement.setAttribute('data-color-scheme','light')`, nil),
		chromedp.Evaluate(`(() => {
			const cs = getComputedStyle(document.getElementById('in-scope'));
			return { in: cs.getPropertyValue('--color-background').trim() };
		})()`, &readVars),
	)
	if err != nil {
		t.Fatalf("chromedp (light): %v", err)
	}
	if readVars["in"] != "#F0FDF4" {
		t.Errorf("light: scoped --color-background = %q, want the scope's #F0FDF4", readVars["in"])
	}

	// Flip the document to dark the way the color-scheme module does:
	// data-color-scheme on <html>.
	err = chromedp.Run(chCtx,
		chromedp.Evaluate(`document.documentElement.setAttribute('data-color-scheme','dark')`, nil),
		chromedp.Evaluate(`(() => {
			const inScope = getComputedStyle(document.getElementById('in-scope'));
			const out = getComputedStyle(document.getElementById('out-of-scope'));
			return {
				in: inScope.getPropertyValue('--color-background').trim(),
				out: out.getPropertyValue('--color-background').trim(),
			};
		})()`, &readVars),
	)
	if err != nil {
		t.Fatalf("chromedp (dark): %v", err)
	}
	if readVars["in"] != "#052E16" {
		t.Errorf("dark: scoped --color-background = %q, want the SCOPE's dark #052E16 (the scope must follow the document with its own palette)", readVars["in"])
	}
	if !strings.EqualFold(readVars["out"], "#111113") {
		t.Errorf("dark: root --color-background = %q, want the root theme's #111113", readVars["out"])
	}
}

// TestNestedScopedThemeOptionsWinByProximity proves options-as-custom-
// properties nest: two COMPLETE scoped themes (a boundary declares the
// full option set), nested A → B → A. The probe in B reads B's values
// against A's, and the innermost A resets to A's. If the option
// declarations shipped only at :root, every probe would read the root
// theme's values and the inner assertions would fail — that is the
// load-bearing check.
func TestNestedScopedThemeOptionsWinByProximity(t *testing.T) {
	if testing.Short() {
		t.Skip("browser e2e: needs a real Chrome")
	}
	a := theme.Default(theme.Overrides{
		Primary:    "#7C3AED", // distinct from the app default's #4F46E5 (see the dark-scope test)
		Components: theme.ComponentOptions{Density: theme.Compact, Button: theme.ButtonOptions{Treatment: theme.Outline, Radius: theme.Square}},
	})
	b := theme.Default(theme.Overrides{
		Primary:    "#0EA5E9",
		Components: theme.ComponentOptions{Density: theme.Comfortable, Button: theme.ButtonOptions{Treatment: theme.Filled, Radius: theme.Pill}},
	})
	refA := style.RegisterThemeOverride(a)
	refB := style.RegisterThemeOverride(b)

	body := fmt.Sprintf(
		`<div class="%s"><p id="probe-a1">in A</p>`+
			`<div class="%s"><p id="probe-b">in B</p>`+
			`<div class="%s"><p id="probe-a2">in inner A</p></div>`+
			`</div></div>`, refA.Class(), refB.Class(), refA.Class())
	srv := themeOptServer(t, theme.Default(), body)

	chCtx := themeOptContext(t)

	var got map[string]string
	err := chromedp.Run(chCtx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.WaitVisible(`#probe-a2`, chromedp.ByID),
		chromedp.Evaluate(`(() => {
			const read = (id) => {
				const cs = getComputedStyle(document.getElementById(id));
				return cs.getPropertyValue('--fui-button-radius').trim() + '|' + cs.getPropertyValue('--fui-density-control-h').trim();
			};
			return { a1: read('probe-a1'), b: read('probe-b'), a2: read('probe-a2') };
		})()`, &got),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	// A is compact + square: 36px controls, radius 0. B is comfortable
	// + pill: 44px, 9999px.
	for _, tc := range []struct {
		probe, want, why string
	}{
		{"a1", "0|36px", "inside A only"},
		{"b", "9999px|44px", "the inner B boundary wins by proximity over the outer A"},
		{"a2", "0|36px", "the innermost A resets to A's values, not B's"},
	} {
		if got[tc.probe] != tc.want {
			t.Errorf("probe %s (%s): --fui-button-radius|--fui-density-control-h = %q, want %q", tc.probe, tc.why, got[tc.probe], tc.want)
		}
	}
}

// TestNestedFieldOptionsWinByProximity is the field-family half of the
// same proof: --fui-field-columns and --fui-field-radius nest the same
// way the button and density variables do.
func TestNestedFieldOptionsWinByProximity(t *testing.T) {
	if testing.Short() {
		t.Skip("browser e2e: needs a real Chrome")
	}
	// Distinct primaries, the same precaution the button test takes:
	// a scoped override's tokens ship in every app.css, so a theme
	// carrying the default primary would trip the variant tests'
	// leak assertions next door.
	a := theme.Default(theme.Overrides{
		Primary:    "#7C3AED",
		Components: theme.ComponentOptions{Field: theme.FieldOptions{Layout: theme.Stacked, Radius: theme.FieldSquare}},
	})
	b := theme.Default(theme.Overrides{
		Primary:    "#0EA5E9",
		Components: theme.ComponentOptions{Field: theme.FieldOptions{Layout: theme.Inline, Radius: theme.FieldRound}},
	})
	refA := style.RegisterThemeOverride(a)
	refB := style.RegisterThemeOverride(b)

	body := fmt.Sprintf(
		`<div class="%s"><p id="fprobe-a">in A</p>`+
			`<div class="%s"><p id="fprobe-b">in B</p></div>`+
			`</div>`, refA.Class(), refB.Class())
	srv := themeOptServer(t, theme.Default(), body)

	chCtx := themeOptContext(t)

	var got map[string]string
	err := chromedp.Run(chCtx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.WaitVisible(`#fprobe-b`, chromedp.ByID),
		chromedp.Evaluate(`(() => {
			const read = (id) => {
				const cs = getComputedStyle(document.getElementById(id));
				return cs.getPropertyValue('--fui-field-columns').trim() + '|' + cs.getPropertyValue('--fui-field-radius').trim();
			};
			return { a: read('fprobe-a'), b: read('fprobe-b') };
		})()`, &got),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	for _, tc := range []struct {
		probe, want, why string
	}{
		{"a", "minmax(0, 1fr)|0", "inside A: stacked columns, square corners"},
		{"b", "minmax(8rem, 1fr) minmax(0, 3fr)|8px", "the inner B boundary wins by proximity (the radius resolves to the theme's --radii-md)"},
	} {
		if got[tc.probe] != tc.want {
			t.Errorf("probe %s (%s): --fui-field-columns|--fui-field-radius = %q, want %q", tc.probe, tc.why, got[tc.probe], tc.want)
		}
	}
}
