//go:build windows && amd64

package windows

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"runtime"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/DonaldMurillo/gofastr/battery/desktop"
	"github.com/DonaldMurillo/gofastr/battery/desktop/internal/win32"
)

var asyncEvalID atomic.Uint64

func (w *winWindow) ID() string { return w.id }

func (w *winWindow) Navigate(rawURL string) error {
	if w.closed.Load() || w.webview == 0 {
		return errWindowClosed()
	}
	p, err := win32.UTF16Ptr(rawURL)
	if err != nil {
		return err
	}
	var navigationErr error
	dispatchErr := w.shell.Main(func() {
		if w.webview == 0 || w.closed.Load() {
			navigationErr = errWindowClosed()
			return
		}
		_, navigationErr = win32.COMCall(w.webview, 5, uintptr(unsafe.Pointer(p)))
	})
	runtime.KeepAlive(p)
	if dispatchErr != nil {
		return dispatchErr
	}
	if navigationErr != nil {
		return fmt.Errorf("desktop/windows: navigate: %w", navigationErr)
	}
	return nil
}

func (w *winWindow) Eval(js string) error {
	if w.closed.Load() || w.webview == 0 {
		return errWindowClosed()
	}
	if win32.CurrentThreadID() == w.shell.uiThreadID {
		_, err := w.evalSubmit(js, nil)
		return err
	}
	ch := make(chan evalResult, 1)
	if _, err := w.evalSubmit(js, ch); err != nil {
		return err
	}
	select {
	case out := <-ch:
		return out.err
	case <-time.After(webviewTimeout):
		return &desktop.Error{Code: desktop.CodeInternal, Message: "JavaScript evaluation timed out"}
	}
}

func (w *winWindow) evalSubmit(js string, ch chan evalResult) (bool, error) {
	if w.webview == 0 {
		return false, errWindowClosed()
	}
	p, err := win32.UTF16Ptr(js)
	if err != nil {
		return false, err
	}
	cb, err := win32.NewCOMHandler(iidExecuteScriptHandler, func(hr, result uintptr) uintptr {
		out := evalResult{}
		if int32(hr) < 0 {
			out.err = win32.HRESULTError(int32(hr))
		} else {
			out.json = win32.ReadUTF16(result, 4<<20)
		}
		if ch != nil {
			select {
			case ch <- out:
			default:
			}
		}
		return 0
	})
	if err != nil {
		return false, err
	}
	callErr := w.shell.Main(func() {
		if w.webview == 0 || w.closed.Load() {
			err = errWindowClosed()
			return
		}
		_, err = win32.COMCall(w.webview, 29, uintptr(unsafe.Pointer(p)), cb)
	})
	win32.Release(cb)
	runtime.KeepAlive(p)
	if callErr != nil {
		return false, callErr
	}
	return true, err
}

