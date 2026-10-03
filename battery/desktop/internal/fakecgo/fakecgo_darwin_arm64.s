//go:build darwin && arm64

#include "textflag.h"

// The fake-cgo layer for darwin/arm64, ported from runtime/cgo's C:
//   x_cgo_init, threadentry        <- gcc_unix.c
//   x_cgo_thread_start             <- gcc_libinit_unix.c
//   x_cgo_notify_runtime_init_done <- gcc_libinit_unix.c
//   x_cgo_sys_thread_start         <- pthread_unix.c (the __APPLE__ branch)
//   x_cgo_sys_thread_create        <- pthread_unix.c
//   x_cgo_setenv/x_cgo_unsetenv    <- gcc_setenv.c
//   crosscall1                     <- gcc_arm64.S
// Every function here runs with the C ABI on a system stack (g0, or a
// brand-new pthread with no g at all in threadentry): no Go code, no
// allocation, no stack growth, no g accesses. C symbol names carry no
// package prefix (TEXT x(SB), not TEXT ·x(SB)), which is how the
// runtime's linknamed _cgo_* variables and rt0_go find them.
//
// Register discipline: symbol references (MOVD sym(SB), r / MOVD r,
// sym(SB)) assemble through REGTMP (R27), which is callee-saved in the
// C ABI, so every function that touches a symbol saves and restores
// R27 (and R28/g for uniformity). Frames stay well under 240 bytes so
// the assembler emits the small-frame prologue (MOVD.W R30, -N(SP)),
// which clobbers no register — the large-frame prologue's SUB into R20
// would destroy a C caller's register.

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
// 48-byte cond; the __opaque tails are zero).

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

// x_cgo_init is called from rt0_go (runtime/asm_arm64.s:123-138) with
// the C ABI before any Go code runs:
// pthread_get_stackaddr_np(self) - pthread_get_stacksize_np(self).
// (The Go 1.27 C sets stacklo to the raw low bound; older releases
// added a guard page, which purego's port preserved. Following the
// current runtime is the safer bet: rt0_go recomputes the guard from
// stacklo immediately after the call.) rt0_go set g0->stackhi to the
// entry SP beforehand, so the sanity check is stacklo < stackhi.
TEXT x_cgo_init(SB), NOSPLIT, $56-0
	STP	(R27, g), 8(RSP)	// REGTMP + g are C-callee-saved; save before symbol refs
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
	MOVD	$1, R2
	MOVD	$runtime·iscgo(SB), R3
	MOVB	R2, (R3)
	MOVD	·noopCrosscall2+0(SB), R2
	MOVD	$runtime·set_crosscall2(SB), R3
	MOVD	R2, (R3)
	MOVD	R0, 24(RSP)		// g0
	MOVD	R1, ·setgGCC+0(SB)	// setg_gcc for threadentry

	BL	libc_pthread_self(SB)
	MOVD	R0, 32(RSP)		// self
	BL	libc_pthread_get_stackaddr_np(SB)
	MOVD	R0, 40(RSP)		// stack high address
	MOVD	32(RSP), R0
	BL	libc_pthread_get_stacksize_np(SB)
	MOVD	40(RSP), R1		// addr
	SUB	R0, R1			// addr - size
	MOVD	24(RSP), R3		// g0
	MOVD	R1, 0(R3)		// g0->stacklo (G layout: stacklo, stackhi)

	// Sanity check the bounds now rather than crashing in a morestack
	// on g0 later (gcc_unix.c _cgo_set_stacklo).
	MOVD	8(R3), R4		// stackhi (entry SP)
	CMP	R1, R4		// flags for stackhi - stacklo
	BHI	4(PC)		// stackhi > stacklo: bounds sane; 4 skips the abort block
	MOVD	$boundsMsg(SB), R1
	MOVD	$30, R2
	BL	abort2<>(SB)
	// unreachable on sane bounds

	LDP	8(RSP), (R27, g)
	RET

// ─── x_cgo_thread_start ──────────────────────────────────────────────

