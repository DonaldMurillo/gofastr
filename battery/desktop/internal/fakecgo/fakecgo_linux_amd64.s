//go:build linux && amd64

#include "textflag.h"

// The fake-cgo layer for linux/amd64: the same port as
// fakecgo_linux_arm64.s, on the System V AMD64 ABI. Read that file for
// the runtime contract and the C sources; this one only records what the
// second ABI changes.
//
//	arguments        DI, SI, DX, CX, R8, R9 (integers), X0-X7 (floats)
//	return           AX (second word DX), X0
//	callee-saved     BX, BP, R12, R13, R14, R15 — including R14, which
//	                 the Go ABI uses as the g register, so a Go call from
//	                 inside a C-ABI function here must not leak it back
//	symbol addresses  LEAQ sym(SB), reg — no REGTMP to preserve, which is
//	                 why the arm64 file's STP (R27, g) prologue has no
//	                 counterpart here
//	PLT scratch      R10 and R11 on ELF; nothing here holds a live value
//	                 in either across a call
//
// Stack alignment is the one thing this ABI is strict about and arm64 is
// not: RSP must be 16-byte aligned at every CALL into C, so RSP is
// 8 mod 16 at a C function's entry. The Go assembler's amd64 prologue
// subtracts framesize+8 (the extra 8 saves BP,
// cmd/internal/obj/x86/obj6.go:637-648), so a declared frame that is 0
// or a multiple of 16 leaves RSP correctly aligned at our own CALLs, and
// anything else would hand glibc a misaligned stack and fault in the
// first SSE store. Every frame below is 0 or a multiple of 16, and the
// local layout is spelled out per function because 0(SP) is the saved BP
// when the frame is 0.

// ─── data ────────────────────────────────────────────────────────────

GLOBL _cgo_init(SB), RODATA, $8
DATA _cgo_init(SB)/8, $x_cgo_init(SB)

GLOBL _cgo_thread_start(SB), RODATA, $8
DATA _cgo_thread_start(SB)/8, $x_cgo_thread_start(SB)

GLOBL _cgo_sys_thread_create(SB), RODATA, $8
DATA _cgo_sys_thread_create(SB)/8, $x_cgo_sys_thread_create(SB)

GLOBL _cgo_notify_runtime_init_done(SB), RODATA, $8
DATA _cgo_notify_runtime_init_done(SB)/8, $x_cgo_notify_runtime_init_done(SB)

GLOBL _cgo_getstackbound(SB), RODATA, $8
DATA _cgo_getstackbound(SB)/8, $x_cgo_getstackbound(SB)

GLOBL runtime·_cgo_setenv(SB), RODATA, $8
DATA runtime·_cgo_setenv(SB)/8, $x_cgo_setenv(SB)

GLOBL runtime·_cgo_unsetenv(SB), RODATA, $8
DATA runtime·_cgo_unsetenv(SB)/8, $x_cgo_unsetenv(SB)

GLOBL _cgo_pthread_key_created(SB), RODATA, $8
DATA _cgo_pthread_key_created(SB)/8, $·xCGOPthreadKeyCreated(SB)

// glibc's PTHREAD_MUTEX_INITIALIZER and PTHREAD_COND_INITIALIZER are
// all-zero structs. The reserved sizes are the larger of the two arches'
// __SIZEOF_PTHREAD_* values, which costs a few bytes of BSS and cannot
// under-allocate.
GLOBL runtime_init_mu(SB), NOPTR, $48
GLOBL runtime_init_cond(SB), NOPTR, $48
GLOBL runtime_init_done(SB), NOPTR, $8

GLOBL oomMsg(SB), RODATA, $48 // 43 bytes used
DATA oomMsg+0(SB)/8, $"runtime/"
DATA oomMsg+8(SB)/8, $"cgo: out"
DATA oomMsg+16(SB)/8, $" of memo"
DATA oomMsg+24(SB)/8, $"ry in th"
DATA oomMsg+32(SB)/8, $"read_sta"
DATA oomMsg+40(SB)/8, $"rt\n"

