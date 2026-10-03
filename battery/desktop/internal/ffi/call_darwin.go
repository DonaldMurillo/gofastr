//go:build darwin && arm64

package ffi

import (
	"sync"
	"unsafe"
)

//go:cgo_import_dynamic libc_qsort qsort "/usr/lib/libSystem.B.dylib"
//go:cgo_import_dynamic libc_dispatch_get_global_queue dispatch_get_global_queue "/usr/lib/libSystem.B.dylib"
//go:cgo_import_dynamic libc_dispatch_async_f dispatch_async_f "/usr/lib/libSystem.B.dylib"
//go:cgo_import_dynamic libc_pthread_self pthread_self "/usr/lib/libSystem.B.dylib"
//go:cgo_import_dynamic libc_snprintf snprintf "/usr/lib/libSystem.B.dylib"

// libc_qsort_trampoline_addr is set by the GLOBL/DATA pair in
// libc_darwin_arm64.s; the GC regression test drives qsort through it.
var libc_qsort_trampoline_addr uintptr

// libc_snprintf_trampoline_addr is the stack-argument oracle: a C
// function outside this package that formats arguments it can only have
// read from the C stack (see stackargs_darwin_arm64_test.go).
var libc_snprintf_trampoline_addr uintptr

// The background-callback test's libc surface (same GLOBL/DATA shape):
// a global dispatch queue delivers a callback on a thread Go never
// created, which is the needm path a UNUserNotificationCenter
// completion takes in a real app.
var (
	libc_dispatch_get_global_queue_trampoline_addr uintptr
	libc_dispatch_async_f_trampoline_addr          uintptr
	libc_pthread_self_trampoline_addr              uintptr
)

// callArgs is the argument block ·callTrampoline reads. Field order is
// load-bearing; the offsets are hard-coded in call_darwin_arm64.s and
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
//	f1   @232  d1 on return
//	f2   @240  d2 on return
//	f3   @248  d3 on return
//
// f1 to f3 are appended at the END so that no existing offset moves.
// They exist because AAPCS64 returns a homogeneous floating-point
// aggregate of up to four members in d0-d3 rather than through the x8
// indirect-result register — an NSRect comes back in d0-d3, which f0
// alone cannot carry. See CallRetHFA4.
type callArgs struct {
	fn   uintptr
	r1   uintptr
	r2   uintptr
	f0   float64
	ret8 uintptr
	ints [8]uintptr
	flts [8]float64
	stks [8]uintptr
	f1   float64
	f2   float64
	f3   float64
}

// Compile-time offset pins: a nonzero (negative) array length fails the
// build the day a field moves.
var _ = [1]struct{}{}[unsafe.Offsetof(callArgs{}.flts)-104]
var _ = [1]struct{}{}[unsafe.Offsetof(callArgs{}.stks)-168]
var _ = [1]struct{}{}[unsafe.Offsetof(callArgs{}.f1)-232]
var _ = [1]struct{}{}[unsafe.Offsetof(callArgs{}.f3)-248]

// callTrampoline is the C-ABI-shaped void call(callArgs*) defined in
// call_darwin_arm64.s; callTrampolineAddr holds its address (the
// GLOBL/DATA pair there, the same shape internal/objc uses for its libc
// trampolines).
func callTrampoline()

var callTrampolineAddr uintptr

// Call invokes the C function at fn with up to eight integer or
// pointer arguments in x0-x7, up to eight doubles in d0-d7, and — when
// ints holds more than eight entries — the remainder as C stack
// arguments (up to eight more, so at most sixteen integer arguments in
// total). It returns whatever the callee left in x0, x1, and d0.
//
// The call goes through runtime.cgocall (entersyscall / asmcgocall /
// exitsyscall), never syscall9's libcCall: cgocall records nothing in
// m.libcall*, so a GC scan of this goroutine while it is parked in C
// cannot mark it for a stack shrink that a callback would then trip
// over ("shrinking stack in libcall", runtime/stack.go:1305), and
// callbacks that grow the goroutine stack are designed for
// (asmcgocall re-derives the goroutine SP from the stack bound after
// the call).
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

