//go:build linux && (amd64 || arm64)

package ffi

import (
	"sync"
	"unsafe"
)

// Args carries the C argument registers as seen at the moment a
// trampoline was entered. Int holds the integer argument registers —
// x0-x7 on arm64, DI/SI/DX/CX/R8/R9 on amd64, where Int[6] and Int[7]
// stay zero because the ABI has only six. Float holds d0-d7 / X0-X7,
// eight on both. Values passed on the C stack beyond the register set
// are not captured; every callback signature in the desktop host —
// GObject signal handlers, GTK callbacks, pthread start routines — fits
// in registers.
type Args struct {
	Int   [8]uintptr
	Float [8]float64
}

// callbackArgs is the frame the assembly trampoline builds on the stack
// and hands to runtime.cgocallback, which hands its address to
// callbackWrap. Field order and sizes must match the offsets spelled out
// in the per-arch callback_linux_*.s (Args.Float lands at +72 on both):
//
//	index  uintptr at cbargs+0
//	args   Args     at cbargs+8    (Int at +8, Float at +72)
//	result uintptr at cbargs+136
type callbackArgs struct {
	index  uintptr
	args   Args
	result uintptr
}

// callbackWrapFn exists so its func value's first word — the ABIInternal
// entry point runtime.cgocallback invokes through a funcval — can be
// loaded into callbackWrapABI for the assembly trampoline. The assembler
// rejects the <ABIInternal> symbol suffix outside package runtime, so
// the entry point is recovered from the func value instead.
var (
	callbackWrapFn  = callbackWrap
	callbackWrapABI uintptr
)

func init() {
	fv := *(*unsafe.Pointer)(unsafe.Pointer(&callbackWrapFn))
	callbackWrapABI = *(*uintptr)(fv)
}

// MaxCallbacks bounds the trampoline table. Slots are never freed:
// GObject closures and pthread start routines handed to C must stay
// valid for the process lifetime.
const MaxCallbacks = 64

// entrySize, the byte length of one callbackasm table entry, is
// per-arch: callback_linux_arm64.go and callback_linux_amd64.go each
// declare it beside the assembly that fixes it. The self-test calls
// entries 0 and 1 and asserts the delta, so a wrong constant fails
// rather than dispatching to the wrong slot.

var (
	callbacksMu sync.Mutex
	callbacks   [MaxCallbacks]func(args *Args) uintptr
	// anyThread marks slots registered with NewCallbackAnyThread: the
	// ThreadCheck hook is skipped for them, because their caller
	// legitimately runs on a thread Go did not create. The runtime
	// handles that through needm on the extra M the fake-cgo layer made
	// it allocate at startup (proc.go:1968-1970).
	anyThread   [MaxCallbacks]bool
	callbackLen int
)

// ThreadCheck, when non-nil, runs inside every callback before the
// registered Go function. The GTK shell installs a main-thread assertion
// here so that a callback arriving on the wrong OS thread panics with
// context instead of corrupting toolkit state. ffi itself stays free of
// a toolkit dependency.
var ThreadCheck func()

// NewCallback registers fn in a fixed slot and returns the address of
// that slot's trampoline, a C function pointer that calls fn(args) and
// forwards its uintptr return value to the C caller. The integer and
// float return registers both carry the result bits, so a callback
// declared to return a double works as well as one returning a pointer.
// It panics when all MaxCallbacks slots are used.
func NewCallback(fn func(args *Args) uintptr) uintptr {
	return newCallback(fn, false)
}

// NewCallbackAnyThread is NewCallback for a callback the native side may
// invoke on a thread Go did not create. The main-thread assertion is
// skipped for it; fn must therefore be safe to run on any thread (signal
// a channel, no GTK calls without a g_idle_add hop). A GIO async
// completion and a libnotify server callback are both this shape.
func NewCallbackAnyThread(fn func(args *Args) uintptr) uintptr {
	return newCallback(fn, true)
}

func newCallback(fn func(args *Args) uintptr, free bool) uintptr {
	callbacksMu.Lock()
	defer callbacksMu.Unlock()
	if callbackLen >= MaxCallbacks {
		panic("ffi: all callback slots are in use; raise MaxCallbacks")
	}
	i := callbackLen
	callbacks[i] = fn
	anyThread[i] = free
	callbackLen++
	return callbackasmAddr + uintptr(i)*entrySize
}

// callbackWrap is entered by runtime.cgocallback (as an ABIInternal
// entry point, frame pointer in x0) on the stack of the goroutine that
// was parked in the cgocall the C callback interrupted, or on a
// freshly borrowed M when the calling thread is not one Go created. It
// is declared here so go vet's asmdecl sees the symbol the assembly
// references.
func callbackWrap(a *callbackArgs) {
	callbacksMu.Lock()
	fn := callbacks[a.index]
	free := anyThread[a.index]
	callbacksMu.Unlock()
	if !free && ThreadCheck != nil {
		ThreadCheck()
	}
	if fn == nil {
		panic("ffi: callback invoked on an unregistered slot")
	}
	a.result = fn(&a.args)
}

// callbackasm is the trampoline table; callbackasm1 is the shared body.
// Both are defined in the per-arch callback_linux_*.s and declared here for
// asmdecl and for taking callbackasm's address via callbackasmAddr.
func callbackasm()

func callbackasm1()

// callbackasmAddr holds the address of callbackasm's first entry. It is
// defined by the GLOBL/DATA pair in the per-arch callback_linux_*.s.
var callbackasmAddr uintptr
