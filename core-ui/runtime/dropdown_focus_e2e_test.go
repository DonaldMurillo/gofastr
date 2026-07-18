package runtime

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

func TestDropdownEscapeRestoresPanelFocusWithoutStealingOtherDismissals(t *testing.T) {
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	dropdownJS, ok := Module("dropdown")
	if !ok {
		t.Fatal("dropdown module not embedded")
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		_, _ = w.Write([]byte(js))
	})
	mux.HandleFunc("/__gofastr/runtime/dropdown.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		_, _ = w.Write([]byte(dropdownJS))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<!doctype html><html><head><title>dropdown-focus</title></head><body>
  <div id="one" data-fui-dropdown-wrap data-fui-dropdown-open>
    <button id="trigger-one" data-fui-dropdown aria-expanded="true">One</button>
    <div id="panel-one" data-fui-dropdown-panel>
      <div><a id="nested-one" href="#one-action">Nested one</a></div>
    </div>
  </div>
  <div id="two" data-fui-dropdown-wrap data-fui-dropdown-open>
    <button id="trigger-two" data-fui-dropdown aria-expanded="true">Two</button>
    <div id="panel-two" data-fui-dropdown-panel>
      <div><button id="nested-two">Nested two</button></div>
    </div>
  </div>
  <button id="outside">Outside</button>
  <script src="/__gofastr/runtime.js"></script>
</body></html>`)
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	ctx := newSeedBrowserCtx(t)

	var result map[string]any
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.WaitVisible(`#nested-two`, chromedp.ByID),
		chromedp.Sleep(300*time.Millisecond),
		// A modal owns Escape while it is active, so the dropdown remains open.
		chromedp.Evaluate(`window.__gofastr._modalStack = ['modal']; document.getElementById('nested-two').focus()`, nil),
		chromedp.KeyEvent("\x1b"),
		chromedp.Sleep(50*time.Millisecond),
		chromedp.Evaluate(`({
			twoOpenDuringModal: !document.getElementById('panel-two').hidden,
			focusDuringModal: document.activeElement.id
		})`, &result),
	); err != nil {
		t.Fatalf("modal precedence: %v", err)
	}
	if result["twoOpenDuringModal"] != true || result["focusDuringModal"] != "nested-two" {
		t.Fatalf("modal must retain Escape precedence, got %#v", result)
	}

	if err := chromedp.Run(ctx,
		// With no modal, Escape closes only the focused/topmost dropdown and
		// restores focus to its own trigger. The other dropdown stays open.
		chromedp.Evaluate(`window.__gofastr._modalStack = []`, nil),
		chromedp.KeyEvent("\x1b"),
		chromedp.Sleep(50*time.Millisecond),
		chromedp.Evaluate(`({
			oneOpen: !document.getElementById('panel-one').hidden,
			twoHidden: document.getElementById('panel-two').hidden,
			twoExpanded: document.getElementById('trigger-two').getAttribute('aria-expanded'),
			active: document.activeElement.id
		})`, &result),
	); err != nil {
		t.Fatalf("focused dropdown Escape: %v", err)
	}
	if result["oneOpen"] != true || result["twoHidden"] != true ||
		result["twoExpanded"] != "false" || result["active"] != "trigger-two" {
		t.Fatalf("Escape must close one dropdown and restore its trigger, got %#v", result)
	}

	if err := chromedp.Run(ctx,
		// Outside-click closes without stealing focus.
		chromedp.Click(`#outside`, chromedp.ByID),
		chromedp.Sleep(50*time.Millisecond),
		chromedp.Evaluate(`({
			oneHidden: document.getElementById('panel-one').hidden,
			active: document.activeElement.id
		})`, &result),
	); err != nil {
		t.Fatalf("outside dismissal: %v", err)
	}
	if result["oneHidden"] != true || result["active"] != "outside" {
		t.Fatalf("outside dismissal must not move focus, got %#v", result)
	}

	if err := chromedp.Run(ctx,
		// Reopen, move focus outside, and dismiss through SPA navigation.
		chromedp.Click(`#trigger-one`, chromedp.ByID),
		chromedp.Evaluate(`document.getElementById('outside').focus();
			document.dispatchEvent(new CustomEvent('gofastr:navigate'))`, nil),
		chromedp.Sleep(100*time.Millisecond),
		chromedp.Evaluate(`({
			oneHidden: document.getElementById('panel-one').hidden,
			oneExpanded: document.getElementById('trigger-one').getAttribute('aria-expanded'),
			active: document.activeElement.id
		})`, &result),
	); err != nil {
		t.Fatalf("navigation dismissal: %v", err)
	}
	if result["oneHidden"] != true || result["oneExpanded"] != "false" ||
		result["active"] != "outside" {
		t.Fatalf("navigation dismissal must not move focus, got %#v", result)
	}
}
