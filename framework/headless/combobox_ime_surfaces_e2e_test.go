package headless

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"
)

func TestComboboxIMEKeepsSurfaces(t *testing.T) {
	for _, surface := range []string{"disclosure", "drawer"} {
		for _, composition := range []string{`isComposing: true`, `isComposing: false, keyCode: 229`} {
			for _, expanded := range []bool{true, false} {
				t.Run(fmt.Sprintf("%s/%s/open=%t", surface, composition, expanded), func(t *testing.T) {
					combo := Combobox(ComboboxProps{
						ID: "ime", Name: "q", Label: "Search", Options: []ComboboxOption{{Label: "Birch"}},
					}, nil)
					body := string(Disclosure(DisclosureProps{ID: "surface", Open: true,
						Summary: render.Text("Options"), Content: combo}, nil))
					if surface == "drawer" {
						body = `<script type="application/json" id="gofastr-routes">[{"path":"/"},{"path":"/editor","intercept":{"from":"/","as":"drawer"}}]</script><a id="open-drawer" href="/editor">Edit</a>`
					}
					server := startBehaviorServer(t, body, func(mux *http.ServeMux) {
						mux.HandleFunc("/editor", func(w http.ResponseWriter, r *http.Request) {
							w.Header().Set("Content-Type", "text/html; charset=utf-8")
							w.Header().Set("X-Gofastr-Overlay", "drawer")
							fmt.Fprint(w, render.Tag("section", map[string]string{"id": "surface"}, combo))
						})
					})
					ctx := behaviorPage(t, server)
					if surface == "drawer" {
						if err := chromedp.Run(ctx, chromedp.Click(`#open-drawer`), chromedp.WaitVisible(`#surface`)); err != nil {
							t.Fatal(err)
						}
					}
					if !pollTrue(ctx, comboboxLoaded) {
						t.Fatal("combobox module did not load")
					}
					if surface == "disclosure" && !pollTrue(ctx, disclosureLoaded) {
						t.Fatal("disclosure module did not load")
					}
					var intact bool
					if err := chromedp.Run(ctx,
						chromedp.Focus(`#ime`),
						chromedp.Evaluate(fmt.Sprintf(`(() => {
							const input = document.getElementById('ime'), list = document.getElementById('ime-listbox');
							input.value = 'しらかば'; input.setAttribute('aria-expanded', '%t'); list.hidden = !%t;
							const event = new KeyboardEvent('keydown', {key: 'Escape', bubbles: true, cancelable: true, %s});
							input.dispatchEvent(event);
							const surface = document.getElementById('surface');
							return !event.defaultPrevented && input.isConnected && document.activeElement === input && input.value === 'しらかば' && input.getAttribute('aria-expanded') === '%t' && list.hidden === !%t && !!surface && (surface.tagName !== 'DETAILS' || surface.open);
						})()`, expanded, expanded, composition, expanded, expanded), &intact),
					); err != nil {
						t.Fatal(err)
					}
					if !intact {
						t.Fatal("IME Escape closed the enclosing surface or changed the combobox")
					}
					if err := chromedp.Run(ctx,
						chromedp.Evaluate(`document.getElementById('ime').value = ''; document.getElementById('ime').focus();`, nil),
						chromedp.KeyEvent(kb.ArrowDown), chromedp.KeyEvent(kb.Escape),
						chromedp.Poll(`document.getElementById('ime').getAttribute('aria-expanded') === 'false' && !!document.getElementById('surface')`, nil),
						chromedp.KeyEvent(kb.Escape),
						chromedp.Poll(`!document.getElementById('surface') || document.getElementById('surface').open === false`, nil),
					); err != nil {
						t.Fatal(err)
					}
				})
			}
		}
	}
}
