//go:build darwin && amd64

package ffi

// entrySize is the byte length of one callbackasm table entry on amd64:
// a single CALL ·callbackasm1(SB), which x86-64 encodes as E8 rel32 and
// nothing shorter — a direct near call has no rel8 form, so the stride
// cannot silently change under a toolchain that prefers compact
// encodings. There is no room in five bytes to also materialize the slot
// number, so callbackasm1 derives it from the return address the CALL
// pushed, exactly as the Go runtime's own amd64 callback table does
// (runtime/sys_windows_amd64.s:131-135 "divide by 5 because each call
// instruction in runtime·callbackasm is 5 bytes long";
// runtime/syscall_windows.go's callbackasmAddr picks entrySize 5 for
// 386/amd64 against 8 for arm/arm64 for the same reason).
const entrySize = 5
