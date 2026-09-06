//go:build linux && amd64

#include "textflag.h"

// ·callTrampoline is the C-ABI void call(callArgs*) runtime.cgocall
// invokes (via asmcgocall, on the g0 stack). It loads the stack, float
// and integer arguments from the block, calls the target, and writes
// AX/DX/X0 back through the block pointer.
//
// The block lives on the heap (ffi.call's sync.Pool), so a callback that
// grows and copies the calling goroutine's stack cannot move it; the
// pointer is stashed in this frame (which lives on the g0 stack and also
// never moves) and re-read after the callee returns.
//
// Offsets into the callArgs block (DI on entry), pinned by the
// compile-time checks in call_linux_amd64.go:
//
//	fn@0, r1@8, r2@16, f0@24, ret8@32 (unused here), ints@40..104,
//	flts@104..168, stks@168..232
//
// Frame $80, locals 0..79:
//
//	 0..63   outgoing stack arguments — System V puts them at 0(RSP) at
//	         the moment of the CALL, one full 8-byte slot each
//	64..71   saved block pointer
//
// asmcgocall hands us a 16-byte-aligned RSP (asm_amd64.s:964-965) and a
// declared frame that is a multiple of 16 keeps it that way at the CALL,
// which this ABI requires and arm64 does not.
//
// R10 and R11 carry the block pointer and the target address up to the
// CALL: both are caller-saved in this ABI, so nothing is owed back, and
// every argument register is already spoken for.
TEXT ·callTrampoline(SB), NOSPLIT, $80-0
	MOVQ	DI, 64(SP)		// block pointer across the call
	MOVQ	DI, R11
	MOVQ	0(R11), R10		// fn

	// Stack arguments beyond the six integer registers.
	MOVQ	168(R11), AX
	MOVQ	AX, 0(SP)
	MOVQ	176(R11), AX
	MOVQ	AX, 8(SP)
	MOVQ	184(R11), AX
	MOVQ	AX, 16(SP)
	MOVQ	192(R11), AX
	MOVQ	AX, 24(SP)
	MOVQ	200(R11), AX
	MOVQ	AX, 32(SP)
	MOVQ	208(R11), AX
	MOVQ	AX, 40(SP)
	MOVQ	216(R11), AX
	MOVQ	AX, 48(SP)
	MOVQ	224(R11), AX
	MOVQ	AX, 56(SP)

	MOVSD	104(R11), X0
	MOVSD	112(R11), X1
	MOVSD	120(R11), X2
	MOVSD	128(R11), X3
	MOVSD	136(R11), X4
	MOVSD	144(R11), X5
	MOVSD	152(R11), X6
	MOVSD	160(R11), X7

	MOVQ	40(R11), DI		// int[0]
	MOVQ	48(R11), SI		// int[1]
	MOVQ	56(R11), DX		// int[2]
	MOVQ	64(R11), CX		// int[3]
	MOVQ	72(R11), R8		// int[4]
	MOVQ	80(R11), R9		// int[5]

	// AL is the count of vector registers a variadic callee must spill
	// to its register save area. Eight is always a safe upper bound and
	// a non-variadic callee ignores it.
	MOVQ	$8, AX
	CALL	R10

	MOVQ	64(SP), R11
	MOVQ	AX, 8(R11)		// r1 = AX
	MOVQ	DX, 16(R11)		// r2 = DX
	MOVSD	X0, 24(R11)		// f0 = X0

	MOVQ	$0, AX			// "errno" for asmcgocall; cgocall ignores it
	RET

GLOBL ·callTrampolineAddr(SB), RODATA, $8
DATA ·callTrampolineAddr(SB)/8, $·callTrampoline(SB)
