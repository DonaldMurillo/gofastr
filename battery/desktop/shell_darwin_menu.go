//go:build darwin && arm64

package desktop

import (
	"encoding/binary"
	"errors"
	"sync"
	"time"

	"github.com/DonaldMurillo/gofastr/battery/desktop/internal/ffi"
	"github.com/DonaldMurillo/gofastr/battery/desktop/internal/objc"
)

// The bridge class: one NSObject subclass whose IMPs are Go callbacks
// (ffi trampolines). Registered once per process, callback slots and
// class registrations are forever, and routed to the active shell.

var (
	bridgeOnce  sync.Once
	bridgeClass objc.ID
)

// ensureBridgeClass registers GofastrDesktopBridge and returns its
// class. Must be called on the main thread (Run does).
func ensureBridgeClass() objc.ID {
	bridgeOnce.Do(func() {
		bridgeClass = objc.RegisterClass("GofastrDesktopBridge", objc.Class("NSObject"), append([]objc.Method{
			{
				Sel:   "userContentController:didReceiveScriptMessage:",
				Types: "v@:@@",
				Fn:    ffi.NewCallback(bridgeScriptMessage),
			},
			{
				Sel:   "webView:didFinishNavigation:",
				Types: "v@:@@",
				Fn:    ffi.NewCallback(bridgeFinishNavigation),
			},
			{
				Sel:   "webView:didFailProvisionalNavigation:withError:",
				Types: "v@:@@@",
				Fn:    ffi.NewCallback(bridgeFailNavigation),
			},
			{
				Sel:   "windowShouldClose:",
				Types: "B@:@",
				Fn:    ffi.NewCallback(bridgeWindowShouldClose),
			},
			{
				// Window-delegate frame notifications: the user moved
				// or resized a window. The IMP reads the frame on the
				// main thread (notifications arrive there) and hands
				// the report to a goroutine, so the UI thread never
				// waits on Go.
				Sel:   "windowDidMove:",
				Types: "v@:@",
				Fn:    ffi.NewCallback(bridgeWindowDidMove),
			},
			{
				// Resizes report at the END of the drag, not per frame.
				Sel:   "windowDidEndLiveResize:",
				Types: "v@:@",
				Fn:    ffi.NewCallback(bridgeWindowDidEndLiveResize),
			},
			{
				Sel:   "applicationShouldTerminateAfterLastWindowClosed:",
				Types: "B@:@",
				Fn:    ffi.NewCallback(bridgeTerminateAfterLastWindow),
			},
			{
				// performSelectorOnMainThread target: drains the
				// perform queue. Unlike the dispatch main queue,
				// performSelector blocks run in modal run-loop modes
				// too, which is what lets the e2e (and any Go code
				// that must run while a permission alert is up) hop
				// onto the UI thread during runModal.
				Sel:   "drainGoQueue:",
				Types: "v@:@",
				Fn:    ffi.NewCallback(bridgeDrainGoQueue),
			},
			{
				// RoleSettings (app menu + tray): routed to the
				// battery's OnSettings, which opens the settings
				// window. Dispatched on a goroutine like menuAction:.
				Sel:   "settingsAction:",
				Types: "v@:@",
				Fn:    ffi.NewCallback(bridgeSettingsAction),
			},
			{
				// RoleShow (tray): order the main window front and
				// activate the app.
				Sel:   "showAction:",
				Types: "v@:@",
				Fn:    ffi.NewCallback(bridgeShowAction),
			},
			{
				// NSTerminateCancel: last-window-close routes through
				// Quit() (windowShouldClose schedules it) so Run
				// returns and the battery drains the app. Letting
				// terminate: proceed would exit(0) without draining.
				// Cmd-Q through the synthesized app menu reaches
				// quitAction:, but a Dock "Quit", an AppleScript
				// `quit app`, or a logout arrives as terminate:. Cancel
				// it (returning NSTerminateNow would exit the process
				// under AppKit without draining the App) and route it
				// through Quit() so Run returns and the battery shuts
				// the App down. Before this handler also called Quit,
				// the process answered "User canceled" to every
				// external quit and kept running.
				Sel:   "applicationShouldTerminate:",
				Types: "q@:@",
				Fn:    ffi.NewCallback(bridgeShouldTerminate),
			},
			{
				Sel:   "menuAction:",
				Types: "v@:@",
				Fn:    ffi.NewCallback(bridgeMenuAction),
			},
			{
				// UNUserNotificationCenterDelegate. Without it macOS
				// suppresses every notification while the app is
				// frontmost, which is exactly when a save fires one; the
				// first signed build posted silently for that reason.
				// The centre may call this on any thread.
				Sel:   "userNotificationCenter:willPresentNotification:withCompletionHandler:",
				Types: "v@:@@@?",
				Fn:    ffi.NewCallbackAnyThread(bridgeWillPresentNotification),
			},
			{
				Sel:   "quitAction:",
				Types: "v@:@",
				Fn:    ffi.NewCallback(bridgeQuitAction),
			},
		}, deepLinkBridgeMethods()...))
	})
	return bridgeClass
}

