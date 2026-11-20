//go:build darwin && (arm64 || amd64)

package ffi

import (
	"sync"
	"unsafe"
)

// Args carries the C argument registers as a trampoline was entered.
type Args struct {
	Int   [8]uintptr
	Float [8]float64
}

type callbackArgs struct {
	index  uintptr
	args   Args
	result uintptr
}

var (
	callbackWrapFn  = callbackWrap
	callbackWrapABI uintptr
)

func init() {
	fv := *(*unsafe.Pointer)(unsafe.Pointer(&callbackWrapFn))
	callbackWrapABI = *(*uintptr)(fv)
}

// MaxCallbacks bounds the trampoline table.
const MaxCallbacks = 64

var (
	callbacksMu sync.Mutex
	callbacks   [MaxCallbacks]func(args *Args) uintptr
	anyThread   [MaxCallbacks]bool
	callbackLen int
)

// ThreadCheck, when non-nil, runs inside every callback before the registered Go function.
var ThreadCheck func()

// NewCallback registers fn and returns its C function pointer; it panics when every slot is used.
func NewCallback(fn func(args *Args) uintptr) uintptr {
	return newCallback(fn, false)
}

// NewCallbackAnyThread is NewCallback for a callback the native side may invoke on a thread Go did not create.
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

func callbackasm()

func callbackasm1()

var callbackasmAddr uintptr
