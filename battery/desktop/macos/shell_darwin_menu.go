//go:build darwin && (arm64 || amd64)

package macos

import (
	"context"
	"encoding/binary"
	"errors"
	"sync"
	"time"

	"github.com/DonaldMurillo/gofastr/battery/desktop"
	"github.com/DonaldMurillo/gofastr/battery/desktop/internal/ffi"
	"github.com/DonaldMurillo/gofastr/battery/desktop/internal/objc"
	"github.com/DonaldMurillo/gofastr/core/textsafe"
)

var (
	bridgeOnce  sync.Once
	bridgeClass objc.ID
)

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
				Sel:   "webView:decidePolicyForNavigationAction:decisionHandler:",
				Types: "v@:@@@?",
				Fn:    ffi.NewCallback(bridgeDecideNavigation),
			},
			{
				Sel:   "windowShouldClose:",
				Types: "B@:@",
				Fn:    ffi.NewCallback(bridgeWindowShouldClose),
			},
			{
				Sel:   "windowDidMove:",
				Types: "v@:@",
				Fn:    ffi.NewCallback(bridgeWindowDidMove),
			},
			{
				Sel:   "windowDidEndLiveResize:",
				Types: "v@:@",
				Fn:    ffi.NewCallback(bridgeWindowDidEndLiveResize),
			},
			{
				Sel:   "windowDidResize:",
				Types: "v@:@",
				Fn:    ffi.NewCallback(bridgeWindowDidResize),
			},
			{
				Sel:   "windowDidBecomeKey:",
				Types: "v@:@",
				Fn:    ffi.NewCallback(bridgeWindowDidBecomeKey),
			},
			{
				Sel:   "windowDidResignKey:",
				Types: "v@:@",
				Fn:    ffi.NewCallback(bridgeWindowDidResignKey),
			},
			{
				Sel:   "workspaceAccessibilityChanged:",
				Types: "v@:@",
				Fn:    ffi.NewCallback(bridgeWorkspaceAccessibility),
			},
			{
				Sel:   "applicationShouldTerminateAfterLastWindowClosed:",
				Types: "B@:@",
				Fn:    ffi.NewCallback(bridgeTerminateAfterLastWindow),
			},
			{
				Sel:   "drainGoQueue:",
				Types: "v@:@",
				Fn:    ffi.NewCallback(bridgeDrainGoQueue),
			},
			{
				Sel:   "settingsAction:",
				Types: "v@:@",
				Fn:    ffi.NewCallback(bridgeSettingsAction),
			},
			{
				Sel:   "showAction:",
				Types: "v@:@",
				Fn:    ffi.NewCallback(bridgeShowAction),
			},
			{
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

var (
	performMu  sync.Mutex
	performFns []func()
)

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

func bridgeScriptMessage(a *ffi.Args) uintptr {
	s := activeShell.Load()
	if s == nil {
		return 0
	}
	message := objc.ID(a.Int[3])
	frame := objc.ID(objc.Send(message, objc.Sel("frameInfo")))
	if frame == 0 || objc.Send(frame, objc.Sel("isMainFrame"))&0xff == 0 {
		return 0
	}
	body := objc.ID(objc.Send(message, objc.Sel("body")))
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

const (
	navigationCancel = 0
	navigationAllow  = 1
)

func bridgeDecideNavigation(a *ffi.Args) uintptr {
	action, handler := objc.ID(a.Int[3]), a.Int[4]
	policy := uintptr(navigationCancel)
	defer func() {
		if r := recover(); r != nil {
			policy = navigationCancel
			if s := activeShell.Load(); s != nil {
				s.logger.Error("desktop: navigation policy panicked", "panic", textsafe.Recovered(r))
			}
		}
		objc.CallBlock(handler, policy)
	}()
	if decideNavigation(activeShell.Load(), action) {
		policy = navigationAllow
	}
	return 0
}

func decideNavigation(s *darwinShell, action objc.ID) bool {
	if s == nil || s.allowNavigation == nil || action == 0 {
		return true
	}
	frame := objc.ID(objc.Send(action, objc.Sel("targetFrame")))
	if frame == 0 || objc.Send(frame, objc.Sel("isMainFrame"))&0xff == 0 {
		return true
	}
	req := objc.ID(objc.Send(action, objc.Sel("request")))
	u := objc.ID(objc.Send(req, objc.Sel("URL")))
	raw := ""
	if u != 0 {
		raw = objc.GoString(objc.ID(objc.Send(u, objc.Sel("absoluteString"))))
	}
	if s.allowNavigation(raw) {
		return true
	}
	s.logger.Warn("desktop: refused a navigation off the app's origin", "host", desktop.NavigationHost(raw))
	return false
}

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
	if w.id == desktop.MainWindowID {
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
	releaseSecondaryWindow(w)
	if onClosed := s.onWindowClosedLocked(); onClosed != nil {
		deliverOnGoroutine(func() { onClosed(w.id) })
	}
	return 1
}

func (s *darwinShell) onWindowClosedLocked() func(string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.onWindowClosed
}

func bridgeWindowDidMove(a *ffi.Args) uintptr {
	reportWindowFrameNote(a)
	return 0
}

func bridgeWindowDidEndLiveResize(a *ffi.Args) uintptr {
	reportWindowFrameNote(a)
	return 0
}

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

func bridgeWindowDidResize(a *ffi.Args) uintptr {
	if s := activeShell.Load(); s != nil {
		if w, ok := windowFromNote(a); ok {
			w.reapplyTrafficInset()
		}
	}
	return 0
}

func bridgeWindowDidBecomeKey(a *ffi.Args) uintptr {
	if s := activeShell.Load(); s != nil {
		if w, ok := windowFromNote(a); ok {
			w.reapplyTrafficInset()
			s.reportWindowActivity(w, true)
		}
	}
	return 0
}

func bridgeWindowDidResignKey(a *ffi.Args) uintptr {
	if s := activeShell.Load(); s != nil {
		if w, ok := windowFromNote(a); ok {
			s.reportWindowActivity(w, false)
		}
	}
	return 0
}

func windowFromNote(a *ffi.Args) (*darwinWindow, bool) {
	s := activeShell.Load()
	if s == nil {
		return nil, false
	}
	note := objc.ID(a.Int[2])
	if note == 0 {
		return nil, false
	}
	win := objc.ID(objc.Send(note, objc.Sel("object")))
	return s.lookupWindow(win)
}

func bridgeWorkspaceAccessibility(a *ffi.Args) uintptr {
	s := activeShell.Load()
	if s == nil {
		return 0
	}
	ws := objc.Send(objc.Class("NSWorkspace"), objc.Sel("sharedWorkspace"))
	s.appearanceChanged(objc.SendBool(objc.ID(ws), objc.Sel("accessibilityDisplayShouldReduceTransparency")))
	return 0
}

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

func bridgeSettingsAction(a *ffi.Args) uintptr {
	if s := activeShell.Load(); s != nil {
		s.mu.Lock()
		onSettings, textCtx := s.onSettings, s.textCtx
		s.mu.Unlock()
		if textCtx == nil {
			textCtx = context.Background()
		}
		if onSettings != nil {
			deliverOnGoroutine(func() { onSettings(textCtx) })
		}
	}
	return 0
}

func bridgeShowAction(a *ffi.Args) uintptr {
	if s := activeShell.Load(); s != nil {
		s.showMainWindow()
	}
	return 0
}

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
	deliverOnGoroutine(func() { onMenu(id) })
	return 0
}

func bridgeShouldTerminate(a *ffi.Args) uintptr {
	bridgeQuitAction(a)
	return 0
}

const unPresentBannerListSound = 16 | 8 | 2

func bridgeWillPresentNotification(a *ffi.Args) uintptr {
	block := a.Int[4]
	if block == 0 {
		return 0
	}
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

func (s *darwinShell) buildMenuBar(plans []desktop.MenuPlan) {
	mainMenu := objc.ID(objc.Send(objc.ID(objc.Send(objc.Class("NSMenu"), objc.Sel("alloc"))), objc.Sel("init")))
	for _, mp := range plans {
		top := objc.ID(objc.Send(objc.ID(objc.Send(objc.Class("NSMenuItem"), objc.Sel("alloc"))), objc.Sel("init")))
		objc.Send(top, objc.Sel("setTitle:"), uintptr(objc.NSString(mp.Title)))
		s.nameMenuItem(top, mp.English, "")
		objc.Send(top, objc.Sel("setSubmenu:"), uintptr(s.buildMenu(mp.Items)))
		objc.Send(mainMenu, objc.Sel("addItem:"), uintptr(top))
	}
	objc.Send(s.nsApp, objc.Sel("setMainMenu:"), uintptr(mainMenu))
	objc.Send(s.nsApp, objc.Sel("setDelegate:"), uintptr(s.bridge))
}

func (s *darwinShell) buildMenu(rows []desktop.MenuItemPlan) objc.ID {
	menu := objc.ID(objc.Send(objc.ID(objc.Send(objc.Class("NSMenu"), objc.Sel("alloc"))), objc.Sel("init")))
	for _, r := range rows {
		var item objc.ID
		if r.Sep {
			item = objc.ID(objc.Send(objc.Class("NSMenuItem"), objc.Sel("separatorItem")))
		} else {
			item = objc.ID(objc.Send(objc.ID(objc.Send(objc.Class("NSMenuItem"), objc.Sel("alloc"))), objc.Sel("init")))
			objc.Send(item, objc.Sel("setTitle:"), uintptr(objc.NSString(r.Title)))
			s.nameMenuItem(item, r.English, r.Role)
			if r.Key != "" {
				objc.Send(item, objc.Sel("setKeyEquivalent:"), uintptr(objc.NSString(r.Key)))
				if r.Mask != 0 {
					objc.Send(item, objc.Sel("setKeyEquivalentModifierMask:"), r.Mask)
				}
			}
			switch {
			case r.FirstResponder != "":
				objc.Send(item, objc.Sel("setAction:"), objc.Sel(r.FirstResponder))
			case r.Role == desktop.RoleSettings:
				objc.Send(item, objc.Sel("setTarget:"), uintptr(s.bridge))
				objc.Send(item, objc.Sel("setAction:"), objc.Sel("settingsAction:"))
			case r.Role == desktop.RoleShow:
				objc.Send(item, objc.Sel("setTarget:"), uintptr(s.bridge))
				objc.Send(item, objc.Sel("setAction:"), objc.Sel("showAction:"))
			case r.Role == desktop.RoleQuit:
				objc.Send(item, objc.Sel("setTarget:"), uintptr(s.bridge))
				objc.Send(item, objc.Sel("setAction:"), objc.Sel("quitAction:"))
			case r.Role == desktop.RoleAbout:
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

type menuName struct{ english, role string }

func (s *darwinShell) nameMenuItem(item objc.ID, english, role string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.menuNames == nil {
		s.menuNames = map[objc.ID]menuName{}
	}
	s.menuNames[item] = menuName{english: english, role: role}
}
