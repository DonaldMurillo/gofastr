//go:build chromium

package sitesearch

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core-ui/runtime"
	_ "github.com/DonaldMurillo/gofastr/framework/headless"
	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/cdproto/target"
	"github.com/chromedp/chromedp"
)

func TestPaletteMiddleClickOpensTab(t *testing.T) {
	module, ok := runtime.Module("headless-combobox")
	if !ok {
		t.Fatal("combobox module is not registered")
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!doctype html><html lang="en"><div data-hui-combobox>
<input id="q" data-hui-combobox-input role="combobox" aria-controls="lb" aria-expanded="false">
<ul id="lb" role="listbox" data-search data-search-all="/search" data-search-all-label="All {{q}}" data-hui-combobox-listbox hidden></ul>
</div><script>window.picks = []; window.__gofastr = {formatNumber: String, navigate: dest => window.picks.push(dest)};</script>
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
		fmt.Fprint(w, `{"results":[{"url":"/birch","title":"Birch","sections":[{"heading":"Leaves","id":"leaves"}]}]}`)
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	ctx := chromedptest.Context(t)
	if err := chromedp.Run(ctx, chromedp.Navigate(server.URL), chromedp.Poll(`!!window.__gofastr._sitesearch`, nil),
		chromedp.SendKeys(`#q`, "birch"), chromedp.Poll(`document.querySelectorAll('#lb [role="option"]').length === 3`, nil)); err != nil {
		t.Fatal(err)
	}
	for index, path := range []string{"/birch", "/birch#leaves", "/search?q=birch"} {
		var point []float64
		if err := chromedp.Run(ctx, chromedp.Evaluate(fmt.Sprintf(`(() => { const row = document.querySelectorAll('#lb [role="option"]')[%d]; const rect = row.firstElementChild.getBoundingClientRect(); return [rect.x + rect.width / 2, rect.y + rect.height / 2]; })()`, index), &point)); err != nil {
			t.Fatal(err)
		}
		waitCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		opened := make(chan target.ID, 1)
		chromedp.ListenBrowser(waitCtx, func(event any) {
			if changed, ok := event.(*target.EventTargetInfoChanged); ok && changed.TargetInfo.URL == server.URL+path {
				select {
				case opened <- changed.TargetInfo.TargetID:
				default:
				}
			}
		})
		err := chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
			for _, eventType := range []input.MouseType{input.MousePressed, input.MouseReleased} {
				if err := input.DispatchMouseEvent(eventType, point[0], point[1]).WithButton(input.Middle).WithClickCount(1).Do(ctx); err != nil {
					return err
				}
			}
			return nil
		}))
		if err != nil {
			cancel()
			t.Fatal(err)
		}
		select {
		case targetID := <-opened:
			if targetID == "" {
				t.Errorf("middle-click did not open %s", path)
			} else if err := target.CloseTarget(targetID).Do(cdp.WithExecutor(ctx, chromedp.FromContext(ctx).Browser)); err != nil {
				t.Error(err)
			}
		case <-waitCtx.Done():
			t.Errorf("middle-click did not open %s", path)
		}
		cancel()
	}
	var intact bool
	if err := chromedp.Run(ctx, chromedp.Evaluate(`window.picks.length === 0 && document.getElementById('q').value === 'birch' && [...document.querySelectorAll('#lb [role="option"]')].every(row => row.matches('a[href][tabindex="-1"]') && row.parentElement.matches('li[role="none"]'))`, &intact)); err != nil {
		t.Fatal(err)
	}
	if !intact {
		t.Error("result links changed the picker or lost native option semantics")
	}
	if err := chromedp.Run(ctx, chromedp.Click(`#lb [role="option"] span`), chromedp.Poll(`window.picks.length === 1 && window.picks[0] === '/birch'`, nil)); err != nil {
		t.Fatal(err)
	}
}
