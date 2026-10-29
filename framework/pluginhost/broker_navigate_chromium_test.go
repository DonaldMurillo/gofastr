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
	"github.com/chromedp/chromedp"
)

// A client-side navigation or an island swap replaces part of the page
// without a reload. The broker mounts the markers the new content
// brings, tears down the instances whose marker left the DOM, and keeps
// the ones whose marker stayed. The "no observer" page takes
// MutationObserver away so the gofastr:navigate scan is the only path.
func TestBrokerFollowsSwappedMarkers(t *testing.T) {
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
		noObserver := ""
		if r.URL.Query().Has("noobserver") {
			noObserver = `<script>window.MutationObserver = undefined;</script>`
		}
		fmt.Fprintf(w, `<!doctype html><html><head>%s
			<script defer src="/broker.js"></script><script defer src="/adapter.js"></script>
			</head><body>
			<div id="keep"><div data-fui-plugin="test"></div></div>
			<div id="swap"><div data-fui-plugin="test"></div></div>
			</body></html>`, noObserver)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	alloc, cancelAlloc := chromedp.NewExecAllocator(context.Background(), append(chromedp.DefaultExecAllocatorOptions[:], chromedp.ExecPath(path), chromedp.NoSandbox, chromedp.WSURLReadTimeout(90*time.Second))...)
	defer cancelAlloc()
	browser, cancelBrowser := chromedp.NewContext(alloc, chromedp.WithBrowserOption(chromedp.WithDialTimeout(90*time.Second)))
	defer cancelBrowser()

	quick := chromedp.WithPollingTimeout(3 * time.Second)
	for _, tc := range []struct {
		name, query string
	}{
		{"observer", ""},
		{"no observer", "?noobserver"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(browser, 30*time.Second)
			defer cancel()
			ctx, cancelTab := chromedp.NewContext(ctx)
			defer cancelTab()
			step := func(what string, actions ...chromedp.Action) {
				t.Helper()
				if err := chromedp.Run(ctx, actions...); err != nil {
					t.Fatalf("%s: %v", what, err)
				}
			}
			step("boot mounts both markers",
				chromedp.Navigate(srv.URL+"/"+tc.query),
				chromedp.Poll(`document.querySelectorAll("[data-fui-plugin] iframe").length === 2`, nil, quick),
				chromedp.Evaluate(`window.keepFrame = document.querySelector("#keep iframe");
					window.gone = document.querySelector("#swap [data-fui-plugin]");
					document.querySelector("#swap").innerHTML = '<div data-fui-plugin="test" id="fresh"></div>';
					window.dispatchEvent(new CustomEvent("gofastr:navigate", {detail: {path: "/b"}}));`, nil),
			)
			step("a marker the navigation brought mounts",
				chromedp.Poll(`!!document.querySelector("#fresh iframe")`, nil, quick))
			step("the marker that left the DOM is torn down",
				chromedp.Poll(`window.gone.querySelector("iframe") === null`, nil, quick))
			// Past the 200ms teardown-ack budget, so a teardown of the
			// retained instance would have finished.
			step("wait out the teardown budget", chromedp.Sleep(400*time.Millisecond))
			var kept bool
			step("read the retained marker",
				chromedp.Evaluate(`document.querySelectorAll("#keep iframe").length === 1 &&
					document.querySelector("#keep iframe") === window.keepFrame`, &kept))
			if !kept {
				t.Fatal("navigation replaced or tore down the plugin whose marker stayed in the DOM")
			}
			if tc.query != "" {
				return
			}
			step("an island insert mounts without a navigation",
				chromedp.Evaluate(`document.body.insertAdjacentHTML("beforeend", '<div data-fui-plugin="test" id="island"></div>')`, nil),
				chromedp.Poll(`!!document.querySelector("#island iframe")`, nil, quick))
			step("an island removal tears down without a navigation",
				chromedp.Evaluate(`window.islandMarker = document.querySelector("#island"); window.islandMarker.remove()`, nil),
				chromedp.Poll(`window.islandMarker.querySelector("iframe") === null`, nil, quick))
			step("the same marker mounts again when it returns",
				chromedp.Evaluate(`document.body.appendChild(window.islandMarker)`, nil),
				chromedp.Poll(`window.islandMarker.querySelectorAll("iframe").length === 1`, nil, quick))
			var leaked bool
			step("a marker added and removed in one batch stays unmounted",
				chromedp.Evaluate(`window.blink = document.createElement("div");
					window.blink.setAttribute("data-fui-plugin", "test");
					document.body.appendChild(window.blink); window.blink.remove();`, nil),
				chromedp.Sleep(100*time.Millisecond),
				chromedp.Evaluate(`window.blink.querySelector("iframe") !== null`, &leaked))
			if leaked {
				t.Fatal("a marker removed before the observer ran was mounted off-document")
			}
		})
	}
}
