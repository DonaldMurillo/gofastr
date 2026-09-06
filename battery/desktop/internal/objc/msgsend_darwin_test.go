//go:build darwin && (arm64 || amd64)

package objc

import (
	"runtime"
	"strings"
	"testing"
)

// The message-send ABI test. It runs against real Foundation classes and
// opens no window, so it belongs in the untagged lane on both
// architectures: `go test ./battery/desktop/internal/...` under
// GOARCH=arm64 and, through Rosetta, under GOARCH=amd64.
//
// Foundation rather than AppKit on purpose. AppKit demands the main
// thread, and a Go test function never runs on it (the main goroutine is
// parked in testing.M.Run, so it is not draining the dispatch main queue
// either and objc.Main would simply time out). Foundation's value types
// are thread-safe, so the test locks whatever thread it got and works
// there.
//
// What each round trip pins:
//
//   - NSString: pointer arguments and a pointer return, the plain
//     objc_msgSend path both architectures share.
//   - NSNumber numberWithDouble: / doubleValue: a double argument in the
//     first float register and a double return from it. On amd64 this is
//     also the evidence that objc_msgSend_fpret is not needed — a
//     CGFloat-sized double comes back in xmm0 from ordinary
//     objc_msgSend, and _fpret is for x87 long double only.
//   - NSValue valueWithRect: / rectValue: a 32-byte aggregate by value
//     and a 32-byte aggregate returned. This is the pair the two ABIs
//     genuinely disagree about — d0-d3 and the x8 indirect-result
//     register on arm64, the caller's stack argument area and
//     objc_msgSend_stret on amd64 — and the only test in the tree that
//     would notice either half being wrong.
func TestFoundationMessageABI(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	if _, err := DlopenGlobal("/System/Library/Frameworks/Foundation.framework/Foundation"); err != nil {
		t.Fatalf("dlopen Foundation: %v", err)
	}

	t.Run("string round trip", func(t *testing.T) {
		const want = "gofastr desktop — ünïcode ✓"
		if got := GoString(NSString(want)); got != want {
			t.Errorf("NSString round trip: got %q, want %q", got, want)
		}
	})

	t.Run("double argument and return", func(t *testing.T) {
		const want = -1234.5625 // exact in binary64, so == is honest here
		n := ID(SendF(Class("NSNumber"), Sel("numberWithDouble:"), nil, []float64{want}))
		if n == 0 {
			t.Fatal("numberWithDouble: returned nil")
		}
		got := SendDouble(n, Sel("doubleValue"), nil, nil)
		if got != want {
			t.Errorf("doubleValue: got %v, want %v: a double does not survive the float registers", got, want)
		}
	})

	t.Run("rect by value and returned", func(t *testing.T) {
		// Four distinct, exactly representable values, so a swapped or
		// dropped member is unmistakable.
		want := Rect{X: 1.5, Y: -2.25, W: 300.125, H: 400.5}
		v := ID(SendRect(Class("NSValue"), Sel("valueWithRect:"), want))
		if v == 0 {
			t.Fatal("valueWithRect: returned nil")
		}
		if got := SendRectRet(v, Sel("rectValue")); got != want {
			t.Errorf("NSRect round trip: got %+v, want %+v", got, want)
		}
	})

	t.Run("rect survives a class method that also takes integers", func(t *testing.T) {
		// NSStringFromRect formats the rect; the receiver and selector
		// are the only integer arguments, but the return is a pointer,
		// so this checks the rect landed where Foundation reads it
		// without relying on our own struct-return path at all.
		want := Rect{X: 10, Y: 20, W: 30, H: 40}
		s := ID(SendRect(Class("NSValue"), Sel("valueWithRect:"), want))
		desc := GoString(ID(Send(s, Sel("description"))))
		if desc == "" {
			t.Fatal("description returned an empty string")
		}
		for _, member := range []string{"10", "20", "30", "40"} {
			if !strings.Contains(desc, member) {
				t.Errorf("NSValue description %q is missing %q: the rect did not arrive intact", desc, member)
			}
		}
	})
}
