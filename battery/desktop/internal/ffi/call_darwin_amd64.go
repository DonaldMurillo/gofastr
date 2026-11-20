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

var libc_qsort_trampoline_addr uintptr

var libc_snprintf_trampoline_addr uintptr

var (
	libc_dispatch_get_global_queue_trampoline_addr uintptr
	libc_dispatch_async_f_trampoline_addr          uintptr
	libc_pthread_self_trampoline_addr              uintptr
)

// IntRegs is the number of integer arguments the C ABI passes in registers.
const IntRegs = 6

// MaxIntArgs and MaxFloatArgs bound one Call's integer and float arguments.
const (
	MaxIntArgs   = IntRegs + 8
	MaxFloatArgs = 8
)

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

var _ = [1]struct{}{}[unsafe.Offsetof(callArgs{}.ints)-40]
var _ = [1]struct{}{}[unsafe.Offsetof(callArgs{}.flts)-88]
var _ = [1]struct{}{}[unsafe.Offsetof(callArgs{}.stks)-152]

func callTrampoline()

var callTrampolineAddr uintptr

// Call invokes the C function at fn and returns rax, rdx and xmm0; integer arguments past six go on the C stack.
func Call(fn uintptr, ints []uintptr, floats []float64) (r1, r2 uintptr, f0 float64) {
	return call(fn, ints, floats, nil)
}

// CallStack is Call for a signature with a MEMORY-class struct argument, such as an NSRect.
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

// CallRetStruct is Call for callees that return a struct too large for the return registers.
func CallRetStruct(fn, ret unsafe.Pointer, ints []uintptr, floats []float64) (r1, r2 uintptr, f0 float64) {
	full := make([]uintptr, 0, len(ints)+1)
	full = append(full, uintptr(ret))
	full = append(full, ints...)
	return call(uintptr(fn), full, floats, nil)
}

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
