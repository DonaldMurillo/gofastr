package desktop

import (
	"bytes"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
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
