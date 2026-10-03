//go:build linux && (amd64 || arm64)

package fakecgo

import (
	_ "unsafe" // for //go:linkname and //go:cgo_import_dynamic
)

// The libc surface this package's assembly calls directly. Two things
// differ from the darwin file next to it:
//
//   - The library is glibc's libc.so.6. Every pthread entry point used
//     here lives there from glibc 2.34 on (the release that folded
//     libpthread.so.0 into libc); Ubuntu 22.04, Debian 12 and RHEL 9
//     are all past that line. On an older glibc the dynamic linker
//     fails the process at exec with "undefined symbol: pthread_create",
//     which is the honest failure for a host that cannot work there.
//   - The stack bounds come from pthread_getattr_np +
//     pthread_attr_getstack (the __GLIBC__ branch of
//     runtime/cgo/pthread_unix.c:92-97), which reports the LOW address
//     directly, instead of darwin's high-address-minus-size pair.
//
// Every function here runs on a system stack with the C ABI and must
// never be routed through a path that needs a g.
//
// The import names carry no glibc symbol version (no `name#GLIBC_2.17`
// form). cmd/link accepts one — go.go:143-144 splits `remote` on "#"
// into Dynimpvers — but an unversioned undefined symbol binds to the
// library's default version at load time, which is what every one of
// these has. The fakecgo self-test running inside the container is the
// proof; a symbol that ever gains a non-default default would fail
// there at exec, not silently.

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

// libc_getpid_trampoline_addr and libc_getenv_trampoline_addr are
// defined by the GLOBL/DATA pairs in libc_linux_arm64.s and
// libc_linux_amd64.s. The address of a cgo_import_dynamic symbol must
// originate in assembly: a Go `var x byte` plus //go:linkname
// materializes a Go data symbol instead and yields a bogus address
// (phase 0b finding, unchanged on Linux).
var libc_getpid_trampoline_addr uintptr
var libc_getenv_trampoline_addr uintptr

// LibcGetpidAddr returns the address of libc getpid. Test-support
// surface: callers need a C function to exercise internal/ffi.Call
// without linking a toolkit.
func LibcGetpidAddr() uintptr { return libc_getpid_trampoline_addr }

// LibcGetenvAddr returns the address of libc getenv, for asserting that
// os.Setenv reached the C environment through x_cgo_setenv.
func LibcGetenvAddr() uintptr { return libc_getenv_trampoline_addr }

// The _cgo_* data cells the runtime linknames are defined as RODATA
// pointer words in fakecgo_linux_arm64.s and fakecgo_linux_amd64.s
// (GLOBL/DATA pairs), one per C function the assembly provides. The
// runtime's declarations (runtime/cgo.go:26-39, no initializer, allowed
// to stay nil) bind to those cells by symbol name, exactly the way
// runtime/cgo's callbacks_unix.go binds them to the C-compiled functions
// in a real cgo build. Runtime consumers, for the record:
//
//	_cgo_init                     runtime/asm_arm64.s:123-138 and asm_amd64.s:189-211 (rt0_go, before any Go code)
//	_cgo_thread_start             runtime/proc.go:2925 (newm1, when iscgo: EVERY M)
//	_cgo_sys_thread_create        runtime/cgo.go:29 (c-archive/c-shared boot; never called in an executable)
//	_cgo_notify_runtime_init_done runtime/proc.go:244-257 (runtime.main, when iscgo; throws if nil)
//	_cgo_setenv                   runtime/env_posix.go (setenv_c, via asmcgocall)
//	_cgo_unsetenv                 runtime/env_posix.go (unsetenv_c, via asmcgocall)
//	_cgo_pthread_key_created      runtime/asm_arm64.s:981-986 and asm_amd64.s:1169-1175 (·cgocallback dropm decision), runtime/proc.go:229
//	_cgo_getstackbound            runtime/cgocall.go:283-306 (callbackUpdateSystemStack, needm path)
//
// The three Linux-only runtime hooks are deliberately left nil, and the
// runtime nil-checks each one before use rather than throwing:
//
//	_cgo_mmap / _cgo_munmap  runtime/cgo_mmap.go:32 and :51 fall back to
//	                         sysMmap/sysMunmap. They exist only so a
//	                         sanitizer's libc interceptors see the
//	                         runtime's mappings; there is no sanitizer here.
//	_cgo_sigaction           runtime/cgo_sigaction.go:36 falls back to
//	                         sysSigaction. Same reason, plus a linux/386
//	                         SA_RESTORER fixup that applies to neither
//	                         arch here.
//
// _cgo_yield, _cgo_callers, _cgo_bindm and the traceback trio stay nil
// too. _cgo_bindm (runtime/proc.go:2717) is what a real cgo build uses
// to keep one M bound to a C thread across calls via a pthread key;
// without it every foreign-thread callback does a full needm/dropm
// pair, which is correct, just slower.