// The perform queue: fns queued for the main thread through
// performSelectorOnMainThread (drainGoQueue:), which, unlike the
// dispatch main queue, runs in modal run-loop modes, so a hop
// scheduled this way executes even while a permission alert's
// runModal owns the UI thread.
var (
	performMu  sync.Mutex
	performFns []func()
)

// bridgeDrainGoQueue is the drainGoQueue: IMP.
func bridgeDrainGoQueue(a *ffi.Args) uintptr {
	for {
		performMu.Lock()
		if len(performFns) == 0 {
			performMu.Unlock()
			return 0
		}
		fn := performFns[0]
		performFns = performFns[1:]
		performMu.Unlock()
		fn()
	}
}

// mainViaPerform runs fn on the UI thread through
// performSelectorOnMainThread and waits (bounded). Modal-safe; the
// e2e alert answerer is the intended caller.
func (s *darwinShell) mainViaPerform(fn func()) error {
	s.mu.Lock()
	bridge := s.bridge
	s.mu.Unlock()
	if bridge == 0 {
		return errors.New("desktop: shell is not running")
	}
	done := make(chan struct{})
	performMu.Lock()
	performFns = append(performFns, func() { defer close(done); fn() })
	performMu.Unlock()
	objc.Send(bridge, objc.Sel("performSelectorOnMainThread:withObject:waitUntilDone:"),
		objc.Sel("drainGoQueue:"), 0, 0)
	select {
	case <-done:
		return nil
	case <-time.After(evalTimeout):
		return errors.New("desktop: mainViaPerform timed out")
	}
}

// bridgeScriptMessage: WKScriptMessage body → the ONE message the
// battery acts on ({"type":"drag"}, the borderless-window drag), or a
// Debug log plus the test seam for everything else. The battery's
// transport is HTTP; this channel exists for the drag (which needs the
// current mouse-down event on the main thread) and the seam.
func bridgeScriptMessage(a *ffi.Args) uintptr {
	s := activeShell.Load()
	if s == nil {
		return 0
	}
	body := objc.ID(objc.Send(objc.ID(a.Int[3]), objc.Sel("body")))
	var text string
	if objc.Send(body, objc.Sel("isKindOfClass:"), uintptr(objc.Class("NSString"))) != 0 {
		text = objc.GoString(body)
	} else {
		text = objc.GoString(objc.ID(objc.Send(body, objc.Sel("description"))))
	}
	if text == `{"type":"drag"}` {
		s.startWindowDrag(objc.ID(a.Int[2]))
		return 0
	}
	s.logger.Debug("desktop: script message", "body", text)
	select {
	case s.scriptMsgs <- text:
	default:
	}
	return 0
}

func bridgeFinishNavigation(a *ffi.Args) uintptr {
	if s := activeShell.Load(); s != nil {
		s.logger.Debug("desktop: navigation finished")
	}
	return 0
}

func bridgeFailNavigation(a *ffi.Args) uintptr {
	if s := activeShell.Load(); s != nil {
		if e := objc.ID(a.Int[4]); e != 0 {
			s.logger.Warn("desktop: navigation failed",
				"error", objc.GoString(objc.ID(objc.Send(e, objc.Sel("localizedDescription")))))
		}
	}
	return 0
}

// bridgeWindowShouldClose routes the sender NSWindow: the main window
// either quits (scheduling the loop stop asynchronously, the callback
// must return immediately) or, with CloseHidesWindow, hides (orderOut:)
// and reports NO so the app stays alive in the menu bar; a secondary
// window closes, unregisters, and reports through OnWindowClosed.
func bridgeWindowShouldClose(a *ffi.Args) uintptr {
	s := activeShell.Load()
	if s == nil {
		return 1
	}
	sender := objc.ID(a.Int[2])
	w, ok := s.lookupWindow(sender)
	if !ok {
		return 1 // not ours (should not happen): allow the close
	}
	if w.id == mainWindowID {
		s.mu.Lock()
		closeHides := s.closeHides
		s.mu.Unlock()
		if closeHides {
			objc.Send(sender, objc.Sel("orderOut:"), 0)
			return 0
		}
		go s.Quit()
		return 1
	}
	s.forgetWindow(w)
	_, wv := w.ids()
	killPage(wv)
	if onClosed := s.onWindowClosedLocked(); onClosed != nil {
		go onClosed(w.id)
	}
	return 1
}

