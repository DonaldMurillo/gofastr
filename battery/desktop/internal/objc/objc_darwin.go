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

// The libobjc / dispatch entry points resolved at init. Everything the
// host calls is here; a missing symbol is an init-time panic naming
// it, per the spike plan.
var (
	symMsgSend           uintptr // objc_msgSend
	symGetClass          uintptr // objc_getClass
	symSelRegisterName   uintptr // sel_registerName
	symAllocClassPair    uintptr // objc_allocateClassPair
	symClassAddMethod    uintptr // class_addMethod
	symRegisterClassPair uintptr // objc_registerClassPair
	symObjectGetClass    uintptr // object_getClass
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
	// The main goroutine owns the main OS thread from here on: AppKit
	// requires every call on it, and runtime.cgocallback needs the
	// thread to be a locked, Go-owned M for callbacks to re-enter Go.
	// The lock is never released; the process leaves the main
	// goroutine via exit(0).
	//
	// Nothing else happens here, on purpose: cmd/gofastr blank-imports
	// the desktop battery, and an eager dlopen would make the CLI load
	// AppKit on every start. Frameworks load on first use through
	// ensureRuntime; the lazy-init test in battery/desktop pins that by
	// counting Dlopens.
	runtime.LockOSThread()

	// Install the thread assertion so any callback that arrives on the
	// wrong thread fails loudly instead of corrupting AppKit state.
	mainThreadID = PthreadSelf()
	ffi.ThreadCheck = func() { AssertMainThread("ffi callback") }
}

// runtimeSymbols maps the libobjc / libdispatch entry points this
// package calls. Every one lives in libSystem (libobjc and
// libdispatch are components of it), which every macOS process — a
// CGO_ENABLED=0 Go binary included — already has loaded, so resolving
// them dlopens NOTHING.
var runtimeSymbols = map[string]*uintptr{
	"objc_msgSend":           &symMsgSend,
	"objc_getClass":          &symGetClass,
	"sel_registerName":       &symSelRegisterName,
	"objc_allocateClassPair": &symAllocClassPair,
	"class_addMethod":        &symClassAddMethod,
	"objc_registerClassPair": &symRegisterClassPair,
	"object_getClass":        &symObjectGetClass,
	"_NSConcreteGlobalBlock": &symConcreteGlobalBlk,
	"_dispatch_main_q":       &symDispatchMainQ,
	"dispatch_async_f":       &symDispatchAsyncF,
}

// runtimeOnce guards the one-time symbol resolution; init deliberately
// does none of it (see above).
var runtimeOnce sync.Once

// ensureRuntime resolves the libobjc / dispatch entry points. It is
// safe on any thread and loads no framework — DlopenCount stays zero
// until OpenFrameworks runs on the main thread. A symbol that stays
// unresolvable is a host bug, not a runtime condition.
func ensureRuntime() {
	runtimeOnce.Do(func() {
		if !resolveRuntimeSymbols() {
			// Not expected: libobjc and libdispatch ship inside
			// libSystem. Loading libobjc explicitly only bumps its
			// refcount (its initializers are trivial), never drags
			// AppKit in, and keeps this path off the main thread.
			if _, err := Dlopen("/usr/lib/libobjc.A.dylib", rtldNow|rtldGlobal); err != nil {
				panic("objc: " + err.Error())
			}
			if !resolveRuntimeSymbols() {
				panic("objc: libobjc/dispatch symbols unresolvable even after loading libobjc")
			}
		}
	})
}

// resolveRuntimeSymbols dlsyms the entry points, reporting success.
// archRuntimeSymbols carries the ones only one architecture has: it is
// empty on arm64 and holds objc_msgSend_stret on amd64, which genuinely
// does not exist in libobjc on arm64.
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

// Send calls objc_msgSend(self, sel, args...). Arguments must be
// integer- or pointer-representable; mixed float or struct-by-value
// signatures need SendF. Everything travels through ffi.Call
// (runtime.cgocall), never syscall9's libcCall: this thread is the
// callback thread, and a parked libcCall record is what the
// GC-vs-callback hazard lives on.
func Send(self ID, sel uintptr, args ...uintptr) uintptr {
	return SendF(self, sel, args, nil)
}

// SendF is Send for signatures with double parameters: floats fill the
// float registers (d0-d7 on arm64, xmm0-xmm7 on amd64), ints fill the
// integer registers after self and the selector, and any further
// integer arguments go to the C stack. Returns the first return
// register.
//
// A struct passed by value is NOT an ordinary float argument on both
// architectures — see SendRect, which is where that difference lives.
func SendF(self ID, sel uintptr, ints []uintptr, floats []float64) uintptr {
	ensureRuntime()
	full := make([]uintptr, 0, 2+len(ints))
	full = append(full, uintptr(self), sel)
	full = append(full, ints...)
	r1, _, _ := ffi.Call(symMsgSend, full, floats)
	return r1
}

// SendDouble is Send for signatures returning a double or a CGFloat,
// which both architectures return in the first float register (d0,
// xmm0) from ordinary objc_msgSend. x86_64's objc_msgSend_fpret is for
// x87 long double returns only and is deliberately not wired up.
func SendDouble(self ID, sel uintptr, ints []uintptr, floats []float64) float64 {
	ensureRuntime()
	full := make([]uintptr, 0, 2+len(ints))
	full = append(full, uintptr(self), sel)
	full = append(full, ints...)
	_, _, f0 := ffi.Call(symMsgSend, full, floats)
	return f0
}

