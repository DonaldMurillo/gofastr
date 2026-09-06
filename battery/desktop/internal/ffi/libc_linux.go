//go:build linux && (amd64 || arm64)

package ffi

import _ "unsafe" // for //go:cgo_import_dynamic

// The libc surface the package's own tests drive through Call. qsort
// invokes a Go comparator thousands of times from inside one C call (the
// GC regression); pthread_create/pthread_join deliver a callback on a
// thread Go never created (the needm path — Linux has no libdispatch, so
// a raw pthread is the closest thing to the GIO/libnotify worker a real
// app will see); pthread_self identifies the thread inside the callback.

//go:cgo_import_dynamic libc_qsort qsort "libc.so.6"
//go:cgo_import_dynamic libc_pthread_create pthread_create "libc.so.6"
//go:cgo_import_dynamic libc_pthread_join pthread_join "libc.so.6"
//go:cgo_import_dynamic libc_pthread_self pthread_self "libc.so.6"

// Set by the GLOBL/DATA pairs in libc_linux_arm64.s and libc_linux_amd64.s. The address of a
// cgo_import_dynamic symbol must originate in assembly; a linknamed byte
// var does not bind without a host object.
var (
	libc_qsort_trampoline_addr          uintptr
	libc_pthread_create_trampoline_addr uintptr
	libc_pthread_join_trampoline_addr   uintptr
	libc_pthread_self_trampoline_addr   uintptr
)
