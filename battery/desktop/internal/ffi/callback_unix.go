//go:build (darwin || linux) && (amd64 || arm64)

package ffi

import (
	"sync"
	"unsafe"
)

// Args carries the C argument registers on entry. Darwin/amd64 also captures
// integer arguments 7–8 from the stack. Linux/amd64 leaves those slots zero.
type Args struct {
	Int   [8]uintptr
	Float [8]float64
}

// callbackArgs is the frame the assembly trampoline builds on the stack
// and hands to runtime.cgocallback, which hands its address to
// callbackWrap. Field order and sizes must match the offsets spelled out
// in the platform's callback assembly:
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

// MaxCallbacks bounds the trampoline table. Slots stay valid for the process lifetime.
const MaxCallbacks = 64

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

// ThreadCheck, when non-nil, runs before callbacks registered with NewCallback.
// The shell uses it to assert the callback runs on the toolkit thread.
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

// NewCallbackAnyThread registers a callback that can run on any OS thread.
// It skips ThreadCheck, so fn must be safe to run off the toolkit thread.
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

// callbackWrap is the ABIInternal entry point runtime.cgocallback invokes on
// the interrupted goroutine or a borrowed M for a foreign thread.
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

// callbackasm is the trampoline table; callbackasm1 is its shared body.
// The platform assembly defines both and its companion Go file sets entrySize.
func callbackasm()

func callbackasm1()

// callbackasmAddr holds the address of callbackasm's first entry.
var callbackasmAddr uintptr
