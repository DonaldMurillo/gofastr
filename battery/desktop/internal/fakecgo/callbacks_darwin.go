//go:build darwin && (arm64 || amd64)

package fakecgo

import (
	_ "unsafe" // for //go:linkname and //go:cgo_import_dynamic
)

// The libc surface this package's assembly calls directly (the same
// import shape golang.org/x/sys and the runtime's own darwin trampolines
// use; cgo_import_dynamic is legal in ordinary Go files with
// CGO_ENABLED=0). The functions run on system stacks with the C ABI and
// must never be routed through syscall9/libcCall, which needs a g.

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

// libc_getpid_trampoline_addr and libc_getenv_trampoline_addr are
// defined by the GLOBL/DATA pairs in libc_darwin_<arch>.s.
var libc_getpid_trampoline_addr uintptr
var libc_getenv_trampoline_addr uintptr

// LibcGetpidAddr returns the address of libc getpid. Test-support
// surface: callers need a C function to exercise internal/ffi.Call
// without linking a framework.
func LibcGetpidAddr() uintptr { return libc_getpid_trampoline_addr }

// LibcGetenvAddr returns the address of libc getenv, for asserting
// that os.Setenv reached the C environment through x_cgo_setenv.
func LibcGetenvAddr() uintptr { return libc_getenv_trampoline_addr }

// The _cgo_* data cells the runtime linknames are defined as RODATA
// pointer words in fakecgo_darwin_<arch>.s (GLOBL/DATA pairs), one per
// C function the assembly provides. The runtime's declarations
// (runtime/cgo.go:13-39, no initializer, allowed to stay nil) bind to
// those cells by symbol name, exactly the way runtime/cgo's
// callbacks_unix.go binds them to the C-compiled functions in a real
// cgo build. Runtime consumers, for the record:
//
//	_cgo_init                     runtime/asm_arm64.s:124, asm_amd64.s:190 (rt0_go, before any Go code)
//	_cgo_thread_start             runtime/proc.go:2925 (newm1, when iscgo)
//	_cgo_sys_thread_create        runtime/cgo.go:29 (c-archive/c-shared boot; never called in an executable)
//	_cgo_notify_runtime_init_done runtime/proc.go:257 (schedinit, only when iscgo was already true there)
//	_cgo_setenv                   runtime/env_posix.go:73 (setenv_c, via asmcgocall)
//	_cgo_unsetenv                 runtime/env_posix.go:82 (unsetenv_c, via asmcgocall)
//	_cgo_pthread_key_created      runtime/asm_arm64.s:982 (·cgocallback dropm decision; asm_amd64.s:1014 is the amd64 twin), runtime/proc.go:229

// _cgo_yield, _cgo_callers, _cgo_getstackbound, and the traceback trio
// stay nil: the runtime nil-checks each before use.
//
// _cgo_getstackbound (runtime/cgocall.go:284) and _cgo_bindm
// (runtime/proc.go:2717 needAndBindM) are consulted on the needm path —
// the path a callback takes when it arrives on a thread Go did NOT
// create — and this host DOES reach it. ffi.NewCallbackAnyThread exists
// for exactly that shape: every UNUserNotificationCenter completion
// handler is delivered on one of the centre's own background queues,
// which is how the first bundled build died with "ffi callback called
// on thread …, want main thread" the moment a note was saved.
// internal/ffi's bgcallback_darwin_test.go drives the same path through
// a global dispatch queue on purpose.
//
// They stay nil anyway, and that is a deliberate posture rather than an
// accident of reach: the runtime nil-checks both before use, so needm
// falls back to its own stack-bound probe and binds no M to a C thread.
// A callback on such a thread must therefore do nothing but signal a
// channel — no AppKit call without a Main hop — which is the contract
// NewCallbackAnyThread's doc comment states.

// runtime_iscgo is runtime.iscgo, pushed for outside linkname access
// (runtime/cgo.go:41-52 names purego as a tolerated consumer). It is
// read-only from Go: x_cgo_init sets it in assembly at rt0_go time,
// before schedinit, because mcommoninit only allocates an M's
// cgoCallers array when iscgo is already true (proc.go:1046) and the
// first cgocall on an M without one nil-faults. This declaration stays
// so tests can read the flag back.
//
//go:linkname runtime_iscgo runtime.iscgo
var runtime_iscgo bool

// noopCrosscall2 is the func value x_cgo_init installs into
// runtime.set_crosscall2 (runtime/cgo.go:54-64, pushed). The runtime
// calls it from runtime.main's iscgo branch (proc.go:252), which runs
// BEFORE any package init (proc.go:207's doInit covers only the
// runtime's own tasks; the program's package inits run from line 260
// on) — so an assignment from this package's init would be too late
// and x_cgo_init does it in assembly instead, the same way a real cgo
// build gets it from runtime/cgo being a linker-injected dependency of
// runtime. The real function wires C's _crosscall2_ptr for dropm on
// exiting C threads; that path never runs without pthread keys, so the
// value only has to be non-nil.
var noopCrosscall2 = func() {}

// setgGCC is the cell x_cgo_init fills with the runtime's setg_gcc and
// threadentry later loads; the Go declaration gives the assembly symbol
// a home in BSS. The same-package name match (·setgGCC) is what binds
// them, the pattern internal/ffi uses for callbackasmAddr.
var setgGCC uintptr

// xCGOPthreadKeyCreated is the word _cgo_pthread_key_created points at
// (callbacks.go:56-59 keeps a C uintptr_t; this is the Go equivalent).
// It stays zero: we never call pthread_key_create, so ·cgocallback's
// read of it (asm_arm64.s:982-986, asm_amd64.s:1014-1018) always takes
// the "no key" branch — dropm on return rather than keeping the M bound
// to the C thread. That branch IS reached: a callback delivered on a
// thread Go did not create (ffi.NewCallbackAnyThread; every
// UNUserNotificationCenter completion) takes the needm path, and taking
// the "no key" branch there is what keeps this host from leaking an M
// per notification callback.
var xCGOPthreadKeyCreated uintptr

// SetgStored reports the setg_gcc pointer x_cgo_init received from
// rt0_go. Non-zero proves rt0_go found our _cgo_init cell and called
// the trampoline before any Go code ran; the self-test asserts it.
func SetgStored() uintptr { return setgGCC }
