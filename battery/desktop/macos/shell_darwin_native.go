//go:build darwin && arm64

package macos

import (
	"errors"
	"fmt"
	"time"

	"github.com/DonaldMurillo/gofastr/battery/desktop"
	"github.com/DonaldMurillo/gofastr/battery/desktop/internal/objc"
)

// The desktop.NativeDriver surface (shell.go) for the darwin shell: the user's
// hands on AppKit and eyes on native state, lifted out of the
// hand-rolled e2e so a desktop test drives the REAL shell through the
// same calls. Every method hops to the main thread; the one that waits
// for a person (ClickPrompt) uses the modal-safe perform hop.

var _ desktop.NativeDriver = (*darwinShell)(nil)

// notifyLogCap bounds the notification log, oldest dropped first.
const notifyLogCap = 64

// recordNotification appends to the log. Show calls it first, before
// the bundle check, so an unbundled host records every notification it
// was handed even though none can be delivered.
func (s *darwinShell) recordNotification(n desktop.Notification) {
	s.notifyLogMu.Lock()
	defer s.notifyLogMu.Unlock()
	s.notifyLog = append(s.notifyLog, n)
	if len(s.notifyLog) > notifyLogCap {
		s.notifyLog = s.notifyLog[len(s.notifyLog)-notifyLogCap:]
	}
}

// NotificationLog implements desktop.NativeDriver: every desktop.Notification Show
// received, bundled or not, in order.
func (s *darwinShell) NotificationLog() []desktop.Notification {
	s.notifyLogMu.Lock()
	defer s.notifyLogMu.Unlock()
	out := make([]desktop.Notification, len(s.notifyLog))
	copy(out, s.notifyLog)
	return out
}

// ScriptPrompts implements desktop.NativeDriver: the decisions answer the next
// permission prompts without an alert, consumed in order (Prompt pops
// the queue; an empty queue means the real alert shows).
func (s *darwinShell) ScriptPrompts(ds ...desktop.Decision) {
	if len(ds) == 0 {
		return
	}
	s.mu.Lock()
	s.promptQueue = append(s.promptQueue, ds...)
	s.mu.Unlock()
}

// ActivateMenu implements desktop.NativeDriver: walk the main menu bar by
// title path and fire the item's own action, exactly as a click does.
func (s *darwinShell) ActivateMenu(titles ...string) error {
	if len(titles) == 0 {
		return errors.New("desktop: ActivateMenu needs a title path")
	}
	var actErr error
	if err := s.Main(func() {
		nsApp := s.appID()
		if nsApp == 0 {
			actErr = errors.New("desktop: the app is not running")
			return
		}
		mainMenu := objc.ID(objc.Send(nsApp, objc.Sel("mainMenu")))
		if mainMenu == 0 {
			actErr = errors.New("desktop: no main menu")
			return
		}
		menu := mainMenu
		var item objc.ID
		for i, title := range titles {
			item = s.matchMenuItem(menu, title)
			if item == 0 {
				actErr = fmt.Errorf("desktop: no menu item %q (path %v)", title, titles)
				return
			}
			if i < len(titles)-1 {
				menu = objc.ID(objc.Send(item, objc.Sel("submenu")))
				if menu == 0 {
					actErr = fmt.Errorf("desktop: menu item %q has no submenu (path %v)", title, titles)
					return
				}
			}
		}
		actErr = s.fireMenuItem(nsApp, item)
	}); err != nil {
		return &desktop.Error{Code: desktop.CodeInternal, Message: desktop.InternalErrorMsg}
	}
	return actErr
}

// OpenSettingsItem implements desktop.NativeDriver: the app menu's own
// Settings item, the one desktop.PlanMenuBar synthesizes for Config.Settings.
func (s *darwinShell) OpenSettingsItem() error {
	var actErr error
	if err := s.Main(func() {
		nsApp := s.appID()
		if nsApp == 0 {
			actErr = errors.New("desktop: the app is not running")
			return
		}
		mainMenu := objc.ID(objc.Send(nsApp, objc.Sel("mainMenu")))
		if mainMenu == 0 {
			actErr = errors.New("desktop: no main menu")
			return
		}
		top := objc.ID(objc.Send(mainMenu, objc.Sel("itemAtIndex:"), 0)) // the app menu
		if top == 0 {
			actErr = errors.New("desktop: the app menu is missing")
			return
		}
		item := s.matchMenuItem(objc.ID(objc.Send(top, objc.Sel("submenu"))), desktop.RoleSettings)
		if item == 0 {
			actErr = errors.New("desktop: no Settings item; Config.Settings is unset")
			return
		}
		actErr = s.fireMenuItem(nsApp, item)
	}); err != nil {
		return &desktop.Error{Code: desktop.CodeInternal, Message: desktop.InternalErrorMsg}
	}
	return actErr
}

