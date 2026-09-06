//go:build darwin && arm64

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
// C still holds the old address — the first draft of this test watched
// snprintf format the right ten characters into memory the buffer no
// longer occupied. runtime.KeepAlive does not help: it keeps the object
// alive, not still. Assigning through a package-level variable is what
// makes the allocation heap and therefore fixed.
var cSink []byte

//go:noinline
func cHeap(b []byte) []byte {
	cSink = b
	return b
}

// TestStackArgsReachC proves that integer arguments past the register
// quartet actually arrive, using a C function outside this package as
// the oracle so the answer cannot come from our own trampolines
// agreeing with each other.
//
// The regression: call() used to write the stack words with
//
//	n := copy(a.ints[:], ints)
//	if n > 8 { copy(a.stks[:], ints[8:]) }
//
// and copy into an 8-element array returns at most 8, so the branch was
// dead and callArgs.stks stayed zero — C read zeros for arguments 9 to
// 16 while Call's doc promised sixteen. Break the fix (drop the
// ints[IntRegs:] copy) and snprintf formats "0|0" instead, which is
// exactly what it did before the fix and what the register-argument
// probe below still shows.
//
// snprintf is the oracle because Apple's arm64 ABI hands EVERY variadic
// argument on the stack: fixed parameters take x0-x2 and the varargs
// start at stack word 0. Padding to IntRegs is therefore not a trick to
// force the stack path, it is the calling convention snprintf reads —
// proven by the second half of this test, where the same two values
// placed in x3 and x4 are ignored and snprintf reads the zeroed stack
// words instead.
func TestStackArgsReachC(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	format := cHeap(append([]byte("%ld|%ld"), 0))

	call := func(ints ...uintptr) (int, string) {
		buf := cHeap(make([]byte, 64))
		args := append([]uintptr{
			uintptr(unsafe.Pointer(&buf[0])),
			uintptr(len(buf)),
			uintptr(unsafe.Pointer(&format[0])),
		}, ints...)
		n, _, _ := Call(libc_snprintf_trampoline_addr, args, nil)
		runtime.KeepAlive(buf)
		runtime.KeepAlive(format)
		end := 0
		for end < len(buf) && buf[end] != 0 {
			end++
		}
		return int(int32(n)), string(buf[:end])
	}

	// x3-x7 padded, the two values at ints[8] and ints[9]: stack words
	// 0 and 1, where a darwin/arm64 variadic callee looks.
	n, got := call(0, 0, 0, 0, 0, 1234567, 89)
	if want := "1234567|89"; got != want || n != len(want) {
		t.Errorf("snprintf with stack arguments wrote %q (n=%d), want %q: arguments past x0-x7 did not reach the C stack", got, n, want)
	}

	// The same values in x3 and x4 instead. A variadic callee on
	// darwin/arm64 never looks at them, so it formats the zeroed stack
	// words — the control that shows the case above really did travel
	// on the stack.
	n, got = call(1234567, 89)
	if want := "0|0"; got != want || n != len(want) {
		t.Errorf("snprintf with register arguments wrote %q (n=%d), want %q: darwin/arm64 passes every variadic argument on the stack", got, n, want)
	}
}

// TestCallRejectsTooManyArgs pins the other half of the fix: past the
// sixteen integer or eight float slots the block can carry, Call says so
// instead of silently dropping arguments on the floor.
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