// CallRetHFA4 is Call for a callee that returns a homogeneous
// floating-point aggregate — a struct whose members are all the same
// floating-point type, four or fewer of them. AAPCS64 returns those in
// d0-d3 and does NOT route them through the x8 indirect-result register,
// however large they are: an NSRect is 32 bytes and still comes back in
// four registers. CallRet8 on such a signature leaves the caller's
// buffer untouched, which is exactly what it did before this existed.
//
// The result is d0, d1, d2, d3 in order; a two-member aggregate uses the
// first two and leaves the rest as whatever the callee happened to have
// in those registers.
//
// System V has no equivalent classification, so this entry point exists
// only on arm64: on amd64 a >16-byte aggregate is MEMORY class whatever
// its members are, and CallRetStruct is the one to use.
func CallRetHFA4(fn uintptr, ints []uintptr, floats []float64) [4]float64 {
	if len(ints) > MaxIntArgs {
		panic("ffi: too many integer arguments for one Call")
	}
	if len(floats) > MaxFloatArgs {
		panic("ffi: too many float arguments for one Call")
	}
	a := callArgsPool.Get().(*callArgs)
	*a = callArgs{fn: fn}
	if len(ints) > IntRegs {
		copy(a.ints[:], ints[:IntRegs])
		copy(a.stks[:], ints[IntRegs:])
	} else {
		copy(a.ints[:], ints)
	}
	copy(a.flts[:], floats)
	runtime_cgocall(*(*unsafe.Pointer)(unsafe.Pointer(&callTrampolineAddr)), unsafe.Pointer(a))
	out := [4]float64{a.f0, a.f1, a.f2, a.f3}
	callArgsPool.Put(a)
	return out
}

// callArgsPool supplies the argument blocks. Pooling keeps them off
// goroutine stacks (stacks copy when a callback grows them; a heap
// address never moves under today's non-moving collector) and
// amortizes the zeroing the register load depends on.
var callArgsPool = sync.Pool{New: func() any { return new(callArgs) }}

// IntRegs is the number of integer/pointer arguments the C ABI passes
// in registers on this architecture: x0-x7 on arm64. Arguments past it
// travel as C stack words, which is where the register/stack split a
// caller has to reason about lives.
const IntRegs = 8

// MaxIntArgs and MaxFloatArgs bound one Call: IntRegs registers plus
// the eight stack words in callArgs.stks, and the eight float
// registers. Overrunning either is a caller bug, so it panics rather
// than truncating — the shape that let the ninth argument silently
// vanish before (the pre-fix code tested the result of copy into an
// 8-element array for > 8, which no copy can ever return, so
// callArgs.stks stayed zero and C saw zeros for arguments 9 to 16).
const (
	MaxIntArgs   = IntRegs + 8
	MaxFloatArgs = 8
)

func call(fn uintptr, ints []uintptr, floats []float64, ret unsafe.Pointer) (r1, r2 uintptr, f0 float64) {
	if len(ints) > MaxIntArgs {
		panic("ffi: too many integer arguments for one Call")
	}
	if len(floats) > MaxFloatArgs {
		panic("ffi: too many float arguments for one Call")
	}
	a := callArgsPool.Get().(*callArgs)
	*a = callArgs{fn: fn, ret8: uintptr(ret)}
	if len(ints) > IntRegs {
		copy(a.ints[:], ints[:IntRegs])
		copy(a.stks[:], ints[IntRegs:])
	} else {
		copy(a.ints[:], ints)
	}
	copy(a.flts[:], floats)
	runtime_cgocall(*(*unsafe.Pointer)(unsafe.Pointer(&callTrampolineAddr)), unsafe.Pointer(a))
	r1, r2, f0 = a.r1, a.r2, a.f0
	callArgsPool.Put(a)
	return
}
