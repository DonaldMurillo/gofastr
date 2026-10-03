//go:build linux && amd64

#include "textflag.h"

// The callback trampoline table and its shared body for linux/amd64, the
// System V twin of callback_linux_arm64.s.
//
// callbackasm is a table of MaxCallbacks entries, each exactly
// entrySize=11 bytes: MOVL $i, R10 (REX.B + B8+r + imm32, six bytes)
// followed by JMP ·callbackasm1(SB) (E9 + rel32, five bytes). Go
// computes entry i's address as callbackasmAddr + i*11, and the
// self-test asserts the stride against two live entries rather than
// trusting the arithmetic.
//
// R10 carries the index, not arm64's R12: on this ABI R12 is
// callee-saved, so writing the index there before jumping into the body
// would destroy a register the C caller owns. R10 is the static chain
// pointer, caller-saved and unused by anything here.
TEXT ·callbackasm(SB),NOSPLIT|NOFRAME,$0-0
	MOVL $0, R10
	JMP ·callbackasm1(SB)
	MOVL $1, R10
	JMP ·callbackasm1(SB)
	MOVL $2, R10
	JMP ·callbackasm1(SB)
	MOVL $3, R10
	JMP ·callbackasm1(SB)
	MOVL $4, R10
	JMP ·callbackasm1(SB)
	MOVL $5, R10
	JMP ·callbackasm1(SB)
	MOVL $6, R10
	JMP ·callbackasm1(SB)
	MOVL $7, R10
	JMP ·callbackasm1(SB)
	MOVL $8, R10
	JMP ·callbackasm1(SB)
	MOVL $9, R10
	JMP ·callbackasm1(SB)
	MOVL $10, R10
	JMP ·callbackasm1(SB)
	MOVL $11, R10
	JMP ·callbackasm1(SB)
	MOVL $12, R10
	JMP ·callbackasm1(SB)
	MOVL $13, R10
	JMP ·callbackasm1(SB)
	MOVL $14, R10
	JMP ·callbackasm1(SB)
	MOVL $15, R10
	JMP ·callbackasm1(SB)
	MOVL $16, R10
	JMP ·callbackasm1(SB)
	MOVL $17, R10
	JMP ·callbackasm1(SB)
	MOVL $18, R10
	JMP ·callbackasm1(SB)
	MOVL $19, R10
	JMP ·callbackasm1(SB)
	MOVL $20, R10
	JMP ·callbackasm1(SB)
	MOVL $21, R10
	JMP ·callbackasm1(SB)
	MOVL $22, R10
	JMP ·callbackasm1(SB)
	MOVL $23, R10
	JMP ·callbackasm1(SB)
	MOVL $24, R10
	JMP ·callbackasm1(SB)
	MOVL $25, R10
	JMP ·callbackasm1(SB)
	MOVL $26, R10
	JMP ·callbackasm1(SB)
	MOVL $27, R10
	JMP ·callbackasm1(SB)
	MOVL $28, R10
	JMP ·callbackasm1(SB)
	MOVL $29, R10
	JMP ·callbackasm1(SB)
	MOVL $30, R10
	JMP ·callbackasm1(SB)
	MOVL $31, R10
	JMP ·callbackasm1(SB)
	MOVL $32, R10
	JMP ·callbackasm1(SB)
	MOVL $33, R10
	JMP ·callbackasm1(SB)
	MOVL $34, R10
	JMP ·callbackasm1(SB)
	MOVL $35, R10
	JMP ·callbackasm1(SB)
	MOVL $36, R10
	JMP ·callbackasm1(SB)
	MOVL $37, R10
	JMP ·callbackasm1(SB)
	MOVL $38, R10
	JMP ·callbackasm1(SB)
	MOVL $39, R10
	JMP ·callbackasm1(SB)
	MOVL $40, R10
	JMP ·callbackasm1(SB)
	MOVL $41, R10
	JMP ·callbackasm1(SB)
	MOVL $42, R10
	JMP ·callbackasm1(SB)
	MOVL $43, R10
	JMP ·callbackasm1(SB)
	MOVL $44, R10
	JMP ·callbackasm1(SB)
	MOVL $45, R10
	JMP ·callbackasm1(SB)
	MOVL $46, R10
	JMP ·callbackasm1(SB)
	MOVL $47, R10
	JMP ·callbackasm1(SB)
	MOVL $48, R10
	JMP ·callbackasm1(SB)
	MOVL $49, R10
	JMP ·callbackasm1(SB)
	MOVL $50, R10
	JMP ·callbackasm1(SB)
	MOVL $51, R10
	JMP ·callbackasm1(SB)
	MOVL $52, R10
	JMP ·callbackasm1(SB)
	MOVL $53, R10
	JMP ·callbackasm1(SB)
	MOVL $54, R10
	JMP ·callbackasm1(SB)
	MOVL $55, R10
	JMP ·callbackasm1(SB)
	MOVL $56, R10
	JMP ·callbackasm1(SB)
	MOVL $57, R10
	JMP ·callbackasm1(SB)
	MOVL $58, R10
	JMP ·callbackasm1(SB)
	MOVL $59, R10
	JMP ·callbackasm1(SB)
	MOVL $60, R10
	JMP ·callbackasm1(SB)
	MOVL $61, R10
	JMP ·callbackasm1(SB)
	MOVL $62, R10
	JMP ·callbackasm1(SB)
	MOVL $63, R10
	JMP ·callbackasm1(SB)

