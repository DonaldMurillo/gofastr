//go:build darwin && arm64

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

// Capability surfaces on the darwin shell: the permission alert
// (NSAlert runModal), the pasteboard, the open/save panels, and user
// notifications. Every native call hops to the main thread through
// onMain; the blocking runModal calls are fine there because they pump
// the run loop.

// nsPasteboardTypeString is NSPasteboardTypeString
// ("public.utf8-plain-text").
const nsPasteboardTypeString = "public.utf8-plain-text"

// modalTimeout bounds a permission alert nobody answers.
const modalTimeout = 10 * time.Minute

// Prompt shows the OS permission alert: "<Capability>.<Method> wants
// permission <Permission>" with Allow / Allow once / Deny, in that
// order (runModal returns 1000/1001/1002).
func (s *darwinShell) Prompt(ctx context.Context, req desktop.PermissionRequest) (desktop.Decision, error) {
	if err := ctx.Err(); err != nil {
		return desktop.DecisionDeny, desktop.ErrCancelled
	}
	decision, queued := s.popPromptDecision()
	if queued {
		return decision, nil
	}

	// The answer travels through a mutex, not a captured local. On a
	// deadline that expires while the alert is ALREADY on screen the
	// queued closure cannot be recalled, so it may write after Prompt
	// returned; a plain local would be a data race and the value would
	// be read by nobody anyway.
	var res promptResult
	if err := s.onMainWithTimeout(modalTimeout, func() {
		alert := objc.ID(objc.Send(objc.ID(objc.Send(objc.Class("NSAlert"), objc.Sel("alloc"))), objc.Sel("init")))
		objc.Send(alert, objc.Sel("setMessageText:"),
			uintptr(objc.NSString(req.Capability+"."+req.Method+" wants permission "+req.Permission)))
		objc.Send(alert, objc.Sel("setInformativeText:"), uintptr(objc.NSString(req.Description)))
		objc.Send(alert, objc.Sel("addButtonWithTitle:"), uintptr(objc.NSString("Allow")))
		objc.Send(alert, objc.Sel("addButtonWithTitle:"), uintptr(objc.NSString("Allow once")))
		objc.Send(alert, objc.Sel("addButtonWithTitle:"), uintptr(objc.NSString("Deny")))
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

// promptResult carries the user's answer back off the main thread. See
// Prompt for why it is not a captured local.
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

// setPromptOverride queues one scripted decision (the e2e seam; see
// ScriptPrompts, which queues any number).
func (s *darwinShell) setPromptOverride(d desktop.Decision) {
	s.mu.Lock()
	s.promptQueue = append(s.promptQueue, d)
	s.mu.Unlock()
}

// popPromptDecision takes the next scripted decision in order.
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

// desktop.Clipboard returns the pasteboard surface.
func (s *darwinShell) Clipboard() desktop.Clipboard { return s }

// generalPasteboard returns [NSPasteboard generalPasteboard].
func generalPasteboard() objc.ID {
	return objc.ID(objc.Send(objc.Class("NSPasteboard"), objc.Sel("generalPasteboard")))
}

// ReadText returns the pasteboard's plain-text content.
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

// WriteText replaces the pasteboard's plain-text content.
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

// desktop.Dialogs returns the file/folder panel surface.
func (s *darwinShell) Dialogs() desktop.Dialogs { return s }

// NSModalResponseOK.
const nsModalOK = 1

// urlPath extracts a file URL's path as a Go string.
func urlPath(u objc.ID) string {
	if u == 0 {
		return ""
	}
	return objc.GoString(objc.ID(objc.Send(u, objc.Sel("path"))))
}

// OpenFile shows NSOpenPanel (runModal); a non-OK response cancels.
func (s *darwinShell) OpenFile(ctx context.Context, opts desktop.OpenOptions) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, desktop.ErrCancelled
	}
	var paths []string
	if err := s.onMain(func() {
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

// SaveFile shows NSSavePanel (runModal); a non-OK response cancels.
func (s *darwinShell) SaveFile(ctx context.Context, opts desktop.SaveOptions) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", desktop.ErrCancelled
	}
	var path string
	ok := false
	if err := s.onMain(func() {
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

// OpenFolder shows a directory-only NSOpenPanel.
func (s *darwinShell) OpenFolder(ctx context.Context) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", desktop.ErrCancelled
	}
	var path string
	ok := false
	if err := s.onMain(func() {
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

// utiOnce guards the lazy UniformTypeIdentifiers load; the framework
// exists on every supported macOS but the load is still first-use so
// an app that never opens a dialog never pays it.
var utiOnce sync.Once
var utiAvailable bool

// loadUTIs dlopens UniformTypeIdentifiers once.
func loadUTIs() bool {
	utiOnce.Do(func() {
		_, err := objc.DlopenGlobal("/System/Library/Frameworks/UniformTypeIdentifiers.framework/UniformTypeIdentifiers")
		utiAvailable = err == nil
	})
	return utiAvailable
}

// allowedContentTypes builds the UTType array for a dialog's filters
// (UTType typeWithFilenameExtension: per extension, all filters
// merged). Returns 0 when filters are absent or nothing resolved, in
// which case the panel is left unfiltered. Filter display names have
// no allowedContentTypes equivalent; macOS shows the resolved type
// names.
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

// Notifier returns the notification surface.
func (s *darwinShell) Notifier() desktop.Notifier { return s }

// notificationID mints the identifier of one UNNotificationRequest: 16
// bytes of crypto/rand, base64url-encoded. The one thing the id must
// never be is guessable from another request's (a time-based id
// collides across rapid fires), so it comes from the CSPRNG like every
// other id this battery mints.
func notificationID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b[:]), nil
}

// UNAuthorizationOptionBadge|Sound|Alert.
const unAuthOptions = 1 | 2 | 4

// notifTimeout bounds each notification round-trip.
const notifTimeout = 10 * time.Second

// errNeedsBundle is the fixed unbundled refusal.
var errNeedsBundle = &desktop.Error{
	Code:    desktop.CodeUnsupported,
	Message: "notifications need an app bundle; run from gofastr desktop build output",
}

// Show posts a user notification. UNUserNotificationCenter throws
// outside a signed bundle, so the bundle check ( mainBundle
// bundleIdentifier ) comes first and never touches the center when it
// fails.
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
	// The bridge is the centre's delegate so foreground notifications
	// are presented (bridgeWillPresentNotification). Set once, on the
	// main thread, before the first request.
	s.notifyDelegateOnce.Do(func() {
		_ = s.onMain(func() {
			objc.Send(center, objc.Sel("setDelegate:"), uintptr(s.bridgeID()))
		})
	})

	// Request authorization (once per launch is the OS's problem; it
	// answers immediately when already granted).
	// One Show at a time: the two completion blocks below are shared
	// (a block costs a callback slot for the process lifetime, and
	// there are 64), so their result channels must belong to one
	// caller at a time.
	s.notifyMu.Lock()
	defer s.notifyMu.Unlock()
	s.notifyOnce.Do(func() {
		s.authRes = make(chan notifyResult, 1)
		s.addRes = make(chan notifyResult, 1)
		// UNUserNotificationCenter calls both completions on a
		// background queue, never the main thread: NewCallbackAnyThread,
		// and the handlers only signal channels.
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
	// Drop a stale result from a timed-out earlier call.
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
		// The user declined in System Settings; a denial is a denial,
		// not an error.
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

// notifyResult is what a notification completion hands back over its
// channel: the outcome plus the NSError's text, already copied. An
// NSError handed to a completion block is only alive for the block's
// duration; the background queue's autorelease pool frees it right
// after, so reading it later from the goroutine that waits on the
// channel dereferenced freed memory (the second bundled crash, a
// SIGSEGV inside localizedDescription). The text is extracted inside
// the block, on the block's own thread, and never the pointer.
type notifyResult struct {
	granted bool
	errText string
	// notAllowed is UNErrorDomain code 1 (UNErrorCodeNotificationsNotAllowed):
	// the app is not permitted to post notifications at all, which on
	// macOS means the bundle is unsigned or unregistered, or the user
	// declined. The bridge reports it as unsupported, not internal.
	notAllowed bool
}

// errNotAllowed is the fixed answer when the notification centre
// refuses the app outright (UNErrorDomain code 1). On macOS an
// unsigned bundle gets this on every launch; `gofastr desktop build`
// ad-hoc signs the bundle when codesign is available, and a
// distributed app needs a real signature.
var errNotAllowed = &desktop.Error{
	Code:    desktop.CodeUnsupported,
	Message: "notifications are not allowed for this app: the bundle is unsigned or the user declined them in System Settings",
}

// nsErrorResult copies what Show needs out of an NSError while it is
// alive: inside the completion block that received it, never later.
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
