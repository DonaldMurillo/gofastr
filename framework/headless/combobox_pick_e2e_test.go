package headless

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/chromedp/chromedp"
)

// A picker in a host form: the form submits the hidden value, never the
// query; a search swaps server rows in, and a click picks one, writing
// its id to the hidden input and its label to the search input. Typing
// that is not a pick goes back on blur; emptying the input clears the
// value.
func TestE2E_ComboboxPickInsideAForm(t *testing.T) {
	box := Combobox(ComboboxProps{ID: "pc", Name: "q", Label: "Customer",
		Island:  &Island{Endpoint: "/opts", Signal: "pick-pc"},
		Pick:    &ComboboxPick{Name: "customer_id", Value: "c1", Label: "Ada"},
		Options: []ComboboxOption{{Value: "c1", Label: "Ada"}},
	}, nil)
	page := `<form id="host">` + string(box) + `</form>`
	b := startBehaviorServer(t, page, func(mux *http.ServeMux) {
		mux.HandleFunc("/opts", func(w http.ResponseWriter, r *http.Request) {
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			opts := []ComboboxOption{{Value: "c1", Label: "Ada"}, {Value: "c2", Label: "Grace"}}
			if q := strings.ToLower(body["q"]); q != "" {
				var keep []ComboboxOption
				for _, o := range opts {
					if strings.Contains(strings.ToLower(o.Label), q) {
						keep = append(keep, o)
					}
				}
				opts = keep
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Write([]byte(ComboboxRows("pc-listbox", opts, nil)))
		})
	})
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, comboboxLoaded) {
		t.Fatal("the picker never loaded headless-combobox")
	}
	formJS := `JSON.stringify(Object.fromEntries(new FormData(document.getElementById('host'))))`
	var form string
	if err := chromedp.Run(ctx, chromedp.Evaluate(formJS, &form)); err != nil {
		t.Fatal(err)
	}
	if form != `{"customer_id":"c1"}` {
		t.Fatalf("the host form submits %s, want only the hidden value", form)
	}
	if err := chromedp.Run(ctx,
		chromedp.Click(`#pc`, chromedp.ByQuery),
		chromedp.SendKeys(`#pc`, "gr", chromedp.ByQuery),
	); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `document.querySelector('#pc-listbox [data-value="c2"]') !== null && document.querySelector('#pc-listbox [data-value="c1"]') === null`) {
		t.Fatal("the search never swapped the server's rows in")
	}
	if err := chromedp.Run(ctx, chromedp.Click(`#pc-listbox [data-value="c2"]`, chromedp.ByQuery)); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `document.querySelector('[data-hui-combobox-value]').value === 'c2' && document.getElementById('pc').value === 'Grace'`) {
		t.Fatal("a click did not pick Grace")
	}
	// Typing that is not a pick goes back on blur.
	if err := chromedp.Run(ctx,
		chromedp.SendKeys(`#pc`, "zz", chromedp.ByQuery),
		chromedp.Evaluate(`document.getElementById('pc').blur()`, nil),
	); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `document.getElementById('pc').value === 'Grace' && document.querySelector('[data-hui-combobox-value]').value === 'c2'`) {
		t.Fatal("unpicked typing did not go back to the picked label")
	}
	// Emptying the input clears the value.
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`(() => { const i = document.getElementById('pc'); i.focus(); i.value = ''; i.blur(); })()`, nil),
	); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `document.querySelector('[data-hui-combobox-value]').value === ''`) {
		t.Fatal("an emptied picker kept its value")
	}
}
