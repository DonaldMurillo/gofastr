//go:build linux && (amd64 || arm64)

package linux

import (
	"context"
	"fmt"
	"html"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"github.com/DonaldMurillo/gofastr/battery/desktop"
	"github.com/DonaldMurillo/gofastr/battery/desktop/internal/ffi"
	"github.com/DonaldMurillo/gofastr/battery/desktop/internal/gtk"
)

const (
	capsResponseAccept = -3
	capsResponseDeny   = 101
	capsResponseOnce   = 102
	capsResponseAllow  = 103
)

// C retains integer tokens, never Go pointers. All requests share these
// callbacks, so repeated clipboard reads and dialogs cannot exhaust ffi's
// process-lifetime callback table.
var (
	capsOperations sync.Map // uintptr -> *capsOperation
	capsNextID     atomic.Uintptr
	// Cancellation cleanup must survive a busy GTK loop. Unlike Main work,
	// this idle source is never withdrawn when a dispatch deadline expires.
	capsCleanupCallback = ffi.NewCallback(func(a *ffi.Args) uintptr {
		if v, ok := capsOperations.Load(a.Int[0]); ok {
			v.(*capsOperation).close(capsResult{err: desktop.ErrCancelled})
		}
		return 0
	})
	capsResponseCallback = ffi.NewCallback(func(a *ffi.Args) uintptr {
		if v, ok := capsOperations.Load(a.Int[2]); ok {
			op := v.(*capsOperation)
			r := capsResult{response: int32(a.Int[1])}
			if op.cancelled.Load() {
				r.err = desktop.ErrCancelled
			} else if op.native {
				if r.response == capsResponseAccept {
					r.paths = capsSelectedFiles(op.handle)
					if len(r.paths) == 0 {
						r.err = desktop.ErrCancelled
					}
				} else {
					r.err = desktop.ErrCancelled
				}
			}
			op.close(r)
		}
		return 0
	})
	capsDestroyCallback = ffi.NewCallback(func(a *ffi.Args) uintptr {
		if v, ok := capsOperations.Load(a.Int[1]); ok {
			op := v.(*capsOperation)
			op.handle = 0 // GTK is already destroying the widget.
			op.close(capsResult{err: desktop.ErrCancelled})
		}
		return 0
	})
	capsClipboardCallback = ffi.NewCallback(func(a *ffi.Args) uintptr {
		if v, ok := capsOperations.LoadAndDelete(a.Int[2]); ok {
			op := v.(*capsOperation)
			r := capsResult{text: capsGoString(a.Int[1])}
			if op.cancelled.Load() {
				r = capsResult{err: desktop.ErrCancelled}
			}
			op.publish(r)
		}
		return 0
	})
)

type capsResult struct {
	text     string
	paths    []string
	response int32
	err      error
}

type capsOperation struct {
	shell         *linuxShell
	id            uintptr
	done          chan struct{}
	once          sync.Once
	result        capsResult // written before done closes
	cancelled     atomic.Bool
	started       atomic.Bool
	cleanupQueued atomic.Bool
	// The native fields are only touched on the GTK thread.
	handle uintptr
	native bool
	prompt bool
}

func newCapsOperation(s *linuxShell, native, prompt bool) *capsOperation {
	op := &capsOperation{shell: s, id: capsNextID.Add(1), done: make(chan struct{}), native: native, prompt: prompt}
	capsOperations.Store(op.id, op)
	return op
}

func (op *capsOperation) publish(r capsResult) {
	op.once.Do(func() {
		op.result = r
		close(op.done)
	})
}

// close runs on the GTK thread. Remove the registry entry before destroying
// a widget: its destroy signal may reenter Go synchronously.
func (op *capsOperation) close(r capsResult) {
	capsOperations.Delete(op.id)
	if handle := op.handle; handle != 0 {
		op.handle = 0
		if op.native {
			capsCall(gtk.Sym.GtkNativeDialogDestroy, handle)
			capsCall(gtk.Sym.GObjectUnref, handle)
		} else {
			capsCall(gtk.Sym.GtkWidgetDestroy, handle)
		}
	}
	op.publish(r)
}

