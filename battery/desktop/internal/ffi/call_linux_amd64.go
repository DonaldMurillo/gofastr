//go:build linux && amd64

package ffi

import (
	"sync"
	"unsafe"
)

// callArgs is the argument block ·callTrampoline reads. The layout is
// identical to the arm64 sibling's — the offsets are pinned below and
// hard-coded in call_linux_amd64.s — but two fields mean something
// different under System V AMD64:
//
//	fn   @0    C function to call
//	r1   @8    AX on return
//	r2   @16   DX on return (the second word of a two-register return)
//	f0   @24   X0 on return
//	ret8 @32   unused on amd64: this ABI has no dedicated
//	           indirect-result register, so CallRet8 passes the
//	           destination as a hidden first integer argument instead
//	           (see call below) and the field stays zero
//	ints @40   integer/pointer arguments; ints[0:6] travel in
//	           DI, SI, DX, CX, R8, R9 — six, not arm64's eight
//	flts @104  float arguments X0-X7
//	stks @168  stack arguments after the first six integer registers
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

// intRegs is how many integer arguments System V AMD64 passes in
// registers. Everything past it becomes a stack argument, one full
// 8-byte slot each.
const intRegs = 6

// callTrampoline is the C-ABI-shaped void call(callArgs*) defined in
// call_linux_amd64.s; callTrampolineAddr holds its address (the
// GLOBL/DATA pair there).
func callTrampoline()

var callTrampolineAddr uintptr

// Call invokes the C function at fn with up to six integer or pointer
// arguments in DI, SI, DX, CX, R8, R9, up to eight doubles in X0-X7,
// and — when ints holds more than six entries — the remainder as C stack
// arguments (up to eight more, so at most fourteen integer arguments in
// total). It returns whatever the callee left in AX, DX, and X0.
//
// The call goes through runtime.cgocall (entersyscall / asmcgocall /
// exitsyscall). cgocall records nothing in m.libcall*, so a GC scan of
// this goroutine while it is parked in C cannot mark it for a stack
// shrink that a callback would then trip over ("shrinking stack in
// libcall", runtime/stack.go:1305), and callbacks that grow the
// goroutine stack are designed for (asmcgocall re-derives the goroutine
// SP from the stack bound after the call).
//
// Variadic callees work: the trampoline sets AL to 8, the count of
// vector registers a variadic callee must spill to its register save
// area, and eight is always a safe upper bound.
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

// CallRet8 is Call for a callee that returns a struct too large for the
// AX:DX / X0:X1 quartet, which System V AMD64 returns through memory:
// the caller passes the destination as a hidden first integer argument
// and every declared argument shifts one register to the right. arm64
// has a dedicated x8 for the same job and needs no shift, which is why
// the name is arm64's and the mechanism here is not.
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
	*a = callArgs{fn: fn}

	if ret != nil {
		// The hidden first argument. Done here rather than in assembly
		// so the trampoline has exactly one argument layout to load.
		var shifted [1 + intRegs + 8]uintptr // one hidden + every slot the block can carry
		shifted[0] = uintptr(ret)
		n := copy(shifted[1:], ints)
		ints = shifted[:n+1]
	}

	copy(a.ints[:intRegs], ints)
	// The spill test is on the caller's slice, not on copy's return
	// value: copy into a fixed-size window can never report more.
	if len(ints) > intRegs {
		copy(a.stks[:], ints[intRegs:])
	}
	copy(a.flts[:], floats)
	runtime_cgocall(*(*unsafe.Pointer)(unsafe.Pointer(&callTrampolineAddr)), unsafe.Pointer(a))
	r1, r2, f0 = a.r1, a.r2, a.f0
	callArgsPool.Put(a)
	return
}
