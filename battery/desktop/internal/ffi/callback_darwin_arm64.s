//go:build darwin && arm64

#include "textflag.h"

// The callback trampoline table and its shared body, the arm64 twin of
// the runtime's runtime/syscall_windows_arm64.s callbackasm1 (the one
// place the Go tree builds this exact shape for arm64).
//
// callbackasm is a table of MaxCallbacks entries, each exactly
// entrySize=8 bytes: MOVD $i, R12 (MOVZ, one instruction) followed by
// B ·callbackasm1 (one instruction). Go computes entry i's address as
// callbackasmAddr + i*8; the untagged self-test calls entries 0 and 1
// to prove the stride.
TEXT ·callbackasm(SB),NOSPLIT|NOFRAME,$0-0
	MOVD $0, R12
	B ·callbackasm1(SB)
	MOVD $1, R12
	B ·callbackasm1(SB)
	MOVD $2, R12
	B ·callbackasm1(SB)
	MOVD $3, R12
	B ·callbackasm1(SB)
	MOVD $4, R12
	B ·callbackasm1(SB)
	MOVD $5, R12
	B ·callbackasm1(SB)
	MOVD $6, R12
	B ·callbackasm1(SB)
	MOVD $7, R12
	B ·callbackasm1(SB)
	MOVD $8, R12
	B ·callbackasm1(SB)
	MOVD $9, R12
	B ·callbackasm1(SB)
	MOVD $10, R12
	B ·callbackasm1(SB)
	MOVD $11, R12
	B ·callbackasm1(SB)
	MOVD $12, R12
	B ·callbackasm1(SB)
	MOVD $13, R12
	B ·callbackasm1(SB)
	MOVD $14, R12
	B ·callbackasm1(SB)
	MOVD $15, R12
	B ·callbackasm1(SB)
	MOVD $16, R12
	B ·callbackasm1(SB)
	MOVD $17, R12
	B ·callbackasm1(SB)
	MOVD $18, R12
	B ·callbackasm1(SB)
	MOVD $19, R12
	B ·callbackasm1(SB)
	MOVD $20, R12
	B ·callbackasm1(SB)
	MOVD $21, R12
	B ·callbackasm1(SB)
	MOVD $22, R12
	B ·callbackasm1(SB)
	MOVD $23, R12
	B ·callbackasm1(SB)
	MOVD $24, R12
	B ·callbackasm1(SB)
	MOVD $25, R12
	B ·callbackasm1(SB)
	MOVD $26, R12
	B ·callbackasm1(SB)
	MOVD $27, R12
	B ·callbackasm1(SB)
	MOVD $28, R12
	B ·callbackasm1(SB)
	MOVD $29, R12
	B ·callbackasm1(SB)
	MOVD $30, R12
	B ·callbackasm1(SB)
	MOVD $31, R12
	B ·callbackasm1(SB)
	MOVD $32, R12
	B ·callbackasm1(SB)
	MOVD $33, R12
	B ·callbackasm1(SB)
	MOVD $34, R12
	B ·callbackasm1(SB)
	MOVD $35, R12
	B ·callbackasm1(SB)
	MOVD $36, R12
	B ·callbackasm1(SB)
	MOVD $37, R12
	B ·callbackasm1(SB)
	MOVD $38, R12
	B ·callbackasm1(SB)
	MOVD $39, R12
	B ·callbackasm1(SB)
	MOVD $40, R12
	B ·callbackasm1(SB)
	MOVD $41, R12
	B ·callbackasm1(SB)
	MOVD $42, R12
	B ·callbackasm1(SB)
	MOVD $43, R12
	B ·callbackasm1(SB)
	MOVD $44, R12
	B ·callbackasm1(SB)
	MOVD $45, R12
	B ·callbackasm1(SB)
	MOVD $46, R12
	B ·callbackasm1(SB)
	MOVD $47, R12
	B ·callbackasm1(SB)
	MOVD $48, R12
	B ·callbackasm1(SB)
	MOVD $49, R12
	B ·callbackasm1(SB)
	MOVD $50, R12
	B ·callbackasm1(SB)
	MOVD $51, R12
	B ·callbackasm1(SB)
	MOVD $52, R12
	B ·callbackasm1(SB)
	MOVD $53, R12
	B ·callbackasm1(SB)
	MOVD $54, R12
	B ·callbackasm1(SB)
	MOVD $55, R12
	B ·callbackasm1(SB)
	MOVD $56, R12
	B ·callbackasm1(SB)
	MOVD $57, R12
	B ·callbackasm1(SB)
	MOVD $58, R12
	B ·callbackasm1(SB)
	MOVD $59, R12
	B ·callbackasm1(SB)
	MOVD $60, R12
	B ·callbackasm1(SB)
	MOVD $61, R12
	B ·callbackasm1(SB)
	MOVD $62, R12
	B ·callbackasm1(SB)
	MOVD $63, R12
	B ·callbackasm1(SB)

GLOBL ·callbackasmAddr(SB), RODATA, $8
DATA ·callbackasmAddr(SB)/8, $·callbackasm(SB)

