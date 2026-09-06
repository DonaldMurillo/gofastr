//go:build linux && arm64

#include "textflag.h"

// The fake-cgo layer for linux/arm64, ported from runtime/cgo's C:
//   x_cgo_init, threadentry        <- gcc_unix.c
//   x_cgo_thread_start             <- gcc_libinit_unix.c
//   x_cgo_notify_runtime_init_done <- gcc_libinit_unix.c
//   x_cgo_sys_thread_start         <- pthread_unix.c (the non-__APPLE__ branch)
//   x_cgo_sys_thread_create        <- pthread_unix.c
//   x_cgo_getstackbound            <- pthread_unix.c (the __GLIBC__ branch)
//   x_cgo_setenv/x_cgo_unsetenv    <- gcc_setenv.c
//   crosscall1                     <- gcc_arm64.S
//
// Every function here runs with the C ABI on a system stack (g0, or a
// brand-new pthread with no g at all in threadentry): no Go code, no
// allocation, no stack growth, no g accesses. C symbol names carry no
// package prefix (TEXT x(SB), not TEXT ·x(SB)), which is how the
// runtime's linknamed _cgo_* variables and rt0_go find them.
//
// Divergences from the darwin/arm64 sibling, all from the C sources:
//
//	sigset_t          128 bytes on Linux, 4 on darwin
//	pthread_attr_t    64 bytes on glibc/LP64 (128 reserved here)
//	SIG_SETMASK       2 on Linux, 3 on darwin
//	EAGAIN            11 on Linux, 35 on darwin
//	stack bounds      pthread_getattr_np + pthread_attr_getstack, which
//	                  reports the LOW address, instead of darwin's
//	                  pthread_get_stackaddr_np minus stacksize
//	thread stack size pthread_attr_getstacksize on a fresh attr (the
//	                  glibc default / RLIMIT_STACK), not the parent's
//	mutex/cond        glibc's PTHREAD_MUTEX_INITIALIZER and
//	                  PTHREAD_COND_INITIALIZER are all-zero, so the
//	                  static blobs need no DATA words
//	timespec.tv_nsec  written as the full 8-byte long it is
//	x_cgo_getstackbound  provided here, so a callback arriving on a
//	                  foreign thread gets real g0 bounds instead of the
//	                  32 KiB estimate (runtime/cgocall.go:277-306)
//
// Register discipline: symbol references (MOVD sym(SB), r / MOVD r,
// sym(SB)) assemble through REGTMP (R27), which is callee-saved in the
// C ABI, so every function that touches a symbol saves and restores R27
// (and R28/g for uniformity). x16 and x17 are scratch for the PLT stubs
// every BL to a libc symbol goes through
// (cmd/link/internal/arm64/asm.go:1200-1220); nothing here holds a live
// value in either across a call.
//
// Frames: the assembler switches to a large-frame prologue above 240
// bytes (cmd/internal/obj/arm64/obj7.go:582-585), and that prologue's
// `SUB $autosize, RSP, R20` scratch destroys R20, a C-callee-saved
// register. Functions that need more space therefore declare $0 and
// extend the frame by hand, the shape internal/ffi's callbackasm1 uses:
// after `SUB $N, RSP` the saved LR sits at N(RSP) and the saved FP at
// N-8(RSP), so the usable window is 8(RSP) through N-16(RSP).

// ─── data ────────────────────────────────────────────────────────────

// The pointer cells the runtime linknames. RODATA words, one per C-ABI
// function below; runtime/cgo.go's no-initializer declarations resolve
// against these by name.

GLOBL _cgo_init(SB), RODATA, $8
DATA _cgo_init(SB)/8, $x_cgo_init(SB)

GLOBL _cgo_thread_start(SB), RODATA, $8
DATA _cgo_thread_start(SB)/8, $x_cgo_thread_start(SB)

GLOBL _cgo_sys_thread_create(SB), RODATA, $8
DATA _cgo_sys_thread_create(SB)/8, $x_cgo_sys_thread_create(SB)

GLOBL _cgo_notify_runtime_init_done(SB), RODATA, $8
DATA _cgo_notify_runtime_init_done(SB)/8, $x_cgo_notify_runtime_init_done(SB)

