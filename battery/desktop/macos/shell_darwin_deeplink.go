//go:build darwin && arm64

package macos

import (
	"sync/atomic"

	"github.com/DonaldMurillo/gofastr/battery/desktop/internal/ffi"
	"github.com/DonaldMurillo/gofastr/battery/desktop/internal/objc"
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
	aeGetURLClass = 0x47555254 // 'GURL'
	aeGetURLID    = 0x47555254 // 'GURL'
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
// object; the reply event is unused for GetURL. Delivery hops to a
// goroutine so the main thread returns to the run loop at once (the
// battery navigates and emits through desktop.Window methods that hop back).
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
	if fn := deepLinkOpen.Load(); fn != nil {
		go (*fn)(raw)
	}
	return 0
}
