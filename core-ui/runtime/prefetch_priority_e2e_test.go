package runtime

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
)

// An eager prefetch is speculative and asks for a low fetch priority; a
// hover prefetch is about to become a click and keeps the default.
// Chrome reports the hint as the request's scheduler priority.
func TestPrefetchPriorityFollowsIntent(t *testing.T) {
	srv, _ := newPreloadSite(t)
	ctx := chromedptest.Context(t, chromedptest.Timeout(90*time.Second))
	var mu sync.Mutex
	prio := map[string]string{}
	chromedp.ListenTarget(ctx, func(ev any) {
		e, ok := ev.(*network.EventRequestWillBeSent)
		if !ok {
			return
		}
		if v, _ := e.Request.Headers["X-Gofastr-Prefetch"].(string); v != "1" {
			return
		}
		u, err := url.Parse(e.Request.URL)
		if err != nil {
			return
		}
		mu.Lock()
		prio[u.Path] = string(e.Request.InitialPriority)
		mu.Unlock()
	})
	seen := func(path string) chromedp.Action {
		return chromedp.ActionFunc(func(ctx context.Context) error {
			for {
				mu.Lock()
				_, ok := prio[path]
				mu.Unlock()
				if ok {
					return nil
				}
				select {
				case <-ctx.Done():
					return fmt.Errorf("no prefetch of %s: %w", path, ctx.Err())
				case <-time.After(50 * time.Millisecond):
				}
			}
		})
	}
	if err := chromedp.Run(ctx,
		network.Enable(),
		chromedp.Navigate(srv.URL+"/hub"),
		chromedp.WaitVisible(`#hub-screen`, chromedp.ByID),
		seen("/eager"),
		chromedp.Poll(`!!window.__gofastr?.loadedModules?.preload`, nil),
		chromedp.Evaluate(`document.getElementById('to-detail').dispatchEvent(new PointerEvent('pointerover', {bubbles: true}))`, nil),
		seen("/detail"),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	eager, hover := prio["/eager"], prio["/detail"]
	if !strings.Contains(eager, "Low") {
		t.Errorf("eager prefetch priority = %q, want Low or VeryLow", eager)
	}
	if strings.Contains(hover, "Low") {
		t.Errorf("hover prefetch priority = %q; a hover is about to be a click and must not yield", hover)
	}
}

// A poll is background freshness: its fetch asks for a low priority so
// it never contends with what the user is waiting on.
func TestPollFetchIsLowPriority(t *testing.T) {
	page := `<!doctype html><html><head></head><body>
<div id="region" data-cui-poll="5s" data-cui-poll-src="/fresh">stale</div>
<script src="/__gofastr/runtime.js"></script></body></html>`
	base := startPollServer(t, page, map[string]http.HandlerFunc{
		"/fresh": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprint(w, `<span>fresh</span>`)
		},
	})
	ctx := chromedptest.Context(t)
	var mu sync.Mutex
	var got string
	chromedp.ListenTarget(ctx, func(ev any) {
		if e, ok := ev.(*network.EventRequestWillBeSent); ok && strings.HasSuffix(e.Request.URL, "/fresh") {
			mu.Lock()
			got = string(e.Request.InitialPriority)
			mu.Unlock()
		}
	})
	if err := chromedp.Run(ctx,
		network.Enable(),
		chromedp.Navigate(base+"/"),
		chromedp.Poll(`document.getElementById('region').textContent === 'fresh'`, nil,
			chromedp.WithPollingInterval(200*time.Millisecond)),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if !strings.Contains(got, "Low") {
		t.Errorf("poll fetch priority = %q, want Low or VeryLow", got)
	}
}

// A widget's scheduled state poll asks for a low priority too; pollNow,
// the refresh after the user's own write, keeps the default. The first
// state request is the mount hydration, so only the ticks after it count.
func TestWidgetPollFetchIsLowPriority(t *testing.T) {
	page := `<!doctype html><html><head></head><body>
<div class="cui-widget cui-pos-bottom-right" data-cui-widget="poller" role="status"><span data-cui-signal="count">0</span></div>
<script src="/__gofastr/runtime.js"></script></body></html>`
	base := startPollServer(t, page, map[string]http.HandlerFunc{
		"/__gofastr/widgets": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `[{"hidden":false,"cfg":{"name":"poller","position":"bottom-right",`+
				`"stylePath":"/core-ui/widget/poller/style.css","chromePath":"/core-ui/widget/poller/chrome",`+
				`"statePath":"/core-ui/widget/poller/state","pollMs":1000}}]`)
		},
		"/core-ui/widget/poller/state": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"count":1}`)
		},
		"/core-ui/widget/poller/chrome": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprint(w, `<div class="cui-widget cui-pos-bottom-right" data-cui-widget="poller" role="status"><span data-cui-signal="count">0</span></div>`)
		},
		"/core-ui/widget/poller/style.css": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/css")
		},
	})
	ctx := chromedptest.Context(t)
	var mu sync.Mutex
	var prios []string
	chromedp.ListenTarget(ctx, func(ev any) {
		if e, ok := ev.(*network.EventRequestWillBeSent); ok && strings.HasSuffix(e.Request.URL, "/poller/state") {
			mu.Lock()
			prios = append(prios, string(e.Request.InitialPriority))
			mu.Unlock()
		}
	})
	if err := chromedp.Run(ctx,
		network.Enable(),
		chromedp.Navigate(base+"/"),
		chromedp.ActionFunc(func(ctx context.Context) error {
			for {
				mu.Lock()
				n := len(prios)
				mu.Unlock()
				if n >= 3 {
					return nil
				}
				select {
				case <-ctx.Done():
					return fmt.Errorf("saw %d state requests: %w", n, ctx.Err())
				case <-time.After(100 * time.Millisecond):
				}
			}
		}),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	mu.Lock()
	ticks := append([]string(nil), prios...)
	mu.Unlock()
	for i, p := range ticks[1:] {
		if !strings.Contains(p, "Low") {
			t.Errorf("scheduled state poll %d priority = %q, want Low or VeryLow (all: %v)", i+1, p, ticks)
		}
	}

	// pollNow is the refresh after the user's own write: it must not yield.
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`window.__gofastr._widgets.poller.pollNow()`, nil),
		chromedp.ActionFunc(func(ctx context.Context) error {
			for {
				mu.Lock()
				n := len(prios)
				mu.Unlock()
				if n > len(ticks) {
					return nil
				}
				select {
				case <-ctx.Done():
					return fmt.Errorf("pollNow sent no state request: %w", ctx.Err())
				case <-time.After(20 * time.Millisecond):
				}
			}
		}),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if p := prios[len(ticks)]; strings.Contains(p, "Low") {
		t.Errorf("pollNow state request priority = %q; the refresh after a write must not yield", p)
	}
}
