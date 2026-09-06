//go:build linux && amd64

#include "textflag.h"

// Address-bearing trampolines for the two libc functions the fakecgo
// self-test calls through internal/ffi (getpid, getenv). The address of
// a cgo_import_dynamic symbol must originate in assembly: a
// //go:linkname'd byte var does not bind without a host object.

TEXT libc_getpid_trampoline<>(SB),NOSPLIT|NOFRAME,$0-0
	JMP	libc_getpid(SB)
GLOBL ·libc_getpid_trampoline_addr(SB), RODATA, $8
DATA ·libc_getpid_trampoline_addr(SB)/8, $libc_getpid_trampoline<>(SB)

TEXT libc_getenv_trampoline<>(SB),NOSPLIT|NOFRAME,$0-0
	JMP	libc_getenv(SB)
GLOBL ·libc_getenv_trampoline_addr(SB), RODATA, $8
DATA ·libc_getenv_trampoline_addr(SB)/8, $libc_getenv_trampoline<>(SB)