// onWindowClosedLocked snapshots the OnWindowClosed callback.
func (s *darwinShell) onWindowClosedLocked() func(string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.onWindowClosed
}

// bridgeWindowDidMove is the windowDidMove: IMP: the user (or a
// programmatic setFrame) moved a window.
func bridgeWindowDidMove(a *ffi.Args) uintptr {
	reportWindowFrameNote(a)
	return 0
}

// bridgeWindowDidEndLiveResize is the windowDidEndLiveResize: IMP: a
// resize drag finished. AppKit fires it once, at the end.
func bridgeWindowDidEndLiveResize(a *ffi.Args) uintptr {
	reportWindowFrameNote(a)
	return 0
}

// reportWindowFrameNote reads the notification's window and hands the
// report on. It runs ON the main thread (notifications arrive there).
func reportWindowFrameNote(a *ffi.Args) {
	s := activeShell.Load()
	if s == nil {
		return
	}
	note := objc.ID(a.Int[2])
	if note == 0 {
		return
	}
	win := objc.ID(objc.Send(note, objc.Sel("object")))
	w, ok := s.lookupWindow(win)
	if !ok {
		return
	}
	s.reportWindowFrame(w)
}

// bridgeTerminateAfterLastWindow keeps the process alive when the
// tray's CloseHidesWindow is set: the app lives in the menu bar, so
// the last window closing must not terminate it.
func bridgeTerminateAfterLastWindow(a *ffi.Args) uintptr {
	if s := activeShell.Load(); s != nil {
		s.mu.Lock()
		closeHides := s.closeHides
		s.mu.Unlock()
		if closeHides {
			return 0
		}
	}
	return 1
}

// bridgeSettingsAction: the app-menu Settings item and the tray's
// RoleSettings item. OnSettings runs on a goroutine (it opens a
// window, which the UI thread must not wait on).
func bridgeSettingsAction(a *ffi.Args) uintptr {
	if s := activeShell.Load(); s != nil {
		s.mu.Lock()
		onSettings := s.onSettings
		s.mu.Unlock()
		if onSettings != nil {
			go onSettings()
		}
	}
	return 0
}

// bridgeShowAction: the tray's RoleShow item. Runs on the main thread
// (a menu action), orders the main window front, activates the app.
func bridgeShowAction(a *ffi.Args) uintptr {
	if s := activeShell.Load(); s != nil {
		s.showMainWindow()
	}
	return 0
}

// bridgeMenuAction: the sender NSMenuItem's tag indexes the plan's
// action-ID table; OnMenu dispatches on a goroutine so the main thread
// returns to the run loop at once (the battery's Navigate path Evals
// synchronously, which would block the UI thread).
func bridgeMenuAction(a *ffi.Args) uintptr {
	s := activeShell.Load()
	if s == nil {
		return 0
	}
	tag := int(objc.Send(objc.ID(a.Int[2]), objc.Sel("tag")))
	s.mu.Lock()
	ids, onMenu := s.actionIDs, s.onMenu
	s.mu.Unlock()
	if tag < 0 || tag >= len(ids) {
		s.logger.Warn("desktop: menu activation with unknown tag", "tag", tag)
		return 0
	}
	if onMenu == nil {
		return 0
	}
	id := ids[tag]
	go onMenu(id)
	return 0
}

// bridgeQuitAction: the menu-bar Quit item. [NSApp terminate:] would
// exit the process without draining, so the quit role stops the run
// loop instead and Run returns normally.
// bridgeShouldTerminate answers applicationShouldTerminate: with
// NSTerminateCancel (0) after scheduling Quit(), so every external quit
// path (Dock, AppleScript, logout) drains through Run like Cmd-Q does.
func bridgeShouldTerminate(a *ffi.Args) uintptr {
	bridgeQuitAction(a)
	return 0
}

// unPresentBannerListSound is UNNotificationPresentationOptionBanner |
// List | Sound (16 | 8 | 2): show the banner, keep it in Notification
// Center, play the sound, even while the app is frontmost.
const unPresentBannerListSound = 16 | 8 | 2

// bridgeWillPresentNotification answers the centre's foreground
// presentation question by invoking the completion block with the
// options above. Args: self, _cmd, center, notification, block. A block
// is a struct whose invoke pointer sits 16 bytes in (isa, flags,
// reserved, invoke); calling it is a plain C call with the block as the
// first argument.
func bridgeWillPresentNotification(a *ffi.Args) uintptr {
	block := a.Int[4]
	if block == 0 {
		return 0
	}
	// Read the invoke pointer through the objc package's C-memory copy
	// (a uintptr-to-Pointer conversion here would trip vet's unsafeptr
	// check, and the block is C memory, not Go memory).
	raw := objc.CopyBytes(block+16, 8)
	if len(raw) != 8 {
		return 0
	}
	invoke := uintptr(binary.LittleEndian.Uint64(raw))
	if invoke != 0 {
		ffi.Call(invoke, []uintptr{block, unPresentBannerListSound}, nil)
	}
	return 0
}

