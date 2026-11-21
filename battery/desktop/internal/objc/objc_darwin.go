//go:build darwin && (arm64 || amd64)

package objc

import (
	"fmt"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/DonaldMurillo/gofastr/battery/desktop/internal/ffi"
)

var (
	symMsgSend           uintptr // objc_msgSend
	symGetClass          uintptr // objc_getClass
	symSelRegisterName   uintptr // sel_registerName
	symAllocClassPair    uintptr // objc_allocateClassPair
	symClassAddMethod    uintptr // class_addMethod
	symRegisterClassPair uintptr // objc_registerClassPair
	symConcreteGlobalBlk uintptr // _NSConcreteGlobalBlock
	symDispatchMainQ     uintptr // _dispatch_main_q (the queue object itself)
	symDispatchAsyncF    uintptr // dispatch_async_f
)

// ID is an Objective-C object pointer (id).
type ID uintptr

var (
	cacheMu sync.Mutex
	classes = map[string]ID{}
	sels    = map[string]uintptr{}
)

func init() {
	runtime.LockOSThread()

	mainThreadID = PthreadSelf()
	ffi.ThreadCheck = func() { AssertMainThread("ffi callback") }
}

var runtimeSymbols = map[string]*uintptr{
	"objc_msgSend":           &symMsgSend,
	"objc_getClass":          &symGetClass,
	"sel_registerName":       &symSelRegisterName,
	"objc_allocateClassPair": &symAllocClassPair,
	"class_addMethod":        &symClassAddMethod,
	"objc_registerClassPair": &symRegisterClassPair,
	"_NSConcreteGlobalBlock": &symConcreteGlobalBlk,
	"_dispatch_main_q":       &symDispatchMainQ,
	"dispatch_async_f":       &symDispatchAsyncF,
}

var runtimeOnce sync.Once

func ensureRuntime() {
	runtimeOnce.Do(func() {
		if !resolveRuntimeSymbols() {
			if _, err := Dlopen("/usr/lib/libobjc.A.dylib", rtldNow|rtldGlobal); err != nil {
				panic("objc: " + err.Error())
			}
			if !resolveRuntimeSymbols() {
				panic("objc: libobjc/dispatch symbols unresolvable even after loading libobjc")
			}
		}
	})
}

func resolveRuntimeSymbols() bool {
	for _, m := range []map[string]*uintptr{runtimeSymbols, archRuntimeSymbols} {
		for name, dst := range m {
			p, err := Dlsym(rtldDefault, name)
			if err != nil {
				return false
			}
			*dst = p
		}
	}
	return true
}

// Send calls objc_msgSend(self, sel, args...).
func Send(self ID, sel uintptr, args ...uintptr) uintptr {
	return SendF(self, sel, args, nil)
}

// SendF is Send for signatures with double parameters.
func SendF(self ID, sel uintptr, ints []uintptr, floats []float64) uintptr {
	ensureRuntime()
	full := make([]uintptr, 0, 2+len(ints))
	full = append(full, uintptr(self), sel)
	full = append(full, ints...)
	r1, _, _ := ffi.Call(symMsgSend, full, floats)
	return r1
}

// SendDouble is Send for signatures returning a double or a CGFloat.
func SendDouble(self ID, sel uintptr, ints []uintptr, floats []float64) float64 {
	ensureRuntime()
	full := make([]uintptr, 0, 2+len(ints))
	full = append(full, uintptr(self), sel)
	full = append(full, ints...)
	_, _, f0 := ffi.Call(symMsgSend, full, floats)
	return f0
}

// SendBool is Send for signatures returning BOOL.
func SendBool(self ID, sel uintptr, args ...uintptr) bool {
	return Send(self, sel, args...)&0xFF != 0
}

// Class returns the named Objective-C class, cached.
func Class(name string) ID {
	ensureRuntime()
	cacheMu.Lock()
	defer cacheMu.Unlock()
	if id, ok := classes[name]; ok {
		return id
	}
	b, p, err := cBytes(name)
	if err != nil {
		panic("objc: " + err.Error())
	}
	r1, _, _ := ffi.Call(symGetClass, []uintptr{p}, nil)
	runtime.KeepAlive(b)
	if r1 == 0 {
		panic("objc: class not found: " + name)
	}
	classes[name] = ID(r1)
	return ID(r1)
}

