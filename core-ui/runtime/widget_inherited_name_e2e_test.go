package runtime

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/chromedp"
)

func TestPrototypeNamedWidgetsMount(t *testing.T) {
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	names := []string{"notes", "toString", "valueOf"}
	var catalog, buttons []string
	mux := http.NewServeMux()
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		_, _ = w.Write([]byte(js))
	})
	handleRuntimeModules(t, mux)
	for _, name := range names {
		catalog = append(catalog, fmt.Sprintf(
			`{"hidden":true,"cfg":{"name":%q,"position":"center","chromePath":"/chrome/%s","stylePath":"/style.css"}}`,
			name, name))
		buttons = append(buttons, fmt.Sprintf(`<button id="open-%s" data-cui-open=%q>%s</button>`, name, name, name))
		chrome := fmt.Sprintf(`<div class="cui-widget" data-cui-widget=%q role="dialog">`+
			`<button type="button" id="close-%s" data-cui-action="close">Close</button></div>`, name, name)
		mux.HandleFunc("/chrome/"+name, func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, chrome)
		})
	}
	mux.HandleFunc("/__gofastr/widgets", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, "["+strings.Join(catalog, ",")+"]")
	})
	mux.HandleFunc("/style.css", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/css")
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!doctype html><html><head><title>names</title>
<script type="application/json" id="gofastr-routes">[{"path":"/"}]</script>
<script>
window.__opens=[];window.__closes=[];
document.addEventListener('fui:widget-open',function(event){window.__opens.push(event.detail.name);});
document.addEventListener('fui:widget-close',function(event){window.__closes.push(event.detail.name);});
</script>
</head><body><main>`+strings.Join(buttons, "")+`</main>
<script src="/__gofastr/runtime.js"></script></body></html>`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	ctx := chromedptest.Context(t)
	wait := chromedp.WithPollingTimeout(5 * time.Second)
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.WaitVisible(`#open-notes`, chromedp.ByID),
		chromedp.Poll(`typeof __gofastr.loadModule === 'function'`, nil, wait),
	); err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		root := fmt.Sprintf(`document.querySelectorAll('[data-cui-widget=%q]').length`, name)
		var mounted, opens, closes, remaining int
		if err := chromedp.Run(ctx,
			chromedp.Click(`#open-`+name, chromedp.ByID),
			chromedp.Poll(root+` === 1`, nil, wait),
			chromedp.Poll(fmt.Sprintf(`Object.prototype.hasOwnProperty.call(__gofastr._widgets, %q)`, name), nil, wait),
			chromedp.Evaluate(fmt.Sprintf(`(() => {
  const entry = __gofastr._widgets[%q];
  __gofastr.mountWidget(entry.cfg, document.querySelector('[data-cui-widget=%q]').outerHTML);
  return %s;
})()`, name, name, root), &mounted),
			chromedp.Evaluate(fmt.Sprintf(`window.__opens.filter(name => name === %q).length`, name), &opens),
			chromedp.Click(`#close-`+name, chromedp.ByID),
			chromedp.Poll(root+` === 0`, nil, wait),
			chromedp.Evaluate(fmt.Sprintf(`window.__closes.filter(name => name === %q).length`, name), &closes),
			chromedp.Evaluate(root, &remaining),
		); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if mounted != 1 {
			t.Errorf("%s: a second mount of the mounted widget left %d roots, want 1", name, mounted)
		}
		if opens != 1 {
			t.Errorf("%s: fui:widget-open fired %d times, want 1", name, opens)
		}
		if closes != 1 {
			t.Errorf("%s: fui:widget-close fired %d times, want 1", name, closes)
		}
		if remaining != 0 {
			t.Errorf("%s: %d roots left after close, want 0", name, remaining)
		}
	}
}
