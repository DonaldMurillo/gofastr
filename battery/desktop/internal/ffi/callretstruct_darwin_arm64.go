//go:build darwin && arm64

package ffi

import "unsafe"

// CallRetStruct is the architecture-neutral name for "call fn, which
// returns a struct too large for the return registers, into ret". On
// arm64 that is exactly CallRet8: the AAPCS64 indirect-result register
// x8 is not an argument register, so the caller's own arguments keep
// their numbering and nothing has to shift.
//
// The amd64 twin has the same signature and the same meaning but a
// different mechanism (System V spends the first integer argument
// register on the hidden pointer), which is the whole reason for the
// shared name: internal/objc's msgSendStruct calls this on both
// architectures and only its choice of objc_msgSend entry point differs.
func CallRetStruct(fn, ret unsafe.Pointer, ints []uintptr, floats []float64) (r1, r2 uintptr, f0 float64) {
	return CallRet8(fn, ret, ints, floats)
}
