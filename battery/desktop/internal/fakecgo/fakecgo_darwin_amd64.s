//go:build darwin && amd64

#include "textflag.h"

// The fake-cgo layer for darwin/amd64, the twin of
// fakecgo_darwin_arm64.s and a port of the same runtime/cgo C:
//   x_cgo_init, threadentry        <- gcc_unix.c
//   x_cgo_thread_start             <- gcc_libinit_unix.c
//   x_cgo_notify_runtime_init_done <- gcc_libinit_unix.c
//   x_cgo_sys_thread_start         <- pthread_unix.c (the __APPLE__ branch)
//   x_cgo_sys_thread_create        <- pthread_unix.c
//   x_cgo_setenv/x_cgo_unsetenv    <- gcc_setenv.c
//   crosscall1                     <- gcc_amd64.S
// Every function here runs with the System V AMD64 C ABI on a system
// stack (g0, or a brand-new pthread with no g at all in threadentry):
// no Go code, no allocation, no stack growth, no g accesses. C symbol
// names carry no package prefix (TEXT x(SB), not TEXT ·x(SB)), which is
// how the runtime's linknamed _cgo_* variables and rt0_go find them.
//
// Register discipline, System V AMD64: arguments arrive in DI, SI, DX,
// CX, R8, R9; AX, CX, DX, SI, DI, R8-R11 are caller-saved and free for
// us; BX, BP, R12-R15 are callee-saved and must reach a C caller
// untouched, so nothing here writes them outside crosscall1's explicit
// save/restore. Unlike arm64 there is no REGTMP to preserve: the
// assembler encodes symbol references directly, so MOVB $1,
// runtime·iscgo(SB) needs no scratch register.
//
// Stack alignment: the ABI wants RSP 16-byte aligned at every CALL
// (System V "the value (%rsp + 8) is always a multiple of 16 when
// control is transferred to the function entry point"). A function
// entered from C has RSP ≡ 8 (mod 16); the assembler's prologue for a
// TEXT with a frame and a CALL in it is PUSHQ BP / MOVQ SP, BP / SUBQ
// $framesize, SP (cmd/internal/obj/x86/obj6.go:636-651 adds the 8-byte
// base-pointer save whenever the symbol is not NOFRAME and is not a
// frameless leaf), so a declared frame size that is a multiple of 16
// lands RSP back on a 16-byte boundary for the CALLs in the body. Every
// frame below is sized that way.

// ─── data ────────────────────────────────────────────────────────────

// The six pointer cells the runtime linknames. RODATA words, one per
// C-ABI function below; runtime/cgo.go's no-initializer declarations
// resolve against these by name.

GLOBL _cgo_init(SB), RODATA, $8
DATA _cgo_init(SB)/8, $x_cgo_init(SB)

GLOBL _cgo_thread_start(SB), RODATA, $8
DATA _cgo_thread_start(SB)/8, $x_cgo_thread_start(SB)

GLOBL _cgo_sys_thread_create(SB), RODATA, $8
DATA _cgo_sys_thread_create(SB)/8, $x_cgo_sys_thread_create(SB)

GLOBL _cgo_notify_runtime_init_done(SB), RODATA, $8
DATA _cgo_notify_runtime_init_done(SB)/8, $x_cgo_notify_runtime_init_done(SB)

// _cgo_setenv/_cgo_unsetenv differ from the rest: env_posix.go:52-65
// push them with a BARE //go:linkname, so the binding symbol is the
// runtime-qualified runtime._cgo_setenv (see runtime/cgo/setenv.go:13,
// which binds the same way), not an unprefixed external name.
GLOBL runtime·_cgo_setenv(SB), RODATA, $8
DATA runtime·_cgo_setenv(SB)/8, $x_cgo_setenv(SB)

GLOBL runtime·_cgo_unsetenv(SB), RODATA, $8
DATA runtime·_cgo_unsetenv(SB)/8, $x_cgo_unsetenv(SB)

// _cgo_pthread_key_created points at a word that stays zero
// (callbacks_darwin.go xCGOPthreadKeyCreated); the runtime reads it to
// decide whether non-Go threads need dropm on exit, which never
// applies here.
GLOBL _cgo_pthread_key_created(SB), RODATA, $8
DATA _cgo_pthread_key_created(SB)/8, $·xCGOPthreadKeyCreated(SB)

