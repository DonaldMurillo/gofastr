//go:build darwin && arm64 && !desktop_e2e

package desktop

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/battery/desktop/internal/objc"
)

// The kAEGetURL handler in the untagged suite. go test runs Test
// functions on spawned goroutines, every objc callback asserts the
// main thread, and the dispatch main queue never drains before a run
// loop: the one check that must call the handler IMP the way AppKit
// would therefore runs in TestMain, on the main thread, AFTER the
// rest of the suite (so the lazy-loading test still sees DlopenCount
// 0 during m.Run; only Foundation is loaded here, no AppKit, no
// window). The desktop_e2e build keeps its own TestMain and drives the
// same handler for real inside [NSApp run], so this file stays out of
// it.

// TestMain runs the suite, then the main-thread Apple-event check.
func TestMain(m *testing.M) {
	code := m.Run()
	if !deepLinkIMPCheck() {
		code = 1
	}
	os.Exit(code)
}

// deepLinkIMPCheck builds a GetURL Apple Event (an
// NSAppleEventDescriptor with the URL as its direct object), registers
// the bridge with the shared NSAppleEventManager through
// installDeepLinkHandler, and invokes the handler IMP through
// objc_msgSend exactly as the manager does. It prints its own failure
// lines (it runs outside m.Run) and reports success.
func deepLinkIMPCheck() bool {
	fail := func(format string, args ...any) {
		fmt.Printf("--- FAIL: deeplink-imp: "+format+"\n", args...)
	}
	// Foundation only: NSAppleEventDescriptor and NSAppleEventManager
	// live there. No AppKit, no window, no run loop needed.
	if _, err := objc.DlopenGlobal("/System/Library/Frameworks/Foundation.framework/Foundation"); err != nil {
		fail("dlopen Foundation: %v", err)
		return false
	}

	bridge := objc.ID(objc.Send(objc.ID(objc.Send(ensureBridgeClass(), objc.Sel("alloc"))), objc.Sel("init")))
	const rawURL = "notes://two?q=1"
	got := make(chan string, 1)
	installDeepLinkHandler(bridge, func(raw string) { got <- raw })

	// Registration read-back, guarded: the introspection selectors
	// are not part of the pinned surface, so a manager that lacks them
	// skips this assertion instead of crashing on an unknown selector.
	mgr := objc.ID(objc.Send(objc.Class("NSAppleEventManager"), objc.Sel("sharedAppleEventManager")))
	if objc.SendBool(mgr, objc.Sel("respondsToSelector:"), objc.Sel("eventHandlerForEventClass:andEventID:")) {
		handler := objc.Send(mgr, objc.Sel("eventHandlerForEventClass:andEventID:"),
			uintptr(aeGetURLClass), uintptr(aeGetURLID))
		if objc.ID(handler) != bridge {
			fail("registered GetURL handler = %#x, want the bridge %#x", handler, uintptr(bridge))
			return false
		}
	}

	// The event: direct object carries the URL string; the target of a
	// received-shape event is irrelevant (this one is never sent).
	direct := objc.Send(objc.Class("NSAppleEventDescriptor"), objc.Sel("descriptorWithString:"), uintptr(objc.NSString(rawURL)))
	reply := objc.Send(objc.Class("NSAppleEventDescriptor"), objc.Sel("nullDescriptor"))
	event := objc.Send(objc.Class("NSAppleEventDescriptor"),
		objc.Sel("appleEventWithEventClass:eventID:targetDescriptor:returnID:transactionID:"),
		uintptr(aeGetURLClass), uintptr(aeGetURLID), uintptr(reply),
		^uintptr(0), // kAutoGenerateReturnID (-1)
		0)           // kAnyTransactionID
	objc.Send(objc.ID(event), objc.Sel("setParamDescriptor:forKeyword:"), uintptr(direct), uintptr(aeKeyDirect))

	// The way AppKit would: objc_msgSend(bridge, sel, event, reply).
	objc.Send(bridge, objc.Sel("handleGetURLEvent:withReplyEvent:"), uintptr(event), uintptr(reply))

	select {
	case raw := <-got:
		if raw != rawURL {
			fail("OnDeepLink received %q, want %q", raw, rawURL)
			return false
		}
		fmt.Println("deeplink-imp: the GetURL IMP delivered", raw, "to OnDeepLink on the main thread")
		return true
	case <-time.After(5 * time.Second):
		fail("OnDeepLink never received the URL")
		return false
	}
}
