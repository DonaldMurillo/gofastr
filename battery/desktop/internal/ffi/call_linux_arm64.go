//go:build linux && arm64

package ffi

import (
	"sync"
	"unsafe"
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
}

var _ = [1]struct{}{}[unsafe.Offsetof(callArgs{}.flts)-104]
var _ = [1]struct{}{}[unsafe.Offsetof(callArgs{}.stks)-168]

const intRegs = 8

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

var callArgsPool = sync.Pool{New: func() any { return new(callArgs) }}

func call(fn uintptr, ints []uintptr, floats []float64, ret unsafe.Pointer) (r1, r2 uintptr, f0 float64) {
	if len(ints) > intRegs+len(callArgs{}.stks) {
		panic("ffi: too many integer arguments for one Call")
	}
	if len(floats) > len(callArgs{}.flts) {
		panic("ffi: too many float arguments for one Call")
	}
	a := callArgsPool.Get().(*callArgs)
	*a = callArgs{fn: fn, ret8: uintptr(ret)}
	copy(a.ints[:], ints)
	if len(ints) > 8 {
		copy(a.stks[:], ints[8:])
	}
	copy(a.flts[:], floats)
	runtime_cgocall(*(*unsafe.Pointer)(unsafe.Pointer(&callTrampolineAddr)), unsafe.Pointer(a))
	r1, r2, f0 = a.r1, a.r2, a.f0
	callArgsPool.Put(a)
	return
}
