package runtime

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// TestLightboxClickBeforeModuleLoadIsReplayed covers the cold-cache gap in
// issue #161. The marker scanner starts lightbox.js, but the module's own
// document click listener does not exist until that request completes. A
// click on the navigation control during that window must be prevented now,
// then replayed after the module arrives.
func TestLightboxClickBeforeModuleLoadIsReplayed(t *testing.T) {
	core, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	lightbox, ok := Module("lightbox")
	if !ok {
		t.Fatal("lightbox module not embedded")
	}

	moduleRequested := make(chan struct{})
	requestOnce := sync.Once{}
	moduleGate := make(chan struct{})
	var releaseOnce sync.Once
	releaseModule := func() { releaseOnce.Do(func() { close(moduleGate) }) }
	defer releaseModule()

	mux := http.NewServeMux()
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		_, _ = w.Write([]byte(core))
	})
	mux.HandleFunc("/__gofastr/runtime/lightbox.js", func(w http.ResponseWriter, _ *http.Request) {
		requestOnce.Do(func() { close(moduleRequested) })
		<-moduleGate
		w.Header().Set("Content-Type", "application/javascript")
		_, _ = w.Write([]byte(lightbox))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<!doctype html><html><head></head><body>
<div id="viewer" data-fui-widget="viewer"></div>
<a data-fui-lightbox-group="photos" data-fui-deeplink="src=one.jpg&group=photos">one</a>
<a data-fui-lightbox-group="photos" data-fui-deeplink="src=two.jpg&group=photos">two</a>
<script src="/__gofastr/runtime.js"></script>
<script>
window.__gofastr.loadedModules.widgets = true;
window.__gofastr._signals = {
  group: { value: 'photos' },
  src: { value: 'one.jpg' }
};
  window.__lbCall = '';
  window.__lbCallCount = 0;
  window.__gofastr.openWidget = function (name, opts) {
    window.__lbCallCount++;
    window.__lbCall = name + ':' + ((((opts || {}).params || {}).src) || '');
  };
  window.__nextDefaultPrevented = false;
document.addEventListener('click', function (e) {
  if (e.target && e.target.closest && e.target.closest('#next')) {
    window.__nextDefaultPrevented = e.defaultPrevented;
  }
});
document.getElementById('viewer').innerHTML =
  '<div data-fui-comp="ui-lightbox" data-fui-lightbox="viewer" data-fui-lightbox-nav="true">' +
  '<button id="next" type="button" data-fui-lightbox-next>Next</button></div>';
</script>
</body></html>`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	ctx := newSeedBrowserCtx(t)
	if err := chromedp.Run(ctx,
		// Use location assignment so this setup action does not wait for the
		// deliberately held dynamic module response to finish the load event.
		chromedp.Evaluate(fmt.Sprintf("location.href = %q", srv.URL+"/"), nil),
		chromedp.WaitVisible(`#next`, chromedp.ByID),
	); err != nil {
		t.Fatalf("chromedp setup: %v", err)
	}

	select {
	case <-moduleRequested:
	case <-time.After(5 * time.Second):
		t.Fatal("lightbox module request did not start")
	}

	var prevented bool
	if err := chromedp.Run(ctx,
		chromedp.Click(`#next`, chromedp.ByID),
		chromedp.Evaluate(`window.__nextDefaultPrevented`, &prevented),
	); err != nil {
		t.Fatalf("chromedp cold-cache click: %v", err)
	}
	if !prevented {
		t.Fatal("lightbox navigation click was not prevented synchronously while lightbox.js was loading")
	}

	releaseModule()
	if err := chromedp.Run(ctx,
		chromedp.Poll(`!!(window.__gofastr&&window.__gofastr.loadedModules&&window.__gofastr.loadedModules.lightbox)`, nil,
			chromedp.WithPollingTimeout(5*time.Second), chromedp.WithPollingInterval(25*time.Millisecond)),
		chromedp.Poll(`window.__lbCall === 'viewer:two.jpg'`, nil,
			chromedp.WithPollingTimeout(5*time.Second), chromedp.WithPollingInterval(25*time.Millisecond)),
		chromedp.Evaluate(`document.getElementById('next').click()`, nil),
		chromedp.Poll(`window.__lbCallCount === 2`, nil,
			chromedp.WithPollingTimeout(5*time.Second), chromedp.WithPollingInterval(25*time.Millisecond)),
	); err != nil {
		t.Fatalf("chromedp replay: %v", err)
	}
}