// x_cgo_thread_start is called from runtime.newm1 via asmcgocall with
// a pointer to the runtime's ThreadStart {G *g; uintptr *tls; func()
// fn} (libcgo.h; g at +0, tls at +8, fn at +16). Port of
// gcc_libinit_unix.c: malloc a copy that outlives the caller's frame,
// then hand it to the OS-dependent half.
TEXT x_cgo_thread_start(SB), NOSPLIT, $40-0
	STP	(R27, g), 8(RSP)
	MOVD	R0, 24(RSP)		// ts

	MOVD	$24, R0			// sizeof(ThreadStart)
	BL	libc_malloc(SB)
	CBZ	R0, oom

	MOVD	24(RSP), R1
	LDP	0(R1), (R2, R3)		// g, tls
	STP	(R2, R3), 0(R0)
	MOVD	16(R1), R2		// fn
	MOVD	R2, 16(R0)

	BL	x_cgo_sys_thread_start(SB)

	LDP	8(RSP), (R27, g)
	RET
oom:
	MOVD	$oomMsg(SB), R1
	MOVD	$43, R2
	BL	abort2<>(SB)

// ─── x_cgo_sys_thread_start ──────────────────────────────────────────

// The darwin branch of pthread_unix.c _cgo_sys_thread_start. Runs on
// the caller's g0 stack. Frame map (autosize 176):
//	  8..23   R27, g
//	 24       ts (ThreadStart copy)
//	 32       ign (sigset_t, 4 bytes on darwin)
//	 40       oset
//	 48       pthread_t p
//	 56       size
//	 64..127  pthread_attr_t (64 bytes)
//	128       tries (w)
//	136..151  timespec for nanosleep
TEXT x_cgo_sys_thread_start(SB), NOSPLIT, $160-0
	STP	(R27, g), 8(RSP)
	MOVD	R0, 24(RSP)		// ts

	// Block every signal around the create, restore the previous mask
	// after (pthread_unix.c sigfillset + pthread_sigmask dance; the
	// new thread inherits the full mask and unblocks in minit).
	MOVD	$32(RSP), R0
	BL	libc_sigfillset(SB)
	MOVD	$3, R0			// SIG_SETMASK
	MOVD	$32(RSP), R1
	MOVD	$40(RSP), R2
	BL	libc_pthread_sigmask(SB)

	MOVD	$64(RSP), R0		// &attr
	BL	libc_pthread_attr_init(SB)
	MOVD	$64(RSP), R0
	MOVD	$1, R1			// PTHREAD_CREATE_DETACHED
	BL	libc_pthread_attr_setdetachstate(SB)

	// __APPLE__: copy the stack size from the parent thread instead of
	// the non-main default (pthread_unix.c lines 32-39).
	BL	libc_pthread_self(SB)
	BL	libc_pthread_get_stacksize_np(SB)
	MOVD	R0, 56(RSP)		// size
	MOVD	$64(RSP), R0
	MOVD	56(RSP), R1
	BL	libc_pthread_attr_setstacksize(SB)

	// Leave stacklo=0 and set stackhi=size; mstart does the rest
	// (runtime/proc.go mstart0: "Cgo may have left stack size in
	// stack.hi").
	MOVD	24(RSP), R1		// ts
	MOVD	0(R1), R2		// ts->g
	MOVD	56(RSP), R3
	MOVD	R3, 8(R2)		// g->stackhi = size

	MOVW	$0, 128(RSP)		// tries
create_loop:
	MOVD	$48(RSP), R0		// &p
	MOVD	$64(RSP), R1		// &attr
	MOVD	$threadentry(SB), R2
	MOVD	24(RSP), R3		// ts
	BL	libc_pthread_create(SB)
	CMPW	$0, R0
	BEQ	create_ok
	CMPW	$35, R0			// EAGAIN on darwin
	BNE	create_fail

	// _cgo_try_pthread_create: sleep (tries+1) ms and retry, 20 tries.
	MOVW	128(RSP), R1
	CMPW	$20, R1
	BGE	create_fail
	MOVW	128(RSP), R1
	ADD	$1, R1, R1
	MOVW	R1, 128(RSP)
	MOVD	$0, 136(RSP)		// tv_sec = 0
	MOVW	R1, R2
	MOVW	$1000, R3
	MULW	R3, R2, R2		// (tries+1) ms
	MOVW	$1000, R3
	MULW	R3, R2, R2		// -> ns
	MOVD	$136(RSP), R0
	MOVD	R2, 8(R0)		// tv_nsec: an 8-byte long at offset 8 of struct timespec (offset 4 landed inside tv_sec)
	MOVD	$136(RSP), R0
	MOVD	$0, R1
	BL	libc_nanosleep(SB)
	B	create_loop

