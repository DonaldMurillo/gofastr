//go:build linux && arm64

package ffi

import (
	"sync"
	"unsafe"
)

// callArgs is the argument block ·callTrampoline reads. Field order is
// load-bearing; the offsets are hard-coded in call_linux_arm64.s and
// pinned by the compile-time checks below:
//
//	fn   @0    C function to call
//	r1   @8    x0 on return
//	r2   @16   x1 on return
//	f0   @24   d0 on return
//	ret8 @32   indirect-struct-return pointer; loaded into x8 before
//	           the call when non-zero
//	ints @40   integer/pointer arguments; ints[0:8] travel in x0-x7,
//	           ints[8:] spill to the C stack as stack arguments
//	flts @104  float arguments d0-d7
//	stks @168  stack arguments after the first eight integer registers
type callArgs struct {
	fn   uintptr
	r1   uintptr
	r2   uintptr
	f0   float64
	ret8 uintptr
	ints [8]uintptr
	flts [8]float64
	stks [8]uintptr
}

// Compile-time offset pins: a nonzero (negative) array length fails the
// build the day a field moves.
var _ = [1]struct{}{}[unsafe.Offsetof(callArgs{}.flts)-104]
var _ = [1]struct{}{}[unsafe.Offsetof(callArgs{}.stks)-168]

// intRegs is how many integer arguments AAPCS64 passes in registers.
// Everything past it becomes a stack argument. The amd64 sibling has
// six; the tests read this rather than hard-coding either.
const intRegs = 8

// callTrampoline is the C-ABI-shaped void call(callArgs*) defined in
// call_linux_arm64.s; callTrampolineAddr holds its address (the
// GLOBL/DATA pair there).
func callTrampoline()

var callTrampolineAddr uintptr

// Call invokes the C function at fn with up to eight integer or pointer
// arguments in x0-x7, up to eight doubles in d0-d7, and — when ints
// holds more than eight entries — the remainder as C stack arguments
// (up to eight more, so at most sixteen integer arguments in total). It
// returns whatever the callee left in x0, x1, and d0.
//
// The call goes through runtime.cgocall (entersyscall / asmcgocall /
// exitsyscall). cgocall records nothing in m.libcall*, so a GC scan of
// this goroutine while it is parked in C cannot mark it for a stack
// shrink that a callback would then trip over ("shrinking stack in
// libcall", runtime/stack.go:1305), and callbacks that grow the
// goroutine stack are designed for (asmcgocall re-derives the goroutine
// SP from the stack bound after the call).
//
// AAPCS64 differences from the darwin sibling, for anyone extending
// this: Linux pads every stack argument to a full 8-byte slot, so the
// stks block is already the correct packing (darwin packs sub-8-byte
// stack arguments tightly and would need a packing-aware trampoline);
// and Linux passes variadic arguments in registers exactly like fixed
// ones, where darwin puts every variadic argument on the stack. Neither
// difference needs a code change here, but a variadic C function called
// through this trampoline is correct on Linux and wrong on darwin.
//
// Pointer arguments in ints point at Go memory at the caller's risk,
// same rule as cgo: the caller must runtime.KeepAlive every Go value
// whose address it passed, across this call. The argument block itself
// is pooled on the heap, so its address is stable even if a callback
// grows and copies the calling goroutine's stack mid-call; the
// trampoline re-reads its address after the callee returns.
func Call(fn uintptr, ints []uintptr, floats []float64) (r1, r2 uintptr, f0 float64) {
	return call(fn, ints, floats, nil)
}

// CallRet8 is Call for callees that return a struct through the
// indirect-result register: ret receives the callee's writes via x8.
// arm64 C uses x8 for struct returns that do not fit the register
// quartet (x0,x1,d0,d1) or are not homogeneous float aggregates.
func CallRet8(fn, ret unsafe.Pointer, ints []uintptr, floats []float64) (r1, r2 uintptr, f0 float64) {
	// fn is a C function pointer, never a Go heap reference, so the
	// uintptr conversion cannot strand a moving pointer.
	return call(uintptr(fn), ints, floats, ret)
}

// callArgsPool supplies the argument blocks. Pooling keeps them off
// goroutine stacks (stacks copy when a callback grows them; a heap
// address never moves under today's non-moving collector) and amortizes
// the zeroing the register load depends on.
var callArgsPool = sync.Pool{New: func() any { return new(callArgs) }}

func call(fn uintptr, ints []uintptr, floats []float64, ret unsafe.Pointer) (r1, r2 uintptr, f0 float64) {
	a := callArgsPool.Get().(*callArgs)
	*a = callArgs{fn: fn, ret8: uintptr(ret)}
	copy(a.ints[:], ints)
	// The spill test is on the caller's slice, not on copy's return
	// value: copy into a [8]uintptr can never report more than 8.
	if len(ints) > 8 {
		copy(a.stks[:], ints[8:])
	}
	copy(a.flts[:], floats)
	runtime_cgocall(*(*unsafe.Pointer)(unsafe.Pointer(&callTrampolineAddr)), unsafe.Pointer(a))
	r1, r2, f0 = a.r1, a.r2, a.f0
	callArgsPool.Put(a)
	return
}
