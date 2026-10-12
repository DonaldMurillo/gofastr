package desktop_test

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/battery/desktop"
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
