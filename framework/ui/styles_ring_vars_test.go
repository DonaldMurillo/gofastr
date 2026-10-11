package ui

import (
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/framework/ui/theme"
)

// Every focus ring in the kit is the same neutral ring, so a keyboard
// user sees one focus treatment across controls. Eighteen sheets still
// drew it in the brand colour after the reskin; this pins the sweep.
func TestFocusRingIsNeutral(t *testing.T) {
	// The bridge spelling var(--fui-primary, var(--color-primary…)) and a
	// thumb's focus halo hid three more from the first pattern, and a
	// danger ring on the repeater's remove button one more.
	// The width is a stroke token since the stroke sweep; the literal
	// spelling stays matched for a sheet that regresses to it.
	colorRing := regexp.MustCompile(`outline:\s*(?:[0-9.]+px|var\(--stroke-focus(?:,\s*[0-9.]+px)?\)) solid var\((--fui-primary,\s*var\()?--color-(primary|danger|accent|info|success|warning)` +
		`|focus-visible[^{]*\{[^}]*box-shadow:[^;}]*var\(--color-(primary|danger|accent|info|success|warning)`)
	thm := style.DefaultTheme()
	for _, e := range registry.All() {
		for _, m := range colorRing.FindAllString(e.CSSFor(thm), -1) {
			t.Errorf("%s: %q; spell the ring outline: var(--stroke-focus, 2px) solid var(--color-text-subtle)", e.Name, m)
		}
	}
}

// A var() naming a token the theme never declares falls back to its
// literal forever and ignores every re-theme: --radius-lg (for
// --radii-lg) and --fonts-mono (for --font-mono) did exactly that.
// Each read must name a theme token, a property some sheet declares,
// or a component knob (--ui-*, --fui-*, --cui-*, --hui-*), which
// exists to be set by a host and is undeclared by design.
func TestSheetVarsNameDeclaredTokens(t *testing.T) {
	// Properties a host page sets, not the theme: --nav-h is the site
	// header height the anchored rail sticks under, --color-overlay the
	// intercept overlay's scrim. Both fall back to a literal unset.
	hostSet := []string{"--nav-h", "--color-overlay"}

	declRe := regexp.MustCompile(`(--[A-Za-z0-9_-]+)\s*:`)
	readRe := regexp.MustCompile(`var\((--[A-Za-z0-9_-]+)`)
	knob := regexp.MustCompile(`^--(ui|fui|cui|hui)-`)

	thm := style.DefaultTheme()
	declared := map[string]bool{}
	for _, root := range []string{
		thm.CSSCustomProperties(),
		theme.Default(theme.Overrides{}).CSSCustomProperties(),
	} {
		for _, m := range declRe.FindAllStringSubmatch(root, -1) {
			declared[m[1]] = true
		}
	}
	// The intercept overlay's sheet ships from core-ui/app, outside the
	// registry; its --radius-lg reads were the case that started this.
	sheets := map[string]string{"app.InterceptOverlayCSS": app.InterceptOverlayCSS()}
	for _, e := range registry.All() {
		css := e.CSSFor(thm)
		sheets[e.Name] = css
		for _, m := range declRe.FindAllStringSubmatch(css, -1) {
			declared[m[1]] = true
		}
	}
	for _, name := range slices.Sorted(func(yield func(string) bool) {
		for k := range sheets {
			if !yield(k) {
				return
			}
		}
	}) {
		seen := map[string]bool{}
		for _, m := range readRe.FindAllStringSubmatch(sheets[name], -1) {
			v := m[1]
			if declared[v] || knob.MatchString(v) || slices.Contains(hostSet, v) || seen[v] {
				continue
			}
			seen[v] = true
			t.Errorf("%s reads %s, which no theme or sheet declares", name, v)
		}
	}
	if !strings.Contains(thm.CSSCustomProperties(), "--radii-lg") {
		t.Fatal("theme root lost --radii-lg: the declared set this test reads is empty or renamed")
	}
}
