package main

import (
	"net/http"
	"testing"

	"github.com/DonaldMurillo/gofastr/battery/desktop"
)

// The floating Quick note widget through the harness: the tray row
// opens it with the widget style, the screen renders design-system
// markup only (drag handle, close button, the same entity form), and
// the page's windows.close drops it.

func TestQuickNoteWidgetOpensFromTrayRow(t *testing.T) {
	h := newHarness(t)

	h.ClickTray("Quick note…")
	h.Wait("the widget window", func() bool { return len(h.Shell.OpenWindowCalls()) == 1 })
	calls := h.Shell.OpenWindowCalls()
	spec := calls[0].Spec
	if calls[0].ID != "w2" {
		t.Fatalf("widget id = %q, want w2 (the normal id rule)", calls[0].ID)
	}
	if spec.Path != "/widget" || calls[0].URL != h.URL("/widget") {
		t.Fatalf("widget path/url = %q / %q", spec.Path, calls[0].URL)
	}
	if spec.Width != 320 || spec.Height != 280 {
		t.Fatalf("widget size = %dx%d, want 320x280", spec.Width, spec.Height)
	}
	s := spec.Style
	if s.Chrome != desktop.ChromeNone || !s.Panel || !s.Transparent {
		t.Fatalf("widget style = %+v, want borderless non-activating transparent panel", s)
	}

	// The widget's screen is design-system components: the drag handle
	// attribute, the close hook, and the entity form with its reset.
	page := h.Get("/widget").AssertStatus(t, http.StatusOK)
	for _, want := range []string{
		"Quick note",
		`data-fui-window-drag`,
		`data-notes-widget-close`,
		`data-fui-rpc="/api/notes"`,
		`data-fui-rpc-reset`,
		`id="widget-note-title"`,
		`Add note`,
	} {
		page.AssertContains(t, want)
	}

	// A second activation focuses the open widget, it does not stack.
	h.ClickTray("Quick note…")
	h.Wait("the focus", func() bool { return h.Window("w2").Focuses() == 1 })
	if n := len(h.Shell.OpenWindowCalls()); n != 1 {
		t.Fatalf("OpenWindow calls after a second tray click = %d, want 1 (focus)", n)
	}
}

func TestQuickNoteWidgetFormCreatesANote(t *testing.T) {
	h := newHarness(t)
	h.ClickTray("Quick note…")
	h.Wait("the widget window", func() bool { return len(h.Shell.OpenWindowCalls()) == 1 })

	// The exact request the widget's form intercept sends.
	id := createNote(t, h, "From the widget", "")
	if id == "" {
		t.Fatal("createNote returned no id")
	}
	h.Get("/").AssertStatus(t, http.StatusOK).AssertContains(t, "From the widget")
	h.Get("/notes/"+id).AssertStatus(t, http.StatusOK).AssertContains(t, "<title>From the widget")
}

func TestQuickNoteWidgetClosesThroughThePage(t *testing.T) {
	h := newHarness(t)
	h.ClickTray("Quick note…")
	h.Wait("the widget window", func() bool { return len(h.WindowIDs()) == 2 })

	// windows.close with the widget's own id, the way the Close button
	// drives it.
	h.Call("windows", "close", map[string]any{"id": "w2"}).AssertOK(t)
	if ids := h.WindowIDs(); len(ids) != 1 {
		t.Fatalf("windows after the page close = %v, want main only", ids)
	}
	if !h.Window("w2").Closed() {
		t.Fatal("the widget's fake window was not closed")
	}

	// The path is free again: the tray row reopens a fresh widget.
	h.ClickTray("Quick note…")
	h.Wait("the reopened widget", func() bool { return len(h.Shell.OpenWindowCalls()) == 2 })
}
