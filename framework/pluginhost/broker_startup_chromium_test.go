//go:build chromium

package pluginhost

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/browserpath"
	cdppage "github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

func TestBrokerWaitsForDeferredAdapters(t *testing.T) {
	path, ok := browserpath.Find()
	if !ok {
		t.Skip("Chrome or Chromium is not installed")
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/broker.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript")
		w.Write(brokerJSBytes)
	})
	mux.HandleFunc("/adapter.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript")
		fmt.Fprint(w, `window.__gofastrPluginHost.register("test", {manifest: {
			entry: "/frame.html", sandbox: ["allow-scripts"], minHeight: "100px", title: "Test plugin"
		}});`)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		if r.URL.Path == "/frame.html" {
			fmt.Fprint(w, "<!doctype html><title>Plugin frame</title>")
			return
		}
		fmt.Fprint(w, `<!doctype html><html><head>
			<script defer src="/broker.js"></script><script defer src="/adapter.js"></script>
			</head><body><div data-fui-plugin="test"></div></body></html>`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	alloc, cancelAlloc := chromedp.NewExecAllocator(context.Background(), append(chromedp.DefaultExecAllocatorOptions[:], chromedp.ExecPath(path), chromedp.NoSandbox, chromedp.WSURLReadTimeout(90*time.Second))...)
	defer cancelAlloc()
	browser, cancelBrowser := chromedp.NewContext(alloc, chromedp.WithBrowserOption(chromedp.WithDialTimeout(90*time.Second)))
	defer cancelBrowser()
	ctx, cancel := context.WithTimeout(browser, 30*time.Second)
	defer cancel()
	var mounted bool
	if err := chromedp.Run(ctx, chromedp.Navigate(srv.URL), chromedp.Evaluate(`!!document.querySelector('[data-fui-plugin="test"] iframe')`, &mounted)); err != nil {
		t.Fatal(err)
	}
	if !mounted {
		t.Fatal("deferred broker scanned before the following adapter registered")
	}
}

// A dynamically inserted broker can run after DOMContentLoaded while an
// unrelated image still keeps readyState interactive. Waiting for that event
// again would leave the broker unbooted, without its navigation cleanup.
func TestBrokerInjectedAfterDOMContentLoaded(t *testing.T) {
	path, ok := browserpath.Find()
	if !ok {
		t.Skip("Chrome or Chromium is not installed")
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/broker.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript")
		w.Write(brokerJSBytes)
	})
	mux.HandleFunc("/hold.png", func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<!doctype html><script>
			document.addEventListener("DOMContentLoaded", () => window.domReady = true);
			</script><img src="/hold.png" alt="">`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	alloc, cancelAlloc := chromedp.NewExecAllocator(context.Background(), append(chromedp.DefaultExecAllocatorOptions[:], chromedp.ExecPath(path), chromedp.NoSandbox, chromedp.WSURLReadTimeout(90*time.Second))...)
	defer cancelAlloc()
	browser, cancelBrowser := chromedp.NewContext(alloc, chromedp.WithBrowserOption(chromedp.WithDialTimeout(90*time.Second)))
	defer cancelBrowser()
	ctx, cancel := context.WithTimeout(browser, 15*time.Second)
	defer cancel()
	if err := chromedp.Run(ctx,
		// CDP navigation returns before load, which the held image prevents.
		chromedp.ActionFunc(func(ctx context.Context) error {
			_, _, _, _, err := cdppage.Navigate(srv.URL).Do(ctx)
			return err
		}),
		chromedp.Poll(`window.domReady === true && document.readyState === "interactive"`, nil),
		chromedp.Evaluate(`(() => {
			const addListener = window.addEventListener;
			window.addEventListener = function(type, ...args) {
				if (type === "gofastr:navigate") window.brokerBooted = true;
				return addListener.call(this, type, ...args);
			};
			const script = document.createElement("script");
			script.src = "/broker.js";
			document.head.appendChild(script);
		})()`, nil),
		chromedp.Poll(`window.brokerBooted === true`, nil, chromedp.WithPollingTimeout(3*time.Second)),
	); err != nil {
		t.Fatalf("late broker must install its navigation lifecycle: %v", err)
	}
}
