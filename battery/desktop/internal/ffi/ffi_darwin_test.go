//go:build darwin && (arm64 || amd64)

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
// The syscall9-era threading contract (a dedicated goroutine that
// locked its thread, worked, and parked forever because cgocallback
// left m.incgo set with no cgocall wrapper to clear it) is gone with
// syscall9: Call balances incgo in cgocall, so ordinary goroutines may
// make C calls and receive callbacks and then continue — exactly what
// cgo binaries do.
func TestCallbackRoundTrip(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	var mu sync.Mutex
	var gotA, gotB [8]uintptr

	// Slot 0: echoes registers 0 and 7 back as the return value.
	addr0 := NewCallback(func(a *Args) uintptr {
		mu.Lock()
		gotA = a.Int
		mu.Unlock()
		return a.Int[0] + a.Int[7]
	})

	// Slot 1 proves the 8-byte table stride: registering a second
	// callback must land exactly entrySize past the first.
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

	// Call entry 0 as C would: (11, 22, ..., 88) in x0..x7.
	r0, _, _ := Call(addr0, []uintptr{11, 22, 33, 44, 55, 66, 77, 88}, nil)
	mu.Lock()
	seenA := gotA
	mu.Unlock()
	for i, w := range []uintptr{11, 22, 33, 44, 55, 66, 77, 88} {
		if seenA[i] != w {
			t.Errorf("entry 0 Int[%d]: got %#x, want %#x", i, seenA[i], w)
		}
	}
	if want := uintptr(11 + 88); r0 != want {
		t.Errorf("entry 0 return: got %#x, want %#x (a lost first-callback result reads as 0; see callback_darwin_arm64.s)", r0, want)
	}

	// Call entry 1 and confirm the index dispatch selects the right
	// closure under the same stride arithmetic Go used.
	r1, _, _ := Call(addr1, []uintptr{0, 7}, nil)
	mu.Lock()
	seenB := gotB
	mu.Unlock()
	if want := uintptr(7 * 3); r1 != want {
		t.Errorf("entry 1 return: got %#x, want %#x", r1, want)
	}
	if seenB[1] != 7 {
		t.Errorf("entry 1 Int[1]: got %#x, want 7", seenB[1])
	}

	// The float argument plumbing: d0 must arrive in the callback and
	// d0 must carry the callback's return bits back (the result slot is
	// mirrored into both x0 and d0 by the trampoline).
	var floatsSeen [8]float64
	rf := NewCallback(func(a *Args) uintptr {
		floatsSeen = a.Float
		return 0
	})
	Call(rf, nil, []float64{1.5, 0, 0, 2.5, 0, 0, 0, 4.25})
	want := [8]float64{1.5, 0, 0, 2.5, 0, 0, 0, 4.25}
	if floatsSeen != want {
		t.Errorf("float plumbing: callback saw %v, want %v", floatsSeen, want)
	}
}

// TestFirstCallbackReturnOnFreshM hunts the "first callback on an M
// loses its result write" anomaly documented in callback_darwin_arm64.s
// on the current (cgocall) path: it forces several brand-new Ms (a
// barrier of locked, busy goroutines exceeds GOMAXPROCS), then makes
// the FIRST trampoline call on each M return a known value.
func TestFirstCallbackReturnOnFreshM(t *testing.T) {
	const workers = 16
	entered := make(chan struct{}, workers)
	release := make(chan struct{})
	results := make(chan uintptr, workers)
	fail := make(chan string, workers)

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
		select {
		case r := <-results:
			if r != 0xC0FFEE {
				t.Fatalf("first callback on a fresh M returned %#x, want 0xC0FFEE (the callbackasm1 first-call anomaly)", r)
			}
		case msg := <-fail:
			t.Fatal(msg)
		}
	}
}

// TestNewCallbackSlotExhaustion pins the fixed-slot contract: the
// registry is bounded by MaxCallbacks and says so loudly. Pure Go: no
// C call.
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
