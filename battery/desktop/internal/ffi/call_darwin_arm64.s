//go:build darwin && arm64

#include "textflag.h"

// ·callTrampoline is the C-ABI void call(callArgs*) runtime.cgocall
// invokes (via asmcgocall, on the g0 stack). It loads the integer,
// float, indirect-result, and stack arguments from the block, calls the
// target, and writes x0/x1/d0 back through the block pointer.
//
// The block lives on the heap (ffi.call's sync.Pool), so a callback
// that grows and copies the calling goroutine's stack cannot move it;
// the pointer is stashed in this frame (which lives on the g0 stack and
// also never moves) and re-read after the callee returns.
//
// Frame: declared $16-0, so the assembler emits the small-frame
// prologue (autosize 32: [SP]=saved LR, [SP-8]=saved FP) and the
// matching epilogue. The body then subtracts another 64 bytes for the
// outgoing stack-argument area, keeping SP 16-byte aligned at the BL
// (asmcgocall hands us an aligned SP and 96 is a multiple of 16).
//
// Offsets into the callArgs block (R0 on entry), pinned by the
// compile-time checks in call_darwin.go:
//
//	fn@0, r1@8, r2@16, f0@24, ret8@32, ints@40..104, flts@104..168,
//	stks@168..232, f1@232, f2@240, f3@248
//
// Frame map after the SUB (offsets from SP):
//
//	 0..63   outgoing stack arguments (stks[0..7])
//	64..71   (free)
//	72       saved block pointer
TEXT ·callTrampoline(SB), NOSPLIT, $16-0
	SUB	$64, RSP

	MOVD	R0, 72(RSP)		// block pointer across the call
	MOVD	0(R0), R9		// fn
	MOVD	32(R0), R8		// x8: indirect result location (0 = none)

	FMOVD	104(R0), F0
	FMOVD	112(R0), F1
	FMOVD	120(R0), F2
	FMOVD	128(R0), F3
	FMOVD	136(R0), F4
	FMOVD	144(R0), F5
	FMOVD	152(R0), F6
	FMOVD	160(R0), F7

	// Stack arguments beyond the eight integer registers.
	LDP	168(R0), (R1, R2)
	STP	(R1, R2), 0(RSP)
	LDP	184(R0), (R1, R2)
	STP	(R1, R2), 16(RSP)
	LDP	200(R0), (R1, R2)
	STP	(R1, R2), 32(RSP)
	LDP	216(R0), (R1, R2)
	STP	(R1, R2), 48(RSP)

	// Integer registers last: the loads consume the base register.
	LDP	88(R0), (R6, R7)	// int[6], int[7]
	LDP	72(R0), (R4, R5)	// int[4], int[5]
	LDP	56(R0), (R2, R3)	// int[2], int[3]
	MOVD	48(R0), R1		// int[1]
	MOVD	40(R0), R0		// int[0] -> x0

	BL	(R9)

	MOVD	72(RSP), R10
	MOVD	R0, 8(R10)		// r1 = x0
	MOVD	R1, 16(R10)		// r2 = x1
	FMOVD	F0, 24(R10)		// f0 = d0
	// d1-d3 as well: AAPCS64 returns a homogeneous floating-point
	// aggregate of up to four members in d0-d3, never through x8, so an
	// NSRect return arrives here and nowhere else (CallRetHFA4).
	FMOVD	F1, 232(R10)		// f1 = d1
	FMOVD	F2, 240(R10)		// f2 = d2
	FMOVD	F3, 248(R10)		// f3 = d3

	MOVD	$0, R0			// "errno" for asmcgocall; cgocall ignores it
	ADD	$64, RSP
	RET

GLOBL ·callTrampolineAddr(SB), RODATA, $8
DATA ·callTrampolineAddr(SB)/8, $·callTrampoline(SB)
