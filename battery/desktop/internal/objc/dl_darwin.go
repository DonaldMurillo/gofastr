//go:build darwin && (arm64 || amd64)

package objc

import (
	"errors"
	"runtime"
	"sync"
	"sync/atomic"

	"github.com/DonaldMurillo/gofastr/battery/desktop/internal/ffi"
)

// The dynamic-loader surface: the only symbols this package imports
// at link time. Everything else in AppKit, WebKit, and libobjc is
// looked up at runtime with dlopen/dlsym, the same shape
// golang.org/x/sys uses for libSystem (see their
// syscall_darwin_libSystem.go and zsyscall_darwin_arm64.s).

var (
	libc_dlopen_trampoline_addr       uintptr
	libc_dlsym_trampoline_addr        uintptr
	libc_dlerror_trampoline_addr      uintptr
	libc_pthread_self_trampoline_addr uintptr
)

//go:cgo_import_dynamic libc_dlopen dlopen "/usr/lib/libSystem.B.dylib"
//go:cgo_import_dynamic libc_dlsym dlsym "/usr/lib/libSystem.B.dylib"
//go:cgo_import_dynamic libc_dlerror dlerror "/usr/lib/libSystem.B.dylib"
//go:cgo_import_dynamic libc_pthread_self pthread_self "/usr/lib/libSystem.B.dylib"

const (
	rtldNow    = 2 // RTLD_NOW
	rtldGlobal = 8 // RTLD_GLOBAL: expose to dlsym(RTLD_DEFAULT)
	// RTLD_DEFAULT on darwin is (void*)-2; as a uintptr that is ^1.
	rtldDefault = ^uintptr(1)
)

// dlopenCount counts successful Dlopen calls. The lazy-init test in
// battery/desktop reads it through DlopenCount to prove that merely
// importing this package (as cmd/gofastr does, through the desktop
// battery) loads no framework: the count must still be zero after
// package init.
var dlopenCount atomic.Int64

// DlopenCount returns the number of successful dlopen calls so far.
func DlopenCount() int { return int(dlopenCount.Load()) }

// Dlopen loads the dynamic library at path and returns its handle.
func Dlopen(path string, mode int) (uintptr, error) {
	b, p, err := cBytes(path)
	if err != nil {
		return 0, err
	}
	r1, _, _ := ffi.Call(libc_dlopen_trampoline_addr, []uintptr{p, uintptr(mode)}, nil)
	runtime.KeepAlive(b)
	if r1 == 0 {
		return 0, errors.New("dlopen " + path + ": " + DLError())
	}
	dlopenCount.Add(1)
	return r1, nil
}

// DlopenGlobal loads a dynamic library with RTLD_NOW|RTLD_GLOBAL,
// exposing its symbols (and Objective-C classes) process-wide. This
// is the lazy-loading wrapper shells use for optional frameworks
// (UniformTypeIdentifiers, UserNotifications) that may be absent.
func DlopenGlobal(path string) (uintptr, error) {
	return Dlopen(path, rtldNow|rtldGlobal)
}

// Dlsym resolves name in handle (or process-wide with RTLD_DEFAULT).
func Dlsym(handle uintptr, name string) (uintptr, error) {
	b, p, err := cBytes(name)
	if err != nil {
		return 0, err
	}
	r1, _, _ := ffi.Call(libc_dlsym_trampoline_addr, []uintptr{handle, p}, nil)
	runtime.KeepAlive(b)
	if r1 == 0 {
		return 0, errors.New("dlsym: symbol not found: " + name)
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

// PthreadSelf returns the calling thread's pthread_t.
func PthreadSelf() uintptr {
	r1, _, _ := ffi.Call(libc_pthread_self_trampoline_addr, nil, nil)
	return r1
}

// frameworksOnce guards the one-time framework load.
var frameworksOnce sync.Once

// OpenFrameworks dlopens AppKit, WebKit, and libobjc with
// RTLD_NOW|RTLD_GLOBAL. It MUST be called on the main thread: a
// dlopen runs the image's static initializers (+load methods) on the
// calling thread, and AppKit requires that to be the main one. The
// desktop shell's Run is the only caller; everything before it
// (package import, shell construction, ensureRuntime) loads nothing,
// which is what keeps cmd/gofastr's blank import of the battery from
// touching AppKit.
func OpenFrameworks() {
	AssertMainThread("OpenFrameworks")
	frameworksOnce.Do(func() {
		for _, path := range []string{
			"/System/Library/Frameworks/AppKit.framework/AppKit",
			"/System/Library/Frameworks/WebKit.framework/WebKit",
			"/usr/lib/libobjc.A.dylib",
		} {
			if _, err := Dlopen(path, rtldNow|rtldGlobal); err != nil {
				panic("objc: " + err.Error())
			}
		}
	})
}
