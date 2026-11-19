//go:build (darwin || linux) && (amd64 || arm64)

package ffi

import (
	"sync/atomic"
	"testing"
	"unsafe"
)

// newGCComparator returns the qsort comparator callback shared by the
// per-OS TestGCCallbackSurvival regression tests: it counts invocations,
// drops a few KB of young garbage, recurses deep enough to grow the
// goroutine stack, and returns the three-way comparison of the two
// int64s the caller passed by pointer.
func newGCComparator(t *testing.T, comparisons *atomic.Int64) uintptr {
	return NewCallback(func(a *Args) uintptr {
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
}