// gcc_libinit_unix.c's static state, with darwin's
// PTHREAD_MUTEX_INITIALIZER / PTHREAD_COND_INITIALIZER laid out by hand
// (pthread_impl.h: _PTHREAD_MUTEX_SIG_init 0x32AAABA7 in the first
// word of a 64-byte mutex, _PTHREAD_COND_SIG_init 0x3CB0B1BB in a
// 48-byte cond; the __opaque tails are zero). The sizes are the LP64
// ones and identical on x86_64 and arm64.

GLOBL runtime_init_mu(SB), NOPTR, $64
DATA runtime_init_mu+0(SB)/8, $0x32AAABA7

GLOBL runtime_init_cond(SB), NOPTR, $48
DATA runtime_init_cond+0(SB)/8, $0x3CB0B1BB

GLOBL runtime_init_done(SB), NOPTR, $8 // int, zero

// Error strings for the two abort paths, C file fidelity (fprintf to
// stderr kept as a write(2) to fd 2).

GLOBL oomMsg(SB), RODATA, $43
DATA oomMsg+0(SB)/8, $"runtime/"
DATA oomMsg+8(SB)/8, $"cgo: out"
DATA oomMsg+16(SB)/8, $" of memo"
DATA oomMsg+24(SB)/8, $"ry in th"
DATA oomMsg+32(SB)/8, $"read_sta"
DATA oomMsg+40(SB)/2, $"rt"
DATA oomMsg+42(SB)/1, $"\n"

// "runtime/cgo: bad stack bounds\n" (gcc_unix.c _cgo_set_stacklo's
// fprintf), 30 bytes.
GLOBL boundsMsg(SB), RODATA, $30
DATA boundsMsg+0(SB)/8, $"runtime/"
DATA boundsMsg+8(SB)/8, $"cgo: bad"
DATA boundsMsg+16(SB)/8, $" stack b"
DATA boundsMsg+24(SB)/6, $"ounds\n"

GLOBL createMsg(SB), RODATA, $36
DATA createMsg+0(SB)/8, $"runtime/"
DATA createMsg+8(SB)/8, $"cgo: pth"
DATA createMsg+16(SB)/8, $"read_cre"
DATA createMsg+24(SB)/8, $"ate fail"
DATA createMsg+32(SB)/4, $"ed\n"

// ─── x_cgo_init ──────────────────────────────────────────────────────

// x_cgo_init is called from rt0_go (runtime/asm_amd64.s:189-211) with
// the C ABI before any Go code runs: DI = g0, SI = setg_gcc, DX and CX
// are the tlsg/tlsbase pair, zero on darwin because the platform's own
// TLS is used. g0->stacklo becomes pthread_get_stackaddr_np(self) -
// pthread_get_stacksize_np(self). (The Go 1.27 C sets stacklo to the
// raw low bound; older releases added a guard page, which purego's port
// preserved. Following the current runtime is the safer bet: rt0_go
// recomputes the guard from stacklo immediately after the call,
// asm_amd64.s:213-218.) rt0_go set g0->stackhi to the entry SP
// beforehand, so the sanity check is stacklo < stackhi.
//
// Frame map (declared $32, so the prologue is PUSHQ BP / SUBQ $32):
//	 0   g0
//	 8   pthread_self()
//	16   stack high address
TEXT x_cgo_init(SB), NOSPLIT, $32-0
	// Two stores the runtime needs before any package init could run
	// (a real cgo binary gets both from the linker wiring runtime/cgo
	// into runtime's own init graph; x_cgo_init is our equivalent hook):
	//
	//   runtime.iscgo = true, before schedinit: mcommoninit(m0) only
	//   allocates m0's cgoCallers array when iscgo is true
	//   (proc.go:1046), and the first cgocall on an M without it
	//   nil-faults.
	//
	//   runtime.set_crosscall2 = noopCrosscall2, because runtime.main's
	//   iscgo branch (proc.go:249-252) checks and calls it BEFORE the
	//   program's package inits run (proc.go:207's doInit covers only
	//   the runtime's own tasks; package inits start at proc.go:260).
	MOVB	$1, runtime·iscgo(SB)
	MOVQ	·noopCrosscall2(SB), AX
	MOVQ	AX, runtime·set_crosscall2(SB)
	MOVQ	SI, ·setgGCC(SB)	// setg_gcc for threadentry
	MOVQ	DI, 0(SP)		// g0

	CALL	libc_pthread_self(SB)
	MOVQ	AX, 8(SP)		// self
	MOVQ	AX, DI
	CALL	libc_pthread_get_stackaddr_np(SB)
	MOVQ	AX, 16(SP)		// stack high address
	MOVQ	8(SP), DI
	CALL	libc_pthread_get_stacksize_np(SB)
	MOVQ	16(SP), CX
	SUBQ	AX, CX			// addr - size
	MOVQ	0(SP), DX		// g0
	MOVQ	CX, 0(DX)		// g0->stacklo (G layout: stacklo, stackhi)

	// Sanity check the bounds now rather than crashing in a morestack
	// on g0 later (gcc_unix.c _cgo_set_stacklo).
	MOVQ	8(DX), R8		// stackhi (entry SP)
	CMPQ	R8, CX
	JA	boundsok
	MOVQ	$boundsMsg(SB), SI
	MOVQ	$30, DX
	CALL	abort2<>(SB)

