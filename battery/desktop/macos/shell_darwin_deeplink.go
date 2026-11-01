//go:build darwin && arm64

package macos

import (
	"log/slog"
	"sync"
	"sync/atomic"

	"github.com/DonaldMurillo/gofastr/battery/desktop/internal/ffi"
	"github.com/DonaldMurillo/gofastr/battery/desktop/internal/objc"
	"github.com/DonaldMurillo/gofastr/core/textsafe"
)

// The darwin deep-link seam: macOS hands an app a URL on its custom
// scheme as a GetURL Apple Event ('GURL'/'GURL') whose direct object
// is the URL string. The bridge class carries the handler method; the
// shared NSAppleEventManager routes events to it once
// installDeepLinkHandler has registered it. The portable half (scheme
// validation, mapping, queue, navigation, the deep_link event) lives
// in deeplink.go.

// Apple Event four-character codes (AEDataModel.h / AERegistry.h):
// the GetURL event is class 'GURL' and id 'GURL', and the URL travels
// as the direct object under keyword '----'.
const (
	aeGetURLClass = 0x4755524C // 'GURL'
	aeGetURLID    = 0x4755524C // 'GURL'
	aeKeyDirect   = 0x2D2D2D2D // '----'
)

// deepLinkBridgeMethods are the bridge-class methods the deep-link
// slice adds: the kAEGetURL handler. The IMP is allocated once per
// process (callback slots are forever; ensureBridgeClass runs once).
func deepLinkBridgeMethods() []objc.Method {
	return []objc.Method{{
		// NSAppleEventManager dispatches installed handlers on the
		// main run loop, so the main-thread assertion every objc
		// callback carries holds here too.
		Sel:   "handleGetURLEvent:withReplyEvent:",
		Types: "v@:@@",
		Fn:    ffi.NewCallback(bridgeGetURLEvent),
	}}
}

// deepLinkOpen carries the OnDeepLink callback the shell's Run
// received in its desktop.WindowConfig. The darwin shell does not keep that
// field itself (shell_darwin.go belongs to another slice), so
// installDeepLinkHandler captures it here: one live run per process,
// replaced only when a new Run hands a new callback.
var deepLinkOpen atomic.Pointer[func(rawURL string)]

// installDeepLinkHandler registers bridge as the GetURL handler with
// the shared NSAppleEventManager. It must run on the main thread
// BEFORE the run loop starts (at the bridge object's creation point in
// Run), so a cold-launch URL is not missed; onOpen is the
// desktop.WindowConfig.OnDeepLink that Run received. Until the one-line call
// site lands in shell_darwin.go, the e2e calls this itself.
func installDeepLinkHandler(bridge objc.ID, onOpen func(rawURL string)) {
	if onOpen != nil {
		deepLinkOpen.Store(&onOpen)
	}
	mgr := objc.ID(objc.Send(objc.Class("NSAppleEventManager"), objc.Sel("sharedAppleEventManager")))
	objc.Send(mgr, objc.Sel("setEventHandler:andSelector:forEventClass:andEventID:"),
		uintptr(bridge), objc.Sel("handleGetURLEvent:withReplyEvent:"),
		uintptr(aeGetURLClass), uintptr(aeGetURLID))
}

// bridgeGetURLEvent is the handleGetURLEvent:withReplyEvent: IMP.
// Args: self, _cmd, event, replyEvent. The URL is the event's direct
// object; the reply event is unused for GetURL. Delivery goes through
// deepLinks, so the main thread returns to the run loop at once (the
// battery navigates and emits through desktop.Window methods that hop
// back) and two URLs reach the battery in the order macOS sent them.
func bridgeGetURLEvent(a *ffi.Args) uintptr {
	var raw string
	if event := objc.ID(a.Int[2]); event != 0 {
		if d := objc.Send(event, objc.Sel("paramDescriptorForKeyword:"), uintptr(aeKeyDirect)); d != 0 {
			raw = objc.GoString(objc.ID(objc.Send(objc.ID(d), objc.Sel("stringValue"))))
		}
	}
	if raw == "" {
		return 0
	}
	if deepLinkOpen.Load() != nil {
		deepLinks.push(raw)
	}
	return 0
}

// maxPendingDeepLinks bounds the URLs waiting for the battery, so a
// stalled host callback cannot grow memory. The oldest is dropped.
const maxPendingDeepLinks = 16

// deepLinks is the process's one GetURL delivery line.
var deepLinks deepLinkLine

// deepLinkLine hands URLs to the installed OnDeepLink from one
// goroutine, in arrival order, off the main thread. Opening two links
// at once sends two GetURL events; a goroutine per event would let the
// second reach the battery first.
type deepLinkLine struct {
	mu       sync.Mutex
	pending  []string
	draining bool
}

// push queues raw and starts the drain when none runs. It never blocks.
func (l *deepLinkLine) push(raw string) {
	l.mu.Lock()
	if len(l.pending) == maxPendingDeepLinks {
		l.pending = l.pending[1:]
	}
	l.pending = append(l.pending, raw)
	start := !l.draining
	l.draining = true
	l.mu.Unlock()
	if start {
		go l.drain()
	}
}

// drain delivers until the line is empty. The callback is read per
// URL, so a URL waiting through a new Run goes to that Run's battery.
func (l *deepLinkLine) drain() {
	for {
		l.mu.Lock()
		if len(l.pending) == 0 {
			l.draining = false
			l.mu.Unlock()
			return
		}
		raw := l.pending[0]
		l.pending = l.pending[1:]
		l.mu.Unlock()
		if fn := deepLinkOpen.Load(); fn != nil {
			deliverDeepLink(raw, *fn)
		}
	}
}

// deliverDeepLink runs the host callback with a recover, so a panic
// drops that one URL instead of the process.
func deliverDeepLink(raw string, fn func(string)) {
	defer func() {
		if v := recover(); v != nil {
			slog.Default().Error("desktop/macos: deep-link callback panicked", "panic", textsafe.Recovered(v))
		}
	}()
	fn(raw)
}