create_ok:
	// Restore the caller's signal mask.
	MOVD	$3, R0
	MOVD	$40(RSP), R1
	MOVD	$0, R2
	BL	libc_pthread_sigmask(SB)
	LDP	8(RSP), (R27, g)
	RET

create_fail:
	MOVD	$createMsg(SB), R1
	MOVD	$36, R2
	BL	abort2<>(SB)

// ─── x_cgo_sys_thread_create ─────────────────────────────────────────

// Port of pthread_unix.c x_cgo_sys_thread_create: create a detached
// thread for func with no Go state. Only reached from C boot paths
// (c-archive/c-shared), never in an executable; implemented for
// fidelity with callbacks_unix.go's contract.
TEXT x_cgo_sys_thread_create(SB), NOSPLIT, $160-0
	STP	(R27, g), 8(RSP)
	MOVD	R0, 24(RSP)		// func

	MOVD	$64(RSP), R0
	BL	libc_pthread_attr_init(SB)
	MOVD	$64(RSP), R0
	MOVD	$1, R1			// PTHREAD_CREATE_DETACHED
	BL	libc_pthread_attr_setdetachstate(SB)

	MOVW	$0, 128(RSP)		// tries
sc_loop:
	MOVD	$48(RSP), R0		// &p
	MOVD	$64(RSP), R1		// &attr
	MOVD	24(RSP), R2		// func
	MOVD	$0, R3			// arg
	BL	libc_pthread_create(SB)
	CMPW	$0, R0
	BEQ	sc_ok
	CMPW	$35, R0			// EAGAIN
	BNE	sc_fail
	MOVW	128(RSP), R1
	CMPW	$20, R1
	BGE	sc_fail
	MOVW	128(RSP), R1
	ADD	$1, R1, R1
	MOVW	R1, 128(RSP)
	MOVD	$0, 136(RSP)
	MOVW	R1, R2
	MOVW	$1000, R3
	MULW	R3, R2, R2
	MOVW	$1000, R3
	MULW	R3, R2, R2
	MOVD	$136(RSP), R0
	MOVD	R2, 8(R0)		// tv_nsec at offset 8 (see above)
	MOVD	$136(RSP), R0
	MOVD	$0, R1
	BL	libc_nanosleep(SB)
	B	sc_loop

sc_ok:
	LDP	8(RSP), (R27, g)
	RET
sc_fail:
	MOVD	$createMsg(SB), R1
	MOVD	$36, R2
	BL	abort2<>(SB)

// ─── threadentry ─────────────────────────────────────────────────────

// threadentry runs on the brand-new pthread with no g. Port of
// gcc_unix.c threadentry: copy the ThreadStart, free the malloc'd
// copy, then crosscall1(ts.fn, setg_gcc, ts.g) — ts.fn is mstart and
// never returns, but the NULL return keeps the pthread contract.
TEXT threadentry(SB), NOSPLIT, $48-0
	STP	(R27, g), 8(RSP)
	MOVD	R0, 40(RSP)		// v
	MOVD	0(R0), R1		// ts.g
	MOVD	16(R0), R2		// ts.fn
	MOVD	R1, 32(RSP)
	MOVD	R2, 24(RSP)
	MOVD	40(RSP), R0
	BL	libc_free(SB)

	MOVD	24(RSP), R0		// fn
	MOVD	·setgGCC+0(SB), R1	// setg_gcc
	MOVD	32(RSP), R2		// g
	BL	crosscall1(SB)

	MOVD	$0, R0
	LDP	8(RSP), (R27, g)
	RET