// Sel returns the registered selector for name, cached.
func Sel(name string) uintptr {
	ensureRuntime()
	cacheMu.Lock()
	defer cacheMu.Unlock()
	if s, ok := sels[name]; ok {
		return s
	}
	b, p, err := cBytes(name)
	if err != nil {
		panic("objc: " + err.Error())
	}
	r1, _, _ := ffi.Call(symSelRegisterName, []uintptr{p}, nil)
	runtime.KeepAlive(b)
	sels[name] = r1
	return r1
}

// NSString returns an autoreleased NSString for s.
func NSString(s string) ID {
	id, _ := NSStringErr(s)
	return id
}

// NSStringErr is NSString with the refusal visible.
func NSStringErr(s string) (ID, error) {
	b, p, err := cBytes(s)
	if err != nil {
		return 0, fmt.Errorf("objc: NSString: %w", err)
	}
	id := Send(Class("NSString"), Sel("stringWithUTF8String:"), p)
	runtime.KeepAlive(b)
	return ID(id), nil
}

func cptr(r uintptr) unsafe.Pointer {
	return *(*unsafe.Pointer)(unsafe.Pointer(&r))
}

// GoString converts an NSString to a Go string (NSUTF8StringEncoding).
func GoString(ns ID) string {
	n := Send(ns, Sel("lengthOfBytesUsingEncoding:"), 4)
	if n == 0 {
		return ""
	}
	p := Send(ns, Sel("UTF8String"))
	if p == 0 {
		return ""
	}
	return string(unsafe.Slice((*byte)(cptr(p)), n))
}

// CopyBytes copies n bytes from a C buffer at p into a freshly allocated Go slice.
func CopyBytes(p uintptr, n uintptr) []byte {
	if p == 0 || n == 0 {
		return nil
	}
	buf := make([]byte, n)
	copy(buf, unsafe.Slice((*byte)(cptr(p)), n))
	return buf
}

// Method is one method added by RegisterClass.
type Method struct {
	Sel   string
	Types string
	Fn    uintptr
}

// RegisterClass allocates a new class pair under name with super and the given methods, registers it, and returns the class.
func RegisterClass(name string, super ID, methods []Method) ID {
	ensureRuntime()
	nb, nm, err := cBytes(name)
	if err != nil {
		panic("objc: RegisterClass: " + err.Error())
	}
	cls, _, _ := ffi.Call(symAllocClassPair, []uintptr{uintptr(super), nm}, nil)
	runtime.KeepAlive(nb)
	if cls == 0 {
		panic("objc: objc_allocateClassPair failed for " + name)
	}
	for _, m := range methods {
		tb, types, err := cBytes(m.Types)
		if err != nil {
			panic("objc: RegisterClass: " + err.Error())
		}
		ok, _, _ := ffi.Call(symClassAddMethod, []uintptr{cls, Sel(m.Sel), m.Fn, types}, nil)
		runtime.KeepAlive(tb)
		if ok == 0 {
			panic(fmt.Sprintf("objc: class_addMethod failed for %s on %s", m.Sel, name))
		}
	}
	ffi.Call(symRegisterClassPair, []uintptr{cls}, nil)
	return ID(cls)
}

var escapeSink atomic.Pointer[byte]

func cBytes(s string) ([]byte, uintptr, error) {
	if strings.IndexByte(s, 0) >= 0 {
		return nil, 0, fmt.Errorf("string contains NUL: %.32q", s)
	}
	b := make([]byte, len(s)+1)
	copy(b, s)
	p := &b[0]
	escapeSink.Store(p)
	return b, uintptr(unsafe.Pointer(p)), nil
}

func cString(p uintptr) string {
	b := unsafe.Slice((*byte)(cptr(p)), 1<<20)
	n := 0
	for n < len(b) && b[n] != 0 {
		n++
	}
	return string(b[:n])
}
