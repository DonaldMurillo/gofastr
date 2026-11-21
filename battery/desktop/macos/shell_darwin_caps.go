//go:build darwin && (arm64 || amd64)

package macos

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"sync"
	"time"

	"github.com/DonaldMurillo/gofastr/battery/desktop"
	"github.com/DonaldMurillo/gofastr/battery/desktop/internal/ffi"
	"github.com/DonaldMurillo/gofastr/battery/desktop/internal/objc"
)

const nsPasteboardTypeString = "public.utf8-plain-text"

const modalTimeout = 10 * time.Minute

func (s *darwinShell) Prompt(ctx context.Context, req desktop.PermissionRequest) (desktop.Decision, error) {
	if err := ctx.Err(); err != nil {
		return desktop.DecisionDeny, desktop.ErrCancelled
	}
	decision, queued := s.popPromptDecision()
	if queued {
		return decision, nil
	}

	var res promptResult
	words := desktop.PromptText(ctx, req)
	if err := s.onMainWithTimeout(modalTimeout, func() {
		alert := objc.ID(objc.Send(objc.ID(objc.Send(objc.Class("NSAlert"), objc.Sel("alloc"))), objc.Sel("init")))
		objc.Send(alert, objc.Sel("setMessageText:"), uintptr(objc.NSString(words.Message)))
		objc.Send(alert, objc.Sel("setInformativeText:"), uintptr(objc.NSString(req.Description)))
		objc.Send(alert, objc.Sel("addButtonWithTitle:"), uintptr(objc.NSString(words.Allow)))
		objc.Send(alert, objc.Sel("addButtonWithTitle:"), uintptr(objc.NSString(words.AllowOnce)))
		objc.Send(alert, objc.Sel("addButtonWithTitle:"), uintptr(objc.NSString(words.Deny)))
		switch objc.Send(alert, objc.Sel("runModal")) {
		case 1000:
			res.set(desktop.DecisionAllow)
		case 1001:
			res.set(desktop.DecisionAllowOnce)
		default:
			res.set(desktop.DecisionDeny)
		}
	}); err != nil {
		return desktop.DecisionDeny, &desktop.Error{Code: desktop.CodeInternal, Message: "permission alert timed out"}
	}
	return res.get(), nil
}

type promptResult struct {
	mu sync.Mutex
	d  desktop.Decision
}

func (p *promptResult) set(d desktop.Decision) {
	p.mu.Lock()
	p.d = d
	p.mu.Unlock()
}

func (p *promptResult) get() desktop.Decision {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.d
}

func (s *darwinShell) setPromptOverride(d desktop.Decision) {
	s.mu.Lock()
	s.promptQueue = append(s.promptQueue, d)
	s.mu.Unlock()
}

func (s *darwinShell) popPromptDecision() (desktop.Decision, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.promptQueue) == 0 {
		return 0, false
	}
	d := s.promptQueue[0]
	s.promptQueue = s.promptQueue[1:]
	return d, true
}

func (s *darwinShell) Clipboard() desktop.Clipboard { return s }

func generalPasteboard() objc.ID {
	return objc.ID(objc.Send(objc.Class("NSPasteboard"), objc.Sel("generalPasteboard")))
}

func (s *darwinShell) ReadText(ctx context.Context) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", desktop.ErrCancelled
	}
	var text string
	if err := s.onMain(func() {
		pb := generalPasteboard()
		if pb == 0 {
			return
		}
		if str := objc.Send(pb, objc.Sel("stringForType:"), uintptr(objc.NSString(nsPasteboardTypeString))); str != 0 {
			text = objc.GoString(objc.ID(str))
		}
	}); err != nil {
		return "", &desktop.Error{Code: desktop.CodeInternal, Message: desktop.InternalErrorMsg}
	}
	return text, nil
}