GLOBL boundsMsg(SB), RODATA, $32 // 30 bytes used
DATA boundsMsg+0(SB)/8, $"runtime/"
DATA boundsMsg+8(SB)/8, $"cgo: bad"
DATA boundsMsg+16(SB)/8, $" stack b"
DATA boundsMsg+24(SB)/8, $"ounds\n"

GLOBL createMsg(SB), RODATA, $40 // 35 bytes used
DATA createMsg+0(SB)/8, $"runtime/"
DATA createMsg+8(SB)/8, $"cgo: pth"
DATA createMsg+16(SB)/8, $"read_cre"
DATA createMsg+24(SB)/8, $"ate fail"
DATA createMsg+32(SB)/8, $"ed\n"

// ─── x_cgo_init ──────────────────────────────────────────────────────

// x_cgo_init(G *g, void (*setg)(void*), void **tlsg, void **tlsbase),
// called from rt0_go (runtime/asm_amd64.s:189-211) before any Go code.
// DI is g0, SI is setg_gcc; DX and CX are zero on linux/amd64
// (asm_amd64.s:195-196, "not used when using platform's TLS"). Note the
// amd64-specific consequence: with a non-nil _cgo_init, rt0_go jumps
// past the settls block entirely (asm_amd64.s:220-256), so the process
// keeps glibc's FS-based TLS instead of Go's own arch_prctl one — which
// is exactly what makes calling into GTK from any M legal.
//
// Frame $160, locals 0..159:
//	  0       g0
//	  8       stack low address
//	 16       stack size
//	 24..151  pthread_attr_t (glibc needs 56 on amd64; 128 reserved)
TEXT x_cgo_init(SB), NOSPLIT, $160-0
	MOVQ	DI, 0(SP)                  // g0
	MOVQ	SI, ·setgGCC+0(SB)         // setg_gcc for threadentry

	// runtime.iscgo before schedinit (mcommoninit allocates m0's
	// cgoCallers only when it is already true, proc.go:1046) and
	// runtime.set_crosscall2 before runtime.main's iscgo branch checks
	// it (proc.go:249-252, ahead of every package init).
	MOVB	$1, runtime·iscgo(SB)
	MOVQ	·noopCrosscall2+0(SB), AX
	MOVQ	AX, runtime·set_crosscall2(SB)

	MOVQ	$0, 8(SP)
	MOVQ	$0, 16(SP)

	LEAQ	24(SP), DI
	CALL	libc_pthread_attr_init(SB)
	CALL	libc_pthread_self(SB)
	MOVQ	AX, DI
	LEAQ	24(SP), SI
	CALL	libc_pthread_getattr_np(SB)
	LEAQ	24(SP), DI
	LEAQ	8(SP), SI                  // &addr (low)
	LEAQ	16(SP), DX                 // &size
	CALL	libc_pthread_attr_getstack(SB)
	LEAQ	24(SP), DI
	CALL	libc_pthread_attr_destroy(SB)

	MOVQ	8(SP), AX                  // stack low address
	TESTQ	AX, AX
	JZ	init_done                  // getattr_np failed; keep rt0_go's estimate

	MOVQ	0(SP), BX                  // g0
	MOVQ	AX, 0(BX)                  // g0->stacklo
	MOVQ	8(BX), CX                  // stackhi (entry SP, set by rt0_go)
	CMPQ	CX, AX
	JHI	init_done                  // stackhi > stacklo: bounds sane
	LEAQ	boundsMsg(SB), SI
	MOVQ	$30, DX
	CALL	abort2<>(SB)               // never returns

init_done:
	RET

// ─── x_cgo_getstackbound ─────────────────────────────────────────────