GLOBL ·callbackasmAddr(SB), RODATA, $8
DATA ·callbackasmAddr(SB)/8, $·callbackasm(SB)

// callbackasm1 is entered by C (a GObject signal closure, a pthread
// start routine, a GIO async completion) with the slot index in R10.
// runtime.cgocallback borrows an M through needm when the calling thread
// is not one Go created, switches onto a goroutine stack, runs
// callbackWrap, and returns here.
//
// The entry stub is a JMP, not a CALL, so RSP is still 8 mod 16 here,
// the ordinary C entry alignment, and the declared frame is a multiple
// of 16 to keep it that way at the CALL into Go.
//
// Frame $224, locals 0..223 (the assembler saves BP at 224(SP) and the
// epilogue restores it):
//
//	  0..23   outgoing ABI0 args for runtime·cgocallback:
//	          fn@0, frame@8, ctxt@16 (amd64 ABI0 puts arg0 at 0(SP);
//	          the CALL pushes the return address below it)
//	 24..63   BX, R12, R13, R14, R15 — callee-saved in the C ABI. BP is
//	          the prologue's job; R14 is on the list because it is also
//	          Go's g register, and the Go call below will overwrite it.
//	 80..223  callbackArgs: index@80, Args.Int@88..151,
//	          Args.Float@152..215, result@216
TEXT ·callbackasm1(SB),NOSPLIT,$224-0
	// Spill the incoming C argument registers into Args.Int. Only six
	// integer registers exist on this ABI, so Int[6] and Int[7] stay
	// zero; a callback signature that needs a seventh integer argument
	// would have to read the C stack, and none in the desktop host does.
	MOVQ	DI, 88(SP)
	MOVQ	SI, 96(SP)
	MOVQ	DX, 104(SP)
	MOVQ	CX, 112(SP)
	MOVQ	R8, 120(SP)
	MOVQ	R9, 128(SP)
	MOVQ	$0, 136(SP)
	MOVQ	$0, 144(SP)

	MOVSD	X0, 152(SP)
	MOVSD	X1, 160(SP)
	MOVSD	X2, 168(SP)
	MOVSD	X3, 176(SP)
	MOVSD	X4, 184(SP)
	MOVSD	X5, 192(SP)
	MOVSD	X6, 200(SP)
	MOVSD	X7, 208(SP)

	// Save the C callee-saved registers the Go code ahead may clobber.
	MOVQ	BX, 24(SP)
	MOVQ	R12, 32(SP)
	MOVQ	R13, 40(SP)
	MOVQ	R14, 48(SP)
	MOVQ	R15, 56(SP)

	// Build callbackArgs in the frame.
	MOVQ	R10, 80(SP)		// index
	MOVQ	$0xdeadbeef, AX
	MOVQ	AX, 216(SP)		// result (magic for debugging)

	// Call runtime·cgocallback(fn, frame, ctxt). fn is the ABIInternal
	// entry point of callbackWrap, loaded from its func value's code
	// word (the assembler rejects the <ABIInternal> symbol suffix
	// outside package runtime).
	MOVQ	·callbackWrapABI(SB), AX
	MOVQ	AX, 0(SP)
	LEAQ	80(SP), AX
	MOVQ	AX, 8(SP)
	MOVQ	$0, 16(SP)
	CALL	runtime·cgocallback(SB)

	// Forward the result to the C caller in AX, and mirror the bits in
	// X0 for signatures that return a float. A pthread start routine
	// reads AX as its void* return, which is why the foreign-thread test
	// can assert on it.
	MOVQ	216(SP), AX
	MOVQ	AX, X0

	MOVQ	24(SP), BX
	MOVQ	32(SP), R12
	MOVQ	40(SP), R13
	MOVQ	48(SP), R14
	MOVQ	56(SP), R15
	RET
