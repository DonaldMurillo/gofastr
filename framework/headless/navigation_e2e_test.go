package headless

// Browser coverage for headless-navigation's theme half: an island
// swap whose inserted ROOT is the theme toggle group itself must still
// get its options initialised. The arrival pass resolves the group
// with within(), so a subtree whose root IS the marker matches — where
// scope.querySelector would see only descendants and leave every
// aria-checked exactly as the server shipped it. Same harness as
// behavior_e2e_test.go.

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/chromedp/chromedp"
)

func TestE2E_ThemeToggleArrivingAsASwapRootIsInitialised(t *testing.T) {
	// The pill group exactly as the component renders it: every option
	// unchecked, because the scheme it should reflect arrives only
	// with the module's pass.
	toggleGroup := `<div data-hui-theme-toggle="" role="radiogroup" aria-label="Colour scheme">` +
		`<button type="button" role="radio" aria-checked="false" data-hui-theme-option="light">Light</button>` +
		`<button type="button" role="radio" aria-checked="false" data-hui-theme-option="auto">Auto</button>` +
		`<button type="button" role="radio" aria-checked="false" data-hui-theme-option="dark">Dark</button>` +
		`</div>`
	// A boot marker beside the region (the back-to-top link): the
	// module must be loaded BEFORE the click, so the proof is the
	// arrival pass on the inserted subtree, not the module loading on
	// the marker the swap brought.
	b := startBehaviorServer(t,
		`<a id="top" href="#main" data-hui-back-to-top="">Top</a>`+
			`<div id="isle" data-fui-signal="theme" data-fui-signal-mode="html"><p id="before">before</p></div>`+
			`<a id="swap" href="?theme=1" data-fui-rpc="/__hui/theme" data-fui-rpc-method="GET" data-fui-rpc-signal="theme">Swap</a>`,
		func(mux *http.ServeMux) {
			mux.HandleFunc("/__hui/theme", func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/html")
				fmt.Fprint(w, toggleGroup)
			})
		})
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, controlsLoadedExpr(NavigationBehaviorName)) {
		t.Fatal("the back-to-top marker never loaded headless-navigation")
	}
	if err := chromedp.Run(ctx, chromedp.Click(`#swap`, chromedp.ByID)); err != nil {
		t.Fatalf("clicking the island anchor: %v", err)
	}
	// The stored scheme is the default (auto) on a fresh profile, so
	// the group's auto option is the one the pass must check.
	const checked = `(() => {
		const opt = document.querySelector('[data-hui-theme-option="auto"]');
		return !!opt && opt.getAttribute('aria-checked') === 'true';
	})()`
	if !pollTrue(ctx, checked) {
		t.Fatal("the toggle group that arrived as a swap root never had its options initialised — " +
			"the arrival pass must match the subtree's root, not only its descendants")
	}
}
