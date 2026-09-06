//go:build darwin && amd64

package objc

import (
	"unsafe"

	"github.com/DonaldMurillo/gofastr/battery/desktop/internal/ffi"
)

// symMsgSendStret is objc_msgSend_stret, the x86_64-only entry point for
// a message whose return value System V classifies as MEMORY. Plain
// objc_msgSend cannot serve those: the hidden return pointer takes rdi,
// which pushes the receiver to rsi, and only _stret knows to look there.
//
// There is no _fpret in this table on purpose. objc_msgSend_fpret exists
// on x86_64 for x87 `long double` returns only; a plain double comes back
// in xmm0 from ordinary objc_msgSend, which is what CGFloat is on a
// 64-bit platform, so nothing in this host needs it. (clang -arch x86_64
// compiles a double-returning message send to a bare tail call to
// objc_msgSend.)
var symMsgSendStret uintptr

var archRuntimeSymbols = map[string]*uintptr{
	"objc_msgSend_stret": &symMsgSendStret,
}

// SendRect is SendF with one leading NSRect parameter.
//
// This is the one place where the amd64 ABI genuinely disagrees with
// arm64 rather than merely renaming registers. An NSRect is 32 bytes:
// four eightbytes, every one of them SSE class. System V's post-merger
// rule 5(c) says an aggregate larger than two eightbytes goes in MEMORY
// unless the first eightbyte is SSE and every later one is SSEUP, and
// plain doubles produce SSE, never SSEUP — so the rect is passed in the
// caller's stack argument area, not in xmm0-xmm3, and it consumes no
// register at all. The integer arguments after it therefore keep
// marching through rdx, rcx, r8 as if the rect were absent.
//
// Proved with the system compiler rather than read off a table:
//
//	void *mkwin(void *self, void *sel, double a, double b, double c,
//	            double d, unsigned long mask, unsigned long backing,
//	            signed char defer) {
//	  NSRect r = {{a, b}, {c, d}};
//	  return ((void *(*)(void *, void *, NSRect, unsigned long,
//	                     unsigned long, signed char))objc_msgSend)(
//	      self, sel, r, mask, backing, defer);
//	}
//
// clang -arch x86_64 -O2 emits `movups %xmm0, (%rsp)` and `movups %xmm1,
// 16(%rsp)` for the rect and moves nothing else before `callq
// _objc_msgSend`: mask, backing and defer were already in rdx, rcx and
// r8 on entry and stay there. The same function built with -arch arm64
// is a single `b _objc_msgSend`, the rect untouched in d0-d3.
//
// A 16-byte aggregate is a different case and does travel in registers:
// the same probe with an NSPoint tail-calls with the two doubles left in
// xmm0 and xmm1. Only the >16-byte ones go to memory.
func SendRect(self ID, sel uintptr, r Rect, ints ...uintptr) uintptr {
	ensureRuntime()
	full := make([]uintptr, 0, 2+len(ints))
	full = append(full, uintptr(self), sel)
	full = append(full, ints...)
	stack := []uintptr{
		*(*uintptr)(unsafe.Pointer(&r.X)),
		*(*uintptr)(unsafe.Pointer(&r.Y)),
		*(*uintptr)(unsafe.Pointer(&r.W)),
		*(*uintptr)(unsafe.Pointer(&r.H)),
	}
	r1, _, _ := ffi.CallStack(symMsgSend, full, nil, stack)
	return r1
}

// SendRectRet dispatches a message returning an NSRect.
//
// System V has no homogeneous-floating-point-aggregate rule — the one
// that lets arm64 return the same struct in d0-d3 — so 32 bytes of
// doubles is plain MEMORY class: the caller hands over a buffer in the
// first integer argument slot and the callee fills it. That means
// objc_msgSend_stret and not objc_msgSend, because the hidden pointer
// takes rdi and pushes the receiver to rsi, where only _stret looks.
// ffi.CallRetStruct prepends the pointer.
//
// clang shows the C half of the same convention: `NSRect getrect(void
// *self, void *sel)` compiled for x86_64 stashes the incoming rdi in rbx
// across the call and returns it in rax, the hidden-pointer-in,
// hidden-pointer-out contract for a MEMORY-class return.
func SendRectRet(self ID, sel uintptr, ints ...uintptr) Rect {
	ensureRuntime()
	full := make([]uintptr, 0, 2+len(ints))
	full = append(full, uintptr(self), sel)
	full = append(full, ints...)
	var r Rect
	ffi.CallRetStruct(cptr(symMsgSendStret), unsafe.Pointer(&r), full, nil)
	return r
}
