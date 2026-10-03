package desktop

import (
	"fmt"
	"testing"
)

// PROPERTY
//
//	Every count of a native resource created from request input is
//	capped, and the refusal is a typed bridge error rather than an
//	unbounded allocation.
//
// SURFACE, windows.open (D-06; PROVEN by execution: 1000 windows, no
// refusal)
//
//	windows.open carries Permission: "" (ungated on the reasoning that
//	the page already lives in a window and can only reach the app's own
//	screens). OpenWindow de-dupes per PATH STRING, and validNavigatePath
//	accepts a query, so "/notes?i=0", "/notes?i=1", ... are all distinct
//	paths. Each one opens a real native window and appends to b.windows,
//	b.winOrder and b.winPaths, none of which is bounded:
//
//	  for (let i = 0; i < 10000; i++)
//	      __gofastr.desktop.windows.open({path: '/?i=' + i})
//
//	Same-origin only, so this is availability, not data reach, but it is
//	an ungated, page-driven allocation of an OS-level resource, and the
//	three maps it grows never shrink for a window the user closes with
//	no OnWindowClosed callback.
//
// The cap belongs in Battery.OpenWindow, not in the capability handler:
// OpenWindow is also the in-process API (menu handlers, OpenSettings)
// and a bound applied at only one of the two callers is the drift this
// package keeps producing.

// TestSecondaryWindowsAreCapped is the core pin.
func TestSecondaryWindowsAreCapped(t *testing.T) {
	b, shell := newTestBattery(t)
	shell.openWindows = make(map[string]*fakeWindow)
	b.windowMu.Lock()
	b.addr = "127.0.0.1:1"
	b.windowMu.Unlock()

	const probe = 512 // far past any plausible cap
	var refusedAt = -1
	for i := 0; i < probe; i++ {
		if _, err := b.OpenWindow(WindowSpec{Path: fmt.Sprintf("/n?i=%d", i)}); err != nil {
			refusedAt = i
			break
		}
	}
	if refusedAt < 0 {
		t.Fatalf("opened %d native windows from one page with no refusal; windows.open is ungated and "+
			"b.windows / b.winOrder / b.winPaths all grow without a bound", probe)
	}
	if refusedAt < 2 {
		t.Fatalf("refused at window %d: the cap must leave room for the app's own secondary windows", refusedAt)
	}
}

// TestCappedOpenRefusesTyped pins the shape of the refusal: a closed-set
// bridge error the page can render, never a panic or a bare error.
func TestCappedOpenRefusesTyped(t *testing.T) {
	b, shell := newTestBattery(t)
	shell.openWindows = make(map[string]*fakeWindow)
	b.windowMu.Lock()
	b.addr = "127.0.0.1:1"
	b.windowMu.Unlock()

	for i := 0; i < 512; i++ {
		_, err := b.OpenWindow(WindowSpec{Path: fmt.Sprintf("/n?i=%d", i)})
		if err == nil {
			continue
		}
		de, ok := err.(*Error)
		if !ok {
			t.Fatalf("OpenWindow refused with %T (%v); the bridge needs a *desktop.Error with a closed-set code", err, err)
		}
		if de.Code != CodeDenied && de.Code != CodeInvalidInput {
			t.Fatalf("OpenWindow refused with code %q; want denied or invalid_input", de.Code)
		}
		return
	}
	t.Fatal("no refusal in 512 opens")
}

// TestReopeningSamePathIsIdempotent is the anti-vacuity half: the cap
// must not break the documented per-path idempotence, or a legitimate
// app that reopens its settings window trips the bound.
func TestReopeningSamePathIsIdempotent(t *testing.T) {
	b, shell := newTestBattery(t)
	shell.openWindows = make(map[string]*fakeWindow)
	b.windowMu.Lock()
	b.addr = "127.0.0.1:1"
	b.windowMu.Unlock()

	for i := 0; i < 64; i++ {
		if _, err := b.OpenWindow(WindowSpec{Path: "/settings"}); err != nil {
			t.Fatalf("reopening the same path was refused at %d: %v; per-path opens are idempotent (focus, not open)", i, err)
		}
	}
	if got := len(b.Windows()); got != 1 {
		t.Fatalf("%d windows for 64 opens of one path, want 1", got)
	}
}