boundsok:
	RET

// ─── x_cgo_thread_start ──────────────────────────────────────────────

// x_cgo_thread_start is called from runtime.newm1 via asmcgocall with
// a pointer to the runtime's ThreadStart {G *g; uintptr *tls; func()
// fn} (libcgo.h; g at +0, tls at +8, fn at +16) in DI. Port of
// gcc_libinit_unix.c: malloc a copy that outlives the caller's frame,
// then hand it to the OS-dependent half.
TEXT x_cgo_thread_start(SB), NOSPLIT, $16-0
	MOVQ	DI, 0(SP)		// ts

	MOVQ	$24, DI			// sizeof(ThreadStart)
	CALL	libc_malloc(SB)
	TESTQ	AX, AX
	JZ	oom

	MOVQ	0(SP), CX		// ts
	MOVQ	0(CX), DX		// ts->g
	MOVQ	DX, 0(AX)
	MOVQ	8(CX), DX		// ts->tls
	MOVQ	DX, 8(AX)
	MOVQ	16(CX), DX		// ts->fn
	MOVQ	DX, 16(AX)

	MOVQ	AX, DI
	CALL	x_cgo_sys_thread_start(SB)
	RET

oom:
	MOVQ	$oomMsg(SB), SI
	MOVQ	$43, DX
	CALL	abort2<>(SB)

// ─── x_cgo_sys_thread_start ──────────────────────────────────────────

// The darwin branch of pthread_unix.c _cgo_sys_thread_start, with ts in
// DI. Runs on the caller's g0 stack. Frame map (declared $128):
//	  0       ts (ThreadStart copy)
//	  8       ign (sigset_t, 4 bytes on darwin)
//	 16       oset
//	 24       pthread_t p
//	 32       size
//	 40..103  pthread_attr_t (64 bytes on LP64 darwin)
//	104       tries (32-bit)
//	112..127  timespec {tv_sec, tv_nsec} for nanosleep
TEXT x_cgo_sys_thread_start(SB), NOSPLIT, $128-0
	MOVQ	DI, 0(SP)		// ts

	// Block every signal around the create, restore the previous mask
	// after (pthread_unix.c sigfillset + pthread_sigmask dance; the
	// new thread inherits the full mask and unblocks in minit).
	LEAQ	8(SP), DI
	CALL	libc_sigfillset(SB)
	MOVQ	$3, DI			// SIG_SETMASK
	LEAQ	8(SP), SI
	LEAQ	16(SP), DX
	CALL	libc_pthread_sigmask(SB)

	LEAQ	40(SP), DI		// &attr
	CALL	libc_pthread_attr_init(SB)
	LEAQ	40(SP), DI
	MOVQ	$1, SI			// PTHREAD_CREATE_DETACHED
	CALL	libc_pthread_attr_setdetachstate(SB)

	// __APPLE__: copy the stack size from the parent thread instead of
	// the non-main thread default (pthread_unix.c lines 32-39).
	CALL	libc_pthread_self(SB)
	MOVQ	AX, DI
	CALL	libc_pthread_get_stacksize_np(SB)
	MOVQ	AX, 32(SP)		// size
	LEAQ	40(SP), DI
	MOVQ	32(SP), SI
	CALL	libc_pthread_attr_setstacksize(SB)

	// Leave stacklo=0 and set stackhi=size; mstart does the rest
	// (runtime/proc.go mstart0: "Cgo may have left stack size in
	// stack.hi").
	MOVQ	0(SP), CX		// ts
	MOVQ	0(CX), DX		// ts->g
	MOVQ	32(SP), R8
	MOVQ	R8, 8(DX)		// g->stackhi = size

	MOVL	$0, 104(SP)		// tries

