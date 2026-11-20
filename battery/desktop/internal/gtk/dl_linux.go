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

var (
	libc_dlopen_trampoline_addr  uintptr
	libc_dlsym_trampoline_addr   uintptr
	libc_dlerror_trampoline_addr uintptr
)

//go:cgo_import_dynamic libc_dlopen dlopen "libc.so.6"
//go:cgo_import_dynamic libc_dlsym dlsym "libc.so.6"
//go:cgo_import_dynamic libc_dlerror dlerror "libc.so.6"

const (
	rtldNow    = 0x00002 // RTLD_NOW
	rtldGlobal = 0x00100 // RTLD_GLOBAL: expose to later lookups
)

// ErrUnavailable reports that a required shared library is not installed on this machine.
var ErrUnavailable = errors.New("gtk: library not installed")

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

// DlopenGlobal loads a shared object with RTLD_NOW|RTLD_GLOBAL.
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

func cBytes(s string) ([]byte, uintptr, error) {
	if strings.IndexByte(s, 0) >= 0 {
		return nil, 0, fmt.Errorf("string contains NUL: %.32q", s)
	}
	b := make([]byte, len(s)+1)
	copy(b, s)
	return b, uintptr(unsafe.Pointer(&b[0])), nil
}

func cString(p uintptr) string {
	b := unsafe.Slice((*byte)(*(*unsafe.Pointer)(unsafe.Pointer(&p))), 1<<20)
	n := 0
	for n < len(b) && b[n] != 0 {
		n++
	}
	return string(b[:n])
}