func bridgeQuitAction(a *ffi.Args) uintptr {
	if s := activeShell.Load(); s != nil {
		go s.Quit()
	}
	return 0
}

// buildMenuBar renders the plan as the NSApp main menu and installs
// the bridge as the application delegate. Call on the main thread
// before the run loop starts.
func (s *darwinShell) buildMenuBar(plans []menuPlan) {
	mainMenu := objc.ID(objc.Send(objc.ID(objc.Send(objc.Class("NSMenu"), objc.Sel("alloc"))), objc.Sel("init")))
	for _, mp := range plans {
		top := objc.ID(objc.Send(objc.ID(objc.Send(objc.Class("NSMenuItem"), objc.Sel("alloc"))), objc.Sel("init")))
		objc.Send(top, objc.Sel("setTitle:"), uintptr(objc.NSString(mp.Title)))
		objc.Send(top, objc.Sel("setSubmenu:"), uintptr(s.buildMenu(mp.Items)))
		objc.Send(mainMenu, objc.Sel("addItem:"), uintptr(top))
	}
	objc.Send(s.nsApp, objc.Sel("setMainMenu:"), uintptr(mainMenu))
	objc.Send(s.nsApp, objc.Sel("setDelegate:"), uintptr(s.bridge))
}

// buildMenu renders planned rows as an NSMenu. First-responder rows
// (the Edit menu) get an action and no target; role rows target the
// bridge or NSApp; action rows target the bridge with their tag.
func (s *darwinShell) buildMenu(rows []menuItemPlan) objc.ID {
	menu := objc.ID(objc.Send(objc.ID(objc.Send(objc.Class("NSMenu"), objc.Sel("alloc"))), objc.Sel("init")))
	for _, r := range rows {
		var item objc.ID
		if r.Sep {
			item = objc.ID(objc.Send(objc.Class("NSMenuItem"), objc.Sel("separatorItem")))
		} else {
			item = objc.ID(objc.Send(objc.ID(objc.Send(objc.Class("NSMenuItem"), objc.Sel("alloc"))), objc.Sel("init")))
			objc.Send(item, objc.Sel("setTitle:"), uintptr(objc.NSString(r.Title)))
			if r.Key != "" {
				objc.Send(item, objc.Sel("setKeyEquivalent:"), uintptr(objc.NSString(r.Key)))
				if r.Mask != 0 {
					objc.Send(item, objc.Sel("setKeyEquivalentModifierMask:"), r.Mask)
				}
			}
			switch {
			case r.FirstResponder != "":
				// Target stays nil: the action reaches the first
				// responder (the WebView's editor) through the
				// responder chain.
				objc.Send(item, objc.Sel("setAction:"), objc.Sel(r.FirstResponder))
			case r.Role == RoleSettings:
				objc.Send(item, objc.Sel("setTarget:"), uintptr(s.bridge))
				objc.Send(item, objc.Sel("setAction:"), objc.Sel("settingsAction:"))
			case r.Role == RoleShow:
				objc.Send(item, objc.Sel("setTarget:"), uintptr(s.bridge))
				objc.Send(item, objc.Sel("setAction:"), objc.Sel("showAction:"))
			case r.Role == RoleQuit:
				objc.Send(item, objc.Sel("setTarget:"), uintptr(s.bridge))
				objc.Send(item, objc.Sel("setAction:"), objc.Sel("quitAction:"))
			case r.Role == RoleAbout:
				objc.Send(item, objc.Sel("setTarget:"), uintptr(s.nsApp))
				objc.Send(item, objc.Sel("setAction:"), objc.Sel("orderFrontStandardAboutPanel:"))
			case r.Action:
				objc.Send(item, objc.Sel("setTarget:"), uintptr(s.bridge))
				objc.Send(item, objc.Sel("setAction:"), objc.Sel("menuAction:"))
				objc.Send(item, objc.Sel("setTag:"), uintptr(r.Tag))
			}
			if len(r.Submenu) > 0 {
				objc.Send(item, objc.Sel("setSubmenu:"), uintptr(s.buildMenu(r.Submenu)))
			}
		}
		objc.Send(menu, objc.Sel("addItem:"), uintptr(item))
	}
	return menu
}
