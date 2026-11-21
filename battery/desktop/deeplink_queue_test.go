package desktop

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type stalledDeepLinkWindow struct {
	*fakeWindow
	entered, release chan struct{}
	first            sync.Once
}

func (w *stalledDeepLinkWindow) Focus() error {
	w.first.Do(func() {
		close(w.entered)
		<-w.release
	})
	return w.fakeWindow.Focus()
}

func TestWarmDeepLinkQueueBoundAndReplay(t *testing.T) {
	var logs bytes.Buffer
	var mapped atomic.Int32
	win := &stalledDeepLinkWindow{fakeWindow: &fakeWindow{id: MainWindowID}, entered: make(chan struct{}), release: make(chan struct{})}
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(win.release) }) }
	t.Cleanup(release)
	b := &Battery{
		logger: slog.New(slog.NewTextHandler(&logs, nil)),
		cfg: Config{DeepLink: &DeepLinkConfig{Scheme: "notes", OnDeepLink: func(u *url.URL) (string, bool) {
			mapped.Add(1)
			return "/" + u.Host + u.Path, true
		}}},
		window: win, windows: map[string]Window{MainWindowID: win}, winOrder: []string{MainWindowID},
	}
	b.deepLinks.flushed = true // Run's ready callback has flushed.
	b.handleDeepLink("notes://first")
	<-win.entered // one link is in flight; the next 18 cannot drain yet.
	for i := 0; i < maxQueuedDeepLinks+2; i++ {
		b.handleDeepLink(fmt.Sprintf("notes://queue/%d", i))
	}
	b.deepLinks.mu.Lock()
	pending := append([]deepLinkDelivery(nil), b.deepLinks.pending...)
	b.deepLinks.mu.Unlock()
	if len(pending) != maxQueuedDeepLinks {
		t.Fatalf("pending = %d, want %d while one delivery is stalled", len(pending), maxQueuedDeepLinks)
	}
	for i, d := range pending {
		if want := fmt.Sprintf("/queue/%d", i+2); d.path != want {
			t.Fatalf("pending[%d] = %q, want %q (oldest dropped first)", i, d.path, want)
		}
	}
	if got := strings.Count(logs.String(), "delivery queue is full"); got != 2 {
		t.Fatalf("overflow warnings = %d, want 2: %s", got, logs.String())
	}
	release()
	waitFor(t, "bounded warm queue to drain", func() bool {
		b.deepLinks.mu.Lock()
		defer b.deepLinks.mu.Unlock()
		return !b.deepLinks.draining
	})
	var navigations []string
	for _, js := range win.evals() {
		// Every page script runs behind the origin guard; before Run the
		// app origin is the empty "http://".
		js = strings.TrimSuffix(strings.TrimPrefix(js, `if (location.origin === "http://") { `), " }")
		if strings.HasPrefix(js, "window.__gofastr.navigate(") {
			navigations = append(navigations, js)
		}
	}
	if len(navigations) != maxQueuedDeepLinks+1 || navigations[0] != `window.__gofastr.navigate("/first")` {
		t.Fatalf("navigation count/first = %q", navigations)
	}
	for i, js := range navigations[1:] {
		if want := fmt.Sprintf(`window.__gofastr.navigate("/queue/%d")`, i+2); js != want {
			t.Fatalf("replay[%d] = %q, want %q", i, js, want)
		}
	}
	if got := mapped.Load(); got != maxQueuedDeepLinks+3 {
		t.Fatalf("mapping calls = %d, want %d (once per arrival, none on replay)", got, maxQueuedDeepLinks+3)
	}
}

func TestWarmDeepLinkOverflowLogIsScrubbed(t *testing.T) {
	var logs bytes.Buffer
	b := &Battery{logger: slog.New(slog.NewTextHandler(&logs, nil))}
	b.deepLinks.draining = true // freeze the private queue without a worker.
	b.enqueueDeepLink(deepLinkDelivery{raw: "notes://bad\ninjected\x1b"})
	for i := 0; i < maxQueuedDeepLinks; i++ {
		b.enqueueDeepLink(deepLinkDelivery{raw: fmt.Sprintf("notes://ok/%d", i)})
	}
	if got := logs.String(); !strings.Contains(got, "notes://bad?injected?") {
		t.Fatalf("overflow log did not scrub the dropped raw URL: %q", got)
	}
}