// queueCleanup is safe from any goroutine. g_idle_add is thread-safe and keeps
// the callback pending until GTK can dispatch it; no timeout can discard it.
// The token is inert if a response or shell shutdown already closed the object.
func (op *capsOperation) queueCleanup() {
	op.cancelled.Store(true)
	if !op.shell.running.Load() || gtk.Sym.GIdleAdd == 0 || !op.cleanupQueued.CompareAndSwap(false, true) {
		return
	}
	if capsCall(gtk.Sym.GIdleAdd, capsCleanupCallback, op.id) == 0 {
		op.cleanupQueued.Store(false)
	}
}

// closeCapabilities must run before gtk_main exits, while native objects can
// still be destroyed on their owning thread.
func (s *linuxShell) closeCapabilities() {
	capsOperations.Range(func(_, value any) bool {
		op := value.(*capsOperation)
		if op.shell == s {
			op.cancelled.Store(true)
			op.close(capsResult{err: desktop.ErrCancelled})
		}
		return true
	})
}

func capsContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

// waitCaps also handles calls made from a ready callback on the GTK thread.
// Iterating the main context there keeps response and cancellation callbacks
// dispatching instead of deadlocking on our own UI thread.
func (s *linuxShell) waitCaps(ctx context.Context, op *capsOperation) capsResult {
	if s.uiThread.Load() == int64(syscall.Gettid()) {
		for {
			if s.quitting.Load() {
				op.cancelled.Store(true)
				op.close(capsResult{err: desktop.ErrCancelled})
				return capsResult{err: desktop.ErrCancelled}
			}
			select {
			case <-op.done:
				if ctx.Err() != nil {
					return capsResult{err: desktop.ErrCancelled}
				}
				return op.result
			case <-ctx.Done():
				op.cancelled.Store(true)
				op.close(capsResult{err: desktop.ErrCancelled})
				return capsResult{err: desktop.ErrCancelled}
			case <-s.done:
				op.close(capsResult{err: desktop.ErrCancelled})
				return capsResult{err: desktop.ErrCancelled}
			default:
				capsCall(gtk.Sym.GMainContextIteration, 0, 0)
				time.Sleep(time.Millisecond)
			}
		}
	}
	select {
	case <-op.done:
		if ctx.Err() != nil {
			return capsResult{err: desktop.ErrCancelled}
		}
		return op.result
	case <-ctx.Done():
		// Return promptly; cleanup remains queued even through a long UI stall.
		op.queueCleanup()
		return capsResult{err: desktop.ErrCancelled}
	case <-s.done:
		return capsResult{err: desktop.ErrCancelled}
	}
}

func (s *linuxShell) launchCaps(ctx context.Context, op *capsOperation, create func() error) error {
	launch := func() error {
		err := s.Main(func() {
			op.started.Store(true)
			// Main can time out while a GTK constructor is still running.
			// Once that call returns, destroy anything the abandoned
			// operation created, even if a second dispatch also timed out.
			defer func() {
				if op.cancelled.Load() {
					op.close(capsResult{err: desktop.ErrCancelled})
				}
			}()
			if ctx.Err() != nil || op.cancelled.Load() {
				op.close(capsResult{err: desktop.ErrCancelled})
				return
			}
			if err := create(); err != nil {
				op.close(capsResult{err: err})
			}
		})
		if err != nil {
			op.cancelled.Store(true)
			op.publish(capsResult{err: err})
			if !op.started.Load() {
				// A queued closure will either be discarded by Main or
				// observe cancellation before creating any native object.
				capsOperations.Delete(op.id)
			} else {
				// The closure may already have returned after showing an
				// asynchronous dialog; its deferred check is then too late.
				op.queueCleanup()
			}
		}
		return err
	}
	if s.uiThread.Load() == int64(syscall.Gettid()) {
		return launch()
	}
	// Do not let the dispatch deadline override a shorter request context.
	launched := make(chan error, 1)
	go func() { launched <- launch() }()
	select {
	case err := <-launched:
		return err
	case <-ctx.Done():
		op.queueCleanup()
		return desktop.ErrCancelled
	case <-s.done:
		op.cancelled.Store(true)
		return desktop.ErrCancelled
	}
}

