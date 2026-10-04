//go:build windows && amd64

package windows

import (
	"testing"
)

func TestCloseReturnsWhenAlreadyClosed(t *testing.T) {
	w := &winWindow{}
	w.closed.Store(true)
	if err := w.Close(); err != nil {
		t.Fatalf("Close on an already-closed window returned %v", err)
	}
}

func TestCloseWindowOnUIThreadGuardsInvalidWindows(t *testing.T) {
	var calls int
	send := func(uintptr, uint32, uintptr, uintptr) { calls++ }
	closeWindowOnUIThread(0, false, send)
	closeWindowOnUIThread(42, true, send)
	if calls != 0 {
		t.Fatalf("sent %d close messages for an invalid window", calls)
	}
	closeWindowOnUIThread(42, false, send)
	if calls != 1 {
		t.Fatalf("sent %d close messages for a live window, want 1", calls)
	}
}