// void x_cgo_getstackbound(uintptr bounds[2]) — pthread_unix.c:76-116,
// __GLIBC__ branch. Frame $160, same layout as x_cgo_init with bounds in
// slot 0.
TEXT x_cgo_getstackbound(SB), NOSPLIT, $160-0
	MOVQ	DI, 0(SP)
	MOVQ	$0, 8(SP)
	MOVQ	$0, 16(SP)

	LEAQ	24(SP), DI
	CALL	libc_pthread_attr_init(SB)
	CALL	libc_pthread_self(SB)
	MOVQ	AX, DI
	LEAQ	24(SP), SI
	CALL	libc_pthread_getattr_np(SB)
	LEAQ	24(SP), DI
	LEAQ	8(SP), SI
	LEAQ	16(SP), DX
	CALL	libc_pthread_attr_getstack(SB)
	LEAQ	24(SP), DI
	CALL	libc_pthread_attr_destroy(SB)

	MOVQ	0(SP), BX                  // bounds
	MOVQ	8(SP), AX                  // addr (low)
	MOVQ	16(SP), CX                 // size
	MOVQ	AX, 0(BX)
	ADDQ	AX, CX
	MOVQ	CX, 8(BX)
	RET

// ─── x_cgo_thread_start ──────────────────────────────────────────────

// gcc_libinit_unix.c:180-196. DI is the runtime's ThreadStart
// {G *g; uintptr *tls; func() fn}. With iscgo true every M in the
// process arrives here (runtime/proc.go:2925).
//
// Frame $16, locals 0..15:
//	  0  ts
TEXT x_cgo_thread_start(SB), NOSPLIT, $16-0
	MOVQ	DI, 0(SP)

	MOVQ	$24, DI                    // sizeof(ThreadStart)
	CALL	libc_malloc(SB)
	TESTQ	AX, AX
	JZ	ts_oom

	MOVQ	0(SP), SI
	MOVQ	0(SI), CX
	MOVQ	CX, 0(AX)                  // g
	MOVQ	8(SI), CX
	MOVQ	CX, 8(AX)                  // tls
	MOVQ	16(SI), CX
	MOVQ	CX, 16(AX)                 // fn

	MOVQ	AX, DI
	CALL	x_cgo_sys_thread_start(SB)
	RET

ts_oom:
	LEAQ	oomMsg(SB), SI
	MOVQ	$43, DX
	CALL	abort2<>(SB)

// ─── x_cgo_sys_thread_start ──────────────────────────────────────────

// pthread_unix.c:18-60, non-__APPLE__ branch, with
// _cgo_try_pthread_create (lines 118-138) inlined.
//
// Frame $432, locals 0..431:
//	  0        ts
//	  8        pthread_t p
//	 16        size
//	 24        tries (int)
//	 32..47    struct timespec {tv_sec@32, tv_nsec@40}
//	 48..175   pthread_attr_t
//	176..303   sigset_t ign   (128 bytes on Linux)
//	304..431   sigset_t oset
TEXT x_cgo_sys_thread_start(SB), NOSPLIT, $432-0
	MOVQ	DI, 0(SP)                  // ts

	// Block every signal around the create; the new thread inherits the
	// full mask and unblocks in minit. SIG_SETMASK is 2 on Linux.
	LEAQ	176(SP), DI
	CALL	libc_sigfillset(SB)
	MOVQ	$2, DI
	LEAQ	176(SP), SI
	LEAQ	304(SP), DX
	CALL	libc_pthread_sigmask(SB)

	LEAQ	48(SP), DI
	CALL	libc_pthread_attr_init(SB)
	LEAQ	48(SP), DI
	MOVQ	$1, SI                     // PTHREAD_CREATE_DETACHED
	CALL	libc_pthread_attr_setdetachstate(SB)

	// Non-darwin: the default stack size off a fresh attr, not the
	// parent thread's.
	MOVQ	$0, 16(SP)
	LEAQ	48(SP), DI
	LEAQ	16(SP), SI
	CALL	libc_pthread_attr_getstacksize(SB)

	// Leave stacklo=0 and set stackhi=size; mstart does the rest.
	MOVQ	0(SP), AX                  // ts
	MOVQ	0(AX), BX                  // ts->g
	MOVQ	16(SP), CX                 // size
	MOVQ	CX, 8(BX)                  // g->stackhi = size

	MOVL	$0, 24(SP)                 // tries