func (s *linuxShell) capsParent() uintptr {
	s.mu.Lock()
	defer s.mu.Unlock()
	if w := s.windows[desktop.MainWindowID]; w != nil && !w.closed.Load() {
		return w.handle
	}
	return 0
}

// acquireCaps serializes user-facing prompts and pickers. Clipboard requests
// remain independent and can complete while a dialog is up.
func (s *linuxShell) acquireCaps(ctx context.Context) (func(), error) {
	s.mu.Lock()
	if s.capGate == nil {
		s.capGate = make(chan struct{}, 1)
	}
	gate := s.capGate
	s.mu.Unlock()
	if s.uiThread.Load() == int64(syscall.Gettid()) {
		select {
		case gate <- struct{}{}:
			return func() { <-gate }, nil
		case <-ctx.Done():
			return nil, desktop.ErrCancelled
		case <-s.done:
			return nil, desktop.ErrCancelled
		default:
			// A reentrant UI callback must not wait on an outer dialog's
			// gate: the outer stack cannot release it until we return.
			return nil, fmt.Errorf("desktop/linux: a native dialog is already open")
		}
	}
	select {
	case gate <- struct{}{}:
		return func() { <-gate }, nil
	case <-ctx.Done():
		return nil, desktop.ErrCancelled
	case <-s.done:
		return nil, desktop.ErrCancelled
	}
}

func (s *linuxShell) Prompt(ctx context.Context, req desktop.PermissionRequest) (desktop.Decision, error) {
	ctx = capsContext(ctx)
	if ctx.Err() != nil {
		return desktop.DecisionDeny, desktop.ErrCancelled
	}
	s.mu.Lock()
	if len(s.promptQueue) != 0 {
		decision := s.promptQueue[0]
		s.promptQueue = s.promptQueue[1:]
		s.mu.Unlock()
		return decision, nil
	}
	s.mu.Unlock()
	words := desktop.PromptText(ctx, req)
	message := words.Message
	if req.Description != "" {
		message += "\n\n" + req.Description
	}
	if !capsValidText(message) {
		return desktop.DecisionDeny, capsInvalid("permission description must be valid text")
	}
	release, err := s.acquireCaps(ctx)
	if err != nil {
		return desktop.DecisionDeny, err
	}
	defer release()
	op := newCapsOperation(s, false, true)
	if err := s.launchCaps(ctx, op, func() error {
		format, text := capsCString("%s"), capsCString(message)
		op.handle = capsCall(gtk.Sym.GtkMessageDialogNew, s.capsParent(), 1, 2, 0,
			uintptr(unsafe.Pointer(&format[0])), uintptr(unsafe.Pointer(&text[0])))
		runtime.KeepAlive(format)
		runtime.KeepAlive(text)
		if op.handle == 0 {
			return fmt.Errorf("desktop/linux: create permission dialog")
		}
		capsSetString(gtk.Sym.GtkWindowSetTitle, op.handle, words.Title)
		for _, button := range []struct {
			text     string
			response uintptr
		}{
			{words.Deny, capsResponseDeny}, {words.AllowOnce, capsResponseOnce}, {words.Allow, capsResponseAllow},
		} {
			label := capsCString(button.text)
			capsCall(gtk.Sym.GtkDialogAddButton, op.handle, uintptr(unsafe.Pointer(&label[0])), button.response)
			runtime.KeepAlive(label)
		}
		capsCall(gtk.Sym.GtkDialogSetDefaultResponse, op.handle, capsResponseDeny)
		capsConnect(op.handle, "response", capsResponseCallback, op.id)
		capsConnect(op.handle, "destroy", capsDestroyCallback, op.id)
		capsCall(gtk.Sym.GtkWidgetShowAll, op.handle)
		return nil
	}); err != nil {
		return desktop.DecisionDeny, err
	}
	r := s.waitCaps(ctx, op)
	if r.err != nil {
		return desktop.DecisionDeny, r.err
	}
	return capsDecision(r.response), nil
}

func capsDecision(response int32) desktop.Decision {
	switch response {
	case capsResponseAllow:
		return desktop.DecisionAllow
	case capsResponseOnce:
		return desktop.DecisionAllowOnce
	default:
		return desktop.DecisionDeny
	}
}