// ─── crosscall1 ──────────────────────────────────────────────────────

// Port of gcc_arm64.S crosscall1: bridge from the C ABI into the gc
// tool chain. Saves the C callee-saved registers, calls setg(g), then
// fn(). The Go ABI treats every register as caller-save, so nothing
// else needs preserving (the C version saves no FP registers either).
// Entered only from threadentry, so SP is already 16-byte aligned at
// the BL sites per the C ABI.
TEXT crosscall1(SB), NOSPLIT|NOFRAME, $0-0
	SUB	$96, RSP
	STP	(R29, R30), 0(RSP)
	MOVD	RSP, R29
	STP	(R27, g), 16(RSP)
	STP	(R25, R26), 32(RSP)
	STP	(R23, R24), 48(RSP)
	STP	(R21, R22), 64(RSP)
	STP	(R19, R20), 80(RSP)

	MOVD	R0, R19			// fn
	MOVD	R1, R20			// setg_gcc
	MOVD	R2, R0			// arg: g

	BL	(R20)			// setg(g): setg_gcc writes TLS
	MOVD	R19, R0
	BL	(R19)			// fn() = mstart; never returns

	LDP	80(RSP), (R19, R20)
	LDP	64(RSP), (R21, R22)
	LDP	48(RSP), (R23, R24)
	LDP	32(RSP), (R25, R26)
	LDP	16(RSP), (R27, g)
	LDP	0(RSP), (R29, R30)
	ADD	$96, RSP
	RET

// ─── x_cgo_notify_runtime_init_done ──────────────────────────────────

// Port of gcc_libinit_unix.c x_cgo_notify_runtime_init_done: set the
// flag under the mutex and broadcast the condition so
// _cgo_wait_runtime_init_done (which we never compile) would wake.
// The runtime's own call site (proc.go:257) only runs when iscgo was
// already true during schedinit, so in this host the function exists
// to keep the _cgo_notify_runtime_init_done contract honest.
TEXT x_cgo_notify_runtime_init_done(SB), NOSPLIT, $24-0
	STP	(R27, g), 8(RSP)
	MOVD	$runtime_init_mu(SB), R0
	BL	libc_pthread_mutex_lock(SB)
	MOVD	$1, R1
	MOVD	R1, runtime_init_done+0(SB)
	MOVD	$runtime_init_cond(SB), R0
	BL	libc_pthread_cond_broadcast(SB)
	MOVD	$runtime_init_mu(SB), R0
	BL	libc_pthread_mutex_unlock(SB)
	LDP	8(RSP), (R27, g)
	RET

// ─── x_cgo_setenv / x_cgo_unsetenv ──────────────────────────────────

// Port of gcc_setenv.c. R0 points at the runtime's argument array
// (env_posix.go:72-73): [k, v] for setenv, [k] for unsetenv. Called
// via asmcgocall from setenv_c/unsetenv_c on every os.Setenv.
TEXT x_cgo_setenv(SB), NOSPLIT, $24-0
	STP	(R27, g), 8(RSP)
	LDP	0(R0), (R1, R2)		// k, v
	MOVD	R1, R0
	MOVD	R2, R1
	MOVD	$1, R2			// overwrite
	BL	libc_setenv(SB)
	LDP	8(RSP), (R27, g)
	RET

TEXT x_cgo_unsetenv(SB), NOSPLIT, $24-0
	STP	(R27, g), 8(RSP)
	MOVD	0(R0), R0		// k
	BL	libc_unsetenv(SB)
	LDP	8(RSP), (R27, g)
	RET

// ─── helpers ─────────────────────────────────────────────────────────

// abort2<>(SB) writes msg (R1, length R2) to stderr and aborts. The
// shared tail of every fatal path; never returns.
TEXT abort2<>(SB), NOSPLIT, $16-0
	STP	(R27, g), 8(RSP)
	MOVD	$2, R0			// stderr
	MOVW	R2, R2
	BL	libc_write(SB)
	BL	libc_abort(SB)		// noreturn; no tail back-edge (keeps the
				// nosplit graph finite for the assembler)
