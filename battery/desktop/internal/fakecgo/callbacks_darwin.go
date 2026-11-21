//go:build darwin && (arm64 || amd64) && !cgo

package fakecgo

import (
	_ "unsafe" // for //go:linkname and //go:cgo_import_dynamic
)

//go:cgo_import_dynamic libc_pthread_self              pthread_self              "/usr/lib/libSystem.B.dylib"
//go:cgo_import_dynamic libc_pthread_get_stackaddr_np  pthread_get_stackaddr_np  "/usr/lib/libSystem.B.dylib"
//go:cgo_import_dynamic libc_pthread_get_stacksize_np  pthread_get_stacksize_np  "/usr/lib/libSystem.B.dylib"
//go:cgo_import_dynamic libc_pthread_attr_init         pthread_attr_init         "/usr/lib/libSystem.B.dylib"
//go:cgo_import_dynamic libc_pthread_attr_setdetachstate pthread_attr_setdetachstate "/usr/lib/libSystem.B.dylib"
//go:cgo_import_dynamic libc_pthread_attr_setstacksize pthread_attr_setstacksize "/usr/lib/libSystem.B.dylib"
//go:cgo_import_dynamic libc_pthread_create            pthread_create            "/usr/lib/libSystem.B.dylib"
//go:cgo_import_dynamic libc_pthread_sigmask           pthread_sigmask           "/usr/lib/libSystem.B.dylib"
//go:cgo_import_dynamic libc_sigfillset                sigfillset                "/usr/lib/libSystem.B.dylib"
//go:cgo_import_dynamic libc_nanosleep                 nanosleep                 "/usr/lib/libSystem.B.dylib"
//go:cgo_import_dynamic libc_pthread_mutex_lock        pthread_mutex_lock        "/usr/lib/libSystem.B.dylib"
//go:cgo_import_dynamic libc_pthread_mutex_unlock      pthread_mutex_unlock      "/usr/lib/libSystem.B.dylib"
//go:cgo_import_dynamic libc_pthread_cond_broadcast    pthread_cond_broadcast    "/usr/lib/libSystem.B.dylib"
//go:cgo_import_dynamic libc_malloc                    malloc                    "/usr/lib/libSystem.B.dylib"
//go:cgo_import_dynamic libc_free                      free                      "/usr/lib/libSystem.B.dylib"
//go:cgo_import_dynamic libc_setenv                    setenv                    "/usr/lib/libSystem.B.dylib"
//go:cgo_import_dynamic libc_unsetenv                  unsetenv                  "/usr/lib/libSystem.B.dylib"
//go:cgo_import_dynamic libc_write                     write                     "/usr/lib/libSystem.B.dylib"
//go:cgo_import_dynamic libc_abort                     abort                     "/usr/lib/libSystem.B.dylib"
//go:cgo_import_dynamic libc_getpid getpid "/usr/lib/libSystem.B.dylib"
//go:cgo_import_dynamic libc_getenv getenv "/usr/lib/libSystem.B.dylib"

var libc_getpid_trampoline_addr uintptr
var libc_getenv_trampoline_addr uintptr

// LibcGetpidAddr returns the address of libc getpid.
func LibcGetpidAddr() uintptr { return libc_getpid_trampoline_addr }

// LibcGetenvAddr returns the address of libc getenv.
func LibcGetenvAddr() uintptr { return libc_getenv_trampoline_addr }

//go:linkname runtime_iscgo runtime.iscgo
var runtime_iscgo bool

var noopCrosscall2 = func() {}

var setgGCC uintptr

var xCGOPthreadKeyCreated uintptr

// SetgStored reports the setg_gcc pointer x_cgo_init received from rt0_go.
func SetgStored() uintptr { return setgGCC }
