//go:build darwin && amd64

package ffi

import (
	"runtime"
	"testing"
	"unsafe"
)

// cSink forces the buffers below onto the heap. A uintptr is invisible
// to escape analysis, so the naive `buf := make([]byte, 64)` compiles to
// a STACK allocation (`go test -gcflags=-m` says "make([]byte, 64) does
// not escape") and a goroutine stack copy is then free to move it while
// C still holds the old address. runtime.KeepAlive does not help: it
// keeps the object alive, not still. Assigning through a package-level
// variable is what makes the allocation heap and therefore fixed.
var cSink []byte

//go:noinline
func cHeap(b []byte) []byte {
	cSink = b
	return b
}

// TestStackArgsReachC proves that integer arguments past the six System
// V registers actually arrive, using a C function outside this package
// as the oracle so the answer cannot come from our own trampolines
// agreeing with each other.
//
// The regression this pins was found on the arm64 side: call() wrote the
// stack words under `if n > 8` where n came from a copy into an
// 8-element array, so the branch was dead and C read zeros for every
// argument past the registers. The amd64 block has six register slots
// rather than eight, which makes the stack path ordinary rather than
// exotic: any message send with more than four arguments after self and
// the selector already needs it.
//
// snprintf is the oracle. Unlike darwin/arm64, System V passes variadic
// arguments in the ordinary registers first, so buf, size and format
// take rdi, rsi, rdx and the first three values take rcx, r8, r9; the
// fourth is the first that has to travel in the caller's stack argument
// area. Break the fix (drop the ints[IntRegs:] copy) and the fourth
// value formats as 0.
func TestStackArgsReachC(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	format := cHeap(append([]byte("%ld|%ld|%ld|%ld"), 0))
	buf := cHeap(make([]byte, 64))

	const want = "11|22|33|1234567"
	n, _, _ := Call(libc_snprintf_trampoline_addr, []uintptr{
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(len(buf)),
		uintptr(unsafe.Pointer(&format[0])),
		11, 22, 33, // rcx, r8, r9
		1234567, // stack word 0
	}, nil)
	runtime.KeepAlive(buf)
	runtime.KeepAlive(format)

	end := 0
	for end < len(buf) && buf[end] != 0 {
		end++
	}
	if got := string(buf[:end]); got != want || int(int32(n)) != len(want) {
		t.Errorf("snprintf wrote %q (n=%d), want %q: the argument past rdi/rsi/rdx/rcx/r8/r9 did not reach the C stack", got, int32(n), want)
	}
}

// TestCallRejectsTooManyArgs pins the other half of the fix: past the
// fourteen integer or eight float slots the block can carry, Call says
// so instead of silently dropping arguments on the floor.
func TestCallRejectsTooManyArgs(t *testing.T) {
	for _, tc := range []struct {
		name   string
		ints   []uintptr
		floats []float64
	}{
		{"ints", make([]uintptr, MaxIntArgs+1), nil},
		{"floats", nil, make([]float64, MaxFloatArgs+1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Error("Call accepted more arguments than the block can carry")
				}
			}()
			Call(libc_pthread_self_trampoline_addr, tc.ints, tc.floats)
		})
	}
}

// TestCallStackRejectsRegisterOverflow pins CallStack's precondition:
// once the caller lays out the stack area itself, register arguments
// cannot spill into it, because only the real C signature knows what
// order the two would interleave in.
func TestCallStackRejectsRegisterOverflow(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("CallStack accepted more integer arguments than there are registers")
		}
	}()
	CallStack(libc_pthread_self_trampoline_addr, make([]uintptr, IntRegs+1), nil, []uintptr{0})
}