// ActivateTray implements desktop.NativeDriver: fire a row of the status
// item's menu by title (role name when untitled).
func (s *darwinShell) ActivateTray(title string) error {
	s.mu.Lock()
	item := s.statusItem
	s.mu.Unlock()
	if item == 0 {
		return errNoTray()
	}
	var actErr error
	if err := s.Main(func() {
		menu := objc.ID(objc.Send(item, objc.Sel("menu")))
		if menu == 0 {
			actErr = errors.New("desktop: the tray item has no menu")
			return
		}
		row := s.matchMenuItem(menu, title)
		if row == 0 {
			actErr = fmt.Errorf("desktop: no tray menu item %q", title)
			return
		}
		actErr = s.fireMenuItem(s.appID(), row)
	}); err != nil {
		return &desktop.Error{Code: desktop.CodeInternal, Message: desktop.InternalErrorMsg}
	}
	return actErr
}

// matchMenuItem finds a row by title, falling back to the OS-standard
// label a role item carries when the plan left its Title empty ("quit"
// matches "Quit <app title>"). Call on the main thread.
func (s *darwinShell) matchMenuItem(menu objc.ID, title string) objc.ID {
	if menu == 0 {
		return 0
	}
	if item := objc.ID(objc.Send(menu, objc.Sel("itemWithTitle:"), uintptr(objc.NSString(title)))); item != 0 {
		return item
	}
	s.mu.Lock()
	appTitle := s.title
	s.mu.Unlock()
	var alt string
	switch title {
	case desktop.RoleQuit:
		alt = "Quit " + appTitle
	case desktop.RoleAbout:
		alt = "About " + appTitle
	case desktop.RoleSettings:
		alt = "Settings…"
	case desktop.RoleShow:
		alt = "Show " + appTitle
	default:
		return 0
	}
	return objc.ID(objc.Send(menu, objc.Sel("itemWithTitle:"), uintptr(objc.NSString(alt))))
}

// fireMenuItem sends the item's own action with the item as the
// sender, the way a click does (the menuAction: IMP reads the sender's
// tag; a nil target routes through the responder chain). Call on the
// main thread.
func (s *darwinShell) fireMenuItem(nsApp, item objc.ID) error {
	action := objc.Send(item, objc.Sel("action"))
	if action == 0 {
		return errors.New("desktop: the menu item has no action")
	}
	target := objc.Send(item, objc.Sel("target"))
	objc.Send(nsApp, objc.Sel("sendAction:to:from:"), action, target, uintptr(item))
	return nil
}

// clickPromptWait bounds how long ClickPrompt waits for the alert.
const clickPromptWait = 20 * time.Second

// ClickPrompt implements desktop.NativeDriver: wait for the modal permission
// alert and click its button titled button, the REAL alert path. The
// hop is performSelectorOnMainThread (modal-safe: the dispatch main
// queue is NOT drained while runModal owns the UI thread, the perform
// queue is).
func (s *darwinShell) ClickPrompt(button string) error {
	if s.appID() == 0 {
		return errors.New("desktop: the app is not running")
	}
	deadline := time.Now().Add(clickPromptWait)
	for {
		clicked := false
		if err := s.mainViaPerform(func() {
			w := objc.Send(s.appID(), objc.Sel("modalWindow"))
			if w == 0 {
				return
			}
			clicked = clickButtonTitled(objc.ID(w), button)
		}); err != nil {
			return err
		}
		if clicked {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("desktop: no modal alert with a %q button appeared within %s", button, clickPromptWait)
		}
		time.Sleep(60 * time.Millisecond)
	}
}

// clickButtonTitled walks a window's view tree and performClick:s the
// first NSButton with the given title (lifted from the e2e verbatim).
func clickButtonTitled(win objc.ID, title string) bool {
	found := false
	buttonClass := objc.Class("NSButton")
	var walk func(v objc.ID)
	walk = func(v objc.ID) {
		if found {
			return
		}
		if objc.Send(v, objc.Sel("isKindOfClass:"), uintptr(buttonClass)) != 0 {
			if objc.GoString(objc.ID(objc.Send(v, objc.Sel("title")))) == title {
				objc.Send(v, objc.Sel("performClick:"), 0)
				found = true
				return
			}
		}
		subs := objc.ID(objc.Send(v, objc.Sel("subviews")))
		n := objc.Send(subs, objc.Sel("count"))
		for j := range n {
			walk(objc.ID(objc.Send(subs, objc.Sel("objectAtIndex:"), j)))
		}
	}
	cv := objc.Send(win, objc.Sel("contentView"))
	if cv == 0 {
		return false
	}
	walk(objc.ID(cv))
	return found
}

