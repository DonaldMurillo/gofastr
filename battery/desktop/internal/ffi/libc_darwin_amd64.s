//go:build darwin && amd64

#include "textflag.h"

// The libc surface internal tests drive through Call: qsort, whose
// comparator invokes a callback trampoline thousands of times from
// inside one C call; the dispatch pair, which delivers a callback on a
// thread Go never created; pthread_self; and snprintf, the external
// oracle for stack arguments. Same address-shape as internal/fakecgo's
// trampolines; the address must originate in assembly because a
// linknamed byte var does not bind to a cgo_import_dynamic symbol
// without a host object.
//
// Each body is a single JMP with no CALL, so the assembler adds no
// base-pointer prologue and the caller's arguments and stack alignment
// pass through untouched.

TEXT libc_qsort_trampoline<>(SB),NOSPLIT,$0-0
	JMP	libc_qsort(SB)
GLOBL ·libc_qsort_trampoline_addr(SB), RODATA, $8
DATA ·libc_qsort_trampoline_addr(SB)/8, $libc_qsort_trampoline<>(SB)

TEXT libc_dispatch_get_global_queue_trampoline<>(SB),NOSPLIT,$0-0
	JMP	libc_dispatch_get_global_queue(SB)
GLOBL ·libc_dispatch_get_global_queue_trampoline_addr(SB), RODATA, $8
DATA ·libc_dispatch_get_global_queue_trampoline_addr(SB)/8, $libc_dispatch_get_global_queue_trampoline<>(SB)

TEXT libc_dispatch_async_f_trampoline<>(SB),NOSPLIT,$0-0
	JMP	libc_dispatch_async_f(SB)
GLOBL ·libc_dispatch_async_f_trampoline_addr(SB), RODATA, $8
DATA ·libc_dispatch_async_f_trampoline_addr(SB)/8, $libc_dispatch_async_f_trampoline<>(SB)

TEXT libc_pthread_self_trampoline<>(SB),NOSPLIT,$0-0
	JMP	libc_pthread_self(SB)
GLOBL ·libc_pthread_self_trampoline_addr(SB), RODATA, $8
DATA ·libc_pthread_self_trampoline_addr(SB)/8, $libc_pthread_self_trampoline<>(SB)

TEXT libc_snprintf_trampoline<>(SB),NOSPLIT,$0-0
	JMP	libc_snprintf(SB)
GLOBL ·libc_snprintf_trampoline_addr(SB), RODATA, $8
DATA ·libc_snprintf_trampoline_addr(SB)/8, $libc_snprintf_trampoline<>(SB)
