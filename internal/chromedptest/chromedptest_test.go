package chromedptest

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"testing"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/target"
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

// pooled returns the shared Chrome for the default options, which the
// caller's earlier Context call started.
func pooled(t *testing.T) *browser {
	t.Helper()
	key, _ := allocatorKey(nil)
	poolMu.Lock()
	defer poolMu.Unlock()
	b := pool[key]
	if b == nil {
		t.Fatal("no shared Chrome in the pool")
	}
	return b
}

// TestDeadChromeIsReplaced: when the shared Chrome dies, no later caller is
// handed it.
//
//   - "liveness check": the pool is held while Chrome dies, so the idle
//     watcher cannot drop it first, and acquire (which has no retry) must
//     see the dropped connection itself and start a fresh Chrome.
//   - "idle watcher": nobody calls acquire; the watcher alone must take
//     the dead Chrome out of the pool and disarm its reaper.
//   - "while opening": Context is called at once, before chromedp has
//     noticed, so the failed window open and its retry catch it.
func TestDeadChromeIsReplaced(t *testing.T) {
	if shellPath() == "" {
		t.Skip("no sh to run the reaper: every Context gets a private Chrome")
	}
	t.Run("liveness check", func(t *testing.T) {
		ctx := Context(t)
		key, _ := allocatorKey(nil)
		cb := chromedp.FromContext(ctx).Browser
		dead := cb.Process().Pid
		poolMu.Lock()
		if err := cb.Process().Kill(); err != nil {
			poolMu.Unlock()
			t.Fatal(err)
		}
		<-cb.LostConnection
		b, err := acquireLocked(newConfig(), key)
		poolMu.Unlock()
		if err != nil {
			t.Fatal(err)
		}
		defer b.release()
		if pid := chromedp.FromContext(b.ctx).Browser.Process().Pid; pid == dead {
			t.Fatalf("acquire handed out the dead Chrome (pid %d)", dead)
		}
	})
	t.Run("idle watcher", func(t *testing.T) {
		Context(t)
		b := pooled(t)
		if err := chromedp.FromContext(b.ctx).Browser.Process().Kill(); err != nil {
			t.Fatal(err)
		}
		deadline := time.Now().Add(10 * time.Second)
		for {
			poolMu.Lock()
			gone := b.retired && pool[b.key] != b
			poolMu.Unlock()
			if gone {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("a Chrome that died idle is still in the pool")
			}
			time.Sleep(10 * time.Millisecond)
		}
		// The watcher disarms before it retires, and poolMu orders the
		// two, so the reaper has exited by now.
		if b.reaper == nil || b.reaper.ProcessState == nil {
			t.Error("the dead Chrome's reaper is still armed")
		}
	})
	t.Run("while opening", func(t *testing.T) {
		ctx := Context(t)
		b := chromedp.FromContext(ctx).Browser
		dead := b.Process().Pid
		if err := b.Process().Kill(); err != nil {
			t.Fatal(err)
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

// TestFailedWindowSparesOthers: a window whose cleanup fails retires the
// shared Chrome, but a window another test still holds on it keeps
// working, and the next caller gets a fresh Chrome.
func TestFailedWindowSparesOthers(t *testing.T) {
	if shellPath() == "" {
		t.Skip("no sh to run the reaper: every Context gets a private Chrome")
	}
	url := originServer(t)
	held := Context(t)
	heldPID := chromePID(t, held)
	t.Run("failing window", func(t *testing.T) {
		ctx := Context(t)
		// Disposing the window's BrowserContext here makes its cleanup's
		// dispose fail, the path that retires a Chrome.
		c := chromedp.FromContext(ctx)
		if err := target.DisposeBrowserContext(c.BrowserContextID).Do(cdp.WithExecutor(ctx, c.Browser)); err != nil {
			t.Fatal(err)
		}
	})
	var title string
	if err := chromedp.Run(held, chromedp.Navigate(url), chromedp.Title(&title)); err != nil {
		t.Fatalf("the held window died with the other window's Chrome: %v", err)
	}
	if title != "origin" {
		t.Errorf("held window loaded %q, want origin", title)
	}
	if pid := chromePID(t, Context(t)); pid == heldPID {
		t.Errorf("the retired Chrome (pid %d) was handed out again", heldPID)
	}
}

// TestReferenceOptionsGetPrivateChrome: an option set holding a func or a
// writer cannot be compared by value, so it never shares a Chrome; two
// ModifyCmdFunc closures over different values would print the same.
func TestReferenceOptionsGetPrivateChrome(t *testing.T) {
	modify := func(v string) Option {
		return AllocatorOptions(chromedp.ModifyCmdFunc(func(cmd *exec.Cmd) {
			cmd.Env = append(cmd.Env, "CHROMEDPTEST_PROBE="+v)
		}))
	}
	a := chromePID(t, Context(t, modify("a")))
	b := chromePID(t, Context(t, modify("b")))
	if a == b {
		t.Errorf("two ModifyCmdFunc option sets shared one Chrome (pid %d)", a)
	}
	if _, shareable := allocatorKey([]chromedp.ExecAllocatorOption{chromedp.CombinedOutput(io.Discard)}); shareable {
		t.Error("a CombinedOutput writer was treated as shareable")
	}
	if _, shareable := allocatorKey([]chromedp.ExecAllocatorOption{chromedp.Flag("lang", "fr")}); !shareable {
		t.Error("a plain flag was treated as unshareable")
	}
}

// TestNoSharingOnWindows: Windows gets no reaper shell, so every Context
// there gets a private Chrome. Git's sh.exe is on PATH on most Windows
// hosts, but its kill cannot signal a native Windows process.
func TestNoSharingOnWindows(t *testing.T) {
	if sh := reaperShell("windows"); sh != "" {
		t.Errorf("reaperShell(windows) = %q, want none", sh)
	}
}
