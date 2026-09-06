//go:build darwin && arm64

#include "textflag.h"

// Trampolines for the four libSystem loader symbols this package
// imports at link time. Same shape as
// golang.org/x/sys/unix/zsyscall_darwin_arm64.s: a hidden JMP
// trampoline per symbol plus a RODATA word holding its address so Go
// code can call it through internal/ffi's syscall9.

TEXT libc_dlopen_trampoline<>(SB),NOSPLIT,$0-0
	JMP	libc_dlopen(SB)
GLOBL ·libc_dlopen_trampoline_addr(SB), RODATA, $8
DATA ·libc_dlopen_trampoline_addr(SB)/8, $libc_dlopen_trampoline<>(SB)

TEXT libc_dlsym_trampoline<>(SB),NOSPLIT,$0-0
	JMP	libc_dlsym(SB)
GLOBL ·libc_dlsym_trampoline_addr(SB), RODATA, $8
DATA ·libc_dlsym_trampoline_addr(SB)/8, $libc_dlsym_trampoline<>(SB)

TEXT libc_dlerror_trampoline<>(SB),NOSPLIT,$0-0
	JMP	libc_dlerror(SB)
GLOBL ·libc_dlerror_trampoline_addr(SB), RODATA, $8
DATA ·libc_dlerror_trampoline_addr(SB)/8, $libc_dlerror_trampoline<>(SB)

TEXT libc_pthread_self_trampoline<>(SB),NOSPLIT,$0-0
	JMP	libc_pthread_self(SB)
GLOBL ·libc_pthread_self_trampoline_addr(SB), RODATA, $8
DATA ·libc_pthread_self_trampoline_addr(SB)/8, $libc_pthread_self_trampoline<>(SB)
