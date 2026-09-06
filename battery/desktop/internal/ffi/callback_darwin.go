//go:build darwin && (arm64 || amd64)

package ffi

import (
	"sync"
	"unsafe"
)

// Args carries the first eight integer-class and eight float-class C
// arguments as they stood the moment a trampoline was entered.
//
// Int is the first eight integer or pointer arguments in signature
// order, wherever the ABI put them: x0-x7 on arm64, and on amd64 the
// six System V integer registers (rdi, rsi, rdx, rcx, r8, r9) followed
// by the first two words of the caller's stack argument area, which is
// where arguments seven and eight live there. Float is d0-d7 / xmm0-xmm7.
//
// Arguments past the eighth integer one are not captured, and neither
// is a MEMORY-class aggregate the caller left in the stack area (an
// NSRect on amd64): every callback signature in the desktop host takes
// pointers and integers only. Slots the callee's real signature does
// not use hold whatever the caller happened to leave there.
type Args struct {
	Int   [8]uintptr
	Float [8]float64
}

// callbackArgs is the frame the assembly trampoline builds on the
// stack and hands to runtime.cgocallback, which hands its address to
// callbackWrap. Field order and sizes must match the offsets spelled
// out in callback_darwin_arm64.s:
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
// loaded into callbackWrapABI for the assembly trampoline. The
// assembler rejects the <ABIInternal> symbol suffix outside package
// runtime, so the entry point is recovered from the func value instead.
var (
	callbackWrapFn  = callbackWrap
	callbackWrapABI uintptr
)

func init() {
	fv := *(*unsafe.Pointer)(unsafe.Pointer(&callbackWrapFn))
	callbackWrapABI = *(*uintptr)(fv)
}

// MaxCallbacks bounds the trampoline table. Slots are never freed:
// IMPs, block invokes, and dispatch function pointers handed to C must
// stay valid for the process lifetime.
const MaxCallbacks = 64

// entrySize, the byte length of one callbackasm table entry, is
// architecture-specific and lives beside each table: 8 on arm64 (a MOVD
// immediate plus a branch), 5 on amd64 (a single CALL rel32, the shape
// the Go runtime itself uses for its callback table in
// runtime/zcallback_windows.s). The untagged self-test calls entries 0
// and 1 to prove the stride on whichever one is built.

var (
	callbacksMu sync.Mutex
	callbacks   [MaxCallbacks]func(args *Args) uintptr
	// anyThread marks slots registered with NewCallbackAnyThread: the
	// ThreadCheck hook is skipped for them, because their caller
	// (a UNUserNotificationCenter completion, a dispatch global
	// queue) legitimately runs on a thread Go did not create. The
	// runtime handles that through needm on the extra M the fake-cgo
	// layer made it allocate at startup (proc.go:1968-1970).
	anyThread   [MaxCallbacks]bool
	callbackLen int
)

// ThreadCheck, when non-nil, runs inside every callback before the
// registered Go function. internal/objc installs AssertMainThread
// here so that a callback arriving on the wrong OS thread panics with
// the selector context instead of corrupting AppKit state. ffi itself
// stays free of an objc dependency.
var ThreadCheck func()

// NewCallback registers fn in a fixed slot and returns the address of
// that slot's trampoline, a C function pointer that calls
// fn(args) and forwards its uintptr return value to the C caller
// (x0 and d0 both carry the result bits). It panics when all
// MaxCallbacks slots are used.
func NewCallback(fn func(args *Args) uintptr) uintptr {
	return newCallback(fn, false)
}

// NewCallbackAnyThread is NewCallback for a callback the native side
// may invoke on a thread Go did not create. The main-thread assertion
// is skipped for it; fn must therefore be safe to run on any thread
// (signal a channel, no AppKit calls without a Main hop). Every
// completion handler of UNUserNotificationCenter is this shape: the
// centre calls them on a background queue, which is how the first
// bundled build died with "ffi callback called on thread …, want main
// thread" the moment a note was saved.
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
// was parked in the cgocall the C callback interrupted. It is declared
// here so go vet's asmdecl sees the symbol the assembly references.
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
// Both are defined in callback_darwin_arm64.s and declared here for
// asmdecl and for taking callbackasm's address via callbackasmAddr.
func callbackasm()

func callbackasm1()

// callbackasmAddr holds the address of callbackasm's first entry. It
// is defined by the GLOBL/DATA pair in callback_darwin_arm64.s, the
// same shape golang.org/x/sys uses for its libc trampoline addresses.
var callbackasmAddr uintptr