create_loop:
	LEAQ	24(SP), DI		// &p
	LEAQ	40(SP), SI		// &attr
	MOVQ	$threadentry(SB), DX
	MOVQ	0(SP), CX		// ts
	CALL	libc_pthread_create(SB)
	TESTL	AX, AX
	JEQ	create_ok
	CMPL	AX, $35			// EAGAIN on darwin
	JNE	create_fail

	// _cgo_try_pthread_create: sleep (tries+1) ms and retry, 20 tries.
	MOVL	104(SP), CX
	CMPL	CX, $20
	JGE	create_fail
	INCL	CX
	MOVL	CX, 104(SP)
	MOVQ	$0, 112(SP)		// tv_sec = 0
	MOVL	CX, AX			// zero-extends into RAX
	IMULL	$1000000, AX		// (tries+1) ms as ns; 20e6 fits in 32 bits
	MOVQ	AX, 120(SP)		// tv_nsec
	LEAQ	112(SP), DI
	MOVQ	$0, SI
	CALL	libc_nanosleep(SB)
	JMP	create_loop

create_ok:
	// Restore the caller's signal mask.
	MOVQ	$3, DI
	LEAQ	16(SP), SI
	MOVQ	$0, DX
	CALL	libc_pthread_sigmask(SB)
	RET

create_fail:
	MOVQ	$createMsg(SB), SI
	MOVQ	$36, DX
	CALL	abort2<>(SB)

// ─── x_cgo_sys_thread_create ─────────────────────────────────────────

// Port of pthread_unix.c x_cgo_sys_thread_create: create a detached
// thread for func (DI) with no Go state. Only reached from C boot paths
// (c-archive/c-shared), never in an executable; implemented for
// fidelity with callbacks_unix.go's contract. Frame map matches
// x_cgo_sys_thread_start.
TEXT x_cgo_sys_thread_create(SB), NOSPLIT, $128-0
	MOVQ	DI, 0(SP)		// func

	LEAQ	40(SP), DI
	CALL	libc_pthread_attr_init(SB)
	LEAQ	40(SP), DI
	MOVQ	$1, SI			// PTHREAD_CREATE_DETACHED
	CALL	libc_pthread_attr_setdetachstate(SB)

	MOVL	$0, 104(SP)		// tries

sc_loop:
	LEAQ	24(SP), DI		// &p
	LEAQ	40(SP), SI		// &attr
	MOVQ	0(SP), DX		// func
	MOVQ	$0, CX			// arg
	CALL	libc_pthread_create(SB)
	TESTL	AX, AX
	JEQ	sc_ok
	CMPL	AX, $35			// EAGAIN
	JNE	sc_fail
	MOVL	104(SP), CX
	CMPL	CX, $20
	JGE	sc_fail
	INCL	CX
	MOVL	CX, 104(SP)
	MOVQ	$0, 112(SP)
	MOVL	CX, AX
	IMULL	$1000000, AX
	MOVQ	AX, 120(SP)
	LEAQ	112(SP), DI
	MOVQ	$0, SI
	CALL	libc_nanosleep(SB)
	JMP	sc_loop

sc_ok:
	RET

sc_fail:
	MOVQ	$createMsg(SB), SI
	MOVQ	$36, DX
	CALL	abort2<>(SB)

// ─── threadentry ─────────────────────────────────────────────────────

// threadentry runs on the brand-new pthread with no g, v in DI. Port of
// gcc_unix.c threadentry: copy the ThreadStart, free the malloc'd copy,
// then crosscall1(ts.fn, setg_gcc, ts.g) — ts.fn is mstart and never
// returns, but the NULL return keeps the pthread contract.
//
// Frame map (declared $32):
//	 0   ts.g
//	 8   ts.fn
TEXT threadentry(SB), NOSPLIT, $32-0
	MOVQ	0(DI), AX		// ts.g
	MOVQ	AX, 0(SP)
	MOVQ	16(DI), AX		// ts.fn
	MOVQ	AX, 8(SP)
	CALL	libc_free(SB)		// v is still in DI

	MOVQ	8(SP), DI		// fn
	MOVQ	·setgGCC(SB), SI	// setg_gcc
	MOVQ	0(SP), DX		// g
	CALL	crosscall1(SB)

	MOVQ	$0, AX
	RET

