package ui_test

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/framework/ui"
	"github.com/DonaldMurillo/gofastr/framework/ui/theme"
	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/chromedp"
)

// A button's chord clicks it while focus sits in a field, and a copy
// of the same chord under an inert layer (a drawer under the top one)
// never wins, though it comes first in the document.
func TestButtonShortcutSkipsInertLayers(t *testing.T) {
	if testing.Short() {
		t.Skip("browser E2E disabled in short mode")
	}
	th := theme.Default()
	var css strings.Builder
	for _, e := range registry.All() {
		css.WriteString(e.CSSFor(th))
	}
	head := "<style>" + th.CSSCustomProperties() + "\n" + css.String() + "</style>"
	layer := func(id string) string {
		return `<form onsubmit="return false">` +
			`<input name="n" id="` + id + `-in">` +
			string(ui.Button(ui.ButtonConfig{Label: "Save", ID: id + "-save", Shortcut: "Mod+S"})) +
			`</form>`
	}
	body := `<div inert>` + layer("under") + `</div>` + layer("top") +
		`<script>document.addEventListener('click', (e) => { const b = e.target.closest('button'); if (b) window.__clicked = b.id; });</script>`
	srv := themeTestPageWithHead(t, head, body)
	ctx := moduleTestCtxURL(t, srv.URL)

	var got struct {
		Clicked string
		Name    string
		Keys    string
	}
	if err := chromedp.Run(ctx,
		chromedp.Focus(`#top-in`, chromedp.ByQuery),
		chromedp.KeyEvent("s", chromedp.KeyModifiers(input.ModifierCtrl)),
		chromedp.Evaluate(`({
			clicked: window.__clicked || "",
			name: document.querySelector("#top-save").getAttribute("aria-label") || "",
			keys: document.querySelector("#top-save").getAttribute("aria-keyshortcuts") || "",
		})`, &got),
	); err != nil {
		t.Fatal(err)
	}
	if got.Clicked != "top-save" {
		t.Errorf("Ctrl+S clicked %q, want the top layer's top-save", got.Clicked)
	}
	if got.Name != "Save" || got.Keys != "Meta+S Control+S" {
		t.Errorf("name %q, keyshortcuts %q; want Save and Meta+S Control+S", got.Name, got.Keys)
	}
}