create_loop:
	LEAQ	8(SP), DI                  // &p
	LEAQ	48(SP), SI                 // &attr
	LEAQ	threadentry(SB), DX
	MOVQ	0(SP), CX                  // ts
	CALL	libc_pthread_create(SB)
	TESTL	AX, AX
	JZ	create_ok
	CMPL	AX, $11                    // EAGAIN on Linux
	JNE	create_fail

	// _cgo_try_pthread_create: sleep (tries+1) ms and retry, 20 tries.
	MOVL	24(SP), CX
	CMPL	CX, $20
	JGE	create_fail
	INCL	CX
	MOVL	CX, 24(SP)
	MOVQ	$0, 32(SP)                 // tv_sec = 0
	MOVL	CX, AX
	IMULL	$1000000, AX               // (tries+1) ms in nanoseconds
	MOVQ	AX, 40(SP)                 // tv_nsec is a full long; the 32-bit
	                                   // IMULL already zeroed AX's top half
	LEAQ	32(SP), DI
	MOVQ	$0, SI
	CALL	libc_nanosleep(SB)
	JMP	create_loop

create_ok:
	MOVQ	$2, DI                     // SIG_SETMASK
	LEAQ	304(SP), SI
	MOVQ	$0, DX
	CALL	libc_pthread_sigmask(SB)
	RET

create_fail:
	LEAQ	createMsg(SB), SI
	MOVQ	$35, DX
	CALL	abort2<>(SB)

// ─── x_cgo_sys_thread_create ─────────────────────────────────────────

// pthread_unix.c:62-74. Only reached from C boot paths
// (c-archive/c-shared), never in an executable.
//
// Frame $176, locals 0..175:
//	  0        func
//	  8        pthread_t p
//	 24        tries (int)
//	 32..47    struct timespec
//	 48..175   pthread_attr_t
TEXT x_cgo_sys_thread_create(SB), NOSPLIT, $176-0
	MOVQ	DI, 0(SP)                  // func

	LEAQ	48(SP), DI
	CALL	libc_pthread_attr_init(SB)
	LEAQ	48(SP), DI
	MOVQ	$1, SI                     // PTHREAD_CREATE_DETACHED
	CALL	libc_pthread_attr_setdetachstate(SB)

	MOVL	$0, 24(SP)

sc_loop:
	LEAQ	8(SP), DI
	LEAQ	48(SP), SI
	MOVQ	0(SP), DX                  // func
	MOVQ	$0, CX                     // arg
	CALL	libc_pthread_create(SB)
	TESTL	AX, AX
	JZ	sc_ok
	CMPL	AX, $11
	JNE	sc_fail
	MOVL	24(SP), CX
	CMPL	CX, $20
	JGE	sc_fail
	INCL	CX
	MOVL	CX, 24(SP)
	MOVQ	$0, 32(SP)
	MOVL	CX, AX
	IMULL	$1000000, AX
	MOVQ	AX, 40(SP)
	LEAQ	32(SP), DI
	MOVQ	$0, SI
	CALL	libc_nanosleep(SB)
	JMP	sc_loop

sc_ok:
	RET

sc_fail:
	LEAQ	createMsg(SB), SI
	MOVQ	$35, DX
	CALL	abort2<>(SB)

// ─── threadentry ─────────────────────────────────────────────────────

// gcc_unix.c:47-63. Runs on the brand-new pthread with no g at all.
//
// Frame $32, locals 0..31:
//	  0  v (the malloc'd ThreadStart)
//	  8  ts.g
//	 16  ts.fn
TEXT threadentry(SB), NOSPLIT, $32-0
	MOVQ	DI, 0(SP)
	MOVQ	0(DI), AX                  // ts.g
	MOVQ	AX, 8(SP)
	MOVQ	16(DI), AX                 // ts.fn
	MOVQ	AX, 16(SP)

	MOVQ	0(SP), DI
	CALL	libc_free(SB)

	MOVQ	16(SP), DI                 // fn
	MOVQ	·setgGCC+0(SB), SI         // setg_gcc
	MOVQ	8(SP), DX                  // g
	CALL	crosscall1(SB)

	MOVQ	$0, AX                     // NULL, per the pthread contract
	RET