// _cgo_getstackbound is optional (runtime/cgocall.go:283 nil-checks it)
// but cheap here, because x_cgo_init already needs the same pthread
// call. With it, an M borrowed by needm for a callback on a foreign
// thread gets that thread's real stack bounds and mp.g0StackAccurate
// instead of the sp+1024/sp-32KiB estimate at cgocall.go:277-279.
//
// Measured, not assumed: renaming this cell so the runtime cannot find
// it leaves TestCallbackFromForeignThread green, 200 Go frames deep and
// all. The estimate is enough for the callback's own Go code, which runs
// on a goroutine stack, not on g0. What the accurate bounds buy is the
// runtime's own systemstack work during that callback — stack-overflow
// detection, traceback, and the "is SP still inside the bounds we
// recorded" fast path that skips re-deriving them on every subsequent
// callback from the same thread.
GLOBL _cgo_getstackbound(SB), RODATA, $8
DATA _cgo_getstackbound(SB)/8, $x_cgo_getstackbound(SB)

// _cgo_setenv/_cgo_unsetenv differ from the rest: runtime's
// env_posix.go pushes them with a BARE //go:linkname, so the binding
// symbol is the runtime-qualified runtime._cgo_setenv (see
// runtime/cgo/setenv.go:13, which binds the same way), not an
// unprefixed external name.
GLOBL runtime·_cgo_setenv(SB), RODATA, $8
DATA runtime·_cgo_setenv(SB)/8, $x_cgo_setenv(SB)

GLOBL runtime·_cgo_unsetenv(SB), RODATA, $8
DATA runtime·_cgo_unsetenv(SB)/8, $x_cgo_unsetenv(SB)

// _cgo_pthread_key_created points at a word that stays zero
// (callbacks_linux.go xCGOPthreadKeyCreated); ·cgocallback reads it to
// decide whether a foreign thread's borrowed M may stay bound, and zero
// means "dropm on the way out", which is right with no key.
GLOBL _cgo_pthread_key_created(SB), RODATA, $8
DATA _cgo_pthread_key_created(SB)/8, $·xCGOPthreadKeyCreated(SB)

// gcc_libinit_unix.c's static state. glibc's PTHREAD_MUTEX_INITIALIZER
// and PTHREAD_COND_INITIALIZER are all-zero structs, so unlike darwin
// there are no signature words to lay out by hand. Sizes are glibc's
// __SIZEOF_PTHREAD_MUTEX_T / __SIZEOF_PTHREAD_COND_T on LP64.

GLOBL runtime_init_mu(SB), NOPTR, $48   // pthread_mutex_t, zeroed
GLOBL runtime_init_cond(SB), NOPTR, $48 // pthread_cond_t, zeroed
GLOBL runtime_init_done(SB), NOPTR, $8  // int, zeroed

// Error strings for the fatal paths, C-file fidelity (fprintf to stderr
// kept as a write(2) to fd 2). The GLOBL sizes round up to a multiple of
// eight; the length passed to write is the real string length.

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

