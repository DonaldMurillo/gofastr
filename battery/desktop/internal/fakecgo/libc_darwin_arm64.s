//go:build darwin && arm64

#include "textflag.h"

// Address-bearing trampolines for the two libc functions the fakecgo
// self-test needs to call through internal/ffi (getpid, getenv).
// A //go:linkname'd byte var does NOT bind to a cgo_import_dynamic
// symbol — without a host object the compiler materializes it as a Go
// data byte — so the address has to come from assembly, the same shape
// internal/objc uses for dlopen/dlsym/dlerror/pthread_self.

TEXT libc_getpid_trampoline<>(SB),NOSPLIT|NOFRAME,$0-0
	JMP	libc_getpid(SB)
GLOBL ·libc_getpid_trampoline_addr(SB), RODATA, $8
DATA ·libc_getpid_trampoline_addr(SB)/8, $libc_getpid_trampoline<>(SB)

TEXT libc_getenv_trampoline<>(SB),NOSPLIT|NOFRAME,$0-0
	JMP	libc_getenv(SB)
GLOBL ·libc_getenv_trampoline_addr(SB), RODATA, $8
DATA ·libc_getenv_trampoline_addr(SB)/8, $libc_getenv_trampoline<>(SB)