// navigatedPaths returns the deep-link navigations a fake window ran,
// with the origin guard stripped.
func navigatedPaths(w *fakeWindow) []string {
	var out []string
	for _, js := range w.evals() {
		js = strings.TrimSuffix(strings.TrimPrefix(js, `if (location.origin === "http://") { `), " }")
		if p, ok := strings.CutPrefix(js, "window.__gofastr.navigate("); ok {
			out = append(out, strings.TrimSuffix(p, ")"))
		}
	}
	return out
}

// TestDeepLinkArrivingAsWindowComesUpIsNotStranded: a link whose
// window check runs before the ready callback publishes the window,
// and whose queue write would land after the flush, used to sit in the
// pre-window queue forever. The hook pins that interleaving: the window
// is published and the flush starts between the check and the write.
// The flush must deliver the link, and it must deliver it once.
func TestDeepLinkArrivingAsWindowComesUpIsNotStranded(t *testing.T) {
	win := &fakeWindow{id: MainWindowID}
	b := &Battery{
		logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
		cfg:     Config{DeepLink: &DeepLinkConfig{Scheme: "notes"}},
		windows: map[string]Window{},
	}
	flushDone := make(chan struct{})
	hook := func() {
		// The UI thread: publish the window, then flush, the order Run's
		// ready callback uses.
		b.windowMu.Lock()
		b.window = win
		b.windows[MainWindowID] = win
		b.windowMu.Unlock()
		go func() {
			b.flushDeepLinks(win)
			close(flushDone)
		}()
		// Give an unguarded flush time to run to completion before the
		// queue write. A guarded one blocks on the queue lock instead.
		select {
		case <-flushDone:
		case <-time.After(100 * time.Millisecond):
		}
	}
	deepLinkRouteHook.Store(&hook)
	t.Cleanup(func() { deepLinkRouteHook.Store(nil) })

	b.handleDeepLink("notes://late/1")
	deepLinkRouteHook.Store(nil)
	<-flushDone

	waitFor(t, "the late link to be delivered", func() bool { return len(navigatedPaths(win)) > 0 })
	b.deepLinks.mu.Lock()
	stranded := len(b.deepLinks.queued)
	b.deepLinks.mu.Unlock()
	if stranded != 0 {
		t.Fatalf("pre-window queue holds %d links after the flush, want 0", stranded)
	}
	waitFor(t, "the delivery line to drain", func() bool {
		b.deepLinks.mu.Lock()
		defer b.deepLinks.mu.Unlock()
		return !b.deepLinks.draining
	})
	if got := navigatedPaths(win); len(got) != 1 || got[0] != `"/late/1"` {
		t.Fatalf("navigations = %q, want exactly [\"/late/1\"]", got)
	}

	// A link after the flush goes straight to the delivery line.
	b.handleDeepLink("notes://after/2")
	waitFor(t, "the post-flush link", func() bool { return len(navigatedPaths(win)) == 2 })
	if got := navigatedPaths(win)[1]; got != `"/after/2"` {
		t.Fatalf("post-flush navigation = %q, want \"/after/2\"", got)
	}
}

// TestDeepLinkBeforeFlushKeepsArrivalOrder: Run's ready callback
// publishes the window and then flushes. A link arriving between the
// two sees the window up, yet it must not overtake the links the flush
// is about to deliver: it waits in the pre-window queue behind them.
func TestDeepLinkBeforeFlushKeepsArrivalOrder(t *testing.T) {
	win := &fakeWindow{id: MainWindowID}
	b := &Battery{
		logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
		cfg:     Config{DeepLink: &DeepLinkConfig{Scheme: "notes"}},
		windows: map[string]Window{},
	}
	for i := range 3 {
		b.handleDeepLink(fmt.Sprintf("notes://cold/%d", i))
	}
	b.windowMu.Lock()
	b.window = win
	b.windows[MainWindowID] = win
	b.windowMu.Unlock()
	b.handleDeepLink("notes://warm/3") // window up, flush not yet run
	b.flushDeepLinks(win)
	waitFor(t, "four deliveries", func() bool { return len(navigatedPaths(win)) == 4 })
	want := []string{`"/cold/0"`, `"/cold/1"`, `"/cold/2"`, `"/warm/3"`}
	got := navigatedPaths(win)
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("delivery order = %q, want %q", got, want)
		}
	}
}

