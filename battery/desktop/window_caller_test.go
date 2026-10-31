package desktop_test

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/battery/desktop"
)

// The window capability's title, setTitle and snapshot used the main
// window whichever window called them, so desktop-notes' Settings
// window, mirroring document.title through setTitle, renamed the main
// window, and a secondary window's snapshot returned the main window.
func TestWindowCapActsOnTheCaller(t *testing.T) {
	h, _ := chromeApp(t, desktop.Config{Title: "Main"})
	w2 := openPath(t, h, "/two")

	h.CallFrom(w2, "window", "setTitle", map[string]any{"title": "Two"}).AssertOK(t)
	if got := h.Window("main").Title(); got != "Main" {
		t.Fatalf("a w2 setTitle renamed main to %q", got)
	}
	if got := h.Window(w2).Title(); got != "Two" {
		t.Fatalf("w2 title = %q, want Two", got)
	}
	var title struct {
		Title string `json:"title"`
	}
	h.CallFrom(w2, "window", "title", nil).MustResult(t, &title)
	if title.Title != "Two" {
		t.Fatalf("w2 title() = %q, want Two", title.Title)
	}
	h.CallFrom("main", "window", "title", nil).MustResult(t, &title)
	if title.Title != "Main" {
		t.Fatalf("main title() = %q, want Main", title.Title)
	}

	// A window that is gone answers not_found; it never falls back to
	// the main window, whose pixels a snapshot would otherwise return.
	h.CallFrom("main", "windows", "close", map[string]any{"id": w2}).AssertOK(t)
	h.CallFrom(w2, "window", "snapshot", nil).AssertCode(t, desktop.CodeNotFound)
	h.CallFrom(w2, "window", "title", nil).AssertCode(t, desktop.CodeNotFound)
	h.CallFrom(w2, "window", "setTitle", map[string]any{"title": "x"}).AssertCode(t, desktop.CodeNotFound)
	h.CallFrom("main", "window", "snapshot", nil).AssertOK(t)
}