func (s *darwinShell) WriteText(ctx context.Context, text string) error {
	if err := ctx.Err(); err != nil {
		return desktop.ErrCancelled
	}
	if err := s.onMain(func() {
		pb := generalPasteboard()
		if pb == 0 {
			return
		}
		objc.Send(pb, objc.Sel("clearContents"))
		objc.Send(pb, objc.Sel("setString:forType:"), uintptr(objc.NSString(text)), uintptr(objc.NSString(nsPasteboardTypeString)))
	}); err != nil {
		return &desktop.Error{Code: desktop.CodeInternal, Message: desktop.InternalErrorMsg}
	}
	return nil
}

func (s *darwinShell) Dialogs() desktop.Dialogs { return s }

const nsModalOK = 1

func urlPath(u objc.ID) string {
	if u == 0 {
		return ""
	}
	return objc.GoString(objc.ID(objc.Send(u, objc.Sel("path"))))
}

func (s *darwinShell) OpenFile(ctx context.Context, opts desktop.OpenOptions) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, desktop.ErrCancelled
	}
	if _, err := desktop.CheckFileDialog(opts, ""); err != nil {
		return nil, err
	}
	var paths []string
	if err := s.onMainWithTimeout(modalTimeout, func() {
		panel := objc.ID(objc.Send(objc.ID(objc.Send(objc.Class("NSOpenPanel"), objc.Sel("alloc"))), objc.Sel("init")))
		objc.Send(panel, objc.Sel("setCanChooseFiles:"), 1)
		objc.Send(panel, objc.Sel("setCanChooseDirectories:"), 0)
		if opts.Multiple {
			objc.Send(panel, objc.Sel("setAllowsMultipleSelection:"), 1)
		}
		if types := allowedContentTypes(opts.Filters); types != 0 {
			objc.Send(panel, objc.Sel("setAllowedContentTypes:"), types)
		}
		if objc.Send(panel, objc.Sel("runModal")) != nsModalOK {
			return
		}
		urls := objc.ID(objc.Send(panel, objc.Sel("URLs")))
		n := objc.Send(urls, objc.Sel("count"))
		for i := uintptr(0); i < n; i++ {
			if p := urlPath(objc.ID(objc.Send(urls, objc.Sel("objectAtIndex:"), i))); p != "" {
				paths = append(paths, p)
			}
		}
	}); err != nil {
		return nil, &desktop.Error{Code: desktop.CodeInternal, Message: desktop.InternalErrorMsg}
	}
	if paths == nil {
		return nil, desktop.ErrCancelled
	}
	return paths, nil
}

func (s *darwinShell) SaveFile(ctx context.Context, opts desktop.SaveOptions) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", desktop.ErrCancelled
	}
	if _, err := desktop.CheckFileDialog(desktop.OpenOptions{}, opts.DefaultName); err != nil {
		return "", err
	}
	var path string
	ok := false
	if err := s.onMainWithTimeout(modalTimeout, func() {
		panel := objc.ID(objc.Send(objc.ID(objc.Send(objc.Class("NSSavePanel"), objc.Sel("alloc"))), objc.Sel("init")))
		if opts.DefaultName != "" {
			objc.Send(panel, objc.Sel("setNameFieldStringValue:"), uintptr(objc.NSString(opts.DefaultName)))
		}
		if objc.Send(panel, objc.Sel("runModal")) != nsModalOK {
			return
		}
		if p := urlPath(objc.ID(objc.Send(panel, objc.Sel("URL")))); p != "" {
			path = p
			ok = true
		}
	}); err != nil {
		return "", &desktop.Error{Code: desktop.CodeInternal, Message: desktop.InternalErrorMsg}
	}
	if !ok {
		return "", desktop.ErrCancelled
	}
	return path, nil
}

