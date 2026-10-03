//go:build linux && arm64

#include "textflag.h"

// Address-bearing trampolines for the two libc functions the fakecgo
// self-test calls through internal/ffi (getpid, getenv). A
// //go:linkname'd byte var does NOT bind to a cgo_import_dynamic symbol
// — without a host object the compiler materializes it as a Go data
// byte — so the address has to come from assembly. On ELF the JMP lands
// on the PLT stub cmd/link builds for an SDYNIMPORT symbol
// (cmd/link/internal/arm64/asm.go:1185-1224), which clobbers x16 and
// x17; nothing here or in fakecgo_linux_arm64.s keeps a live value in
// either across a call.

TEXT libc_getpid_trampoline<>(SB),NOSPLIT|NOFRAME,$0-0
	JMP	libc_getpid(SB)
GLOBL ·libc_getpid_trampoline_addr(SB), RODATA, $8
DATA ·libc_getpid_trampoline_addr(SB)/8, $libc_getpid_trampoline<>(SB)

TEXT libc_getenv_trampoline<>(SB),NOSPLIT|NOFRAME,$0-0
	JMP	libc_getenv(SB)
GLOBL ·libc_getenv_trampoline_addr(SB), RODATA, $8
DATA ·libc_getenv_trampoline_addr(SB)/8, $libc_getenv_trampoline<>(SB)
