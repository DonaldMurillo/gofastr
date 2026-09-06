//go:build darwin && amd64

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
// libc_darwin_amd64.s; the GC regression test drives qsort through it.
var libc_qsort_trampoline_addr uintptr

// libc_snprintf_trampoline_addr is the stack-argument oracle: a C
// function outside this package that formats arguments it can only have
// read from the C stack (see stackargs_darwin_amd64_test.go).
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

// IntRegs is the number of integer/pointer arguments the System V AMD64
// ABI passes in registers: rdi, rsi, rdx, rcx, r8, r9. Six, against
// arm64's eight — the one place a caller that lays out its own C stack
// arguments has to know which architecture it is on.
const IntRegs = 6

// MaxIntArgs and MaxFloatArgs bound one Call: IntRegs registers plus the
// eight stack words in callArgs.stks, and the eight SSE registers.
// Overrunning either is a caller bug, so it panics rather than
// truncating.
const (
	MaxIntArgs   = IntRegs + 8
	MaxFloatArgs = 8
)

// callArgs is the argument block ·callTrampoline reads. Field order is
// load-bearing; the offsets are hard-coded in call_darwin_amd64.s and
// pinned by the compile-time checks below:
//
//	fn     @0    C function to call
//	r1     @8    rax on return
//	r2     @16   rdx on return (the second half of a two-eightbyte
//	             INTEGER return, e.g. a 16-byte struct)
//	f0     @24   xmm0 on return
//	nfloat @32   value for al: how many SSE registers carry arguments.
//	             System V requires it on every call to a variadic
//	             callee and ignores it elsewhere, so it is always set.
//	ints   @40   integer/pointer arguments; ints[0:6] travel in
//	             rdi, rsi, rdx, rcx, r8, r9
//	flts   @88   float arguments, xmm0-xmm7
//	stks   @152  the outgoing C stack argument area, in order
//
// There is no arm64-style ret8 field: System V returns a MEMORY-class
// struct through a hidden pointer that occupies the FIRST integer
// argument slot (proved by clang: `NSRect getrect(void*, void*)`
// compiled for x86_64 keeps the caller's rdi in rbx across the call and
// returns it in rax), so CallRetStruct prepends it to ints instead of
// giving it a register of its own.
type callArgs struct {
	fn     uintptr
	r1     uintptr
	r2     uintptr
	f0     float64
	nfloat uintptr
	ints   [IntRegs]uintptr
	flts   [8]float64
	stks   [8]uintptr
}

// Compile-time offset pins: a nonzero (negative) array length fails the
// build the day a field moves.
var _ = [1]struct{}{}[unsafe.Offsetof(callArgs{}.ints)-40]
var _ = [1]struct{}{}[unsafe.Offsetof(callArgs{}.flts)-88]
var _ = [1]struct{}{}[unsafe.Offsetof(callArgs{}.stks)-152]

// callTrampoline is the C-ABI-shaped void call(callArgs*) defined in
// call_darwin_amd64.s; callTrampolineAddr holds its address (the
// GLOBL/DATA pair there, the same shape internal/objc uses for its libc
// trampolines).
func callTrampoline()

var callTrampolineAddr uintptr

// Call invokes the C function at fn with up to six integer or pointer
// arguments in rdi, rsi, rdx, rcx, r8, r9, up to eight doubles in
// xmm0-xmm7, and — when ints holds more than six entries — the
// remainder as C stack arguments (up to eight more, so at most fourteen
// integer arguments in total). It returns whatever the callee left in
// rax, rdx, and xmm0.
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
// same rule as cgo, and one degree worse: a uintptr is invisible to
// escape analysis, so a Go buffer whose address is passed this way must
// be heap-allocated. runtime.KeepAlive keeps it alive, not still, and a
// stack-allocated one moves under a goroutine stack copy while C holds
// the old address. The argument block itself is pooled on the heap, so
// its address is stable even if a callback grows and copies the calling
// goroutine's stack mid-call; the trampoline re-reads its address after
// the callee returns.
func Call(fn uintptr, ints []uintptr, floats []float64) (r1, r2 uintptr, f0 float64) {
	return call(fn, ints, floats, nil)
}

// CallStack is Call for a signature with a MEMORY-class argument — a
// struct System V passes in the argument area rather than in registers,
// such as an NSRect. stack is the outgoing stack area laid out by the
// caller in eightbyte words, and ints must therefore be at most IntRegs
// long: anything past that would have to follow the caller's own words,
// which only the real C signature can order correctly.
//
// This entry point exists only on amd64. arm64 has no use for it: every
// aggregate the desktop host passes is a homogeneous float aggregate
// that travels in d0-d7 there, which is what SendRect uses.
func CallStack(fn uintptr, ints []uintptr, floats []float64, stack []uintptr) (r1, r2 uintptr, f0 float64) {
	if len(ints) > IntRegs {
		panic("ffi: CallStack cannot order register arguments after a caller-laid-out stack area")
	}
	if len(stack) > len(callArgs{}.stks) {
		panic("ffi: stack argument area too large for one CallStack")
	}
	if len(floats) > MaxFloatArgs {
		panic("ffi: too many float arguments for one CallStack")
	}
	a := callArgsPool.Get().(*callArgs)
	*a = callArgs{fn: fn, nfloat: uintptr(len(floats))}
	copy(a.ints[:], ints)
	copy(a.flts[:], floats)
	copy(a.stks[:], stack)
	runtime_cgocall(*(*unsafe.Pointer)(unsafe.Pointer(&callTrampolineAddr)), unsafe.Pointer(a))
	r1, r2, f0 = a.r1, a.r2, a.f0
	callArgsPool.Put(a)
	return
}

// CallRetStruct is Call for callees that return a struct too large for
// the return registers. ret receives the callee's writes.
//
// System V classifies an aggregate larger than two eightbytes as MEMORY
// and returns it through a hidden pointer supplied by the caller as the
// first integer argument, so ints holds the LOGICAL arguments and the
// hidden pointer is prepended here; that costs one of the six integer
// registers. (arm64 spends no argument register on this: it has a
// dedicated indirect-result register, x8, which is why CallRet8 there
// takes the same arguments and leaves the register numbering alone.)
//
// For an Objective-C message this must be dispatched through
// objc_msgSend_stret, not objc_msgSend: with the hidden pointer in rdi
// the receiver has moved to rsi, and only the _stret entry point knows
// that. See internal/objc's msgSendStruct.
func CallRetStruct(fn, ret unsafe.Pointer, ints []uintptr, floats []float64) (r1, r2 uintptr, f0 float64) {
	full := make([]uintptr, 0, len(ints)+1)
	// fn and ret are C-visible pointers the caller already owns; the
	// uintptr conversion cannot strand a moving pointer.
	full = append(full, uintptr(ret))
	full = append(full, ints...)
	return call(uintptr(fn), full, floats, nil)
}

// callArgsPool supplies the argument blocks. Pooling keeps them off
// goroutine stacks (stacks copy when a callback grows them; a heap
// address never moves under today's non-moving collector) and
// amortizes the zeroing the register load depends on.
var callArgsPool = sync.Pool{New: func() any { return new(callArgs) }}

func call(fn uintptr, ints []uintptr, floats []float64, _ unsafe.Pointer) (r1, r2 uintptr, f0 float64) {
	if len(ints) > MaxIntArgs {
		panic("ffi: too many integer arguments for one Call")
	}
	if len(floats) > MaxFloatArgs {
		panic("ffi: too many float arguments for one Call")
	}
	a := callArgsPool.Get().(*callArgs)
	*a = callArgs{fn: fn, nfloat: uintptr(len(floats))}
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