// ─── crosscall1 ──────────────────────────────────────────────────────

// Port of gcc_amd64.S crosscall1: bridge from the C ABI into the gc
// tool chain. Saves the C callee-saved registers, calls setg(g), then
// fn(). The Go ABI treats every register as caller-save, so nothing
// else needs preserving (the C version saves no XMM registers either,
// and SysV makes every XMM caller-saved anyway).
//
// The six pushes leave RSP ≡ 8 (mod 16) at both CALLs, exactly as
// gcc_amd64.S:27-42 leaves it. That is one word short of the System V
// call alignment, and deliberately kept: both callees are gc-toolchain
// assembly (setg_gcc is runtime/asm_amd64.s:1200, three instructions
// with no stack use; fn is mstart, Go ABI0, which requires only 8-byte
// alignment) and mstart re-establishes 16-byte alignment for every
// later C call by going through asmcgocall's ANDQ $~15, SP
// (runtime/asm_amd64.s:963). Porting the C faithfully is the rule for
// this file; a "fix" here would diverge from the reference for no gain.
TEXT crosscall1(SB), NOSPLIT|NOFRAME, $0-0
	PUSHQ	BX
	PUSHQ	BP
	PUSHQ	R12
	PUSHQ	R13
	PUSHQ	R14
	PUSHQ	R15

	MOVQ	DI, BX			// fn
	MOVQ	DX, DI			// arg of setg_gcc: g
	CALL	SI			// setg(g): setg_gcc writes TLS and R14
	CALL	BX			// fn() = mstart; never returns

	POPQ	R15
	POPQ	R14
	POPQ	R13
	POPQ	R12
	POPQ	BP
	POPQ	BX
	RET

// ─── x_cgo_notify_runtime_init_done ──────────────────────────────────

// Port of gcc_libinit_unix.c x_cgo_notify_runtime_init_done: set the
// flag under the mutex and broadcast the condition so
// _cgo_wait_runtime_init_done (which we never compile) would wake.
// The runtime's own call site (proc.go:257) only runs when iscgo was
// already true during schedinit, so in this host the function exists
// to keep the _cgo_notify_runtime_init_done contract honest.
TEXT x_cgo_notify_runtime_init_done(SB), NOSPLIT, $16-0
	MOVQ	$runtime_init_mu(SB), DI
	CALL	libc_pthread_mutex_lock(SB)
	MOVQ	$1, runtime_init_done(SB)
	MOVQ	$runtime_init_cond(SB), DI
	CALL	libc_pthread_cond_broadcast(SB)
	MOVQ	$runtime_init_mu(SB), DI
	CALL	libc_pthread_mutex_unlock(SB)
	RET

// ─── x_cgo_setenv / x_cgo_unsetenv ──────────────────────────────────

// Port of gcc_setenv.c. DI points at the runtime's argument array
// (env_posix.go:72-73): [k, v] for setenv, [k] for unsetenv. Called
// via asmcgocall from setenv_c/unsetenv_c on every os.Setenv.
TEXT x_cgo_setenv(SB), NOSPLIT, $16-0
	MOVQ	8(DI), SI		// v
	MOVQ	0(DI), AX		// k, before DI is overwritten
	MOVQ	AX, DI
	MOVQ	$1, DX			// overwrite
	CALL	libc_setenv(SB)
	RET

TEXT x_cgo_unsetenv(SB), NOSPLIT, $16-0
	MOVQ	0(DI), DI		// k
	CALL	libc_unsetenv(SB)
	RET

// ─── helpers ─────────────────────────────────────────────────────────

// abort2<>(SB) writes msg (SI, length DX) to stderr and aborts. The
// shared tail of every fatal path; never returns.
TEXT abort2<>(SB), NOSPLIT, $16-0
	MOVQ	$2, DI			// stderr
	CALL	libc_write(SB)
	CALL	libc_abort(SB)		// noreturn; no tail back-edge (keeps the
					// nosplit graph finite for the assembler)
