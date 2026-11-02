//go:build chromium

package ui

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/ui/theme"
)

// The reveal button is a toggle button whose word never changes
// ("Show"), so the pressed state has to be seen on the button: the
// kit paints aria-pressed="true" on the soft surface, as a hovered
// button, and leaves the resting button transparent. Light and dark.
func TestPasswordRevealPaintsItsPressedState(t *testing.T) {
	th := theme.Default()
	for _, scheme := range []string{"light", "dark"} {
		css := th.CSSCustomProperties() + passwordInputStyle.Entry().CSSFor(th)
		page := PasswordInput(PasswordInputConfig{Name: "pw", ID: "pw"})
		m := geometryOf(t, css, page, `(() => {
			document.documentElement.setAttribute('data-color-scheme', '`+scheme+`');
			const btn = document.querySelector('[data-hui-reveal]');
			const soft = document.createElement('span');
			soft.style.background = 'var(--color-surface-soft)';
			document.body.append(soft);
			const want = getComputedStyle(soft).backgroundColor;
			const rest = getComputedStyle(btn).backgroundColor;
			btn.setAttribute('aria-pressed', 'true');
			const pressed = getComputedStyle(btn).backgroundColor;
			return {pressedSoft: pressed === want ? 1 : 0, restClear: rest === 'rgba(0, 0, 0, 0)' ? 1 : 0, softSet: want !== 'rgba(0, 0, 0, 0)' ? 1 : 0};
		})()`)
		if m["softSet"] != 1 {
			t.Fatalf("%s: --color-surface-soft did not resolve; the probe proves nothing", scheme)
		}
		if m["restClear"] != 1 {
			t.Errorf("%s: the resting reveal button is not transparent", scheme)
		}
		if m["pressedSoft"] != 1 {
			t.Errorf("%s: the pressed reveal button is not painted on --color-surface-soft: its word never changes, so nothing shows it is on", scheme)
		}
	}
}

// The input sat transparent on the shell's surface with no colour of
// its own, so the browser's field text (black) painted a revealed
// password black on the dark surface (seen in the D10 screenshots).
// The typed text takes --color-text in both schemes, as .fui-input's
// does.
func TestPasswordInputTextFollowsTheScheme(t *testing.T) {
	th := theme.Default()
	css := th.CSSCustomProperties() + passwordInputStyle.Entry().CSSFor(th)
	for _, scheme := range []string{"light", "dark"} {
		m := geometryOf(t, css, PasswordInput(PasswordInputConfig{Name: "pw", ID: "pw"}), `(() => {
			document.documentElement.setAttribute('data-color-scheme', '`+scheme+`');
			const probe = document.createElement('span');
			probe.style.color = 'var(--color-text)';
			document.body.append(probe);
			return {same: getComputedStyle(document.getElementById('pw')).color === getComputedStyle(probe).color ? 1 : 0};
		})()`)
		if m["same"] != 1 {
			t.Errorf("%s: the password input's text is not --color-text", scheme)
		}
	}
}