func (s *linuxShell) ScriptPrompts(ds ...desktop.Decision) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, d := range ds {
		if d < desktop.DecisionDeny || d > desktop.DecisionAllow {
			d = desktop.DecisionDeny
		}
		s.promptQueue = append(s.promptQueue, d)
	}
}

func (s *linuxShell) ClickPrompt(button string) error {
	var response uintptr
	switch strings.ToLower(strings.TrimSpace(button)) {
	case "allow":
		response = capsResponseAllow
	case "allow once":
		response = capsResponseOnce
	case "deny":
		response = capsResponseDeny
	default:
		return capsInvalid("unknown permission button")
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		found := false
		if err := s.Main(func() {
			capsOperations.Range(func(_, value any) bool {
				op := value.(*capsOperation)
				if op.shell == s && op.prompt && op.handle != 0 {
					found = true
					capsCall(gtk.Sym.GtkDialogResponse, op.handle, response)
					return false
				}
				return true
			})
		}); err != nil {
			return err
		}
		if found {
			return nil
		}
		time.Sleep(10 * time.Millisecond)
	}
	return &desktop.Error{Code: desktop.CodeNotFound, Message: "no permission dialog is open"}
}

func (s *linuxShell) Clipboard() desktop.Clipboard { return s }
func (s *linuxShell) Dialogs() desktop.Dialogs     { return s }

func (s *linuxShell) ReadText(ctx context.Context) (string, error) {
	ctx = capsContext(ctx)
	if ctx.Err() != nil {
		return "", desktop.ErrCancelled
	}
	op := newCapsOperation(s, false, false)
	if err := s.launchCaps(ctx, op, func() error {
		clipboard := capsClipboard()
		if clipboard == 0 {
			return fmt.Errorf("desktop/linux: clipboard unavailable")
		}
		capsCall(gtk.Sym.GtkClipboardRequestText, clipboard, capsClipboardCallback, op.id)
		return nil
	}); err != nil {
		return "", err
	}
	r := s.waitCaps(ctx, op)
	return r.text, r.err
}

func (s *linuxShell) WriteText(ctx context.Context, text string) error {
	ctx = capsContext(ctx)
	if ctx.Err() != nil {
		return desktop.ErrCancelled
	}
	if !capsValidText(text) || uint64(len(text)) > 1<<31-1 {
		return capsInvalid("clipboard content must be valid text")
	}
	op := newCapsOperation(s, false, false)
	if err := s.launchCaps(ctx, op, func() error {
		clipboard := capsClipboard()
		if clipboard == 0 {
			return fmt.Errorf("desktop/linux: clipboard unavailable")
		}
		b := capsCString(text)
		capsCall(gtk.Sym.GtkClipboardSetText, clipboard, uintptr(unsafe.Pointer(&b[0])), uintptr(len(text)))
		runtime.KeepAlive(b)
		op.close(capsResult{})
		return nil
	}); err != nil {
		return err
	}
	return s.waitCaps(ctx, op).err
}

// libnotify is optional; GTK/WebKit still work when it is not installed.
func (s *linuxShell) Notifier() desktop.Notifier { return s }

