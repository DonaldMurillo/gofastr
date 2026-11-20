//go:build darwin && arm64

package objc

import (
	"github.com/DonaldMurillo/gofastr/battery/desktop/internal/ffi"
)

var archRuntimeSymbols = map[string]*uintptr{}

// SendRect is SendF with one leading NSRect parameter.
func SendRect(self ID, sel uintptr, r Rect, ints ...uintptr) uintptr {
	return SendF(self, sel, ints, []float64{r.X, r.Y, r.W, r.H})
}

// SendRectRet dispatches a message returning an NSRect.
func SendRectRet(self ID, sel uintptr, ints ...uintptr) Rect {
	ensureRuntime()
	full := make([]uintptr, 0, 2+len(ints))
	full = append(full, uintptr(self), sel)
	full = append(full, ints...)
	d := ffi.CallRetHFA4(symMsgSend, full, nil)
	return Rect{X: d[0], Y: d[1], W: d[2], H: d[3]}
}
