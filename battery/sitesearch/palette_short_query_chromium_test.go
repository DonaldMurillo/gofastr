//go:build chromium

package sitesearch

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/core-ui/runtime"
	_ "github.com/DonaldMurillo/gofastr/framework/headless"
	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
)

func TestPaletteShortQueryCloses(t *testing.T) {
	module, ok := runtime.Module("headless-combobox")
	if !ok {
		t.Fatal("combobox module is not registered")
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!doctype html><html lang="en"><meta charset="utf-8">
<div data-hui-combobox><input data-hui-combobox-input id="q" role="combobox" aria-controls="lb" aria-expanded="false">
<ul id="lb" role="listbox" hidden data-search data-hui-combobox-listbox data-hui-combobox-static data-hui-combobox-count="{{n}} results"></ul>
<p data-hui-combobox-status data-hui-combobox-no-results="No matches"></p></div>
<script>window.__gofastr = {formatNumber: String};</script>
<script src="/combobox.js"></script><script src="/client.js"></script>`)
	})
	mux.HandleFunc("/combobox.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript")
		fmt.Fprint(w, module)
	})
	mux.HandleFunc("/client.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript")
		fmt.Fprint(w, clientJS)
	})
	mux.HandleFunc(Root+"query", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"results":[{"url":"/cedar","title":"Cedar"}]}`)
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	ctx := chromedptest.Context(t)
	if err := chromedp.Run(ctx, chromedp.Navigate(server.URL), chromedp.Poll(`!!window.__gofastr._sitesearch`, nil)); err != nil {
		t.Fatal(err)
	}
	for _, short := range []string{"x", "!", "𐐀", ""} {
		var closed bool
		if err := chromedp.Run(ctx,
			chromedp.Evaluate(`document.getElementById('q').value = 'cedar'; document.getElementById('q').dispatchEvent(new Event('input', {bubbles: true}))`, nil),
			chromedp.Poll(`!!document.querySelector('#lb [data-search-result]')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
			chromedp.Evaluate(`document.getElementById('q').value = `+jsString(short)+`; document.getElementById('q').dispatchEvent(new Event('input', {bubbles: true}))`, nil),
			chromedp.Evaluate(`document.getElementById('q').getAttribute('aria-expanded') === 'false' && !document.getElementById('q').getAttribute('aria-activedescendant') && document.getElementById('lb').hidden && !document.querySelector('#lb [data-search-result]') && document.querySelector('[data-hui-combobox-status]').textContent === ''`, &closed),
		); err != nil {
			t.Fatal(err)
		}
		if !closed {
			t.Errorf("non-searchable query %q left an open or stale result list", short)
		}
	}
	var commands bool
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`(() => {
			const command = document.createElement('li');
			command.id = 'catalog'; command.setAttribute('role', 'option'); command.textContent = 'Catalog';
			document.getElementById('lb').append(command);
			document.getElementById('q').value = 'c';
			document.getElementById('q').dispatchEvent(new Event('input', {bubbles: true}));
		})()`, nil),
		chromedp.Evaluate(`document.getElementById('q').getAttribute('aria-expanded') === 'true' && !document.getElementById('catalog').hidden && document.querySelector('[data-hui-combobox-status]').textContent === '1 results'`, &commands),
	); err != nil {
		t.Fatal(err)
	}
	if !commands {
		t.Error("short query hid a matching command or retained a stale count")
	}
}
