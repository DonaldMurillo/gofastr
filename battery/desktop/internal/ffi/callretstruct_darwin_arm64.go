//go:build darwin && arm64

package ffi

import "unsafe"

// CallRetStruct calls fn, which returns a struct too large for the return registers, into ret.
func CallRetStruct(fn, ret unsafe.Pointer, ints []uintptr, floats []float64) (r1, r2 uintptr, f0 float64) {
	return CallRet8(fn, ret, ints, floats)
}