// x_cgo_init is called from rt0_go (runtime/asm_arm64.s:123-138) with
// the C ABI before any Go code runs. Signature in C is
// x_cgo_init(G *g, void (*setg)(void*), void **tlsg, void **tlsbase);
// on linux/arm64 (non-android) rt0_go passes 0 for the third argument
// and the platform's own TLS is used, so there is no x_cgo_inittls hook
// to call (gcc_unix.c:42-44 skips it when the weak symbol is absent,
// and only gcc_android.c ever defines it).
//
// Frame (declared $168, so the assembler stays on the small-frame
// prologue; usable offsets 8..175):
//	  8..23   saved R27, g
//	 24       g0
//	 32       stack low address
//	 40       stack size
//	 48..175  pthread_attr_t (glibc needs 64; 128 reserved)
TEXT x_cgo_init(SB), NOSPLIT, $168-0
	STP	(R27, g), 8(RSP)	// REGTMP + g are C-callee-saved; save before symbol refs

	// Two stores the runtime needs before any package init could run
	// (a real cgo binary gets both from the linker wiring runtime/cgo
	// into runtime's own init graph; x_cgo_init is our equivalent hook):
	//
	//   runtime.iscgo = true, before schedinit: mcommoninit(m0) only
	//   allocates m0's cgoCallers array when iscgo is true
	//   (proc.go:1046), and the first cgocall on an M without it
	//   nil-faults (cgocall.go:151). On linux/arm64 the same store also
	//   switches runtime.save_g/load_g onto the TLS slot
	//   (tls_arm64.s:15,36) for the rest of the process.
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

	// pthread_getattr_np's out-parameters, pre-zeroed so a failed call
	// leaves a defined zero rather than stack garbage.
	MOVD	$0, 32(RSP)
	MOVD	$0, 40(RSP)

	// _cgo_set_stacklo, the __GLIBC__ branch of x_cgo_getstackbound.
	// pthread_attr_init first: before glibc 2.32 pthread_getattr_np did
	// not always initialize the attr itself (Go issue #65625).
	MOVD	$48(RSP), R0
	BL	libc_pthread_attr_init(SB)
	BL	libc_pthread_self(SB)
	MOVD	$48(RSP), R1
	BL	libc_pthread_getattr_np(SB)
	MOVD	$48(RSP), R0
	MOVD	$32(RSP), R1		// &addr (low)
	MOVD	$40(RSP), R2		// &size
	BL	libc_pthread_attr_getstack(SB)
	MOVD	$48(RSP), R0
	BL	libc_pthread_attr_destroy(SB)

	MOVD	32(RSP), R1		// stack low address
	MOVD	24(RSP), R3		// g0

	// A zero low address means pthread_getattr_np failed. The C would
	// store it anyway and then pass the lo < hi check with stacklo 0,
	// leaving g0 with no usable guard; keeping rt0_go's SP-64KiB
	// estimate is strictly safer and cannot happen on a glibc main
	// thread in the first place.
	CBZ	R1, init_done

	MOVD	R1, 0(R3)		// g0->stacklo (G layout: stacklo, stackhi)

	// Sanity check the bounds now rather than crashing in a morestack
	// on g0 later (gcc_unix.c _cgo_set_stacklo).
	MOVD	8(R3), R4		// stackhi (entry SP, set by rt0_go)
	CMP	R1, R4			// flags for stackhi - stacklo
	BHI	init_done		// stackhi > stacklo: bounds sane
	MOVD	$boundsMsg(SB), R1
	MOVD	$30, R2
	BL	abort2<>(SB)		// never returns

init_done:
	LDP	8(RSP), (R27, g)
	RET

// ─── x_cgo_getstackbound ─────────────────────────────────────────────

// void x_cgo_getstackbound(uintptr bounds[2]) — the __GLIBC__ branch of
// pthread_unix.c:76-116. Called through asmcgocall from
// callbackUpdateSystemStack (runtime/cgocall.go:283-297) when a
// callback arrives on a thread the runtime has no accurate g0 bounds
// for. Same frame map as x_cgo_init.
TEXT x_cgo_getstackbound(SB), NOSPLIT, $168-0
	STP	(R27, g), 8(RSP)
	MOVD	R0, 24(RSP)		// bounds
	MOVD	$0, 32(RSP)		// addr
	MOVD	$0, 40(RSP)		// size

	MOVD	$48(RSP), R0
	BL	libc_pthread_attr_init(SB)
	BL	libc_pthread_self(SB)
	MOVD	$48(RSP), R1
	BL	libc_pthread_getattr_np(SB)
	MOVD	$48(RSP), R0
	MOVD	$32(RSP), R1
	MOVD	$40(RSP), R2
	BL	libc_pthread_attr_getstack(SB)
	MOVD	$48(RSP), R0
	BL	libc_pthread_attr_destroy(SB)

	MOVD	24(RSP), R3		// bounds
	MOVD	32(RSP), R1		// addr (low)
	MOVD	40(RSP), R2		// size
	MOVD	R1, 0(R3)		// bounds[0] = addr
	ADD	R1, R2, R2
	MOVD	R2, 8(R3)		// bounds[1] = addr + size

	LDP	8(RSP), (R27, g)
	RET

// ─── x_cgo_thread_start ──────────────────────────────────────────────

