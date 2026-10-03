//go:build darwin && (arm64 || amd64)

package objc

import (
	"strings"
	"testing"
	"time"
)

// PROPERTY 1
//
//	A Go buffer whose address crosses into C as a uintptr is HEAP
//	resident, never stack allocated.
//
// The only use the compiler can see for &b[0] in cBytes is a conversion
// to uintptr, which escape analysis does not count as an escape, so it
// is free to put the buffer in the caller's frame. A goroutine stack
// MOVES: a callback that grows it while C still holds the pointer (this
// host runs callbacks from inside C on purpose) copies the buffer
// elsewhere and leaves the C side reading vacated memory.
// runtime.KeepAlive keeps a value LIVE; it does not keep it in PLACE.
// Same rule ffi.Call's callArgsPool exists for.

// TestCBytesBufferIsHeapResident pins the MECHANISM, not the effect.
//
// Counting heap allocations would be vacuous here: cBytes is not
// inlinable today and returns the slice, so `go build -gcflags=-m`
// reports "make([]byte, len(s) + 1) escapes to heap" with or without
// the sink — a measurement that cannot fail is not a test. What the
// sink defends against is a FUTURE inlining change: inlined into a
// caller that only converts &b[0] to a uintptr, the buffer becomes
// stack-allocatable, and a goroutine stack moves.
//
// Storing the pointer into a package-level variable IS the escape, so
// asserting the store happened asserts the escape is real and cannot be
// optimized away. Verified by mutation: delete the store and this
// fails.
func TestCBytesBufferIsHeapResident(t *testing.T) {
	for _, s := range []string{"", "a", strings.Repeat("x", 200)} {
		escapeSink.Store(nil)
		b, p, err := cBytes(s)
		if err != nil || p == 0 || len(b) != len(s)+1 {
			t.Fatalf("cBytes(%q) = (%d bytes, %#x, %v)", s, len(b), p, err)
		}
		if got := escapeSink.Load(); got != &b[0] {
			t.Errorf("cBytes(%q) did not publish the buffer's address (%p, want %p): without that "+
				"escape the compiler is free to stack-allocate it once cBytes becomes inlinable, and a "+
				"callback that grows the goroutine stack while C holds the pointer moves it out from "+
				"under the C side. runtime.KeepAlive keeps a value live, not in place", s, got, &b[0])
		}
	}
}

// PROPERTY 2
//
//	The Objective-C crossing never panics on a bad string.
//
// A Go panic here is FATAL and unrecoverable: NSString runs inside an
// ffi trampoline entered through runtime.cgocallback, unwinding into
// the main goroutine parked in [NSApp run] with no recover on the path.
// One page-supplied string carrying a NUL used to end the process.
// Refusing the input is the caller's job (the bridge chokepoint answers
// invalid_input); this is the crossing declining to be a kill switch.
func TestNSStringNeverPanicsOnNUL(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("the Objective-C crossing panicked on a NUL-bearing string: %v\n"+
				"that panic is fatal: it crosses runtime.cgocallback with no recover", r)
		}
	}()

	nul := "before" + string(rune(0)) + "after"

	if got := NSString(nul); got != 0 {
		t.Errorf("NSString(NUL-bearing) = %#x, want the nil NSString (0)", got)
	}
	id, err := NSStringErr(nul)
	if err == nil {
		t.Error("NSStringErr(NUL-bearing) returned no error; the refusal must be visible to a caller that asks")
	}
	if id != 0 {
		t.Errorf("NSStringErr(NUL-bearing) = %#x, want 0", id)
	}
	if err != nil && !strings.Contains(err.Error(), "NUL") {
		t.Errorf("error %q does not name the problem", err)
	}
}

// PROPERTY 3
//
//	Main's deadline belongs to the caller, and a timed-out job leaves
//	nothing behind on the queue.
//
// A fixed ten seconds silently voided every permission alert a user was
// slower than that to answer (NSAlert's runModal blocks until the
// click), and the abandoned closure stayed queued: it ran later, wrote
// through the caller's captured variables after the caller had
// returned, and stacked one more modal alert per retry.
//
// `go test` never drains the dispatch main queue (the main goroutine is
// the test runner, not a run loop), so every job here times out — which
// is exactly the condition under test.
func TestMainTimeoutDropsTheQueuedJob(t *testing.T) {
	before := pendingMainJobs()

	start := time.Now()
	err := MainWithTimeout(40*time.Millisecond, func() { t.Error("the job must not have run") })
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("MainWithTimeout returned nil with no run loop to drain the queue")
	}
	if elapsed > 2*time.Second {
		t.Errorf("waited %s for a 40ms deadline: the caller's timeout is not the one being honoured", elapsed)
	}
	if got := pendingMainJobs(); got != before {
		t.Errorf("pending jobs %d -> %d: a timed-out job stayed on the queue, so it will run later, "+
			"write through a caller that has already returned, and stack another alert", before, got)
	}
}

// TestMainDefaultTimeoutIsShort pins that the default stays the short
// one for work that returns on its own, so only callers that wait for a
// person opt into a long deadline.
func TestMainDefaultTimeoutIsShort(t *testing.T) {
	if DefaultMainTimeout > 30*time.Second {
		t.Errorf("DefaultMainTimeout = %s; the default is for work that returns on its own", DefaultMainTimeout)
	}
}

// pendingMainJobs reports the queue depth.
func pendingMainJobs() int {
	mainMu.Lock()
	defer mainMu.Unlock()
	return len(mainFns)
}
