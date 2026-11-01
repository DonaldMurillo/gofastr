package runtime

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/chromedp"
)

// A boolean attribute is on whenever it is present, so writing
// disabled="false" disables. An attr-mode signal set to false must
// remove the attribute, and checked/value must track the live property,
// which stops following the attribute once the reader has typed or
// clicked. aria-* keeps its "false" text: there it is a value.
func TestSignalAttrFalseRemovesBoolean(t *testing.T) {
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	bind := func(signal, attr string) string {
		return fmt.Sprintf(`data-cui-signal="%s" data-cui-signal-mode="attr" data-cui-signal-attr="%s"`, signal, attr)
	}
	mux := http.NewServeMux()
	handleRuntimeModules(t, mux)
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		fmt.Fprint(w, js)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<!doctype html><html><head><title>attr</title></head><body>
<button id="btn" `+bind("busy", "disabled")+` disabled>save</button>
<p id="note" `+bind("gone", "hidden")+` hidden>note</p>
<details id="more" `+bind("expanded", "open")+` open><summary>s</summary>body</details>
<select id="sel"><option>a</option><option id="opt" `+bind("pick", "selected")+`>b</option></select>
<input id="box" type="checkbox" `+bind("agree", "checked")+`>
<input id="name" `+bind("name", "value")+` value="first">
<button id="tog" `+bind("exp", "aria-expanded")+` aria-expanded="true">t</button>
<span id="ready">ready</span>
<script src="/__gofastr/runtime.js"></script></body></html>`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))
	var off, on map[string]any
	read := `(() => {
  const $ = (id) => document.getElementById(id);
  return {
    btnAttr: $('btn').hasAttribute('disabled'), btnProp: $('btn').disabled,
    noteAttr: $('note').hasAttribute('hidden'), noteProp: $('note').hidden,
    moreAttr: $('more').hasAttribute('open'), moreProp: $('more').open,
    optProp: $('opt').selected,
    boxAttr: $('box').hasAttribute('checked'), boxProp: $('box').checked,
    name: $('name').value,
    aria: $('tog').getAttribute('aria-expanded'),
  };
})()`
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		// The reader dirties the checkbox, both options (picking b, then
		// a) and the text field first: from here their attributes no
		// longer drive what they show.
		chromedp.Click(`#box`, chromedp.ByID),
		chromedp.SetValue(`#sel`, "b", chromedp.ByID),
		chromedp.SetValue(`#sel`, "a", chromedp.ByID),
		chromedp.Click(`#name`, chromedp.ByID),
		chromedp.SendKeys(`#name`, "typed", chromedp.ByID),
		chromedp.Evaluate(`(() => { const G = window.__gofastr;
  for (const n of ['busy', 'gone', 'expanded', 'pick', 'agree', 'exp']) G.setSignal(n, false);
  G.setSignal('name', 'second'); return true; })()`, nil),
		chromedp.Evaluate(read, &off),
		chromedp.Evaluate(`(() => { const G = window.__gofastr;
  for (const n of ['busy', 'gone', 'expanded', 'pick', 'agree', 'exp']) G.setSignal(n, true);
  return true; })()`, nil),
		chromedp.Evaluate(read, &on),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	wantOff := map[string]any{
		"btnAttr": false, "btnProp": false, "noteAttr": false, "noteProp": false,
		"moreAttr": false, "moreProp": false, "optProp": false,
		"boxAttr": false, "boxProp": false, "name": "second", "aria": "false",
	}
	wantOn := map[string]any{
		"btnAttr": true, "btnProp": true, "noteAttr": true, "noteProp": true,
		"moreAttr": true, "moreProp": true, "optProp": true,
		"boxAttr": true, "boxProp": true, "name": "second", "aria": "true",
	}
	for k, w := range wantOff {
		if off[k] != w {
			t.Errorf("after false: %s = %v, want %v", k, off[k], w)
		}
	}
	for k, w := range wantOn {
		if on[k] != w {
			t.Errorf("after true: %s = %v, want %v", k, on[k], w)
		}
	}
}