// x_cgo_thread_start is called from runtime.newm1 (proc.go:2925) via
// asmcgocall with a pointer to the runtime's ThreadStart
// {G *g; uintptr *tls; func() fn} (libcgo.h; g at +0, tls at +8, fn at
// +16). Port of gcc_libinit_unix.c:180-196: malloc a copy that outlives
// the caller's frame, then hand it to the OS-dependent half. With iscgo
// true this is how EVERY M in the process is created, so a bug here is
// not a slow leak, it is the second goroutine that needs a thread.
TEXT x_cgo_thread_start(SB), NOSPLIT, $40-0
	STP	(R27, g), 8(RSP)
	MOVD	R0, 24(RSP)		// ts

	MOVD	$24, R0			// sizeof(ThreadStart)
	BL	libc_malloc(SB)
	CBZ	R0, ts_oom

	MOVD	24(RSP), R1
	LDP	0(R1), (R2, R3)		// g, tls
	STP	(R2, R3), 0(R0)
	MOVD	16(R1), R2		// fn
	MOVD	R2, 16(R0)

	BL	x_cgo_sys_thread_start(SB)

	LDP	8(RSP), (R27, g)
	RET

ts_oom:
	MOVD	$oomMsg(SB), R1
	MOVD	$43, R2
	BL	abort2<>(SB)

// ─── x_cgo_sys_thread_start ──────────────────────────────────────────

// The non-__APPLE__ branch of pthread_unix.c:18-60, with
// _cgo_try_pthread_create (lines 118-138) inlined. Runs on the caller's
// g0 stack.
//
// Frame extended by hand (see the header note): after SUB $528 the
// usable window is 8..512.
//	  8..23    saved R27, g
//	 24        ts (the ThreadStart copy)
//	 32        pthread_t p
//	 40        size
//	 48        tries (word)
//	 56..71    struct timespec {tv_sec@56, tv_nsec@64}
//	 80..207   pthread_attr_t
//	208..335   sigset_t ign
//	336..463   sigset_t oset
TEXT x_cgo_sys_thread_start(SB), NOSPLIT, $0-0
	SUB	$528, RSP
	STP	(R27, g), 8(RSP)
	MOVD	R0, 24(RSP)		// ts

	// Block every signal around the create and restore the previous
	// mask after: the new thread inherits the full mask and unblocks in
	// minit. SIG_SETMASK is 2 on Linux (3 on darwin).
	MOVD	$208(RSP), R0
	BL	libc_sigfillset(SB)
	MOVD	$2, R0			// SIG_SETMASK
	MOVD	$208(RSP), R1
	MOVD	$336(RSP), R2
	BL	libc_pthread_sigmask(SB)

	MOVD	$80(RSP), R0		// &attr
	BL	libc_pthread_attr_init(SB)
	MOVD	$80(RSP), R0
	MOVD	$1, R1			// PTHREAD_CREATE_DETACHED
	BL	libc_pthread_attr_setdetachstate(SB)

	// Non-darwin: take the default stack size off the fresh attr
	// (glibc reports RLIMIT_STACK, typically 8 MiB) instead of copying
	// the parent thread's.
	MOVD	$0, 40(RSP)
	MOVD	$80(RSP), R0
	MOVD	$40(RSP), R1		// &size
	BL	libc_pthread_attr_getstacksize(SB)

	// Leave stacklo=0 and set stackhi=size; mstart does the rest
	// (runtime/proc.go mstart0: "Cgo may have left stack size in
	// stack.hi").
	MOVD	24(RSP), R1		// ts
	MOVD	0(R1), R2		// ts->g
	MOVD	40(RSP), R3
	MOVD	R3, 8(R2)		// g->stackhi = size

	MOVW	$0, 48(RSP)		// tries

create_loop:
	MOVD	$32(RSP), R0		// &p
	MOVD	$80(RSP), R1		// &attr
	MOVD	$threadentry(SB), R2
	MOVD	24(RSP), R3		// ts
	BL	libc_pthread_create(SB)
	CMPW	$0, R0
	BEQ	create_ok
	CMPW	$11, R0			// EAGAIN on Linux (35 on darwin)
	BNE	create_fail

	// _cgo_try_pthread_create: sleep (tries+1) ms and retry, 20 tries.
	MOVW	48(RSP), R1
	CMPW	$20, R1
	BGE	create_fail
	ADD	$1, R1, R1
	MOVW	R1, 48(RSP)
	MOVD	$0, 56(RSP)		// tv_sec = 0
	MOVW	R1, R2
	MOVW	$1000, R3
	MULW	R3, R2, R2		// (tries+1) * 1000
	MOVW	$1000, R3
	MULW	R3, R2, R2		// -> nanoseconds
	MOVD	R2, 64(RSP)		// tv_nsec is a full long on LP64
	MOVD	$56(RSP), R0
	MOVD	$0, R1
	BL	libc_nanosleep(SB)
	B	create_loop

