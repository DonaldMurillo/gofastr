//go:build darwin && arm64 && !desktop_e2e

package macos

import (
	"fmt"
	"os"
	"runtime"
	"testing"
	"time"
	"unsafe"

	"github.com/DonaldMurillo/gofastr/battery/desktop/internal/ffi"
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

	// Registration read-back through the Apple Event Manager's own
	// table (NSAppleEventManager installs through it), keyed by the
	// codes LaunchServices sends, spelled out: the constants once read
	// 'GURT' and every other step here still passed. An earlier
	// read-back asked NSAppleEventManager eventHandlerForEventClass:,
	// which it does not answer, so it never ran.
	if err := getURLHandlerInstalled(); err != nil {
		fail("%v", err)
		return false
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

// getURLHandlerInstalled asks AEGetEventHandler (CoreServices) whether a
// handler is installed for 'GURL'/'GURL'. errAEHandlerNotFound (-1717)
// means LaunchServices' URLs would reach nothing.
func getURLHandlerInstalled() error {
	cs, err := objc.DlopenGlobal("/System/Library/Frameworks/CoreServices.framework/CoreServices")
	if err != nil {
		return fmt.Errorf("dlopen CoreServices: %v", err)
	}
	sym, err := objc.Dlsym(cs, "AEGetEventHandler")
	if err != nil {
		return err
	}
	// OSErr AEGetEventHandler(AEEventClass, AEEventID,
	//     AEEventHandlerUPP *handler, SRefCon *refcon, Boolean isSys)
	out := new([2]uintptr)
	r1, _, _ := ffi.Call(sym, []uintptr{
		uintptr(fourCC("GURL")), uintptr(fourCC("GURL")),
		uintptr(unsafe.Pointer(&out[0])), uintptr(unsafe.Pointer(&out[1])), 0,
	}, nil)
	runtime.KeepAlive(out)
	if code := int16(r1); code != 0 {
		return fmt.Errorf("no handler installed for the GetURL event 'GURL'/'GURL' (OSErr %d)", code)
	}
	return nil
}
