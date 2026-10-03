//go:build linux && (amd64 || arm64)

package ffi

import (
	"unsafe"

	// The fake-cgo layer: its x_cgo_init sets runtime.iscgo from rt0_go
	// and it defines the _cgo_* symbols the runtime then demands.
	// runtime.cgocall throws "cgocall unavailable" without it
	// (runtime/cgocall.go:135), so every binary that calls ffi imports
	// fakecgo right here, ahead of any use of Call.
	_ "github.com/DonaldMurillo/gofastr/battery/desktop/internal/fakecgo"
)

// The two runtime symbols this package pulls, each with the place in the
// Go 1.27 source that pushes it outward for outside callers
// (go.dev/issue/67401 tracks the "hall of shame" push directives; the Go
// team removes none of them without a release note, and the untagged
// self-test in ffi_linux_test.go exercises every hook listed here).

// runtime_cgocall calls fn(arg) on the system stack with the C ABI,
// wrapped in entersyscall/asmcgocall/exitsyscall. On Linux it is also
// the ONLY way to run C on a system stack at all: there is no
// syscall.syscall9 outside darwin, and cgocall itself refuses to run
// unless runtime.iscgo is true (cgocall.go:135), which is the whole
// reason internal/fakecgo exists.
//
// Pushed by runtime/cgocall.go:118-133
// ("widely used packages access it using linkname; notable members of
// the hall of shame include github.com/ebitengine/purego").
//
//go:linkname runtime_cgocall runtime.cgocall
func runtime_cgocall(fn, arg unsafe.Pointer) int32

// runtime_cgocallback is declared (never called from Go) so the runtime
// symbol is retained for the BL runtime·cgocallback(SB) in
// the per-arch callback_linux_*.s. It is entered on the system stack (g0) of the
// calling thread, borrowing an M through needm when the thread is not
// one Go created, switches to the calling goroutine's stack, calls
// fn(frame), and switches back.
//
// Pushed by runtime/stubs.go:213-219 (same hall-of-shame note).
//
//go:linkname runtime_cgocallback runtime.cgocallback
func runtime_cgocallback(fn, frame, ctxt uintptr)
