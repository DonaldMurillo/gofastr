//go:build darwin && amd64

package objc

import (
	"runtime"
	"testing"
	"unsafe"
)

func TestRectReturnBufferIsStable(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if _, err := DlopenGlobal("/System/Library/Frameworks/Foundation.framework/Foundation"); err != nil {
		t.Fatal(err)
	}
	selector := Sel("rectValue")
	for _, want := range []Rect{{X: 1.5, Y: -2.25, W: 300.125, H: 400.5}, {X: -4, Y: 8, W: 16, H: 32}} {
		value := ID(SendRect(Class("NSValue"), Sel("valueWithRect:"), want))
		escapeSink.Store(nil)
		if got := SendRectRet(value, selector); got != want {
			t.Fatalf("rect return = %+v, want %+v", got, want)
		}
		buffer := escapeSink.Load()
		if buffer == nil {
			t.Fatal("struct-return buffer did not escape before crossing into C")
		}
		if got := *(*Rect)(unsafe.Pointer(buffer)); got != want {
			t.Fatalf("heap return buffer = %+v, want %+v", got, want)
		}
	}
}
