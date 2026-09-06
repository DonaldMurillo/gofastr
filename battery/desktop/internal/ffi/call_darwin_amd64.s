//go:build darwin && amd64

#include "textflag.h"

// ·callTrampoline is the C-ABI void call(callArgs*) runtime.cgocall
// invokes (via asmcgocall, on the g0 stack, with the block pointer in
// DI — runtime/asm_amd64.s:914 "DI = first argument in AMD64 ABI"). It
// loads the integer, float and stack arguments from the block, calls the
// target, and writes rax/rdx/xmm0 back through the block pointer.
//
// The block lives on the heap (ffi.call's sync.Pool), so a callback that
// grows and copies the calling goroutine's stack cannot move it; the
// pointer is stashed in this frame (which lives on the g0 stack and also
// never moves) and re-read after the callee returns.
//
// Frame: declared $80, and because the body contains a CALL the
// assembler adds an 8-byte base-pointer save on top
// (cmd/internal/obj/x86/obj6.go:636-651), so the prologue is
// PUSHQ BP / MOVQ SP, BP / SUBQ $80, SP and SP moves by 88. asmcgocall
// hands us a 16-byte-aligned SP and then CALLs, so SP ≡ 8 (mod 16) on
// entry and ≡ 0 after the prologue — the System V requirement at the
// CALL below ("(%rsp + 8) is a multiple of 16 at the callee's entry
// point"). Any declared frame size that is a multiple of 16 preserves
// that; 80 is the smallest one that holds the layout.
//
// Offsets into the callArgs block (DI on entry), pinned by the
// compile-time checks in call_darwin_amd64.go:
//
//	fn@0, r1@8, r2@16, f0@24, nfloat@32, ints@40..88,
//	flts@88..152, stks@152..216
//
// Frame map (offsets from SP after the prologue):
//
//	 0..63   outgoing C stack arguments (stks[0..7])
//	64..71   saved block pointer
//	72..79   scratch
//
// Registers: only the System V caller-saved set is touched (AX, CX, DX,
// SI, DI, R8-R11). BX, BP, R12-R15 belong to the C caller and the
// prologue/epilogue own BP.
TEXT ·callTrampoline(SB), NOSPLIT, $80-0
	MOVQ	DI, 64(SP)		// block pointer across the call
	MOVQ	0(DI), R11		// fn
	MOVQ	32(DI), AX		// nfloat -> al: System V's vector-register
					// count for a variadic callee (clang emits
					// the same "movb $2, %al" before a call
					// passing two doubles to a variadic
					// function); ignored by every other callee.

	// The outgoing stack argument area. Written before the argument
	// registers are loaded, while DI still points at the block.
	MOVQ	152(DI), R10
	MOVQ	R10, 0(SP)
	MOVQ	160(DI), R10
	MOVQ	R10, 8(SP)
	MOVQ	168(DI), R10
	MOVQ	R10, 16(SP)
	MOVQ	176(DI), R10
	MOVQ	R10, 24(SP)
	MOVQ	184(DI), R10
	MOVQ	R10, 32(SP)
	MOVQ	192(DI), R10
	MOVQ	R10, 40(SP)
	MOVQ	200(DI), R10
	MOVQ	R10, 48(SP)
	MOVQ	208(DI), R10
	MOVQ	R10, 56(SP)

	MOVSD	88(DI), X0
	MOVSD	96(DI), X1
	MOVSD	104(DI), X2
	MOVSD	112(DI), X3
	MOVSD	120(DI), X4
	MOVSD	128(DI), X5
	MOVSD	136(DI), X6
	MOVSD	144(DI), X7

	// Integer registers last: the loads consume the base register.
	MOVQ	80(DI), R9
	MOVQ	72(DI), R8
	MOVQ	64(DI), CX
	MOVQ	56(DI), DX
	MOVQ	48(DI), SI
	MOVQ	40(DI), DI

	CALL	R11

	MOVQ	64(SP), R10
	MOVQ	AX, 8(R10)		// r1 = rax
	MOVQ	DX, 16(R10)		// r2 = rdx
	MOVSD	X0, 24(R10)		// f0 = xmm0

	XORL	AX, AX			// "errno" for asmcgocall; cgocall ignores it
	RET

GLOBL ·callTrampolineAddr(SB), RODATA, $8
DATA ·callTrampolineAddr(SB)/8, $·callTrampoline(SB)
