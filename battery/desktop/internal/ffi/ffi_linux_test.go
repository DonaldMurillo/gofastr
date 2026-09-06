//go:build linux && (amd64 || arm64)

package ffi

import (
	"runtime"
	"sync"
	"testing"
)

// The FFI self-test ("the linkname self-test" in docs/desktop-plan.md):
// it drives every pulled runtime hook once — Call through
// runtime.cgocall into a trampoline entry, and runtime.cgocallback
// underneath it — and asserts the argument and return plumbing. A Go
// release that removes or reshapes either hook fails here first, in the
// untagged `go test ./battery/desktop/internal/...` lane, not in a
// user's app.
//
// The tests read intRegs rather than hard-coding eight: AAPCS64 passes
// eight integer arguments in registers, System V AMD64 six, and the
// difference is the one place the two Linux ports are not the same shape.
func TestCallbackRoundTrip(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	var mu sync.Mutex
	var gotA, gotB [8]uintptr

	// Slot 0: echoes the first and last register argument back as the
	// return value.
	addr0 := NewCallback(func(a *Args) uintptr {
		mu.Lock()
		gotA = a.Int
		mu.Unlock()
		return a.Int[0] + a.Int[intRegs-1]
	})

	// Slot 1 proves the table stride: registering a second callback must
	// land exactly entrySize past the first.
	addr1 := NewCallback(func(a *Args) uintptr {
		mu.Lock()
		gotB = a.Int
		mu.Unlock()
		return a.Int[1] * 3
	})

	if addr1-addr0 != entrySize {
		t.Errorf("table stride: entry 1 at %#x, entry 0 at %#x, delta %d, want %d", addr1, addr0, addr1-addr0, entrySize)
	}
	if addr0 < callbackasmAddr || addr0 >= callbackasmAddr+MaxCallbacks*entrySize {
		t.Errorf("entry 0 address %#x outside table [%#x,%#x)", addr0, callbackasmAddr, callbackasmAddr+MaxCallbacks*entrySize)
	}

	// Call entry 0 as C would: 11, 22, ... in the integer registers.
	want := make([]uintptr, intRegs)
	for i := range want {
		want[i] = uintptr(11 * (i + 1))
	}
	r0, _, _ := Call(addr0, want, nil)
	mu.Lock()
	seenA := gotA
	mu.Unlock()
	for i, w := range want {
		if seenA[i] != w {
			t.Errorf("entry 0 Int[%d]: got %#x, want %#x", i, seenA[i], w)
		}
	}
	if w := want[0] + want[intRegs-1]; r0 != w {
		t.Errorf("entry 0 return: got %#x, want %#x (a lost first-callback result reads as 0; see callbackasm1)", r0, w)
	}

	// Call entry 1 and confirm the index dispatch selects the right
	// closure under the same stride arithmetic Go used.
	r1, _, _ := Call(addr1, []uintptr{0, 7}, nil)
	mu.Lock()
	seenB := gotB
	mu.Unlock()
	if w := uintptr(7 * 3); r1 != w {
		t.Errorf("entry 1 return: got %#x, want %#x", r1, w)
	}
	if seenB[1] != 7 {
		t.Errorf("entry 1 Int[1]: got %#x, want 7", seenB[1])
	}

	// The float argument plumbing: all eight float registers must arrive
	// in the callback (both ABIs have eight), and the callback's return
	// bits must come back in the float return register too, because the
	// trampoline mirrors the result slot into both.
	var floatsSeen [8]float64
	rf := NewCallback(func(a *Args) uintptr {
		floatsSeen = a.Float
		return 0
	})
	Call(rf, nil, []float64{1.5, 0, 0, 2.5, 0, 0, 0, 4.25})
	wantF := [8]float64{1.5, 0, 0, 2.5, 0, 0, 0, 4.25}
	if floatsSeen != wantF {
		t.Errorf("float plumbing: callback saw %v, want %v", floatsSeen, wantF)
	}
}

// TestStackArgsSpill pins the integer arguments past the register set,
// which travel as full 8-byte C stack slots. The callback trampoline
// captures only the register arguments, so the assertion is on the
// callee side: a Go callback registered as the target sees exactly
// intRegs of them, and the call must still return cleanly with a
// balanced stack (a mis-sized outgoing area corrupts the return address
// and crashes here rather than anywhere useful later).
func TestStackArgsSpill(t *testing.T) {
	var seen [8]uintptr
	cb := NewCallback(func(a *Args) uintptr {
		seen = a.Int
		return a.Int[intRegs-1]
	})
	args := make([]uintptr, intRegs+8)
	for i := range args {
		args[i] = uintptr(i + 1)
	}
	r, _, _ := Call(cb, args, nil)
	if r != uintptr(intRegs) {
		t.Errorf("return with %d integer arguments: got %d, want %d", len(args), r, intRegs)
	}
	for i := range intRegs {
		if seen[i] != uintptr(i+1) {
			t.Errorf("Int[%d] = %d, want %d", i, seen[i], i+1)
		}
	}
}

// TestFirstCallbackReturnOnFreshM hunts the "first callback on an M
// loses its result write" anomaly documented in the callback assembly:
// it forces several brand-new Ms (a barrier of locked, busy goroutines
// exceeds GOMAXPROCS), then makes the FIRST trampoline call on each M
// return a known value. With iscgo true every one of those Ms was
// created through fakecgo's x_cgo_thread_start, so this also exercises
// the thread-start port under contention.
func TestFirstCallbackReturnOnFreshM(t *testing.T) {
	const workers = 16
	entered := make(chan struct{}, workers)
	release := make(chan struct{})
	results := make(chan uintptr, workers)

	for range workers {
		go func() {
			runtime.LockOSThread()
			entered <- struct{}{}
			<-release
			fn := NewCallback(func(a *Args) uintptr { return 0xC0FFEE })
			r, _, _ := Call(fn, []uintptr{1}, nil)
			results <- r
			runtime.UnlockOSThread()
		}()
	}
	for range workers {
		<-entered
	}
	// All workers hold distinct locked Ms and are parked on release, so
	// their pending first calls will run on Ms that have never executed
	// a callback.
	close(release)
	for range workers {
		if r := <-results; r != 0xC0FFEE {
			t.Fatalf("first callback on a fresh M returned %#x, want 0xC0FFEE (the callbackasm1 first-call anomaly)", r)
		}
	}
}

// TestNewCallbackSlotExhaustion pins the fixed-slot contract: the
// registry is bounded by MaxCallbacks and says so loudly. Pure Go: no C
// call.
func TestNewCallbackSlotExhaustion(t *testing.T) {
	used := callbackLen
	for callbackLen < MaxCallbacks {
		NewCallback(func(a *Args) uintptr { return 0 })
	}
	defer func() {
		if r := recover(); r == nil {
			t.Error("NewCallback did not panic at slot exhaustion")
		} else if _, ok := r.(string); !ok {
			t.Errorf("panic value is %T (%v), want string", r, r)
		}
		callbackLen = used // not a real free; keeps other tests runnable
	}()
	NewCallback(func(a *Args) uintptr { return 0 })
}