func (s *darwinShell) OpenFolder(ctx context.Context) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", desktop.ErrCancelled
	}
	var path string
	ok := false
	if err := s.onMainWithTimeout(modalTimeout, func() {
		panel := objc.ID(objc.Send(objc.ID(objc.Send(objc.Class("NSOpenPanel"), objc.Sel("alloc"))), objc.Sel("init")))
		objc.Send(panel, objc.Sel("setCanChooseFiles:"), 0)
		objc.Send(panel, objc.Sel("setCanChooseDirectories:"), 1)
		if objc.Send(panel, objc.Sel("runModal")) != nsModalOK {
			return
		}
		if p := urlPath(objc.ID(objc.Send(panel, objc.Sel("URL")))); p != "" {
			path = p
			ok = true
		}
	}); err != nil {
		return "", &desktop.Error{Code: desktop.CodeInternal, Message: desktop.InternalErrorMsg}
	}
	if !ok {
		return "", desktop.ErrCancelled
	}
	return path, nil
}

var utiOnce sync.Once
var utiAvailable bool

func loadUTIs() bool {
	utiOnce.Do(func() {
		_, err := objc.DlopenGlobal("/System/Library/Frameworks/UniformTypeIdentifiers.framework/UniformTypeIdentifiers")
		utiAvailable = err == nil
	})
	return utiAvailable
}

func allowedContentTypes(filters []desktop.FileFilter) uintptr {
	if len(filters) == 0 || !loadUTIs() {
		return 0
	}
	arr := objc.ID(objc.Send(objc.Class("NSMutableArray"), objc.Sel("array")))
	if arr == 0 {
		return 0
	}
	added := uintptr(0)
	for _, f := range filters {
		for _, ext := range f.Extensions {
			t := objc.Send(objc.Class("UTType"), objc.Sel("typeWithFilenameExtension:"), uintptr(objc.NSString(ext)))
			if t == 0 {
				continue
			}
			objc.Send(arr, objc.Sel("addObject:"), t)
			added++
		}
	}
	if added == 0 {
		return 0
	}
	return uintptr(arr)
}

func (s *darwinShell) Notifier() desktop.Notifier { return s }

func notificationID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b[:]), nil
}

const unAuthOptions = 1 | 2 | 4

const notifTimeout = 10 * time.Second

var errNeedsBundle = &desktop.Error{
	Code:    desktop.CodeUnsupported,
	Message: "notifications need an app bundle; run from gofastr desktop build output",
}