func (s *linuxShell) Show(ctx context.Context, n desktop.Notification) error {
	ctx = capsContext(ctx)
	if ctx.Err() != nil {
		return desktop.ErrCancelled
	}
	if !capsValidText(n.Title) || !capsValidText(n.Subtitle) || !capsValidText(n.Body) {
		return capsInvalid("notification must be valid text")
	}
	op := newCapsOperation(s, false, false)
	if err := s.launchCaps(ctx, op, func() error {
		if gtk.Sym.NotifyInit == 0 || gtk.Sym.NotifySetAppName == 0 || gtk.Sym.NotifyGetServerCaps == 0 || gtk.Sym.NotifyNotificationNew == 0 || gtk.Sym.NotifyNotificationShow == 0 {
			return desktop.ErrUnsupported
		}
		appName := s.cfg.Title
		if appName == "" {
			appName = "GoFastr"
		}
		app := capsCString(appName)
		initialized := capsCall(gtk.Sym.NotifyInit, uintptr(unsafe.Pointer(&app[0])))
		if initialized == 0 {
			runtime.KeepAlive(app)
			return fmt.Errorf("desktop/linux: initialize notifications")
		}
		// notify_init keeps the first caller's name for the life of the
		// process. A later shell lifetime with another title must set its own.
		capsCall(gtk.Sym.NotifySetAppName, uintptr(unsafe.Pointer(&app[0])))
		runtime.KeepAlive(app)
		body := n.Body
		if n.Subtitle != "" {
			body = n.Subtitle + "\n" + body
		}
		// Capabilities belong to the current daemon; do not cache across owner
		// replacement. Plain-body daemons display entity escapes literally.
		body = capsNotificationBody(body)
		title, content := capsCString(n.Title), capsCString(body)
		notification := capsCall(gtk.Sym.NotifyNotificationNew, uintptr(unsafe.Pointer(&title[0])), uintptr(unsafe.Pointer(&content[0])), 0)
		runtime.KeepAlive(title)
		runtime.KeepAlive(content)
		if notification == 0 {
			return fmt.Errorf("desktop/linux: create notification")
		}
		defer capsCall(gtk.Sym.GObjectUnref, notification)
		var nativeError uintptr
		shown := capsCall(gtk.Sym.NotifyNotificationShow, notification, uintptr(unsafe.Pointer(&nativeError)))
		runtime.KeepAlive(&nativeError)
		if nativeError != 0 {
			capsCall(gtk.Sym.GErrorFree, nativeError)
		}
		if shown == 0 {
			return fmt.Errorf("desktop/linux: show notification")
		}
		op.close(capsResult{})
		return nil
	}); err != nil {
		return err
	}
	return s.waitCaps(ctx, op).err
}

func capsNotificationBody(body string) string {
	list := capsCall(gtk.Sym.NotifyGetServerCaps)
	if list == 0 {
		return html.EscapeString(body)
	}
	defer capsCall(gtk.Sym.GListFreeFull, list, gtk.Sym.GFree)
	// The list and strings are owned. NULL means either failure or no caps;
	// only an explicit plain-body capability allows sending unescaped text.
	type glist struct{ data, next, previous uintptr }
	plain, markup := false, false
	for node := list; node != 0; {
		item := *(*glist)(capsNativePointer(node))
		switch capsGoString(item.data) {
		case "body":
			plain = true
		case "body-markup":
			markup = true
		}
		node = item.next
	}
	if plain && !markup {
		return body
	}
	return html.EscapeString(body)
}

func capsClipboard() uintptr {
	display := capsCall(gtk.Sym.GdkDisplayGetDefault)
	if display == 0 {
		return 0
	}
	return capsCall(gtk.Sym.GtkClipboardGetDefault, display)
}

func (s *linuxShell) OpenFile(ctx context.Context, opts desktop.OpenOptions) ([]string, error) {
	return s.chooseFiles(ctx, 0, dialogOpenFile.Text(ctx), dialogOpen.Text(ctx), opts, "")
}

func (s *linuxShell) SaveFile(ctx context.Context, opts desktop.SaveOptions) (string, error) {
	paths, err := s.chooseFiles(ctx, 1, dialogSaveFile.Text(ctx), dialogSave.Text(ctx), desktop.OpenOptions{}, opts.DefaultName)
	if err != nil {
		return "", err
	}
	return paths[0], nil
}

func (s *linuxShell) OpenFolder(ctx context.Context) (string, error) {
	paths, err := s.chooseFiles(ctx, 2, dialogOpenFolder.Text(ctx), dialogSelect.Text(ctx), desktop.OpenOptions{}, "")
	if err != nil {
		return "", err
	}
	return paths[0], nil
}

