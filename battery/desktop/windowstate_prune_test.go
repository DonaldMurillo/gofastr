package desktop_test

import (
	"context"
	"testing"

	"github.com/DonaldMurillo/gofastr/battery/desktop"
	"github.com/DonaldMurillo/gofastr/battery/desktop/appstate"
	"github.com/DonaldMurillo/gofastr/battery/desktop/desktoptest"
)

// TestClosedSecondaryWindowFrameIsForgotten: secondary window ids are
// never reused (w2, w3, ...), so a frame remembered per id and never
// pruned grows the "windows" entry by one frame per open until it
// passes the appstate value cap, after which every later write,
// including the main window's frame and path, fails for good. A
// closed secondary window's frame leaves the store on close.
func TestClosedSecondaryWindowFrameIsForgotten(t *testing.T) {
	dir := t.TempDir()
	app, d := wsApp(t, desktop.Config{Title: "WS", RememberWindows: true}, dir, nil)
	h := desktoptest.Run(t, app, d)

	w, err := d.OpenWindow(desktop.WindowSpec{Path: "/two", Title: "Two", Width: 300, Height: 200})
	if err != nil {
		t.Fatal(err)
	}
	id := w.ID()
	h.Wait("the secondary window", func() bool { return h.Window(id) != nil })
	moved := desktop.Frame{X: 10, Y: 20, Width: 300, Height: 200}
	h.MoveWindow(id, moved)
	h.Wait("the state file to hold the secondary frame", func() bool {
		wins := windowsJSON(t, dir, "windowstate.test")
		return wins[id].Frame != nil && *wins[id].Frame == moved
	})

	h.CloseWindow(id)
	h.Wait("the closed window's frame to leave the state file", func() bool {
		wins := windowsJSON(t, dir, "windowstate.test")
		_, still := wins[id]
		return wins != nil && !still
	})
}

func TestDelayedFrameDoesNotRestoreClosedWindow(t *testing.T) {
	app, d := wsApp(t, desktop.Config{RememberWindows: true}, t.TempDir(), nil)
	h := desktoptest.Run(t, app, d)
	id := openPath(t, h, "/two")
	cfg, _ := h.Shell.Config()
	h.CloseWindow(id)

	cfg.OnWindowFrame(id, desktop.Frame{X: 10, Y: 20, Width: 300, Height: 200})
	entries, _, err := appstate.Get[map[string]any](d.State(), "windows")
	if err != nil {
		t.Fatal(err)
	}
	if _, found := entries[id]; found {
		t.Fatalf("late frame report restored closed window %s", id)
	}
}

func TestDelayedSettingsFrameDoesNotOverwriteReopenedWindow(t *testing.T) {
	app, d := wsApp(t, desktop.Config{
		RememberWindows: true,
		Settings:        &desktop.WindowSpec{Path: "/settings"},
	}, t.TempDir(), nil)
	h := desktoptest.Run(t, app, d)
	first, err := d.OpenSettings(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	h.CloseWindow(first.ID())
	second, err := d.OpenSettings(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	cfg, _ := h.Shell.Config()
	report := cfg.WindowFrameReporter()
	want := desktop.Frame{X: 10, Y: 20, Width: 300, Height: 200}
	report(second, want)
	report(first, desktop.Frame{X: 70, Y: 80, Width: 600, Height: 400})
	entries, _, err := appstate.Get[map[string]struct{ Frame desktop.Frame }](d.State(), "windows")
	if err != nil || entries[second.ID()].Frame != want {
		t.Fatalf("reopened Settings frame = %+v, err = %v, want %+v", entries[second.ID()].Frame, err, want)
	}
}