func (s *darwinShell) Show(ctx context.Context, n desktop.Notification) error {
	s.recordNotification(n)
	if err := ctx.Err(); err != nil {
		return desktop.ErrCancelled
	}
	bundled := false
	if err := s.onMain(func() {
		bundle := objc.ID(objc.Send(objc.Class("NSBundle"), objc.Sel("mainBundle")))
		if bundle == 0 {
			return
		}
		if id := objc.Send(bundle, objc.Sel("bundleIdentifier")); id != 0 {
			bundled = true
		}
	}); err != nil {
		return &desktop.Error{Code: desktop.CodeInternal, Message: desktop.InternalErrorMsg}
	}
	if !bundled {
		return errNeedsBundle
	}

	center := objc.ID(objc.Send(objc.Class("UNUserNotificationCenter"), objc.Sel("currentNotificationCenter")))
	if center == 0 {
		return &desktop.Error{Code: desktop.CodeInternal, Message: desktop.InternalErrorMsg}
	}
	s.notifyDelegateOnce.Do(func() {
		_ = s.onMain(func() {
			objc.Send(center, objc.Sel("setDelegate:"), uintptr(s.bridgeID()))
		})
	})

	s.notifyMu.Lock()
	defer s.notifyMu.Unlock()
	s.notifyOnce.Do(func() {
		s.authRes = make(chan notifyResult, 1)
		s.addRes = make(chan notifyResult, 1)
		s.authBlock = objc.NewBlock(ffi.NewCallbackAnyThread(func(a *ffi.Args) uintptr {
			select {
			case s.authRes <- nsErrorResult(a.Int[1] != 0, objc.ID(a.Int[2])):
			default:
			}
			return 0
		}))
		s.addBlock = objc.NewBlock(ffi.NewCallbackAnyThread(func(a *ffi.Args) uintptr {
			select {
			case s.addRes <- nsErrorResult(true, objc.ID(a.Int[1])):
			default:
			}
			return 0
		}))
	})
	select {
	case <-s.authRes:
	default:
	}
	select {
	case <-s.addRes:
	default:
	}
	authRes, addRes, authBlock, addBlock := s.authRes, s.addRes, s.authBlock, s.addBlock
	if err := s.onMain(func() {
		objc.Send(center, objc.Sel("requestAuthorizationWithOptions:completionHandler:"), unAuthOptions, authBlock)
	}); err != nil {
		return &desktop.Error{Code: desktop.CodeInternal, Message: desktop.InternalErrorMsg}
	}
	var auth notifyResult
	select {
	case auth = <-authRes:
	case <-time.After(notifTimeout):
		return &desktop.Error{Code: desktop.CodeInternal, Message: "notification authorization timed out"}
	}
	if auth.notAllowed {
		return errNotAllowed
	}
	if auth.errText != "" {
		s.logger.Error("desktop: notification authorization failed", "error", auth.errText)
		return &desktop.Error{Code: desktop.CodeInternal, Message: desktop.InternalErrorMsg}
	}
	if !auth.granted {
		return nil
	}

	content := objc.ID(objc.Send(objc.ID(objc.Send(objc.Class("UNMutableNotificationContent"), objc.Sel("alloc"))), objc.Sel("init")))
	objc.Send(content, objc.Sel("setTitle:"), uintptr(objc.NSString(n.Title)))
	if n.Subtitle != "" {
		objc.Send(content, objc.Sel("setSubtitle:"), uintptr(objc.NSString(n.Subtitle)))
	}
	if n.Body != "" {
		objc.Send(content, objc.Sel("setBody:"), uintptr(objc.NSString(n.Body)))
	}
	id, err := notificationID()
	if err != nil {
		return &desktop.Error{Code: desktop.CodeInternal, Message: desktop.InternalErrorMsg}
	}
	request := objc.ID(objc.Send(objc.Class("UNNotificationRequest"),
		objc.Sel("requestWithIdentifier:content:trigger:"), uintptr(objc.NSString(id)), uintptr(content), 0))
	if err := s.onMain(func() {
		objc.Send(center, objc.Sel("addNotificationRequest:withCompletionHandler:"), uintptr(request), addBlock)
	}); err != nil {
		return &desktop.Error{Code: desktop.CodeInternal, Message: desktop.InternalErrorMsg}
	}
	select {
	case e := <-addRes:
		if e.notAllowed {
			return errNotAllowed
		}
		if e.errText != "" {
			s.logger.Error("desktop: notification delivery failed", "error", e.errText)
			return &desktop.Error{Code: desktop.CodeInternal, Message: desktop.InternalErrorMsg}
		}
		return nil
	case <-time.After(notifTimeout):
		return &desktop.Error{Code: desktop.CodeInternal, Message: "notification delivery timed out"}
	}
}

type notifyResult struct {
	granted    bool
	errText    string
	notAllowed bool
}

var errNotAllowed = &desktop.Error{
	Code:    desktop.CodeUnsupported,
	Message: "notifications are not allowed for this app: the bundle is unsigned or the user declined them in System Settings",
}

func nsErrorResult(granted bool, e objc.ID) notifyResult {
	r := notifyResult{granted: granted}
	if e == 0 {
		return r
	}
	if desc := objc.ID(objc.Send(e, objc.Sel("localizedDescription"))); desc != 0 {
		r.errText = objc.GoString(desc)
	} else {
		r.errText = "unknown error"
	}
	if domain := objc.ID(objc.Send(e, objc.Sel("domain"))); domain != 0 && objc.GoString(domain) == "UNErrorDomain" {
		if code := objc.Send(e, objc.Sel("code")); code == 1 {
			r.notAllowed = true
		}
	}
	return r
}
