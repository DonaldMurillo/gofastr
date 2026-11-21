//go:build linux && (amd64 || arm64)

package ffi

import _ "unsafe" // for //go:cgo_import_dynamic

//go:cgo_import_dynamic libc_qsort qsort "libc.so.6"
//go:cgo_import_dynamic libc_pthread_create pthread_create "libc.so.6"
//go:cgo_import_dynamic libc_pthread_join pthread_join "libc.so.6"
//go:cgo_import_dynamic libc_pthread_self pthread_self "libc.so.6"

var (
	libc_qsort_trampoline_addr          uintptr
	libc_pthread_create_trampoline_addr uintptr
	libc_pthread_join_trampoline_addr   uintptr
	libc_pthread_self_trampoline_addr   uintptr
)
