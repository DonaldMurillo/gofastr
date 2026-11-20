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

var libc_qsort_trampoline_addr uintptr

var libc_snprintf_trampoline_addr uintptr

var (
	libc_dispatch_get_global_queue_trampoline_addr uintptr
	libc_dispatch_async_f_trampoline_addr          uintptr
	libc_pthread_self_trampoline_addr              uintptr
)

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

var _ = [1]struct{}{}[unsafe.Offsetof(callArgs{}.flts)-104]
var _ = [1]struct{}{}[unsafe.Offsetof(callArgs{}.stks)-168]
var _ = [1]struct{}{}[unsafe.Offsetof(callArgs{}.f1)-232]
var _ = [1]struct{}{}[unsafe.Offsetof(callArgs{}.f3)-248]

func callTrampoline()

var callTrampolineAddr uintptr

// Call invokes the C function at fn and returns x0, x1 and d0; integer arguments past x0-x7 go on the C stack.
func Call(fn uintptr, ints []uintptr, floats []float64) (r1, r2 uintptr, f0 float64) {
	return call(fn, ints, floats, nil)
}

// CallRet8 is Call for a callee returning a struct through x8 into ret.
func CallRet8(fn, ret unsafe.Pointer, ints []uintptr, floats []float64) (r1, r2 uintptr, f0 float64) {
	return call(uintptr(fn), ints, floats, ret)
}

// CallRetHFA4 is Call for a callee returning a homogeneous float aggregate of up to four members in d0-d3.
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

var callArgsPool = sync.Pool{New: func() any { return new(callArgs) }}

// IntRegs is the number of integer arguments the C ABI passes in registers.
const IntRegs = 8

// MaxIntArgs and MaxFloatArgs bound one Call's integer and float arguments.
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
