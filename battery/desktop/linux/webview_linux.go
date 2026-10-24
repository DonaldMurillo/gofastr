//go:build linux && (amd64 || arm64)

package linux

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"github.com/DonaldMurillo/gofastr/battery/desktop"
	"github.com/DonaldMurillo/gofastr/battery/desktop/internal/ffi"
	"github.com/DonaldMurillo/gofastr/battery/desktop/internal/gtk"
)

var asyncSequence atomic.Uint64
var asyncResults sync.Map // integer token -> chan webResult
var pngBuffers sync.Map   // integer token -> *bytes.Buffer

type webResult struct {
	data []byte
	err  error
}

// GError is a uint32 domain, int32 code, and an aligned message pointer.
type gError struct {
	domain  uint32
	code    int32
	message uintptr
}

func takeGError(p uintptr) error {
	if p == 0 {
		return nil
	}
	message := readString((*gError)(nativePointer(p)).message)
	call(gtk.Sym.GErrorFree, p)
	return &desktop.Error{Code: desktop.CodeInternal, Message: message}
}
func publishResult(token uintptr, result webResult) {
	if value, ok := asyncResults.LoadAndDelete(token); ok {
		value.(chan webResult) <- result
	}
}

func finishJS(object, result, token, finish uintptr) {
	p := new(uintptr)
	var pin runtime.Pinner
	pin.Pin(p)
	defer pin.Unpin()
	value := call(finish, object, result, uintptr(unsafe.Pointer(p)))
	out := webResult{err: takeGError(*p)}
	if value != 0 {
		if out.err == nil {
			str := call(gtk.Sym.JscValueToJSON, value, 0)
			if str == 0 {
				out.data = []byte("null")
			} else {
				text, complete := readBoundedString(str, 4<<20)
				if !complete {
					out.err = &desktop.Error{Code: desktop.CodeInternal, Message: "JavaScript result exceeds 4 MiB"}
				} else {
					out.data = []byte(text)
				}
				call(gtk.Sym.GFree, str)
			}
		}
		call(gtk.Sym.GObjectUnref, value)
	}
	publishResult(token, out)
}

var evalCallback = ffi.NewCallback(func(a *ffi.Args) uintptr {
	finishJS(a.Int[0], a.Int[1], a.Int[2], gtk.Sym.WebkitWebViewEvalJSFinish)
	return 0
})
var asyncCallback = ffi.NewCallback(func(a *ffi.Args) uintptr {
	finishJS(a.Int[0], a.Int[1], a.Int[2], gtk.Sym.WebkitWebViewCallAsyncJSFinish)
	return 0
})

func (w *linuxWindow) evalSubmit(ctx context.Context, script string, async bool, token uintptr) error {
	if err := validateString(script); err != nil {
		return err
	}
	return w.withMainContext(ctx, func() {
		withString(script, func(p uintptr) {
			if async {
				call(gtk.Sym.WebkitWebViewCallAsyncJS, w.webview, p, uintptr(len(script)), 0, 0, 0, 0, asyncCallback, token)
			} else {
				call(gtk.Sym.WebkitWebViewEvalJS, w.webview, p, uintptr(len(script)), 0, 0, 0, evalCallback, token)
			}
		})
	})
}
func (w *linuxWindow) Eval(js string) error {
	// ready and native callbacks run on the GTK thread and must never wait for
	// WebKit's asynchronous result. The completion still consumes/free resources.
	if w.shell.uiThread.Load() == int64(syscall.Gettid()) {
		return w.evalSubmit(context.Background(), js, false, 0)
	}
	_, err := w.waitWeb(context.Background(), func(token uintptr) error { return w.evalSubmit(context.Background(), js, false, token) })
	return err
}
func (w *linuxWindow) EvalAsync(ctx context.Context, body string) (json.RawMessage, error) {
	data, err := w.waitWeb(ctx, func(token uintptr) error { return w.evalSubmit(ctx, body, true, token) })
	return json.RawMessage(data), err
}
func (w *linuxWindow) waitWeb(ctx context.Context, submit func(uintptr) error) ([]byte, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if w.closed.Load() {
		return nil, errClosed()
	}
	if w.shell.uiThread.Load() == int64(syscall.Gettid()) {
		return nil, errors.New("desktop/linux: cannot wait for WebKit on the UI thread")
	}
	token := uintptr(asyncSequence.Add(1))
	ch := make(chan webResult, 1)
	asyncResults.Store(token, ch)
	defer asyncResults.Delete(token)
	// Main can wait for a busy UI queue. Keep context cancellation independent
	// of that wait; the submitted closure also checks ctx on the GTK thread.
	go func() {
		if err := submit(token); err != nil {
			publishResult(token, webResult{err: err})
		}
	}()
	timer := time.NewTimer(30 * time.Second)
	defer timer.Stop()
	select {
	case out := <-ch:
		return out.data, out.err
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-w.shell.done:
		return nil, errStopped()
	case <-timer.C:
		return nil, &desktop.Error{Code: desktop.CodeInternal, Message: "WebKit operation timed out"}
	}
}

var pngWriteCallback = ffi.NewCallback(func(a *ffi.Args) uintptr {
	value, ok := pngBuffers.Load(a.Int[0])
	if !ok {
		return 11 /* CAIRO_STATUS_WRITE_ERROR */
	}
	buffer := value.(*bytes.Buffer)
	n := int(a.Int[2])
	if n < 0 || n > 64<<20 || buffer.Len()+n > 64<<20 {
		return 11
	}
	buffer.Write(unsafe.Slice((*byte)(nativePointer(a.Int[1])), n))
	return 0
})
var snapshotCallback = ffi.NewCallback(func(a *ffi.Args) uintptr {
	p := new(uintptr)
	var pin runtime.Pinner
	pin.Pin(p)
	defer pin.Unpin()
	surface := call(gtk.Sym.WebkitWebViewGetSnapshotFinish, a.Int[0], a.Int[1], uintptr(unsafe.Pointer(p)))
	out := webResult{err: takeGError(*p)}
	if surface != 0 {
		if out.err == nil {
			buffer := new(bytes.Buffer)
			pngBuffers.Store(a.Int[2], buffer)
			status := call(gtk.Sym.CairoSurfaceWriteToPNGStream, surface, pngWriteCallback, a.Int[2])
			pngBuffers.Delete(a.Int[2])
			if status != 0 {
				out.err = fmt.Errorf("desktop/linux: encode snapshot: cairo status %d", status)
			} else {
				out.data = buffer.Bytes()
			}
		}
		call(gtk.Sym.CairoSurfaceDestroy, surface)
	}
	if surface == 0 && out.err == nil {
		out.err = errors.New("desktop/linux: WebKit returned no snapshot")
	}
	publishResult(a.Int[2], out)
	return 0
})

func (w *linuxWindow) Snapshot(ctx context.Context) ([]byte, error) {
	return w.waitWeb(ctx, func(token uintptr) error {
		return w.withMainContext(ctx, func() {
			call(gtk.Sym.WebkitWebViewGetSnapshot, w.webview, 0 /* VISIBLE */, 0, 0, snapshotCallback, token)
		})
	})
}

var _ desktop.PageEvaluator = (*linuxWindow)(nil)

func (w *linuxWindow) withMainContext(ctx context.Context, fn func()) error {
	if ctx == nil {
		ctx = context.Background()
	}
	var err error
	if dispatchErr := w.withMain(func() {
		if err = ctx.Err(); err == nil {
			fn()
		}
	}); dispatchErr != nil {
		return dispatchErr
	}
	return err
}
