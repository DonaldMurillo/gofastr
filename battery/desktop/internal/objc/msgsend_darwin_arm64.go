//go:build darwin && arm64

package objc

import (
	"github.com/DonaldMurillo/gofastr/battery/desktop/internal/ffi"
)

// archRuntimeSymbols is empty on arm64. AAPCS64 has a dedicated
// indirect-result register (x8) that is not an argument register, so
// objc_msgSend handles a struct return with the receiver still in x0 and
// libobjc ships no _stret or _fpret variant for this architecture at
// all: dlsym("objc_msgSend_stret") fails here.
var archRuntimeSymbols = map[string]*uintptr{}

// SendRect is SendF with one leading NSRect parameter. On arm64 an
// NSRect is a homogeneous floating-point aggregate of four members,
// which AAPCS64 passes in d0-d3; the integer arguments follow in x2..x7
// (and the C stack past that) exactly as if the rect were not there.
// Covers initWithFrame:configuration: and
// initWithContentRect:styleMask:backing:defer:.
//
// clang -arch arm64 compiles a call of that shape to a bare tail branch
// to objc_msgSend with the four doubles already in d0-d3 and the
// trailing arguments already in x2-x4, which is this layout. The amd64
// twin of this file has the same call compiled for x86_64, where it is
// not this layout at all.
func SendRect(self ID, sel uintptr, r Rect, ints ...uintptr) uintptr {
	return SendF(self, sel, ints, []float64{r.X, r.Y, r.W, r.H})
}

// SendRectRet dispatches a message returning an NSRect.
//
// The size rule everyone remembers — "an aggregate over 16 bytes is
// returned through the x8 indirect-result register" — does not apply
// here, and the first draft of this function believed it did and read
// back four zeros. AAPCS64 classifies an aggregate whose members are all
// the same floating-point type, four or fewer of them, as a homogeneous
// floating-point aggregate and returns it in d0-d3 whatever its size;
// x8 is for everything else. An NSRect is 32 bytes and four doubles, so
// it comes back in registers.
//
// Watched, not reasoned about: passing a buffer in x8 and calling
// objc_msgSend for -[NSValue rectValue] leaves the buffer untouched and
// puts 1.5, the rect's origin.x, in d0.
func SendRectRet(self ID, sel uintptr, ints ...uintptr) Rect {
	ensureRuntime()
	full := make([]uintptr, 0, 2+len(ints))
	full = append(full, uintptr(self), sel)
	full = append(full, ints...)
	d := ffi.CallRetHFA4(symMsgSend, full, nil)
	return Rect{X: d[0], Y: d[1], W: d[2], H: d[3]}
}
