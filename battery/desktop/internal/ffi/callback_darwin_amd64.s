//go:build darwin && amd64

#include "textflag.h"

// The callback trampoline table and its shared body, the amd64 twin of
// callback_darwin_arm64.s and a direct port of the one place the Go tree
// builds this exact shape for amd64: runtime/zcallback_windows.s (the
// table) and runtime/sys_windows_amd64.s:112-176 (the body), with the
// Windows register set swapped for System V and the Windows halves of
// runtime/cgo/abi_amd64.h's PUSH/POP_REGS_HOST_TO_ABI0 swapped for the
// SysV halves (abi_amd64.h:74-97: BX, R12-R15 and BP, no XMM, because
// System V makes every XMM register caller-saved).
//
// callbackasm is a table of MaxCallbacks entries, each exactly
// entrySize=5 bytes: one CALL ·callbackasm1(SB). Go computes entry i's
// address as callbackasmAddr + i*5 and hands that to C as a function
// pointer; callbackasm1 recovers i from the return address the CALL
// pushed. The untagged self-test calls entries 0 and 1 to prove the
// stride, and the exhaustion test registers all 64.
TEXT ·callbackasm(SB),NOSPLIT|NOFRAME,$0-0
	CALL	·callbackasm1(SB)
	CALL	·callbackasm1(SB)
	CALL	·callbackasm1(SB)
	CALL	·callbackasm1(SB)
	CALL	·callbackasm1(SB)
	CALL	·callbackasm1(SB)
	CALL	·callbackasm1(SB)
	CALL	·callbackasm1(SB)
	CALL	·callbackasm1(SB)
	CALL	·callbackasm1(SB)
	CALL	·callbackasm1(SB)
	CALL	·callbackasm1(SB)
	CALL	·callbackasm1(SB)
	CALL	·callbackasm1(SB)
	CALL	·callbackasm1(SB)
	CALL	·callbackasm1(SB)
	CALL	·callbackasm1(SB)
	CALL	·callbackasm1(SB)
	CALL	·callbackasm1(SB)
	CALL	·callbackasm1(SB)
	CALL	·callbackasm1(SB)
	CALL	·callbackasm1(SB)
	CALL	·callbackasm1(SB)
	CALL	·callbackasm1(SB)
	CALL	·callbackasm1(SB)
	CALL	·callbackasm1(SB)
	CALL	·callbackasm1(SB)
	CALL	·callbackasm1(SB)
	CALL	·callbackasm1(SB)
	CALL	·callbackasm1(SB)
	CALL	·callbackasm1(SB)
	CALL	·callbackasm1(SB)
	CALL	·callbackasm1(SB)
	CALL	·callbackasm1(SB)
	CALL	·callbackasm1(SB)
	CALL	·callbackasm1(SB)
	CALL	·callbackasm1(SB)
	CALL	·callbackasm1(SB)
	CALL	·callbackasm1(SB)
	CALL	·callbackasm1(SB)
	CALL	·callbackasm1(SB)
	CALL	·callbackasm1(SB)
	CALL	·callbackasm1(SB)
	CALL	·callbackasm1(SB)
	CALL	·callbackasm1(SB)
	CALL	·callbackasm1(SB)
	CALL	·callbackasm1(SB)
	CALL	·callbackasm1(SB)
	CALL	·callbackasm1(SB)
	CALL	·callbackasm1(SB)
	CALL	·callbackasm1(SB)
	CALL	·callbackasm1(SB)
	CALL	·callbackasm1(SB)
	CALL	·callbackasm1(SB)
	CALL	·callbackasm1(SB)
	CALL	·callbackasm1(SB)
	CALL	·callbackasm1(SB)
	CALL	·callbackasm1(SB)
	CALL	·callbackasm1(SB)
	CALL	·callbackasm1(SB)
	CALL	·callbackasm1(SB)
	CALL	·callbackasm1(SB)
	CALL	·callbackasm1(SB)
	CALL	·callbackasm1(SB)

GLOBL ·callbackasmAddr(SB), RODATA, $8
DATA ·callbackasmAddr(SB)/8, $·callbackasm(SB)