// PostDeepLink implements desktop.NativeDriver: hand the URL to the GetURL
// handler exactly as the OS would.
func (s *darwinShell) PostDeepLink(rawURL string) error {
	var sendErr error
	if err := s.Main(func() {
		sendErr = postGetURLEvent(s.bridgeID(), rawURL)
	}); err != nil {
		return &desktop.Error{Code: desktop.CodeInternal, Message: desktop.InternalErrorMsg}
	}
	return sendErr
}

// postGetURLEvent builds the GURL Apple Event (the URL as its direct
// object, exactly what the OS delivers) and hands it to the handler
// NSAppleEventManager has registered, through the selector the manager
// itself invokes, on the main thread the manager dispatches on. Must
// run on the main thread.
//
// The Mach hop a real open takes cannot be synthesized in a test: an
// unbundled go-test process answers procNotFound (-600) to every
// self-send shape (typeCurrentProcess, a literal and a
// GetCurrentProcess PSN, typeKernelProcessID) and connectionInvalid
// (-608) to AEProcessAppleEvent, all probed 2026-09-05. Everything
// downstream of the port, registration, dispatch, the handler IMP,
// the battery path, the page, runs for real here.
func postGetURLEvent(bridge objc.ID, raw string) error {
	direct := objc.Send(objc.Class("NSAppleEventDescriptor"),
		objc.Sel("descriptorWithString:"), uintptr(objc.NSString(raw)))
	reply := objc.Send(objc.Class("NSAppleEventDescriptor"), objc.Sel("nullDescriptor"))
	if direct == 0 || reply == 0 {
		return fmt.Errorf("building the GetURL descriptors failed")
	}
	event := objc.Send(objc.Class("NSAppleEventDescriptor"),
		objc.Sel("appleEventWithEventClass:eventID:targetDescriptor:returnID:transactionID:"),
		uintptr(aeGetURLClass), uintptr(aeGetURLID), uintptr(reply),
		^uintptr(0), // kAutoGenerateReturnID (-1)
		0)           // kAnyTransactionID
	if event == 0 {
		return fmt.Errorf("appleEventWithEventClass returned nil")
	}
	objc.Send(objc.ID(event), objc.Sel("setParamDescriptor:forKeyword:"), uintptr(direct), uintptr(aeKeyDirect))
	objc.Send(bridge, objc.Sel("handleGetURLEvent:withReplyEvent:"), uintptr(event), uintptr(reply))
	return nil
}

// CloseWindowNative implements desktop.NativeDriver: the red button. darwin
// desktop.Window.Close already routes performClose: against the delegate (or
// drives windowShouldClose: by hand on a borderless window), so this
// is exactly that.
func (s *darwinShell) CloseWindowNative(id string) error {
	w, ok := s.windowByID(id)
	if !ok {
		return &desktop.Error{Code: desktop.CodeNotFound, Message: "no window with id " + id}
	}
	return w.Close()
}

// WindowState implements desktop.NativeDriver: the OS facts, read on the main
// thread.
func (s *darwinShell) WindowState(id string) (desktop.WindowState, error) {
	w, ok := s.windowByID(id)
	if !ok {
		return desktop.WindowState{}, &desktop.Error{Code: desktop.CodeNotFound, Message: "no window with id " + id}
	}
	win, _ := w.ids()
	if win == 0 {
		return desktop.WindowState{}, errWindowClosed()
	}
	var st desktop.WindowState
	if err := s.Main(func() {
		st.Title = objc.GoString(objc.ID(objc.Send(win, objc.Sel("title"))))
		st.Visible = objc.Send(win, objc.Sel("isVisible")) != 0
		st.Key = objc.Send(win, objc.Sel("isKeyWindow")) != 0
		st.Level = int(int64(objc.Send(win, objc.Sel("level"))))
		st.StyleMask = uint64(objc.Send(win, objc.Sel("styleMask")))
		st.Class = objc.GoString(objc.ID(objc.Send(objc.ID(objc.Send(win, objc.Sel("class"))), objc.Sel("description"))))
		if cv := objc.ID(objc.Send(win, objc.Sel("contentView"))); cv != 0 {
			st.ContentClass = objc.GoString(objc.ID(objc.Send(objc.ID(objc.Send(cv, objc.Sel("class"))), objc.Sel("description"))))
		}
		frame := objc.SendRectRet(win, objc.Sel("frame"))
		f := rectToFrame(frame, readScreens())
		st.X, st.Y = f.X, f.Y
		st.Width, st.Height = int(frame.W), int(frame.H)
	}); err != nil {
		return desktop.WindowState{}, &desktop.Error{Code: desktop.CodeInternal, Message: desktop.InternalErrorMsg}
	}
	return st, nil
}

// StatusItemTitle reads the tray button's title natively: the e2e
// readback seam (statusItemButtonTitle), exported so the external test
// package can assert tray.setTitle reached the status item.
func (s *darwinShell) StatusItemTitle() string {
	return s.statusItemButtonTitle()
}
