//go:build linux && amd64

#include "textflag.h"

// Address-bearing trampolines for the libc functions the package's own
// tests call through Call. Same shape as the arm64 sibling: a hidden JMP
// per symbol plus a RODATA word holding its address, because a
// //go:linkname'd byte var does not bind to a cgo_import_dynamic symbol
// without a host object.

TEXT libc_qsort_trampoline<>(SB),NOSPLIT|NOFRAME,$0-0
	JMP	libc_qsort(SB)
GLOBL ·libc_qsort_trampoline_addr(SB), RODATA, $8
DATA ·libc_qsort_trampoline_addr(SB)/8, $libc_qsort_trampoline<>(SB)

TEXT libc_pthread_create_trampoline<>(SB),NOSPLIT|NOFRAME,$0-0
	JMP	libc_pthread_create(SB)
GLOBL ·libc_pthread_create_trampoline_addr(SB), RODATA, $8
DATA ·libc_pthread_create_trampoline_addr(SB)/8, $libc_pthread_create_trampoline<>(SB)

TEXT libc_pthread_join_trampoline<>(SB),NOSPLIT|NOFRAME,$0-0
	JMP	libc_pthread_join(SB)
GLOBL ·libc_pthread_join_trampoline_addr(SB), RODATA, $8
DATA ·libc_pthread_join_trampoline_addr(SB)/8, $libc_pthread_join_trampoline<>(SB)

TEXT libc_pthread_self_trampoline<>(SB),NOSPLIT|NOFRAME,$0-0
	JMP	libc_pthread_self(SB)
GLOBL ·libc_pthread_self_trampoline_addr(SB), RODATA, $8
DATA ·libc_pthread_self_trampoline_addr(SB)/8, $libc_pthread_self_trampoline<>(SB)
