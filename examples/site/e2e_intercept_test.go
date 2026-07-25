package main

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

const catalogDetail = "/examples/catalog/kettle"

// The half that matters most: an intercepting route is still a real
// page. If this regresses, the URL people share renders nothing, and no
// amount of overlay polish makes up for it. Plain HTTP, no JavaScript.
func TestInterceptRouteIsStillAPage(t *testing.T) {
	base := siteE2EServer(t)

	get := func(t *testing.T, path string, hdr map[string]string) (*http.Response, string) {
		t.Helper()
		req, err := http.NewRequest(http.MethodGet, base+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		for k, v := range hdr {
			req.Header.Set(k, v)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		return resp, string(body)
	}

	t.Run("hard load renders the full page", func(t *testing.T) {
		resp, body := get(t, catalogDetail, nil)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d", resp.StatusCode)
		}
		if !strings.Contains(body, "Pour-over kettle") {
			t.Error("detail content missing from the canonical render")
		}
		if !strings.Contains(body, "<html") {
			t.Error("hard load must be a full document, not a fragment")
		}
		if resp.Header.Get("X-Gofastr-Overlay") != "" {
			t.Error("a hard load must never be an overlay")
		}
	})

	// The client asks; the server decides. A forged origin gets the
	// ordinary partial, not overlay chrome.
	t.Run("server refuses an overlay from an undeclared origin", func(t *testing.T) {
		resp, _ := get(t, catalogDetail, map[string]string{
			"X-Gofastr-Navigate":  "1",
			"X-Gofastr-Intercept": "1",
			"X-Gofastr-From":      "/examples/workspace",
		})
		if got := resp.Header.Get("X-Gofastr-Overlay"); got != "" {
			t.Errorf("overlay granted from an undeclared origin: %q", got)
		}
	})

	t.Run("server grants an overlay from the declared origin", func(t *testing.T) {
		resp, body := get(t, catalogDetail, map[string]string{
			"X-Gofastr-Navigate":  "1",
			"X-Gofastr-Intercept": "1",
			"X-Gofastr-From":      "/examples/catalog",
		})
		if got := resp.Header.Get("X-Gofastr-Overlay"); got != "drawer" {
			t.Fatalf("X-Gofastr-Overlay = %q, want %q", got, "drawer")
		}
		if !strings.Contains(body, `role="complementary"`) {
			t.Error("overlay variant is missing its drawer scaffolding")
		}
		// Same content as the canonical render — the overlay is a
		// wrapper, not a second version of the screen.
		if !strings.Contains(body, "Pour-over kettle") {
			t.Error("overlay variant lost the screen's content")
		}
	})

	// The list's own state must not disqualify it as an origin.
	t.Run("origin query string still matches", func(t *testing.T) {
		resp, _ := get(t, catalogDetail, map[string]string{
			"X-Gofastr-Navigate":  "1",
			"X-Gofastr-Intercept": "1",
			"X-Gofastr-From":      "/examples/catalog?page=2&sort=name",
		})
		if resp.Header.Get("X-Gofastr-Overlay") != "drawer" {
			t.Error("a paged list is still the declared origin")
		}
	})

	// Without the intercept header this is an ordinary SPA partial.
	t.Run("plain partial is unchanged", func(t *testing.T) {
		resp, body := get(t, catalogDetail, map[string]string{"X-Gofastr-Navigate": "1"})
		if resp.Header.Get("X-Gofastr-Overlay") != "" {
			t.Error("overlay granted without the client asking")
		}
		if strings.Contains(body, "<html") {
			t.Error("a partial must be a fragment")
		}
	})
}

// The browser half: click through from the list and the detail arrives
// as a drawer over a list that never unmounted; Escape closes it and
// hands the URL back without a refetch.
func TestInterceptOpensAsDrawerAndClosesToList(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e")
	}
	base := siteE2EServer(t)
	ctx := siteBrowserCtx(t)

	const overlay = `!!document.querySelector('[data-fui-intercept-overlay]')`
	const listAlive = `!!document.querySelector('.cat-list')`

	var urlOpen, urlClosed, overlayAs, overlayText string
	var overlayOpen, listStillThere, overlayGone, listAfter bool

	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/examples/catalog"),
		chromedp.WaitVisible(`a[href="/examples/catalog/kettle"]`),

		chromedp.Click(`a[href="/examples/catalog/kettle"]`, chromedp.ByQuery),
		chromedp.Sleep(2*time.Second),
		chromedp.Location(&urlOpen),
		chromedp.Evaluate(overlay, &overlayOpen),
		chromedp.Evaluate(`document.querySelector('[data-fui-intercept-overlay]')?.getAttribute('data-fui-intercept-as') || ""`, &overlayAs),
		chromedp.Evaluate(`document.querySelector('[data-fui-intercept-overlay]')?.textContent || ""`, &overlayText),
		// The list is still mounted underneath — that is what makes the
		// close cheap and the scroll position survive.
		chromedp.Evaluate(listAlive, &listStillThere),

		chromedp.KeyEvent(""), // Escape
		chromedp.Sleep(2*time.Second),
		chromedp.Location(&urlClosed),
		chromedp.Evaluate(overlay, &overlayGone),
		chromedp.Evaluate(listAlive, &listAfter),
	); err != nil {
		t.Fatal(err)
	}

	if !overlayOpen {
		t.Fatalf("clicking a row did not open an overlay (url = %q)", urlOpen)
	}
	if !strings.Contains(urlOpen, catalogDetail) {
		t.Errorf("URL did not move to the detail route: %q", urlOpen)
	}
	if overlayAs != "drawer" {
		t.Errorf("overlay presentation = %q, want drawer", overlayAs)
	}
	if !strings.Contains(overlayText, "Pour-over kettle") {
		t.Errorf("overlay did not contain the detail: %q", overlayText)
	}
	if !listStillThere {
		t.Error("the list unmounted — the overlay replaced the page instead of covering it")
	}
	if overlayGone {
		t.Error("Escape did not close the overlay")
	}
	if !strings.HasSuffix(urlClosed, "/examples/catalog") {
		t.Errorf("closing did not return the URL to the list: %q", urlClosed)
	}
	if !listAfter {
		t.Error("the list is gone after closing")
	}
}
