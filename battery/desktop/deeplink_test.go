package desktop_test

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/battery/desktop"
	"github.com/DonaldMurillo/gofastr/battery/desktop/desktoptest"
)

// Deep links through the harness: the mapping notes://host/path?q to
// the app path /host/path?q, the refusals (wrong scheme, userinfo,
// length, traversal), the OnDeepLink override, and the queue that
// holds links until the window is up.

// deepLinkEvent mirrors the deep_link event body.
type deepLinkEvent struct {
	URL  string `json:"url"`
	Path string `json:"path"`
}

// gatedShell wraps the fake shell so a test can hold the ready callback
// back: Run records the WindowConfig, hands it over, and only then (on
// release) proceeds to the real fake-Shell Run. That window between
// "config recorded" and "window up" is exactly when the OS delivers a
// cold-launch link, deterministically.
type gatedShell struct {
	*desktoptest.Shell
	cfg     chan desktop.WindowConfig
	release chan struct{}
}

func (g *gatedShell) Run(ctx context.Context, w desktop.WindowConfig, ready func(desktop.Window)) error {
	g.cfg <- w
	<-g.release
	return g.Shell.Run(ctx, w, ready)
}

// TestDeepLinkNavigatesAndEmits: a link on the configured scheme
// focuses the main window, navigates it client-side, and reaches the
// page as a deep_link event carrying the raw URL and the mapped path.
func TestDeepLinkNavigatesAndEmits(t *testing.T) {
	app, d := uiApp(t, desktop.Config{
		Title:    "DL",
		DeepLink: &desktop.DeepLinkConfig{Scheme: "notes"},
	})
	h := desktoptest.Run(t, app, d)

	h.OpenURL("notes://notes/123?x=1")

	h.Wait("the deep-link navigation", func() bool { return len(h.Navigations()) == 1 })
	if got := h.Navigations()[0]; got != "/notes/123?x=1" {
		t.Fatalf("deep-link navigation = %q, want /notes/123?x=1", got)
	}
	ev := h.WaitEvent("deep_link")
	var p deepLinkEvent
	if err := ev.Unmarshal(&p); err != nil {
		t.Fatalf("deep_link payload: %v", err)
	}
	if p.URL != "notes://notes/123?x=1" || p.Path != "/notes/123?x=1" {
		t.Fatalf("deep_link payload = %+v, want url notes://notes/123?x=1 path /notes/123?x=1", p)
	}
	if got := h.Window("main").Focuses(); got != 1 {
		t.Fatalf("main window focuses = %d, want 1 (a deep link focuses it)", got)
	}
}

// TestDeepLinkSchemeAloneMapsToRoot: scheme:// with no host and no
// path maps to the app root.
func TestDeepLinkSchemeAloneMapsToRoot(t *testing.T) {
	app, d := uiApp(t, desktop.Config{
		Title:    "DL",
		DeepLink: &desktop.DeepLinkConfig{Scheme: "notes"},
	})
	h := desktoptest.Run(t, app, d)

	h.OpenURL("notes://")

	h.Wait("the root navigation", func() bool { return len(h.Navigations()) == 1 })
	if got := h.Navigations()[0]; got != "/" {
		t.Fatalf("scheme-only navigation = %q, want /", got)
	}
}

// TestDeepLinkRefusesOffSchemeUserinfoLength: a link on another
// scheme, one carrying userinfo, and one over 2048 bytes are each
// dropped; the scheme match itself is case-insensitive, so NOTES://
// is the one accepted link.
func TestDeepLinkRefusesOffSchemeUserinfoLength(t *testing.T) {
	app, d := uiApp(t, desktop.Config{
		Title:    "DL",
		DeepLink: &desktop.DeepLinkConfig{Scheme: "notes"},
	})
	h := desktoptest.Run(t, app, d)

	h.OpenURL("other://notes/123")                      // wrong scheme
	h.OpenURL("notes://user:secret@notes/123")          // userinfo
	h.OpenURL("notes://x/" + strings.Repeat("a", 2100)) // too long
	h.OpenURL("NOTES://notes/123")                      // accepted: scheme match folds case
	h.Wait("the case-insensitive link", func() bool { return len(h.Navigations()) == 1 })
	if got := h.Navigations()[0]; got != "/notes/123" {
		t.Fatalf("navigation = %q, want /notes/123 (only the accepted link)", got)
	}

	// The refusals after the accepted link stay absent from both the
	// navigation and the event record. They are synchronous, so a short
	// beat makes the absence meaningful.
	h.OpenURL("other://y")
	h.OpenURL("notes://u:p@y")
	time.Sleep(50 * time.Millisecond)
	if got := len(h.Navigations()); got != 1 {
		t.Fatalf("navigations = %d after two more refusals, want 1", got)
	}
	if got := len(h.Events()); got != 1 {
		t.Fatalf("events = %d after two more refusals, want 1 (deep_link only)", got)
	}
}

