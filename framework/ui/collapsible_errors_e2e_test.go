package ui_test

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/ui"
	"github.com/chromedp/chromedp"
)

// A refused save whose error lands inside closed Collapsibles opens every
// one around it: open, aria-expanded and what is on screen agree, so the
// first click on a summary closes it rather than "opening" content
// already shown.
func TestCollapsibleOpensOnFieldError(t *testing.T) {
	section := ui.Collapsible(ui.CollapsibleConfig{Summary: "Advanced", ID: "outer"},
		ui.Collapsible(ui.CollapsibleConfig{Summary: "Billing", ID: "sec"},
			ui.TextField(ui.TextFieldConfig{Name: "estimate", Label: "Estimate", ID: "f-estimate"})))
	body := `<form id="f" data-cui-rpc="/rpc/save" data-cui-rpc-method="POST">` + string(section) +
		`<button type="submit" id="go">Save</button></form><p id="ready">ready</p>`
	ctx := moduleTestCtxMux(t, body, func(mux *http.ServeMux) {
		mux.HandleFunc("/rpc/save", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnprocessableEntity)
			fmt.Fprint(w, `{"error":"validation failed","fields":{"estimate":["must be an integer"]},"success":false}`)
		})
	})
	if !pollJS(ctx, moduleLoaded("headless-disclosure")) {
		t.Fatal("the collapsible never loaded headless-disclosure")
	}
	if err := chromedp.Run(ctx, chromedp.Click(`#go`, chromedp.ByID)); err != nil {
		t.Fatal(err)
	}
	if !pollJS(ctx, `document.querySelector('#f-estimate').getAttribute('aria-invalid')==='true'`) {
		t.Fatal("the 422 never marked the field invalid")
	}
	state := `(function(){var out=[];['outer','sec'].forEach(function(id){var d=document.getElementById(id);` +
		`out.push(d.open, d.querySelector(':scope > summary').getAttribute('aria-expanded'));});` +
		`out.push(document.querySelector('#f-estimate').checkVisibility());return JSON.stringify(out);})()`
	if !pollJS(ctx, state+`==='[true,"true",true,"true",true]'`) {
		t.Fatalf("[outer open, expanded, inner open, expanded, visible] = %s, want all open", evalString(ctx, state))
	}
}