create_ok:
	// Restore the caller's signal mask.
	MOVD	$2, R0			// SIG_SETMASK
	MOVD	$336(RSP), R1
	MOVD	$0, R2
	BL	libc_pthread_sigmask(SB)
	LDP	8(RSP), (R27, g)
	ADD	$528, RSP
	RET

create_fail:
	MOVD	$createMsg(SB), R1
	MOVD	$35, R2
	BL	abort2<>(SB)

// ─── x_cgo_sys_thread_create ─────────────────────────────────────────

// Port of pthread_unix.c:62-74: create a detached thread for func with
// no Go state. Only reached from C boot paths (c-archive/c-shared),
// never in an executable; implemented for fidelity with the
// callbacks_unix.go contract.
//
// Frame extended by hand; usable window 8..256.
//	  8..23    saved R27, g
//	 24        func
//	 32        pthread_t p
//	 48        tries (word)
//	 56..71    struct timespec
//	 80..207   pthread_attr_t
TEXT x_cgo_sys_thread_create(SB), NOSPLIT, $0-0
	SUB	$272, RSP
	STP	(R27, g), 8(RSP)
	MOVD	R0, 24(RSP)		// func

	MOVD	$80(RSP), R0
	BL	libc_pthread_attr_init(SB)
	MOVD	$80(RSP), R0
	MOVD	$1, R1			// PTHREAD_CREATE_DETACHED
	BL	libc_pthread_attr_setdetachstate(SB)

	MOVW	$0, 48(RSP)		// tries

sc_loop:
	MOVD	$32(RSP), R0		// &p
	MOVD	$80(RSP), R1		// &attr
	MOVD	24(RSP), R2		// func
	MOVD	$0, R3			// arg
	BL	libc_pthread_create(SB)
	CMPW	$0, R0
	BEQ	sc_ok
	CMPW	$11, R0			// EAGAIN
	BNE	sc_fail
	MOVW	48(RSP), R1
	CMPW	$20, R1
	BGE	sc_fail
	ADD	$1, R1, R1
	MOVW	R1, 48(RSP)
	MOVD	$0, 56(RSP)
	MOVW	R1, R2
	MOVW	$1000, R3
	MULW	R3, R2, R2
	MOVW	$1000, R3
	MULW	R3, R2, R2
	MOVD	R2, 64(RSP)
	MOVD	$56(RSP), R0
	MOVD	$0, R1
	BL	libc_nanosleep(SB)
	B	sc_loop

sc_ok:
	LDP	8(RSP), (R27, g)
	ADD	$272, RSP
	RET

sc_fail:
	MOVD	$createMsg(SB), R1
	MOVD	$35, R2
	BL	abort2<>(SB)

// ─── threadentry ─────────────────────────────────────────────────────

// threadentry runs on the brand-new pthread with no g. Port of
// gcc_unix.c:47-63: copy the ThreadStart, free the malloc'd copy, then
// crosscall1(ts.fn, setg_gcc, ts.g) — ts.fn is mstart and never
// returns, but the NULL return keeps the pthread contract. There is no
// x_cgo_threadentry_platform on Linux (only gcc_netbsd.c defines one).
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
// fn(). The Go ABI treats every register as caller-save, so nothing else
// needs preserving (the C version saves no FP registers either).
// Entered only from threadentry, so SP is already 16-byte aligned at the
// BL sites per the C ABI.
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

	BL	(R20)			// setg(g): setg_gcc writes the TLS slot
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

// Port of gcc_libinit_unix.c:96-102: set the flag under the mutex and
// broadcast the condition so _cgo_wait_runtime_init_done (which we never
// compile) would wake. runtime.main calls this through cgocall
// (proc.go:257) on every iscgo binary, and throws at proc.go:244-246 if
// the cell is nil, so it has to exist and has to return.
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
// (env_posix.go): [k, v] for setenv, [k] for unsetenv. Called via
// asmcgocall from setenv_c/unsetenv_c on every os.Setenv, which is what
// keeps libc's environ in sync with Go's — GTK and GDK read
// GDK_BACKEND, DISPLAY and WAYLAND_DISPLAY from it.
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
