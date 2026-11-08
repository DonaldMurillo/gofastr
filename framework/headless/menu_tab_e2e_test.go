package headless

import (
	"strconv"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"
)

// Tab and Shift+Tab out of an open menu close it, whichever element
// holds focus when the key goes down. A menu whose rows are all
// disabled keeps focus on its trigger after Enter opens it (there is
// no row to focus), so the trigger's own Tab is the only close the
// reader gets. The keys are real trusted presses: Enter runs the
// summary's native toggle, and Tab moves focus through the browser's
// own sequential navigation. Where focus lands is checked on the
// summary path only: the caller-owned path's details has no summary
// element, so the browser supplies one, a focus stop the module does
// not render.
func TestMenuTabClosesFromEveryTrigger(t *testing.T) {
	rows := func(disabled bool) []MenuItem {
		return []MenuItem{
			{Label: "Publish", Href: "/publish", Disabled: disabled},
			{Label: "Archive", Href: "/archive", Disabled: disabled},
		}
	}
	for _, tc := range []struct {
		name      string
		disabled  bool
		trigger   render.HTML
		openFocus string
	}{
		{name: "summary, rows disabled", disabled: true, openFocus: "summary"},
		{name: "summary, rows enabled", openFocus: "Publish"},
		{name: "button, rows disabled", disabled: true, trigger: render.HTML(`<button type="button" id="cmds-trigger">Commands</button>`), openFocus: "Commands"},
		{name: "button, rows enabled", trigger: render.HTML(`<button type="button" id="cmds-trigger">Commands</button>`), openFocus: "Publish"},
	} {
		for _, back := range []bool{false, true} {
			name := tc.name + ", Tab"
			if back {
				name = tc.name + ", Shift+Tab"
			}
			t.Run(name, func(t *testing.T) {
				menu := Menu(MenuProps{ID: "cmds", Label: "Commands", TriggerElement: tc.trigger, Items: rows(tc.disabled)}, nil)
				page := `<button type="button" id="before">Before</button>` + string(menu) + `<button type="button" id="after">After</button>`
				ctx := behaviorPage(t, startBehaviorServer(t, page))
				if !pollTrue(ctx, menuLoaded) || !pollTrue(ctx, disclosureLoaded) {
					t.Fatal("menu modules did not load")
				}
				trigger := `details[data-hui-menu="cmds"] > summary`
				if tc.trigger != "" {
					trigger = `#cmds-trigger`
				}
				if err := chromedp.Run(ctx,
					chromedp.Focus(trigger, chromedp.ByQuery),
					chromedp.KeyEvent(kb.Enter),
				); err != nil {
					t.Fatal(err)
				}
				focused := `(() => {
					const el = document.activeElement;
					if (!el) return '';
					if (el.tagName === 'SUMMARY') return 'summary';
					return el.textContent.trim();
				})()`
				if !pollTrue(ctx, `document.querySelector('details[data-hui-menu="cmds"]').open`) {
					t.Fatal("Enter on the trigger did not open the menu")
				}
				if !pollTrue(ctx, focused+` === `+strconv.Quote(tc.openFocus)) {
					t.Fatalf("after Enter focus is on %q, want %q", evalString(t, ctx, focused), tc.openFocus)
				}
				tab := chromedp.KeyEvent(kb.Tab)
				if back {
					tab = chromedp.KeyEvent(kb.Tab, chromedp.KeyModifiers(input.ModifierShift))
				}
				if err := chromedp.Run(ctx, tab); err != nil {
					t.Fatal(err)
				}
				if !pollTrue(ctx, `!document.querySelector('details[data-hui-menu="cmds"]').open`) {
					t.Fatalf("the menu stayed open after the key; focus is on %q", evalString(t, ctx, focused))
				}
				if tc.trigger != "" {
					return
				}
				want := "After"
				if back && tc.disabled {
					want = "Before"
				} else if back {
					want = "summary"
				}
				if got := evalString(t, ctx, focused); got != want {
					t.Fatalf("focus after the key is on %q, want %q", got, want)
				}
			})
		}
	}
}
