package uihost

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/style"
)

// style.Contribute is documented as THE one-off-screen-styles path
// (ui-getting-started.md §"Co-located screen styles"). The host must
// fan contributed fragments into app.css itself — with theme-token
// substitution — so a bare Contribute works on any app without
// hand-wiring style.Apply into WithCustomCSS.
func TestAppCSSServesContributedStyles(t *testing.T) {
	style.ResetRegistryForTest()
	t.Cleanup(style.ResetRegistryForTest)

	style.Contribute(func(ss *style.StyleSheet) {
		ss.Rule(".contrib-hero").
			Set("padding", "{spacing.lg}").
			End()
	})

	ds := newTestUIHost() // no WithCustomCSS wiring at all

	req := httptest.NewRequest("GET", "/__gofastr/app.css", nil)
	w := httptest.NewRecorder()
	ds.ServeHTTP(w, req)

	body := w.Body.String()
	if !strings.Contains(body, ".contrib-hero") {
		t.Fatalf("app.css missing contributed .contrib-hero rule — style.Contribute is a silent no-op:\n%s",
			truncate(body, 600))
	}
	if !strings.Contains(body, "var(--spacing-lg)") {
		t.Errorf("contributed rule lost theme-token substitution ({spacing.lg} -> var(--spacing-lg)):\n%s",
			truncate(body, 600))
	}
}

// Contributed fragments must land AFTER the WithCustomCSS payload —
// the layer the hand-wired style.Apply pattern put them in — so a
// screen can override host base rules by re-declaring the selector.
func TestContributedCSSAfterCustomCSS(t *testing.T) {
	style.ResetRegistryForTest()
	t.Cleanup(style.ResetRegistryForTest)

	style.Contribute(func(ss *style.StyleSheet) {
		ss.Rule(".order-probe").Set("color", "{colors.primary}").End()
	})

	ds := newTestUIHost()
	WithCustomCSS(".order-probe { color: red; }")(ds)

	// Match complete rules: the theme's :root block legitimately references
	// var(--color-primary) before custom CSS.
	const customRule = ".order-probe { color: red; }"
	const contributedRule = ".order-probe {\n  color: var(--color-primary);\n}"

	body := ds.AppCSS()
	custom := strings.Index(body, customRule)
	contributed := strings.Index(body, contributedRule)
	if custom == -1 || contributed == -1 {
		t.Fatalf("app.css missing custom (%d) or contributed (%d) rule:\n%s",
			custom, contributed, truncate(body, 800))
	}
	if contributed < custom {
		t.Errorf("contributed rule emitted BEFORE customCSS — screens can no longer override host base rules (contributed at %d, custom at %d)",
			contributed, custom)
	}
}
