//go:build darwin && (arm64 || amd64)

package ffi

import (
	"math/rand/v2"
	"runtime"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"
)

// TestGCCallbackSurvival is the regression test for the spike's GC
// hazard: a C call that parks the goroutine must be compatible with
// the garbage collector shrinking that goroutine's stack while a
// native callback runs Go code on it.
//
// The shape: one locked thread sorts 4096 int64s through libc qsort,
// so ONE C call invokes the Go comparator ~50k times; the comparator
// allocates several KB and grows the goroutine stack through a
// recursive helper, so both stack growth (morestack inside the
// callback) and stack shrink (GC scan of the parked, in-syscall
// goroutine) happen repeatedly. While the sort runs, one goroutine
// cycles runtime.GC() + debug.FreeOSMemory() and another allocates
// garbage.
//
// On the old syscall.syscall9/libcCall path this throws
//
//	runtime: shrinking stack in libcall
//
// from runtime/stack.go:1305: libcCall records m.libcallsp for the
// duration of the C call, the GC scan of the parked goroutine sets
// preemptShrink, and the comparator's next stack check resolves it
// with the record still live — before any GODEBUG escape is consulted.
// runtime.cgocall keeps no libcall record, and asmcgocall re-derives
// the goroutine SP from the stack bound after the call, so copies
// during callbacks are designed for. The throw was reproduced against
// syscall9 in this same test file (temporary linkname, since deleted;
// output in the phase report) before the path was removed.
//
// Run with -count=3 before believing any change here.
func TestGCCallbackSurvival(t *testing.T) {
	debug.SetGCPercent(100) // the spike ran with GC off; the fix must not need that

	const n = 4096
	data := make([]int64, n)
	seedA, seedB := rand.Uint64(), rand.Uint64()
	r := rand.New(rand.NewPCG(seedA, seedB))
	for i := range data {
		data[i] = r.Int64()
	}

	var comparisons atomic.Int64
	compar := NewCallback(func(a *Args) uintptr {
		c := comparisons.Add(1)
		x := *(*int64)(*(*unsafe.Pointer)(unsafe.Pointer(&a.Int[0])))
		y := *(*int64)(*(*unsafe.Pointer)(unsafe.Pointer(&a.Int[1])))
		// A few KB of young garbage per callback, plus stack growth:
		// the recursion runs ~60-360 frames deep (well past the 8 KiB
		// initial stack), and the GC goroutine's FreeOSMemory hammers
		// the shrink direction between callbacks.
		_ = make([]byte, 4<<10)
		depth := 60 + int(c%300)
		if growStack(depth) != depth {
			t.Error("growStack lost count")
		}
		switch {
		case x < y:
			return ^uintptr(0) // -1
		case x > y:
			return 1
		}
		return 0
	})

	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		cycles := 0
		for {
			select {
			case <-stop:
				t.Logf("GC goroutine: %d cycles", cycles)
				return
			default:
			}
			runtime.GC()
			debug.FreeOSMemory()
			cycles++
		}
	}()
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			b := make([]byte, 1<<20)
			b[0] = 1
			b[len(b)-1] = 2
		}
	}()

	done := make(chan struct{})
	go func() {
		defer close(done)
		runtime.LockOSThread()
		Call(libc_qsort_trampoline_addr, []uintptr{
			uintptr(unsafe.Pointer(&data[0])), uintptr(n), 8, compar,
		}, nil)
		runtime.UnlockOSThread()
	}()
	select {
	case <-done:
	case <-time.After(120 * time.Second):
		t.Fatal("qsort did not finish in 120s")
	}
	close(stop)
	wg.Wait()

	if c := comparisons.Load(); c < 10000 {
		t.Errorf("only %d comparator invocations, want >= 10000", c)
	}
	for i := 1; i < len(data); i++ {
		if data[i-1] > data[i] {
			t.Fatalf("array not sorted at %d (pcg seeds %d/%d): %d > %d — a comparator return value was lost", i, seedA, seedB, data[i-1], data[i])
		}
	}
}

// growStack recurses depth frames with a per-frame live slice so the
// compiler cannot tail-call it away; the last frame reports the depth
// it reached.
func growStack(depth int) int {
	if depth <= 0 {
		return 0
	}
	var pad [64]byte // keeps a frame alive
	pad[0] = byte(depth)
	return growStack(depth-1) + 1
}