func (s *linuxShell) chooseFiles(ctx context.Context, action uintptr, title, accept string, opts desktop.OpenOptions, name string) ([]string, error) {
	ctx = capsContext(ctx)
	if ctx.Err() != nil {
		return nil, desktop.ErrCancelled
	}
	patterns, err := desktop.CheckFileDialog(opts, name)
	if err != nil {
		return nil, err
	}
	release, err := s.acquireCaps(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	op := newCapsOperation(s, true, false)
	if err := s.launchCaps(ctx, op, func() error {
		t, a, c := capsCString(title), capsCString(accept), capsCString(dialogCancel.Text(ctx))
		op.handle = capsCall(gtk.Sym.GtkFileChooserNativeNew, uintptr(unsafe.Pointer(&t[0])), s.capsParent(), action,
			uintptr(unsafe.Pointer(&a[0])), uintptr(unsafe.Pointer(&c[0])))
		runtime.KeepAlive(t)
		runtime.KeepAlive(a)
		runtime.KeepAlive(c)
		if op.handle == 0 {
			return fmt.Errorf("desktop/linux: create file chooser")
		}
		capsCall(gtk.Sym.GtkNativeDialogSetModal, op.handle, 1)
		capsCall(gtk.Sym.GtkFileChooserSetLocalOnly, op.handle, 1)
		if opts.Multiple {
			capsCall(gtk.Sym.GtkFileChooserSetSelectMultiple, op.handle, 1)
		}
		if action == 1 {
			capsCall(gtk.Sym.GtkFileChooserSetDoOverwriteConfirmation, op.handle, 1)
		}
		if name != "" {
			capsSetString(gtk.Sym.GtkFileChooserSetCurrentName, op.handle, name)
		}
		for i, filter := range opts.Filters {
			f := capsCall(gtk.Sym.GtkFileFilterNew)
			if f == 0 {
				return fmt.Errorf("desktop/linux: create file filter")
			}
			label := filter.Name
			if label == "" {
				label = strings.Join(patterns[i], ", ")
			}
			capsSetString(gtk.Sym.GtkFileFilterSetName, f, label)
			for _, pattern := range patterns[i] {
				capsSetString(gtk.Sym.GtkFileFilterAddPattern, f, pattern)
			}
			capsCall(gtk.Sym.GtkFileChooserAddFilter, op.handle, f) // takes the floating reference
		}
		capsConnect(op.handle, "response", capsResponseCallback, op.id)
		capsCall(gtk.Sym.GtkNativeDialogShow, op.handle)
		return nil
	}); err != nil {
		return nil, err
	}
	r := s.waitCaps(ctx, op)
	return r.paths, r.err
}

func capsSelectedFiles(chooser uintptr) []string {
	list := capsCall(gtk.Sym.GtkFileChooserGetFilenames, chooser)
	if list == 0 {
		return nil
	}
	defer capsCall(gtk.Sym.GSListFree, list)
	type gslist struct{ data, next uintptr }
	var paths []string
	for node := list; node != 0; {
		item := *(*gslist)(capsNativePointer(node))
		if item.data != 0 {
			path := capsGoString(item.data)
			capsCall(gtk.Sym.GFree, item.data)
			if path != "" {
				paths = append(paths, path)
			}
		}
		node = item.next
	}
	return paths
}

func capsConnect(object uintptr, signal string, callback, token uintptr) {
	b := capsCString(signal)
	capsCall(gtk.Sym.GSignalConnectData, object, uintptr(unsafe.Pointer(&b[0])), callback, token, 0, 0)
	runtime.KeepAlive(b)
}

func capsSetString(fn, object uintptr, value string) {
	b := capsCString(value)
	capsCall(fn, object, uintptr(unsafe.Pointer(&b[0])))
	runtime.KeepAlive(b)
}

func capsCString(s string) []byte { return append([]byte(s), 0) }
func capsValidText(s string) bool { return desktop.NativeText(s) }
func capsInvalid(message string) error {
	return &desktop.Error{Code: desktop.CodeInvalidInput, Message: message}
}

// capsNativePointer reinterprets a foreign address returned by GTK/GLib.
// These addresses are not retained pointers into Go-managed memory.
func capsNativePointer(p uintptr) unsafe.Pointer {
	return *(*unsafe.Pointer)(unsafe.Pointer(&p))
}

func capsGoString(ptr uintptr) string {
	if ptr == 0 {
		return ""
	}
	p := capsNativePointer(ptr)
	n := 0
	for *(*byte)(unsafe.Add(p, n)) != 0 {
		n++
	}
	return string(unsafe.Slice((*byte)(p), n))
}

//go:uintptrescapes
func capsCall(fn uintptr, args ...uintptr) uintptr {
	r, _, _ := ffi.Call(fn, args, nil)
	return r
}