// callbackasm1 is entered from a table entry's CALL, which C reached
// through an IMP, a block invoke, or a dispatch function pointer. It is
// reached only while a Go-owned M sits inside a runtime.cgocall call
// (ffi.Call), which is what runtime.cgocallback requires: it switches
// onto that M's parked goroutine, runs callbackWrap, and returns here.
//
// Stack on entry (E = SP, ≡ 0 mod 16 because C called the table entry
// with SP ≡ 0 and each of the two CALLs pushed eight bytes):
//
//	 0(SP)  return address into the table entry — the slot number
//	 8(SP)  return address into C
//	16(SP)  the caller's first stack argument (integer argument 7)
//	24(SP)  the second (integer argument 8)
//
// The table return address is lifted out and dropped straight away, so
// the final RET goes back to C rather than into the middle of the table
// (runtime/sys_windows_amd64.s:126-128 does the same). After that SP ≡ 8
// and the $232 frame — which is 8 mod 16 — restores SP ≡ 0 for the CALL
// to runtime·cgocallback, System V's requirement at a call site.
//
// Frame map, offsets from SP after the ADJSP (the C return address then
// sits at 232(SP), the caller's stack arguments at 240 and 248):
//
//	  0..23   outgoing ABI0 arguments for runtime·cgocallback:
//	          fn@0, frame@8, ctxt@16 (runtime/asm_amd64.s:1014 declares
//	          it $24-24, so the callee reads fn+0(FP) at our 0(SP))
//	 24..31   scratch, for bouncing the result into xmm0
//	 32..175  callbackArgs: index@32, Args.Int@40..103,
//	          Args.Float@104..167, result@168
//	176..215  BX, R12, R13, R14, R15: callee-saved in System V and
//	          caller-saved in the Go ABI
//	224..231  saved BP, deliberately the topmost word of the frame so
//	          that [BP] is the caller's BP and [BP+8] is the return
//	          address into C, which keeps the frame-pointer chain a
//	          traceback can walk (abi_amd64.h:82-83 places it the same
//	          way, for the same reason)
TEXT ·callbackasm1(SB),NOSPLIT|NOFRAME,$0-0
	MOVQ	0(SP), R10		// return address into the table entry
	ADDQ	$8, SP			// drop it: our RET goes straight to C
	ADJSP	$232

	// Spill the incoming C argument registers into callbackArgs.args.
	// Integer arguments seven and eight are not in registers on System
	// V; they are the first two words of the caller's stack argument
	// area, so Args.Int keeps meaning "the first eight integer
	// arguments" on both architectures.
	MOVQ	DI, 40(SP)
	MOVQ	SI, 48(SP)
	MOVQ	DX, 56(SP)
	MOVQ	CX, 64(SP)
	MOVQ	R8, 72(SP)
	MOVQ	R9, 80(SP)
	MOVQ	240(SP), R11
	MOVQ	R11, 88(SP)
	MOVQ	248(SP), R11
	MOVQ	R11, 96(SP)
	MOVSD	X0, 104(SP)
	MOVSD	X1, 112(SP)
	MOVSD	X2, 120(SP)
	MOVSD	X3, 128(SP)
	MOVSD	X4, 136(SP)
	MOVSD	X5, 144(SP)
	MOVSD	X6, 152(SP)
	MOVSD	X7, 160(SP)

	// Save the C callee-saved registers the Go code ahead may clobber.
	MOVQ	BX, 176(SP)
	MOVQ	R12, 184(SP)
	MOVQ	R13, 192(SP)
	MOVQ	R14, 200(SP)
	MOVQ	R15, 208(SP)
	MOVQ	BP, 224(SP)
	LEAQ	224(SP), BP

	// index = (table return address - callbackasm)/entrySize - 1; the
	// minus one because the return address points past the entry that
	// called us.
	MOVQ	R10, AX
	MOVQ	$·callbackasm(SB), DX
	SUBQ	DX, AX
	MOVQ	$0, DX
	MOVQ	$5, CX
	DIVQ	CX
	SUBQ	$1, AX
	MOVQ	AX, 32(SP)		// callbackArgs.index

	MOVQ	$0xdeadbeef, R11
	MOVQ	R11, 168(SP)		// result (magic for debugging)

	// Call runtime·cgocallback(fn, frame, ctxt). fn is the ABIInternal
	// entry point of callbackWrap, loaded from its func value's code
	// word (the assembler rejects the <ABIInternal> symbol suffix
	// outside package runtime).
	MOVQ	·callbackWrapABI(SB), AX
	MOVQ	AX, 0(SP)
	LEAQ	32(SP), AX
	MOVQ	AX, 8(SP)
	MOVQ	$0, 16(SP)
	CALL	runtime·cgocallback(SB)

	// Forward the result to the C caller in rax, and mirror the bits in
	// xmm0 for signatures that return a float.
	MOVQ	168(SP), AX
	MOVQ	AX, 24(SP)
	MOVSD	24(SP), X0

	MOVQ	176(SP), BX
	MOVQ	184(SP), R12
	MOVQ	192(SP), R13
	MOVQ	200(SP), R14
	MOVQ	208(SP), R15
	MOVQ	224(SP), BP
	ADJSP	$-232

	RET
