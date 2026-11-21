//go:build linux && (amd64 || arm64) && !cgo

package fakecgo

import (
	_ "unsafe" // for //go:linkname and //go:cgo_import_dynamic
)

//go:cgo_import_dynamic libc_pthread_self              pthread_self              "libc.so.6"
//go:cgo_import_dynamic libc_pthread_getattr_np        pthread_getattr_np        "libc.so.6"
//go:cgo_import_dynamic libc_pthread_attr_init         pthread_attr_init         "libc.so.6"
//go:cgo_import_dynamic libc_pthread_attr_destroy      pthread_attr_destroy      "libc.so.6"
//go:cgo_import_dynamic libc_pthread_attr_getstack     pthread_attr_getstack     "libc.so.6"
//go:cgo_import_dynamic libc_pthread_attr_getstacksize pthread_attr_getstacksize "libc.so.6"
//go:cgo_import_dynamic libc_pthread_attr_setdetachstate pthread_attr_setdetachstate "libc.so.6"
//go:cgo_import_dynamic libc_pthread_create            pthread_create            "libc.so.6"
//go:cgo_import_dynamic libc_pthread_sigmask           pthread_sigmask           "libc.so.6"
//go:cgo_import_dynamic libc_sigfillset                sigfillset                "libc.so.6"
//go:cgo_import_dynamic libc_nanosleep                 nanosleep                 "libc.so.6"
//go:cgo_import_dynamic libc_pthread_mutex_lock        pthread_mutex_lock        "libc.so.6"
//go:cgo_import_dynamic libc_pthread_mutex_unlock      pthread_mutex_unlock      "libc.so.6"
//go:cgo_import_dynamic libc_pthread_cond_broadcast    pthread_cond_broadcast    "libc.so.6"
//go:cgo_import_dynamic libc_malloc                    malloc                    "libc.so.6"
//go:cgo_import_dynamic libc_free                      free                      "libc.so.6"
//go:cgo_import_dynamic libc_setenv                    setenv                    "libc.so.6"
//go:cgo_import_dynamic libc_unsetenv                  unsetenv                  "libc.so.6"
//go:cgo_import_dynamic libc_write                     write                     "libc.so.6"
//go:cgo_import_dynamic libc_abort                     abort                     "libc.so.6"
//go:cgo_import_dynamic libc_getpid getpid "libc.so.6"
//go:cgo_import_dynamic libc_getenv getenv "libc.so.6"

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
