//go:build darwin && amd64

#include "textflag.h"

// Address-bearing trampolines for the two libc functions the fakecgo
// self-test needs to call through internal/ffi (getpid, getenv), the
// twin of libc_darwin_arm64.s. A //go:linkname'd byte var does NOT bind
// to a cgo_import_dynamic symbol — without a host object the compiler
// materializes it as a Go data byte — so the address has to come from
// assembly, the same shape golang.org/x/sys uses in
// zsyscall_darwin_amd64.s.
//
// The trampolines contain no CALL, so the assembler adds no base-pointer
// prologue (cmd/internal/obj/x86/obj6.go:636-651) and the JMP is a true
// tail jump that leaves the caller's arguments and stack alignment
// exactly as they arrived.

TEXT libc_getpid_trampoline<>(SB),NOSPLIT,$0-0
	JMP	libc_getpid(SB)
GLOBL ·libc_getpid_trampoline_addr(SB), RODATA, $8
DATA ·libc_getpid_trampoline_addr(SB)/8, $libc_getpid_trampoline<>(SB)

TEXT libc_getenv_trampoline<>(SB),NOSPLIT,$0-0
	JMP	libc_getenv(SB)
GLOBL ·libc_getenv_trampoline_addr(SB), RODATA, $8
DATA ·libc_getenv_trampoline_addr(SB)/8, $libc_getenv_trampoline<>(SB)