func TestColdDeepLinkQueueIsBounded(t *testing.T) {
	b := New(Config{ID: "coldq.example.app", Shell: newFakeShell(), Logger: testLogger(t), DeepLink: &DeepLinkConfig{Scheme: "notes"}})
	for i := range 3 * maxQueuedDeepLinks {
		b.handleDeepLink(fmt.Sprintf("notes://q/%d", i))
	}
	q := b.deepLinkQueueState()
	q.mu.Lock()
	n, first := len(q.queued), q.queued[0].path
	q.mu.Unlock()
	if n != maxQueuedDeepLinks || first != fmt.Sprintf("/q/%d", 2*maxQueuedDeepLinks) {
		t.Fatalf("pre-window queue holds %d links starting at %s, want the newest %d", n, first, maxQueuedDeepLinks)
	}
}

type panickingFocusWindow struct{ *fakeWindow }

func (panickingFocusWindow) Focus() error { panic("focus blew up") }

func TestDeepLinkDeliveryPanicIsContained(t *testing.T) {
	b := New(Config{ID: "dlpanic.example.app", Shell: newFakeShell(), Logger: testLogger(t), DeepLink: &DeepLinkConfig{Scheme: "notes"}})
	w := panickingFocusWindow{&fakeWindow{id: MainWindowID}}
	b.window = w
	b.windows[MainWindowID] = w
	b.flushDeepLinks(w)
	b.handleDeepLink("notes://a")
	b.handleDeepLink("notes://b")
	deadline := time.Now().Add(5 * time.Second)
	for {
		q := b.deepLinkQueueState()
		q.mu.Lock()
		idle := !q.draining && len(q.pending) == 0
		q.mu.Unlock()
		if idle {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("the drain stopped after a delivery panicked")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestDeepLinkSchemeBounds(t *testing.T) {
	for _, scheme := range []string{"", "a" + strings.Repeat("b", maxDeepLinkSchemeLen)} {
		if err := validateDeepLink(&DeepLinkConfig{Scheme: scheme}); err == nil {
			t.Errorf("scheme %q accepted", scheme)
		}
	}
	b := New(Config{ID: "dlscheme.example.app", Shell: newFakeShell(), Logger: testLogger(t), DeepLink: &DeepLinkConfig{Scheme: "notes"}})
	b.cfg.DeepLink.Scheme = "NOTES"
	if path, ok := b.mapDeepLink("notes://a"); ok {
		t.Fatalf("a link was mapped to %q under a scheme off the grammar set after New", path)
	}
}

func TestScrubDeepLinkCutsAndMasks(t *testing.T) {
	if got := scrubDeepLink(strings.Repeat("a", 500)); len(got) != 200 {
		t.Fatalf("scrubbed length = %d, want 200", len(got))
	}
	if got := scrubDeepLink("a\x7fb\x00c\x1bd"); got != "a?b?c?d" {
		t.Fatalf("scrubDeepLink = %q, want a?b?c?d", got)
	}
}

type bootingPageWindow struct {
	*fakeWindow
	asks atomic.Int32
}

func (w *bootingPageWindow) EvalAsync(context.Context, string) (json.RawMessage, error) {
	if w.asks.Add(1) < 3 {
		return json.RawMessage("false"), nil
	}
	return json.RawMessage("true"), nil
}

func (w *bootingPageWindow) Eval(js string) error {
	if w.asks.Load() < 3 {
		return fmt.Errorf("eval before the page's runtime booted")
	}
	return w.fakeWindow.Eval(js)
}

func TestDeepLinkWaitsForThePageRuntime(t *testing.T) {
	b := New(Config{ID: "dlwait.example.app", Shell: newFakeShell(), Logger: testLogger(t), DeepLink: &DeepLinkConfig{Scheme: "notes"}})
	w := &bootingPageWindow{fakeWindow: &fakeWindow{id: MainWindowID}}
	b.window = w
	b.windows[MainWindowID] = w
	b.deliverDeepLink(w, "notes://a", "/a")
	if n := len(w.fakeWindow.evaluated); n == 0 {
		t.Fatal("the navigate eval ran before the page's runtime answered")
	}
}
