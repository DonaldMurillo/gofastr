//go:build darwin && (arm64 || amd64)

package macos

import (
	"errors"
	"fmt"
	"time"

	"github.com/DonaldMurillo/gofastr/battery/desktop"
	"github.com/DonaldMurillo/gofastr/battery/desktop/internal/objc"
)

var _ desktop.NativeDriver = (*darwinShell)(nil)

const notifyLogCap = 64

func (s *darwinShell) recordNotification(n desktop.Notification) {
	s.notifyLogMu.Lock()
	defer s.notifyLogMu.Unlock()
	s.notifyLog = append(s.notifyLog, n)
	if len(s.notifyLog) > notifyLogCap {
		s.notifyLog = s.notifyLog[len(s.notifyLog)-notifyLogCap:]
	}
}

func (s *darwinShell) NotificationLog() []desktop.Notification {
	s.notifyLogMu.Lock()
	defer s.notifyLogMu.Unlock()
	out := make([]desktop.Notification, len(s.notifyLog))
	copy(out, s.notifyLog)
	return out
}

func (s *darwinShell) ScriptPrompts(ds ...desktop.Decision) {
	if len(ds) == 0 {
		return
	}
	s.mu.Lock()
	s.promptQueue = append(s.promptQueue, ds...)
	s.mu.Unlock()
}

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

func (s *darwinShell) matchMenuItem(menu objc.ID, title string) objc.ID {
	if menu == 0 {
		return 0
	}
	n := int(objc.Send(menu, objc.Sel("numberOfItems")))
	items := make([]objc.ID, 0, n)
	for i := range n {
		items = append(items, objc.ID(objc.Send(menu, objc.Sel("itemAtIndex:"), uintptr(i))))
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, item := range items {
		if name, ok := s.menuNames[item]; ok && name.english != "" && name.english == title {
			return item
		}
	}
	for _, item := range items {
		if name, ok := s.menuNames[item]; ok && name.role != "" && name.role == title {
			return item
		}
	}
	return 0
}

func (s *darwinShell) fireMenuItem(nsApp, item objc.ID) error {
	action := objc.Send(item, objc.Sel("action"))
	if action == 0 {
		return errors.New("desktop: the menu item has no action")
	}
	target := objc.Send(item, objc.Sel("target"))
	objc.Send(nsApp, objc.Sel("sendAction:to:from:"), action, target, uintptr(item))
	return nil
}

const clickPromptWait = 20 * time.Second

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

func (s *darwinShell) PostDeepLink(rawURL string) error {
	var sendErr error
	if err := s.Main(func() {
		sendErr = postGetURLEvent(s.bridgeID(), rawURL)
	}); err != nil {
		return &desktop.Error{Code: desktop.CodeInternal, Message: desktop.InternalErrorMsg}
	}
	return sendErr
}

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

func (s *darwinShell) CloseWindowNative(id string) error {
	w, ok := s.windowByID(id)
	if !ok {
		return &desktop.Error{Code: desktop.CodeNotFound, Message: "no window with id " + id}
	}
	return w.Close()
}

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
	closed := false
	if err := s.Main(func() {
		if win, _ = w.ids(); win == 0 {
			closed = true
			return
		}
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
		st.CGWindowID = int64(objc.Send(win, objc.Sel("windowNumber")))
		st.TitlebarTransparent = objc.Send(win, objc.Sel("titlebarAppearsTransparent")) != 0
		if toolbar := objc.ID(objc.Send(win, objc.Sel("toolbar"))); toolbar != 0 {
			st.ToolbarStyle = toolbarStyleName(uintptr(objc.Send(win, objc.Sel("toolbarStyle"))))
		}
		st.TitleVisibility = titleVisibilityName(uintptr(objc.Send(win, objc.Sel("titleVisibility"))))
		st.Material, st.SidebarWidth = materialState(win)
	}); err != nil {
		return desktop.WindowState{}, &desktop.Error{Code: desktop.CodeInternal, Message: desktop.InternalErrorMsg}
	}
	if closed {
		return desktop.WindowState{}, errWindowClosed()
	}
	return st, nil
}

func (s *darwinShell) MakeKey(id string) error {
	w, ok := s.windowByID(id)
	if !ok {
		return &desktop.Error{Code: desktop.CodeNotFound, Message: "no window with id " + id}
	}
	win, _ := w.ids()
	if win == 0 {
		return errWindowClosed()
	}
	closed := false
	if err := s.onMain(func() {
		if win, _ = w.ids(); win == 0 {
			closed = true
			return
		}
		objc.Send(win, objc.Sel("makeKeyAndOrderFront:"), 0)
	}); err != nil {
		return &desktop.Error{Code: desktop.CodeInternal, Message: desktop.InternalErrorMsg}
	}
	if closed {
		return errWindowClosed()
	}
	return nil
}

func (s *darwinShell) StatusItemTitle() string {
	return s.statusItemButtonTitle()
}

func (s *darwinShell) StatusItemToolTip() string {
	return s.statusItemButtonAttr("toolTip")
}
