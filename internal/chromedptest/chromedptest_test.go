package chromedptest

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/chromedp/chromedp"
)

// originServer serves one blank page, so every Context below navigates to
// the same origin and would see that origin's cookies and storage if they
// leaked between BrowserContexts.
func originServer(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte("<!doctype html><title>origin</title><p>origin</p>"))
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

// chromePID is the process ID of the Chrome behind ctx.
func chromePID(t *testing.T, ctx context.Context) int {
	t.Helper()
	proc := chromedp.FromContext(ctx).Browser.Process()
	if proc == nil {
		t.Fatal("no chrome process behind the context")
	}
	return proc.Pid
}

// TestContextsAreIsolated: two Context calls share one Chrome but not its
// state. A cookie and a localStorage entry written in the first window are
// absent from the second, on the same origin, while the first is still open.
func TestContextsAreIsolated(t *testing.T) {
	url := originServer(t)
	first := Context(t)
	if err := chromedp.Run(first,
		chromedp.Navigate(url),
		chromedp.Evaluate(`document.cookie = "leak=1; path=/"; localStorage.setItem("leak", "1")`, nil),
	); err != nil {
		t.Fatal(err)
	}
	var cookie, stored string
	if err := chromedp.Run(first, chromedp.Evaluate(`document.cookie + "|" + localStorage.getItem("leak")`, &cookie)); err != nil {
		t.Fatal(err)
	}
	if cookie != "leak=1|1" {
		t.Fatalf("first window did not keep its own state: %q", cookie)
	}

	second := Context(t)
	if err := chromedp.Run(second,
		chromedp.Navigate(url),
		chromedp.Evaluate(`document.cookie`, &cookie),
		chromedp.Evaluate(`String(localStorage.getItem("leak"))`, &stored),
	); err != nil {
		t.Fatal(err)
	}
	if cookie != "" {
		t.Errorf("second window sees the first window's cookie: %q", cookie)
	}
	if stored != "null" {
		t.Errorf("second window sees the first window's localStorage: %q", stored)
	}
}

// TestChildTabSharesTheTestsState: a tab opened from the returned context
// with chromedp.NewContext lands in the test's BrowserContext, so a test
// that opens a second tab (cross-tab storage, BroadcastChannel) still sees
// its own state there.
func TestChildTabSharesTheTestsState(t *testing.T) {
	url := originServer(t)
	ctx := Context(t)
	if err := chromedp.Run(ctx,
		chromedp.Navigate(url),
		chromedp.Evaluate(`localStorage.setItem("shared", "1")`, nil),
	); err != nil {
		t.Fatal(err)
	}
	tab, cancel := chromedp.NewContext(ctx)
	defer cancel()
	var stored string
	if err := chromedp.Run(tab,
		chromedp.Navigate(url),
		chromedp.Evaluate(`String(localStorage.getItem("shared"))`, &stored),
	); err != nil {
		t.Fatal(err)
	}
	if stored != "1" {
		t.Errorf("child tab does not share the test's localStorage: %q", stored)
	}
}

// TestContextsShareOneChrome: calls with the same allocator options reuse
// one Chrome process; a call with different flags gets its own.
func TestContextsShareOneChrome(t *testing.T) {
	if shellPath() == "" {
		t.Skip("no sh to run the reaper: every Context gets a private Chrome")
	}
	a := chromePID(t, Context(t))
	b := chromePID(t, Context(t, WindowSize(390, 844)))
	if a != b {
		t.Errorf("same options started two Chromes: pids %d and %d", a, b)
	}
	c := chromePID(t, Context(t, AllocatorOptions(chromedp.Flag("lang", "fr"))))
	if c == a {
		t.Errorf("different allocator flags reused the shared Chrome (pid %d)", a)
	}
}

// TestDeadChromeIsReplaced: when the shared Chrome dies, the next Context
// call starts a fresh one instead of failing every later test. "between
// tests" lets chromedp notice the dropped connection first, so the pool's
// liveness check is what catches it; "while opening" calls at once, before
// chromedp has noticed, so the failed window open and its retry catch it.
func TestDeadChromeIsReplaced(t *testing.T) {
	if shellPath() == "" {
		t.Skip("no sh to run the reaper: every Context gets a private Chrome")
	}
	for _, tc := range []struct {
		name     string
		waitLost bool
	}{
		{"between tests", true},
		{"while opening", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := Context(t)
			b := chromedp.FromContext(ctx).Browser
			dead := b.Process().Pid
			if err := b.Process().Kill(); err != nil {
				t.Fatal(err)
			}
			if tc.waitLost {
				<-b.LostConnection
			}
			next := Context(t)
			if pid := chromePID(t, next); pid == dead {
				t.Fatalf("Context handed out the dead Chrome (pid %d)", dead)
			}
			var title string
			if err := chromedp.Run(next, chromedp.Navigate(originServer(t)), chromedp.Title(&title)); err != nil {
				t.Fatalf("replacement Chrome does not work: %v", err)
			}
			if title != "origin" {
				t.Errorf("replacement Chrome loaded %q, want origin", title)
			}
		})
	}
}
