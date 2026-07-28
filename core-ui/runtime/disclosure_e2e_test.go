package runtime

import (
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// TestDisclosureModuleKeepsA11yBehaviour proves the disclosure feature
// still works after it moved from core into a demand-loaded module.
//
// The module carries keyboard and assistive-tech behaviour that native
// <details> does not provide: aria-expanded on the summary, Escape from
// anywhere on the page, and the opt-in inert focus trap. Core keeps only
// the close-on-navigate lines. A source-grep cannot tell whether the
// module actually loads and wires itself, so this drives a real browser.
func TestDisclosureModuleKeepsA11yBehaviour(t *testing.T) {
	g := startGadgetServer(t, `[]`, `
<div id="sibling">other content</div>
<details id="d" data-fui-disclosure data-fui-disclosure-trap>
  <summary id="s">Menu</summary>
  <a id="inside" href="/x">item</a>
</details>`)

	ctx := newSeedBrowserCtx(t)
	var expandedClosed, expandedOpen, siblingInert, openAfterEsc, expandedAfterEsc string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(g.Srv.URL+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		// The module is fetched by the initial marker scan; give it a
		// moment to arrive and run its own pass over the SSR markup.
		chromedp.Sleep(500*time.Millisecond),
		chromedp.Evaluate(`String(document.getElementById('s').getAttribute('aria-expanded'))`, &expandedClosed),
		chromedp.Click(`#s`, chromedp.ByID),
		chromedp.Sleep(150*time.Millisecond),
		chromedp.Evaluate(`String(document.getElementById('s').getAttribute('aria-expanded'))`, &expandedOpen),
		chromedp.Evaluate(`String(document.getElementById('sibling').hasAttribute('inert'))`, &siblingInert),
		chromedp.KeyEvent(""),
		chromedp.Sleep(150*time.Millisecond),
		chromedp.Evaluate(`String(document.getElementById('d').hasAttribute('open'))`, &openAfterEsc),
		chromedp.Evaluate(`String(document.getElementById('s').getAttribute('aria-expanded'))`, &expandedAfterEsc),
	); err != nil {
		t.Fatal(err)
	}

	if expandedClosed != "false" {
		t.Errorf("initial pass did not mirror aria-expanded on a server-rendered disclosure: got %q, want \"false\"", expandedClosed)
	}
	if expandedOpen != "true" {
		t.Errorf("aria-expanded not mirrored on open: got %q, want \"true\"", expandedOpen)
	}
	if siblingInert != "true" {
		t.Errorf("focus trap did not make body siblings inert on open: got %q", siblingInert)
	}
	if openAfterEsc != "false" {
		t.Errorf("Escape did not close the disclosure: still open (%q)", openAfterEsc)
	}
	if expandedAfterEsc != "false" {
		t.Errorf("aria-expanded not mirrored on close: got %q, want \"false\"", expandedAfterEsc)
	}
}

// The trap's `inert` is released by the details element's own `toggle`
// event, and a DETACHED <details> never fires one. Both SPA swap paths
// detach it: swapLayoutShell replaces the whole [data-fui-layout] subtree,
// and swapMainContent writes main.innerHTML before its close-open-
// disclosures sweep can see a drawer that lived inside <main>. Without a
// release the inert sticks to every remaining <body> child — out of the
// focus order AND the accessibility tree — for the life of the tab.
func TestTrapReleasesInertOnDetach(t *testing.T) {
	// detach is the JS the runtime itself runs, verbatim in shape: the
	// cross-layout shell replaceWith, and the main-content innerHTML write.
	cases := []struct{ name, detach string }{
		{"layout shell swap", `(() => {
			const cur = document.querySelector('[data-fui-layout]');
			const el = document.createElement('div');
			el.setAttribute('data-fui-layout', 'app');
			el.innerHTML = '<main role="main">app shell</main>';
			cur.replaceWith(el);
		})()`},
		{"main content swap", `document.querySelector('[role="main"]').innerHTML = '<p>next screen</p>'`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g := startGadgetServer(t, `[]`, `
<div id="sibling">other content</div>
<div id="shell" data-fui-layout="marketing">
  <main role="main">
    <details id="d" data-fui-disclosure data-fui-disclosure-trap>
      <summary id="s">Menu</summary>
      <a id="inside" href="/x">item</a>
    </details>
  </main>
</div>`)

			ctx := newSeedBrowserCtx(t)
			var inertWhileOpen, inertAfterDetach string
			if err := chromedp.Run(ctx,
				chromedp.Navigate(g.Srv.URL+"/"),
				chromedp.WaitVisible(`#ready`, chromedp.ByID),
				chromedp.Sleep(600*time.Millisecond),
				chromedp.Click(`#s`, chromedp.ByID),
				chromedp.Sleep(200*time.Millisecond),
				chromedp.Evaluate(`String(document.getElementById('sibling').hasAttribute('inert'))`, &inertWhileOpen),
				chromedp.Evaluate(tc.detach, nil),
				chromedp.Sleep(300*time.Millisecond),
				chromedp.Evaluate(`String(document.getElementById('sibling').hasAttribute('inert'))`, &inertAfterDetach),
			); err != nil {
				t.Fatal(err)
			}

			if inertWhileOpen != "true" {
				t.Fatalf("precondition: the trap never engaged (sibling inert=%q)", inertWhileOpen)
			}
			if inertAfterDetach == "true" {
				t.Errorf("focus trap leaked: #sibling is still inert after the open trapped "+
					"disclosure was detached (%s)", tc.name)
			}
		})
	}
}
