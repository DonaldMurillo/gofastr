//go:build linux && (amd64 || arm64)

package gtk

import (
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync/atomic"
	"unsafe"

	"github.com/DonaldMurillo/gofastr/battery/desktop/internal/ffi"
)

// The dynamic-loader surface: the only symbols this package imports at
// link time. Everything in GTK, GLib, GObject and WebKitGTK is looked up
// at runtime with dlopen/dlsym.
//
// dlopen, dlsym and dlerror live in libc.so.6 from glibc 2.34 on (before
// that they were in libdl.so.2, which is now an empty compatibility
// stub). That is the same floor internal/fakecgo's pthread imports set,
// so there is one glibc requirement for the whole Linux host, not two.

var (
	libc_dlopen_trampoline_addr  uintptr
	libc_dlsym_trampoline_addr   uintptr
	libc_dlerror_trampoline_addr uintptr
)

//go:cgo_import_dynamic libc_dlopen dlopen "libc.so.6"
//go:cgo_import_dynamic libc_dlsym dlsym "libc.so.6"
//go:cgo_import_dynamic libc_dlerror dlerror "libc.so.6"

// Linux dlfcn flags (glibc bits/dlfcn.h). RTLD_GLOBAL is 0x100 here,
// not darwin's 8.
const (
	rtldNow    = 0x00002 // RTLD_NOW
	rtldGlobal = 0x00100 // RTLD_GLOBAL: expose to later lookups
)

// ErrUnavailable reports that a required shared library is not installed
// on this machine. Callers check for it with errors.Is to tell "no
// WebKitGTK here" apart from "WebKitGTK is broken".
var ErrUnavailable = errors.New("gtk: library not installed")

// dlopenCount counts successful Dlopen calls, so a lazy-init test can
// prove that merely importing this package loads nothing.
var dlopenCount atomic.Int64

// DlopenCount returns the number of successful dlopen calls so far.
func DlopenCount() int { return int(dlopenCount.Load()) }

// Dlopen loads the shared object at path and returns its handle.
func Dlopen(path string, mode int) (uintptr, error) {
	b, p, err := cBytes(path)
	if err != nil {
		return 0, err
	}
	r1, _, _ := ffi.Call(libc_dlopen_trampoline_addr, []uintptr{p, uintptr(mode)}, nil)
	runtime.KeepAlive(b)
	if r1 == 0 {
		return 0, fmt.Errorf("dlopen %s: %s: %w", path, DLError(), ErrUnavailable)
	}
	dlopenCount.Add(1)
	return r1, nil
}

// DlopenGlobal loads a shared object with RTLD_NOW|RTLD_GLOBAL. GTK's
// stack is interdependent (WebKitGTK resolves GObject symbols out of
// whatever is already in the process), so every library this package
// opens is opened globally.
func DlopenGlobal(path string) (uintptr, error) {
	return Dlopen(path, rtldNow|rtldGlobal)
}

// Dlsym resolves name in handle.
func Dlsym(handle uintptr, name string) (uintptr, error) {
	b, p, err := cBytes(name)
	if err != nil {
		return 0, err
	}
	r1, _, _ := ffi.Call(libc_dlsym_trampoline_addr, []uintptr{handle, p}, nil)
	runtime.KeepAlive(b)
	if r1 == 0 {
		return 0, fmt.Errorf("dlsym: symbol not found: %s", name)
	}
	return r1, nil
}

// DLError returns the last dl error message, or "" when none is set.
func DLError() string {
	r1, _, _ := ffi.Call(libc_dlerror_trampoline_addr, nil, nil)
	if r1 == 0 {
		return ""
	}
	return cString(r1)
}

// cBytes copies s into a NUL-terminated buffer and returns the buffer
// (which the caller keeps alive) plus the address of its first byte. A
// path or symbol name carrying an interior NUL is refused rather than
// silently truncated at the C boundary.
func cBytes(s string) ([]byte, uintptr, error) {
	if strings.IndexByte(s, 0) >= 0 {
		return nil, 0, fmt.Errorf("string contains NUL: %.32q", s)
	}
	b := make([]byte, len(s)+1)
	copy(b, s)
	return b, uintptr(unsafe.Pointer(&b[0])), nil
}

// cString reads a C string at p into a Go string. The length is found by
// scanning for the NUL terminator, bounded to 1 MiB so a bad pointer
// cannot loop forever.
func cString(p uintptr) string {
	b := unsafe.Slice((*byte)(*(*unsafe.Pointer)(unsafe.Pointer(&p))), 1<<20)
	n := 0
	for n < len(b) && b[n] != 0 {
		n++
	}
	return string(b[:n])
}
