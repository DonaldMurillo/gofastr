//go:build darwin && amd64

package objc

import (
	"runtime"
	"unsafe"

	"github.com/DonaldMurillo/gofastr/battery/desktop/internal/ffi"
)

var symMsgSendStret uintptr

var archRuntimeSymbols = map[string]*uintptr{
	"objc_msgSend_stret": &symMsgSendStret,
}

// SendRect is SendF with one leading NSRect parameter.
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
func SendRectRet(self ID, sel uintptr, ints ...uintptr) Rect {
	ensureRuntime()
	full := make([]uintptr, 0, 2+len(ints))
	full = append(full, uintptr(self), sel)
	full = append(full, ints...)
	result := new(Rect)
	escapeSink.Store((*byte)(unsafe.Pointer(result)))
	ffi.CallRetStruct(cptr(symMsgSendStret), unsafe.Pointer(result), full, nil)
	runtime.KeepAlive(result)
	return *result
}