// SendRectRet dispatches a message that returns an NSRect. The two
// architectures disagree about how a 32-byte aggregate of doubles comes
// back — in d0-d3 on arm64, through a hidden pointer and
// objc_msgSend_stret on amd64 — so each msgsend_darwin_<arch>.go carries
// its own implementation, with the evidence for it.

// SendBool is Send for signatures returning BOOL (ObjC BOOL is a
// one-byte value in the first return register on both 64-bit
// architectures: bool on arm64, signed char on x86_64).
func SendBool(self ID, sel uintptr, args ...uintptr) bool {
	return Send(self, sel, args...)&0xFF != 0
}

// Class returns the named Objective-C class, cached. It panics when
// the class does not exist: every class this package touches is a
// system class, and a missing one is a host bug, not a runtime
// condition.
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

// Sel returns the registered selector for name, cached. A selector is
// an opaque pointer; it is passed around as uintptr.
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

// NSString returns an autoreleased NSString for s. The spike keeps one
// process-lifetime autorelease pool, so the value stays valid. A string
// carrying an embedded NUL yields the nil NSString (0); see NSStringErr
// for why that is not a panic and where the refusal belongs instead.
func NSString(s string) ID {
	id, _ := NSStringErr(s)
	return id
}

// NSStringErr is NSString with the refusal visible. A Go string carrying
// an embedded NUL cannot cross into C at all (the C string ends at the
// NUL), and this crossing runs inside an ffi trampoline entered through
// runtime.cgocallback, where a Go panic is FATAL: it unwinds into the
// main goroutine parked in [NSApp run] and there is no recover on the
// path. So a NUL is an error here and the nil NSString (0) from
// NSString, never a panic — every AppKit setter this host passes one to
// tolerates nil.
//
// Refusing the input is the CALLER's job, at the layer that owns it:
// the bridge chokepoint answers invalid_input for a page-supplied
// string carrying a NUL, so this is defence in depth for every other
// source (menu titles, clipboard round-trips, a plugin's Native()).
func NSStringErr(s string) (ID, error) {
	b, p, err := cBytes(s)
	if err != nil {
		return 0, fmt.Errorf("objc: NSString: %w", err)
	}
	id := Send(Class("NSString"), Sel("stringWithUTF8String:"), p)
	runtime.KeepAlive(b)
	return ID(id), nil
}

// cptr reinterprets a C pointer returned in x0 as unsafe.Pointer. The
// double-deref form is the syscall-package idiom for carrying FFI
// pointers without a direct uintptr→unsafe.Pointer conversion.
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

// CopyBytes copies n bytes from a C buffer at p into a freshly
// allocated Go slice. Use for NSData/NSString payloads returned by
// native calls; p must remain valid for the duration (it does: the
// process-lifetime autorelease pool keeps native results alive).
func CopyBytes(p uintptr, n uintptr) []byte {
	if p == 0 || n == 0 {
		return nil
	}
	buf := make([]byte, n)
	copy(buf, unsafe.Slice((*byte)(cptr(p)), n))
	return buf
}

// Method is one method added by RegisterClass. Fn is a trampoline
// address from ffi.NewCallback; Types is the Objective-C type
// encoding, e.g. "v@:@@" for -(void)m:(id)a:(id)b.
type Method struct {
	Sel   string
	Types string
	Fn    uintptr
}

// RegisterClass allocates a new class pair under name with super and
// the given methods, registers it, and returns the class. The IMPs are
// the ffi trampolines, so every call lands in Go through
// runtime.cgocallback on the main thread.
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

// ObjectGetClass returns the class of an instance (object_getClass),
// for instanceMethodSignatureForSelector: in Invoke.
func ObjectGetClass(obj ID) ID {
	ensureRuntime()
	r1, _, _ := ffi.Call(symObjectGetClass, []uintptr{uintptr(obj)}, nil)
	return ID(r1)
}

// escapeSink is written by cBytes for one reason: to force the buffer
// it allocates onto the HEAP. See cBytes for the argument; it is
// deliberately never read.
var escapeSink atomic.Pointer[byte]

// cBytes copies s into a NUL-terminated, HEAP-RESIDENT NUL-terminated
// buffer and returns it with the address of its first byte. The caller
// must runtime.KeepAlive the slice (not the uintptr, which the GC does
// not treat as a reference) across the FFI call that consumes the
// pointer.
//
// Why the buffer must be on the heap, not just kept alive: the only use
// the compiler can see for &b[0] is a conversion to uintptr, which
// escape analysis does not treat as an escape. It is therefore free to
// stack-allocate b — and a goroutine stack MOVES. A callback that grows
// the stack while C still holds the pointer (asmcgocall re-derives the
// SP after the call, and this host runs callbacks from inside C on
// purpose) copies the buffer elsewhere and leaves the C side reading
// vacated memory. runtime.KeepAlive keeps the value LIVE; it does not
// keep it in PLACE. This is the same rule ffi.Call's callArgsPool
// exists for: "a heap address never moves under today's non-moving
// collector". Storing the pointer into a package-level variable is an
// unambiguous escape the compiler cannot see through, so the make
// below always lands on the heap.
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

// cString reads a C string at p into a Go string. The length is found
// by scanning for the NUL terminator, bounded to 1 MiB so a bad
// pointer cannot loop forever.
func cString(p uintptr) string {
	b := unsafe.Slice((*byte)(cptr(p)), 1<<20)
	n := 0
	for n < len(b) && b[n] != 0 {
		n++
	}
	return string(b[:n])
}