// callbackasm1 is entered by C (objc_msgSend dispatching an IMP, a
// block invoke, dispatch_async_f) with the slot index in R12. It is
// reached only while a Go-owned M sits inside a runtime.cgocall call
// (ffi.Call), which is what runtime.cgocallback requires: it switches
// onto that M's parked goroutine, runs callbackWrap, and returns here.
//
// Frame map (TEXT $368-0; the assembler's prologue makes autosize 384
// and leaves [SP-8]=saved FP, [SP+0]=saved LR, user area [SP+8,384)):
//
//	  8..87   scratch: outgoing ABI0 args for runtime·cgocallback
//	              (callee's arg0 lands at 8(RSP): fn@8, frame@16, ctxt@24;
//	               [SP+0] stays the live saved-LR slot)
//	 88..167  R19-R28, callee-saved in the C ABI (R28 is also g)
//	168..231  F8-F15, callee-saved in the C ABI
//	232..375  callbackArgs: index@232, Args.Int@240..303,
//	          Args.Float@304..367, result@368
TEXT ·callbackasm1(SB),NOSPLIT,$0-0
	// The declared frame is $0 so the assembler emits the small-frame
	// prologue (MOVD.W R30, -16(SP)), which clobbers no register. A
	// declared frame over 240 bytes would switch to the large-frame
	// prologue, whose SUB $autosize, RSP, R20 scratch use destroys the
	// incoming R20 — a C-callee-saved register the runtime's own
	// syscallN_trampoline keeps its stack pointer in. The body extends
	// the frame manually instead (the runtime's amd64 callbackasm1 does
	// the same with SUBQ), by 336 bytes on top of the 16 the prologue
	// owns: 352 total, 16-byte aligned throughout.
	//
	// Frame map, offsets from SP after the SUB (the saved LR sits at
	// 336(RSP); [SP-8] holds the saved FP for the epilogue):
	//
	//	  8..31   outgoing ABI0 args for runtime·cgocallback:
	//	          fn@8, frame@16, ctxt@24 (the callee reads fn+0(FP)
	//	          at its SP_entry+8; [0(RSP)] is the reserved slot)
	//	 32..111  R19-R28, callee-saved in the C ABI (R28 is also g)
	//	112..175  F8-F15, callee-saved in the C ABI
	//	176..319  callbackArgs: index@176, Args.Int@184..247,
	//	          Args.Float@248..311, result@312
	SUB	$336, RSP

	// Spill the incoming C argument registers.
	MOVD	$184(RSP), R14
	STP	(R0, R1), (0*8)(R14)
	STP	(R2, R3), (2*8)(R14)
	STP	(R4, R5), (4*8)(R14)
	STP	(R6, R7), (6*8)(R14)
	FSTPD	(F0, F1), 248(RSP)
	FSTPD	(F2, F3), 264(RSP)
	FSTPD	(F4, F5), 280(RSP)
	FSTPD	(F6, F7), 296(RSP)

	// Save the C callee-saved registers the Go code ahead may clobber.
	STP	(R19, R20), 32(RSP)
	STP	(R21, R22), 48(RSP)
	STP	(R23, R24), 64(RSP)
	STP	(R25, R26), 80(RSP)
	STP	(R27, g), 96(RSP)
	FSTPD	(F8, F9), 112(RSP)
	FSTPD	(F10, F11), 128(RSP)
	FSTPD	(F12, F13), 144(RSP)
	FSTPD	(F14, F15), 160(RSP)

	// Build callbackArgs in the frame.
	MOVD	R12, 176(RSP)		// index
	MOVD	$0xdeadbeef, R13
	MOVD	R13, 312(RSP)		// result (magic for debugging)

	// Call runtime·cgocallback(fn, frame, ctxt). fn is the ABIInternal
	// entry point of callbackWrap, loaded from its func value's code
	// word (the assembler rejects the <ABIInternal> symbol suffix
	// outside package runtime).
	MOVD	·callbackWrapABI(SB), R0
	MOVD	$176(RSP), R1
	MOVD	$0, R2
	STP	(R0, R1), 8(RSP)
	MOVD	R2, 24(RSP)
	BL	runtime·cgocallback(SB)

	// Forward the result to the C caller in x0, and mirror the bits in
	// d0 for signatures that return a float.
	//
	// History: on the syscall9/libcCall entry path the FIRST callback
	// on a given M could read this slot back as zero (binary-layout
	// dependent) even though callbackWrap stored the return value, so
	// callers burned warm-up calls. On the cgocall path it does not
	// reproduce: ffi.TestFirstCallbackReturnOnFreshM pins the first
	// callback on 16 fresh Ms, and the qsort GC regression asserts all
	// ~50k comparator returns. The likely mechanism: callbackWrap now
	// runs with the M balanced in cgocall's entersyscall/exitsyscall
	// and incgo accounting, so the first-entry path
	// (callbackUpdateSystemStack, exitsyscall slow path, newextram)
	// never observes a half-entered C call.
	MOVD	312(RSP), R0
	MOVD	R0, 8(RSP)
	FMOVD	8(RSP), F0

	// Restore the C callee-saved registers and give back the manual
	// frame before RET (the epilogue pops LR from [SP+0]).
	LDP	32(RSP), (R19, R20)
	LDP	48(RSP), (R21, R22)
	LDP	64(RSP), (R23, R24)
	LDP	80(RSP), (R25, R26)
	LDP	96(RSP), (R27, g)
	FLDPD	112(RSP), (F8, F9)
	FLDPD	128(RSP), (F10, F11)
	FLDPD	144(RSP), (F12, F13)
	FLDPD	160(RSP), (F14, F15)
	ADD	$336, RSP

	RET
