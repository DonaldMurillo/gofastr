//go:build linux && amd64

package ffi

// entrySize is the byte length of one callbackasm table entry on amd64:
// MOVL $i, R10 (REX.B prefix, opcode B8+r, imm32 — six bytes, and the
// imm32 form is used for every index, so entry 0 is the same length as
// entry 63) plus JMP ·callbackasm1(SB) (E9 plus rel32, five bytes).
// See callback_linux_amd64.s.
const entrySize = 11