// ─── crosscall1 ──────────────────────────────────────────────────────

// Port of gcc_amd64.S crosscall1: bridge from the C ABI into the gc tool
// chain. DI=fn, SI=setg_gcc, DX=g. Saves the six C-callee-saved
// registers; the Go ABI treats everything as caller-save, so nothing
// else needs preserving.
//
// The C original reaches its two calls with RSP 8 mod 16, which is fine
// there because both callees are Go functions and Go does not require
// the C alignment. This version reserves 56 bytes instead of pushing 48,
// which stores the same six registers AND leaves RSP 0 mod 16 — free,
// and it means a future setg_gcc that touches SSE cannot be surprised.
//	 0 BX   8 BP   16 R12   24 R13   32 R14   40 R15
TEXT crosscall1(SB), NOSPLIT|NOFRAME, $0-0
	SUBQ	$56, SP
	MOVQ	BX, 0(SP)
	MOVQ	BP, 8(SP)
	MOVQ	R12, 16(SP)
	MOVQ	R13, 24(SP)
	MOVQ	R14, 32(SP)
	MOVQ	R15, 40(SP)

	MOVQ	DI, BX                     // fn
	MOVQ	DX, DI                     // arg of setg_gcc: g
	CALL	SI                         // setg(g): writes the TLS slot and R14
	CALL	BX                         // fn() = mstart; never returns

	MOVQ	0(SP), BX
	MOVQ	8(SP), BP
	MOVQ	16(SP), R12
	MOVQ	24(SP), R13
	MOVQ	32(SP), R14
	MOVQ	40(SP), R15
	ADDQ	$56, SP
	RET

// ─── x_cgo_notify_runtime_init_done ──────────────────────────────────

// gcc_libinit_unix.c:96-102. runtime.main calls this through cgocall
// (proc.go:257) and throws at proc.go:244-246 if the cell is nil.
// Frame $0: no locals, and the assembler's 8-byte BP save leaves RSP
// correctly aligned for the three C calls.
TEXT x_cgo_notify_runtime_init_done(SB), NOSPLIT, $0-0
	LEAQ	runtime_init_mu(SB), DI
	CALL	libc_pthread_mutex_lock(SB)
	MOVQ	$1, AX
	MOVQ	AX, runtime_init_done(SB)
	LEAQ	runtime_init_cond(SB), DI
	CALL	libc_pthread_cond_broadcast(SB)
	LEAQ	runtime_init_mu(SB), DI
	CALL	libc_pthread_mutex_unlock(SB)
	RET

// ─── x_cgo_setenv / x_cgo_unsetenv ──────────────────────────────────

// gcc_setenv.c. DI points at the runtime's argument array: [k, v] for
// setenv, [k] for unsetenv (runtime/env_posix.go, via asmcgocall). This
// is what keeps libc's environ in step with os.Setenv, which GDK reads
// for DISPLAY, WAYLAND_DISPLAY and GDK_BACKEND.
TEXT x_cgo_setenv(SB), NOSPLIT, $0-0
	MOVQ	8(DI), SI                  // v, read before DI is overwritten
	MOVQ	0(DI), DI                  // k
	MOVQ	$1, DX                     // overwrite
	CALL	libc_setenv(SB)
	RET

TEXT x_cgo_unsetenv(SB), NOSPLIT, $0-0
	MOVQ	0(DI), DI                  // k
	CALL	libc_unsetenv(SB)
	RET

// ─── helpers ─────────────────────────────────────────────────────────

// abort2<>(SB) writes msg (SI, length DX) to stderr and aborts. The
// shared tail of every fatal path; never returns, so it has no RET and
// no back-edge into the nosplit call graph.
TEXT abort2<>(SB), NOSPLIT, $0-0
	MOVQ	$2, DI                     // stderr
	CALL	libc_write(SB)
	CALL	libc_abort(SB)