func (w *winWindow) EvalAsync(ctx context.Context, body string) (json.RawMessage, error) {
	if w.closed.Load() || w.webview == 0 {
		return nil, errWindowClosed()
	}
	if win32.CurrentThreadID() == w.shell.uiThreadID {
		return nil, &desktop.Error{Code: desktop.CodeInternal, Message: "page evaluation cannot run on the UI thread"}
	}
	if ctx == nil {
		ctx = context.Background()
	}
	id := fmt.Sprintf("%d-%d", time.Now().UnixNano(), asyncEvalID.Add(1))
	idJSON, _ := json.Marshal(id)
	keyJSON, _ := json.Marshal("__gofastrEvalResults")
	kick := `(function(){const k=` + string(keyJSON) + `,id=` + string(idJSON) + `;` +
		`const q=window[k]||(window[k]=Object.create(null));` +
		`Promise.resolve().then(()=>((async()=>{` + body + `})())).then(v=>{q[id]={value:v===undefined?null:v}},` +
		`e=>{q[id]={error:String((e&&e.message)||e),code:(e&&e.code)||"internal"}});return true})()`
	start := make(chan evalResult, 1)
	if _, err := w.evalSubmit(kick, start); err != nil {
		return nil, err
	}
	deadline := time.Now().Add(webviewTimeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	select {
	case out := <-start:
		if out.err != nil {
			return nil, &desktop.Error{Code: desktop.CodeInternal, Message: out.err.Error()}
		}
	case <-ctx.Done():
		return nil, &desktop.Error{Code: desktop.CodeInternal, Message: "page evaluation timed out"}
	case <-time.After(time.Until(deadline)):
		return nil, &desktop.Error{Code: desktop.CodeInternal, Message: "page evaluation timed out"}
	}
	poll := `(()=>{const q=window[` + string(keyJSON) + `];if(!q||!Object.prototype.hasOwnProperty.call(q,` + string(idJSON) + `))return null;const v=q[` + string(idJSON) + `];delete q[` + string(idJSON) + `];return v})()`
	for {
		ch := make(chan evalResult, 1)
		if _, err := w.evalSubmit(poll, ch); err != nil {
			return nil, err
		}
		select {
		case out := <-ch:
			if out.err != nil {
				return nil, &desktop.Error{Code: desktop.CodeInternal, Message: out.err.Error()}
			}
			if out.json != "null" && out.json != "" {
				var result struct {
					Value json.RawMessage `json:"value"`
					Error string          `json:"error"`
					Code  string          `json:"code"`
				}
				if err := json.Unmarshal([]byte(out.json), &result); err != nil {
					return nil, &desktop.Error{Code: desktop.CodeInternal, Message: "page evaluation returned invalid JSON"}
				}
				if result.Error != "" {
					return nil, &desktop.Error{Code: desktop.CodeInternal, Message: result.Error}
				}
				if len(result.Value) == 0 {
					return json.RawMessage("null"), nil
				}
				return result.Value, nil
			}
		case <-ctx.Done():
			return nil, &desktop.Error{Code: desktop.CodeInternal, Message: "page evaluation timed out"}
		case <-time.After(time.Until(deadline)):
			return nil, &desktop.Error{Code: desktop.CodeInternal, Message: "page evaluation timed out"}
		}
		select {
		case <-ctx.Done():
			return nil, &desktop.Error{Code: desktop.CodeInternal, Message: "page evaluation timed out"}
		case <-time.After(25 * time.Millisecond):
		}
	}
}

func (w *winWindow) Snapshot(ctx context.Context) ([]byte, error) {
	if w.closed.Load() || w.webview == 0 {
		return nil, errWindowClosed()
	}
	if win32.CurrentThreadID() == w.shell.uiThreadID {
		return nil, &desktop.Error{Code: desktop.CodeInternal, Message: "snapshot cannot run on the UI thread"}
	}
	stream, err := win32.CreateStreamOnHGlobal()
	if err != nil {
		return nil, err
	}
	ch := make(chan evalResult, 1)
	cb, err := win32.NewCOMHandler(iidCapturePreviewHandler, func(hr, _ uintptr) uintptr {
		out := evalResult{}
		if int32(hr) < 0 {
			out.err = win32.HRESULTError(int32(hr))
		} else {
			b := win32.CopyHGlobal(win32.HGlobalFromStream(stream))
			if len(b) == 0 {
				out.err = errors.New("WebView2 CapturePreview returned no image data")
			} else {
				out.json = string(b)
			}
		}
		win32.Release(stream)
		select {
		case ch <- out:
		default:
		}
		return 0
	})
	if err != nil {
		win32.Release(stream)
		return nil, err
	}
	callErr := w.shell.Main(func() {
		if w.webview == 0 || w.closed.Load() {
			err = errWindowClosed()
			return
		}
		_, err = win32.COMCall(w.webview, 30, 0 /* PNG */, stream, cb)
	})
	win32.Release(cb)
	if callErr != nil || err != nil {
		win32.Release(stream)
		if callErr != nil {
			return nil, callErr
		}
		return nil, err
	}
	timer := time.NewTimer(webviewTimeout)
	defer timer.Stop()
	select {
	case out := <-ch:
		if out.err != nil {
			return nil, &desktop.Error{Code: desktop.CodeInternal, Message: out.err.Error()}
		}
		return []byte(out.json), nil
	case <-ctx.Done():
		return nil, desktop.ErrCancelled
	case <-timer.C:
		return nil, &desktop.Error{Code: desktop.CodeInternal, Message: "snapshot timed out"}
	}
}

func (w *winWindow) Title() string {
	title := w.title
	_ = w.shell.Main(func() {
		if w.hwnd != 0 {
			title = win32.GetWindowText(w.hwnd)
		}
	})
	if title != "" {
		w.title = title
	}
	return title
}

func (w *winWindow) SetTitle(title string) error {
	w.title = title
	var nativeErr error
	mainErr := w.shell.Main(func() {
		if w.hwnd != 0 {
			nativeErr = win32.SetWindowText(w.hwnd, title)
		}
	})
	if mainErr != nil {
		return mainErr
	}
	return nativeErr
}

func (w *winWindow) Focus() error {
	return w.shell.Main(func() {
		if w.hwnd != 0 {
			if win32.IsIconic(w.hwnd) {
				win32.ShowWindowCmd(w.hwnd, win32.SW_RESTORE)
			} else {
				win32.ShowWindow(w.hwnd)
			}
			win32.BringWindowToTop(w.hwnd)
			win32.SetForegroundWindow(w.hwnd)
		}
	})
}

func (w *winWindow) Native() uintptr { return w.webview }

func (w *winWindow) Frame() (desktop.Frame, error) {
	var out desktop.Frame
	err := w.shell.Main(func() {
		if w.closed.Load() || !win32.IsWindow(w.hwnd) {
			return
		}
		if win32.IsIconic(w.hwnd) || win32.IsZoomed(w.hwnd) {
			out = w.frame
			return
		}
		l, t, r, b, ok := win32.GetWindowRect(w.hwnd)
		if !ok {
			return
		}
		out = frameFromWindowRect(w.hwnd, l, t, r, b)
		w.frame = out
	})
	if err != nil {
		return desktop.Frame{}, err
	}
	if out.Width == 0 || out.Height == 0 {
		return desktop.Frame{}, errWindowClosed()
	}
	return out, nil
}

func (w *winWindow) SetFrame(f desktop.Frame) error {
	if f.Width <= 0 || f.Height <= 0 {
		return &desktop.Error{Code: desktop.CodeInvalidInput, Message: "frame width and height must be positive"}
	}
	var nativeErr error
	err := w.shell.Main(func() {
		screenDPI := win32.SystemDPI()
		sizeDPI := windowDPI(w.hwnd)
		nativeErr = win32.MoveWindow(w.hwnd,
			int(pointsToPixels(f.X, screenDPI)), int(pointsToPixels(f.Y, screenDPI)),
			int(pointsToPixels(f.Width, sizeDPI)), int(pointsToPixels(f.Height, sizeDPI)), true)
	})
	if err == nil {
		if nativeErr != nil {
			return nativeErr
		}
		w.frame = f
		if w.shell.onFrame != nil {
			go w.shell.onFrame(w.id, f)
		}
	}
	return err
}

func (w *winWindow) SetSidebarWidth(points int) error {
	if points < 0 || points > desktop.MaxSidebarWidth {
		return &desktop.Error{Code: desktop.CodeInvalidInput, Message: "sidebar width is outside the supported range"}
	}
	w.sidebarWidth = points
	return nil
}

func (w *winWindow) Close() error {
	if w.hwnd == 0 || w.closed.Load() {
		return nil
	}
	return win32.PostMessage(w.hwnd, wmClose, 0, 0)
}

func errWindowClosed() error {
	return &desktop.Error{Code: desktop.CodeUnsupported, Message: "window is not open"}
}
