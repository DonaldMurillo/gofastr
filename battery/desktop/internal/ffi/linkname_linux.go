//go:build linux && (amd64 || arm64)

package ffi

import (
	"unsafe"
)

//go:linkname runtime_cgocall runtime.cgocall
func runtime_cgocall(fn, arg unsafe.Pointer) int32

//go:linkname runtime_cgocallback runtime.cgocallback
func runtime_cgocallback(fn, frame, ctxt uintptr)
