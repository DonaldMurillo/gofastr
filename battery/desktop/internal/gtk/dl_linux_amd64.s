//go:build linux && amd64

#include "textflag.h"

// Trampolines for the three libc loader symbols this package imports at
// link time. Same shape as the arm64 sibling: a hidden JMP trampoline
// per symbol plus a RODATA word holding its address, because a
// //go:linkname'd byte var does not bind to a cgo_import_dynamic symbol
// without a host object.

TEXT libc_dlopen_trampoline<>(SB),NOSPLIT|NOFRAME,$0-0
	JMP	libc_dlopen(SB)
GLOBL ·libc_dlopen_trampoline_addr(SB), RODATA, $8
DATA ·libc_dlopen_trampoline_addr(SB)/8, $libc_dlopen_trampoline<>(SB)

TEXT libc_dlsym_trampoline<>(SB),NOSPLIT|NOFRAME,$0-0
	JMP	libc_dlsym(SB)
GLOBL ·libc_dlsym_trampoline_addr(SB), RODATA, $8
DATA ·libc_dlsym_trampoline_addr(SB)/8, $libc_dlsym_trampoline<>(SB)

TEXT libc_dlerror_trampoline<>(SB),NOSPLIT|NOFRAME,$0-0
	JMP	libc_dlerror(SB)
GLOBL ·libc_dlerror_trampoline_addr(SB), RODATA, $8
DATA ·libc_dlerror_trampoline_addr(SB)/8, $libc_dlerror_trampoline<>(SB)
