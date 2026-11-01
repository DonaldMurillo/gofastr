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
			`<div id="isle" data-cui-signal="theme" data-cui-signal-mode="html"><p id="before">before</p></div>`+
			`<a id="swap" href="?theme=1" data-cui-rpc="/__hui/theme" data-cui-rpc-method="GET" data-cui-rpc-signal="theme">Swap</a>`,
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

// A single-key chord is a typed character inside a contenteditable
// editor or a role="textbox" widget, the same as inside an input: "/"
// must not pull focus out of the editor. A chord fires with Shift only
// when it names Shift or Shift is how its character is typed ("?").
func TestE2E_ChordSkipsRichTextEditors(t *testing.T) {
	page := `<div data-hui-shortcut-focus="/" data-hui-shortcut-target="#q"><input id="q" name="q"></div>` +
		`<button type="button" data-hui-shortcut-click="k" onclick="window.__k=(window.__k||0)+1">K</button>` +
		`<button type="button" data-hui-shortcut-click="?" onclick="window.__help=(window.__help||0)+1">Help</button>` +
		`<div id="ed" contenteditable="true">draft</div>` +
		`<div id="tb" role="textbox" tabindex="0">draft</div>`
	b := startBehaviorServer(t, page)
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, controlsLoadedExpr(NavigationBehaviorName)) {
		t.Fatal("the chord marker never loaded headless-navigation")
	}
	press := func(target, init string) string {
		return `document.getElementById('` + target + `').focus();` +
			`document.getElementById('` + target + `').dispatchEvent(new KeyboardEvent('keydown',{` + init + `,bubbles:true,cancelable:true}));`
	}
	var got struct {
		Ed   bool `json:"ed"`
		Tb   bool `json:"tb"`
		K    int  `json:"k"`
		Help int  `json:"help"`
	}
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(press("ed", `key:'/'`), nil),
		chromedp.Evaluate(`window.__ed = document.activeElement === document.getElementById('ed')`, nil),
		chromedp.Evaluate(press("tb", `key:'/'`), nil),
		chromedp.Evaluate(`window.__tb = document.activeElement === document.getElementById('tb')`, nil),
		chromedp.Evaluate(`document.body.focus(); document.activeElement.blur && document.activeElement.blur();`+
			`document.dispatchEvent(new KeyboardEvent('keydown',{key:'K',shiftKey:true,bubbles:true,cancelable:true}));`+
			`document.dispatchEvent(new KeyboardEvent('keydown',{key:'?',shiftKey:true,bubbles:true,cancelable:true}));`, nil),
		chromedp.Evaluate(`({ed: window.__ed, tb: window.__tb, k: window.__k||0, help: window.__help||0})`, &got),
	); err != nil {
		t.Fatal(err)
	}
	if !got.Ed {
		t.Error(`"/" typed in a contenteditable editor moved focus to the search box`)
	}
	if !got.Tb {
		t.Error(`"/" typed in a role="textbox" widget moved focus to the search box`)
	}
	if got.K != 0 {
		t.Error(`Shift+K fired the unshifted "k" chord`)
	}
	if got.Help != 1 {
		t.Errorf(`"?" (typed with Shift) fired %d times, want 1`, got.Help)
	}
}
