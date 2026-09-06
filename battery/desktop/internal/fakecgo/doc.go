// Package fakecgo provides the runtime-support symbols the Go runtime
// demands once runtime.iscgo is true, without cgo: a darwin port of
// runtime/cgo's C glue (gcc_unix.c, gcc_libinit_unix.c, pthread_unix.c,
// gcc_setenv.c, and gcc_arm64.S or gcc_amd64.S) to Go assembly plus
// cgo_import_dynamic libc references, one assembly file per
// architecture.
//
// Why it exists: the desktop host must call C and receive C callbacks
// from a CGO_ENABLED=0 binary. runtime.cgocall is the only call path
// that survives a never-returning C call such as [NSApp run] with the
// garbage collector on — the libcCall path behind syscall.syscall9
// records m.libcallsp for the duration of the C call, and a GC-driven
// stack shrink of that parked goroutine then throws "shrinking stack in
// libcall" (runtime/stack.go:1305) before any GODEBUG escape is
// consulted. runtime.cgocall does entersyscall/asmcgocall/exitsyscall
// and never touches m.libcall*, but it throws "cgocall unavailable"
// unless runtime.iscgo is true, and iscgo obliges the process to
// provide the _cgo_* entry points the runtime linknames
// (runtime/cgo.go:13-24). That is this package.
//
// The port is from the Go 1.27 sources in the module cache, not from
// ebitengine/purego's internal/fakecgo; any divergence is noted against
// the C file it came from.
//
// Importing this package has three runtime-visible effects:
//
//   - rt0_go finds _cgo_init non-nil and calls it before any Go code
//     runs, so g0's stack bounds on the main thread become the real
//     pthread stack bounds instead of the 64 KiB startup estimate.
//   - Every M created after this package's init runs (iscgo is set
//     there) is created through _cgo_thread_start: a detached pthread
//     with the parent's stack size, signals blocked during creation,
//     and crosscall1/setg/mstart as its entry path.
//   - os.Setenv/os.Unsetenv forward to libc setenv/unsetenv through
//     _cgo_setenv/_cgo_unsetenv, keeping the C environ in sync.
//
// The package is darwin/arm64, darwin/amd64, linux/arm64, and linux/amd64; on every other
// GOOS/GOARCH it reduces to this doc comment. The Go side is identical
// on both — only the assembly differs, and only in the way the two
// calling conventions differ (arguments in x0-x7 against rdi, rsi, rdx,
// cx, r8, r9; symbol references through REGTMP against direct memory
// operands; a frame-size rule that keeps the stack 16-byte aligned at
// each call on both). The Linux fake-cgo layer planned in
// docs/desktop-plan.md is a separate port of the same C sources.
package fakecgo