// runtime_iscgo is runtime.iscgo, pushed for outside linkname access
// (runtime/cgo.go:41-52 names purego as a tolerated consumer). It is
// read-only from Go: x_cgo_init sets it in assembly at rt0_go time,
// before schedinit, because mcommoninit only allocates an M's
// cgoCallers array when iscgo is already true (proc.go:1046) and the
// first cgocall on an M without one nil-faults. This declaration stays
// so tests can read the flag back.
//
// On Linux the flag does one more thing than it does on darwin, and it
// is the reason the binary has to come out dynamically linked.
//
// On arm64, runtime.save_g and runtime.load_g branch on iscgo
// (runtime/tls_arm64.s:15 and :36) and start keeping g in thread-local
// storage at TPIDR_EL0 + runtime.tls_g instead of returning
// immediately. On amd64 the effect is at startup instead: rt0_go jumps
// past its whole settls block when _cgo_init is non-nil
// (runtime/asm_amd64.s:212-256), so the process keeps glibc's FS-based
// TLS rather than installing Go's own with arch_prctl. Either way g now
// lives in a thread-local slot the loader has to have reserved.
//
// The internal linker emits the PT_TLS program header only alongside
// PT_DYNAMIC (cmd/link/internal/ld/elf.go:2070-2083), and without PT_TLS
// there is no static TLS block for that slot to live in. The
// cgo_import_dynamic directives above are what make the binary dynamic
// (cmd/link/internal/ld/go.go:105-160 sets havedynamic and records the
// DT_NEEDED), so the two requirements are satisfied by one mechanism —
// no -linkmode flag, no host toolchain. `readelf -l` on the test binary
// shows INTERP, DYNAMIC and TLS together, which is the check to repeat
// if a Go release ever changes this.
//
//go:linkname runtime_iscgo runtime.iscgo
var runtime_iscgo bool

// noopCrosscall2 is the func value x_cgo_init installs into
// runtime.set_crosscall2 (runtime/cgo.go:54-64, pushed). The runtime
// calls it from runtime.main's iscgo branch (proc.go:249-252), which
// runs BEFORE any package init (proc.go:207's doInit covers only the
// runtime's own tasks; the program's package inits run from line 260 on)
// — so an assignment from this package's init would be too late and
// x_cgo_init does it in assembly instead. The real function wires C's
// x_crosscall2_ptr so a pthread-key destructor can dropm on an exiting
// C thread; that path needs _cgo_pthread_key_created to be non-zero,
// which it never is here, so the value only has to be non-nil.
var noopCrosscall2 = func() {}

// setgGCC is the cell x_cgo_init fills with the runtime's setg_gcc and
// threadentry later loads; the Go declaration gives the assembly symbol
// a home in BSS. The same-package name match (·setgGCC) is what binds
// them.
var setgGCC uintptr

// xCGOPthreadKeyCreated is the word _cgo_pthread_key_created points at
// (runtime/cgo/gcc_libinit_unix.c:29 keeps a C uintptr_t; this is the Go
// equivalent). It stays zero: we never call pthread_key_create, so
// ·cgocallback's read of it (asm_arm64.s:981-986, asm_amd64.s:1169-1175)
// always takes the "dropm" branch, which is what a callback on a foreign
// thread needs when no key exists to hold the binding.
var xCGOPthreadKeyCreated uintptr

// SetgStored reports the setg_gcc pointer x_cgo_init received from
// rt0_go. Non-zero proves rt0_go found our _cgo_init cell and called the
// trampoline before any Go code ran; the self-test asserts it.
func SetgStored() uintptr { return setgGCC }