// TestDeepLinkRefusesTraversal: a host or path segment of ".." fails
// the navigate-path grammar and is dropped.
func TestDeepLinkRefusesTraversal(t *testing.T) {
	app, d := uiApp(t, desktop.Config{
		Title:    "DL",
		DeepLink: &desktop.DeepLinkConfig{Scheme: "notes"},
	})
	h := desktoptest.Run(t, app, d)

	h.OpenURL("notes://host/../etc")
	h.OpenURL("notes://../secret")
	h.OpenURL("notes://ok/1")
	h.Wait("the good link", func() bool { return len(h.Navigations()) == 1 })
	if got := h.Navigations()[0]; got != "/ok/1" {
		t.Fatalf("navigation = %q, want /ok/1 (traversal links dropped)", got)
	}
}

// TestDeepLinkOverride: OnDeepLink replaces the default mapping, and
// ok=false drops the link without a navigation.
func TestDeepLinkOverride(t *testing.T) {
	app, d := uiApp(t, desktop.Config{
		Title: "DL",
		DeepLink: &desktop.DeepLinkConfig{
			Scheme: "notes",
			OnDeepLink: func(u *url.URL) (string, bool) {
				if u.Host == "no" {
					return "", false
				}
				return "/custom" + u.Path, true
			},
		},
	})
	h := desktoptest.Run(t, app, d)

	h.OpenURL("notes://no/x") // override declines: dropped
	h.OpenURL("notes://a/x")  // override maps: /custom/x
	h.Wait("the override navigation", func() bool { return len(h.Navigations()) == 1 })
	if got := h.Navigations()[0]; got != "/custom/x" {
		t.Fatalf("override navigation = %q, want /custom/x", got)
	}
}

// TestDeepLinkInvalidSchemeDropsEverything: a DeepLink config whose
// scheme fails the grammar drops every link (the lazy validation the
// New-time check would have caught as a panic).
func TestDeepLinkInvalidSchemeIsAConstructionError(t *testing.T) {
	// "Notes" fails the scheme grammar (uppercase first letter). A bad
	// scheme is a wiring error: New panics, the same posture as a bad
	// menu or Settings path, so no battery ever runs with one.
	defer func() {
		v := recover()
		if v == nil {
			t.Fatal("New accepted DeepLink.Scheme \"Notes\"")
		}
		if msg, _ := v.(string); !strings.Contains(msg, "DeepLink.Scheme") {
			t.Fatalf("panic = %v, want the DeepLink.Scheme message", v)
		}
	}()
	uiApp(t, desktop.Config{Title: "DL", DeepLink: &desktop.DeepLinkConfig{Scheme: "Notes"}})
}

// TestDeepLinksQueueUntilWindow: links delivered before the window is
// up are queued (at most 16, oldest dropped) and replayed in order
// once the boot navigation finished.
func TestDeepLinksQueueUntilWindow(t *testing.T) {
	t.Setenv("GOFASTR_DESKTOP_DATA_DIR", t.TempDir())
	inner := desktoptest.NewShell()
	gated := &gatedShell{Shell: inner, cfg: make(chan desktop.WindowConfig, 1), release: make(chan struct{})}
	app, d := uiApp(t, desktop.Config{
		Title:    "DL",
		Shell:    gated,
		DeepLink: &desktop.DeepLinkConfig{Scheme: "notes"},
	})

	runErr := make(chan error, 1)
	go func() { runErr <- d.Run(app) }()
	t.Cleanup(func() {
		inner.Quit()
		select {
		case err := <-runErr:
			if err != nil {
				t.Errorf("Battery.Run returned %v", err)
			}
		case <-time.After(10 * time.Second):
			t.Error("Battery.Run did not return after Quit")
		}
	})

	cfg := <-gated.cfg // Run entered, config recorded, window NOT up
	for i := range 18 {
		cfg.OnDeepLink(fmt.Sprintf("notes://q/%d", i))
	}
	close(gated.release) // ready proceeds; the queued links flush

	// 16 deliveries, one navigate eval each (q/0 and q/1 dropped).
	deadline := time.Now().Add(10 * time.Second)
	paths := collectQueuedPaths(inner)
	for len(paths) != 16 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
		paths = collectQueuedPaths(inner)
	}
	if len(paths) != 16 {
		t.Fatalf("navigate evals = %d, want 16 (18 queued, oldest 2 dropped)", len(paths))
	}
	for i, p := range paths {
		if want := fmt.Sprintf("/q/%d", i+2); p != want {
			t.Fatalf("queued path %d = %q, want %q (replay order, oldest dropped)", i, p, want)
		}
	}
	events := 0
	for _, js := range inner.Window("main").Evals() {
		if ev, ok := desktoptest.ParseEvent(js); ok && ev.Name == "deep_link" {
			events++
		}
	}
	if events != 16 {
		t.Fatalf("deep_link events = %d, want 16", events)
	}
}

// collectQueuedPaths reads the fake main window's navigate evals. The
// gated shell has no window until release, so nil yields no paths.
func collectQueuedPaths(shell *desktoptest.Shell) []string {
	w := shell.Window("main")
	if w == nil {
		return nil
	}
	var paths []string
	for _, js := range w.Evals() {
		if p, ok := desktoptest.ParseNavigate(js); ok {
			paths = append(paths, p)
		}
	}
	return paths
}
