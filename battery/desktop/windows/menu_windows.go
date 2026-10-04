//go:build windows && amd64

package windows

import (
	"fmt"
	"strings"

	"github.com/DonaldMurillo/gofastr/battery/desktop"
	"github.com/DonaldMurillo/gofastr/battery/desktop/internal/win32"
)

func (s *winShell) installMenus(hwnd uintptr, title string, menu *desktop.Menu, hasSettings bool) error {
	plans, ids := desktop.PlanMenuBar(title, menu, hasSettings)
	s.actionIDs = ids
	root, err := win32.CreateMenu()
	if err != nil {
		return err
	}
	for _, p := range plans {
		popup, err := win32.CreatePopupMenu()
		if err != nil {
			return err
		}
		if err := s.appendRows(popup, p.Items, []string{p.Title}); err != nil {
			return err
		}
		if err := win32.AppendMenu(root, win32.MF_POPUP, popup, p.Title); err != nil {
			return err
		}
	}
	if err := win32.SetMenu(hwnd, root); err != nil {
		return err
	}
	win32.DrawMenuBar(hwnd)
	return nil
}

func pathKey(path []string) string { return strings.Join(path, "\x00") }

func (s *winShell) appendRows(menu uintptr, rows []desktop.MenuItemPlan, prefix []string) error {
	for _, row := range rows {
		if row.Sep {
			if err := win32.AppendMenu(menu, win32.MF_SEPARATOR, 0, ""); err != nil {
				return err
			}
			continue
		}
		label := row.Title
		if label == "" && row.Role != "" {
			label = row.Role
		}
		path := append(append([]string(nil), prefix...), label)
		if len(row.Submenu) != 0 {
			sub, err := win32.CreatePopupMenu()
			if err != nil {
				return err
			}
			if err := s.appendRows(sub, row.Submenu, path); err != nil {
				return err
			}
			if err := win32.AppendMenu(menu, win32.MF_POPUP, sub, acceleratorLabel(row)); err != nil {
				return err
			}
			continue
		}
		id := uint16(1000 + len(s.menuActions))
		action := menuAction{role: row.Role, firstResponder: row.FirstResponder, path: path, label: label, key: row.Key, mask: row.Mask}
		if row.Action && row.Tag >= 0 && row.Tag < len(s.actionIDs) {
			action.id = s.actionIDs[row.Tag]
		}
		s.menuActions[id] = action
		if err := win32.AppendMenu(menu, win32.MF_STRING, uintptr(id), acceleratorLabel(row)); err != nil {
			return err
		}
	}
	return nil
}

func acceleratorLabel(row desktop.MenuItemPlan) string {
	label := row.Title
	if label == "" {
		label = row.Role
	}
	if row.Key == "" {
		return label
	}
	mods := make([]string, 0, 3)
	if row.Mask&(1<<20) != 0 || row.Mask&(1<<18) != 0 {
		mods = append(mods, "Ctrl")
	}
	if row.Mask&(1<<19) != 0 {
		mods = append(mods, "Alt")
	}
	if row.Mask&(1<<17) != 0 {
		mods = append(mods, "Shift")
	}
	mods = append(mods, strings.ToUpper(row.Key))
	return label + "\t" + strings.Join(mods, "+")
}

func (s *winShell) handleMenuCommand(id uint16) {
	s.mu.RLock()
	a, ok := s.menuActions[id]
	s.mu.RUnlock()
	if ok {
		s.invokeMenuAction(a)
	}
}

func (s *winShell) invokeMenuAction(a menuAction) {
	if a.firstResponder != "" {
		s.invokeEditorCommand(a.firstResponder)
		return
	}
	switch a.role {
	case desktop.RoleQuit:
		s.Quit()
	case desktop.RoleSettings:
		if s.onSettings != nil {
			go s.onSettings()
		}
	case desktop.RoleShow:
		s.mu.RLock()
		w := s.windowsByID[desktop.MainWindowID]
		s.mu.RUnlock()
		if w != nil {
			_ = w.Focus()
		}
	case desktop.RoleAbout:
		win32.MessageBox(s.hwnd, "A desktop application built with gofastr.", "About", 0)
	default:
		if a.id != "" && s.onMenu != nil {
			go s.onMenu(a.id)
		}
	}
}

func (s *winShell) invokeEditorCommand(selector string) {
	var key uint16
	switch selector {
	case "undo:":
		key = 'Z'
	case "redo:":
		key = 'Y'
	case "cut:":
		key = 'X'
	case "copy:":
		key = 'C'
	case "paste:":
		key = 'V'
	case "selectAll:":
		key = 'A'
	default:
		return
	}
	s.mu.RLock()
	w := s.windowsByID[desktop.MainWindowID]
	s.mu.RUnlock()
	if w != nil {
		_ = s.Main(func() {
			if w.closed.Load() || w.hwnd == 0 {
				return
			}
			if err := w.Focus(); err != nil {
				return
			}
			if w.controller != 0 {
				_, _ = win32.COMCall(w.controller, 12, 0 /* COREWEBVIEW2_MOVE_FOCUS_REASON_PROGRAMMATIC */)
			}
			_ = win32.SendKeyboardShortcut(key, 0x11 /* VK_CONTROL */)
		})
	}
}

func (s *winShell) findAction(path []string, tray bool) (menuAction, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if tray && s.tray != nil {
		if a, ok := s.tray.actions[pathKey(path)]; ok {
			return a, true
		}
	}
	for _, a := range s.menuActions {
		if len(a.path) != len(path) {
			continue
		}
		match := true
		for i := range path {
			if !strings.EqualFold(a.path[i], path[i]) {
				match = false
				break
			}
		}
		if match {
			return a, true
		}
	}
	return menuAction{}, false
}

func (s *winShell) ActivateMenu(titles ...string) error {
	a, ok := s.findAction(titles, false)
	if !ok {
		return fmt.Errorf("desktop/windows: menu item %q was not found", strings.Join(titles, " > "))
	}
	s.invokeMenuAction(a)
	return nil
}

func (s *winShell) ActivateTray(title string) error {
	a, ok := s.findAction([]string{title}, true)
	if !ok {
		return fmt.Errorf("desktop/windows: tray item %q was not found", title)
	}
	s.invokeMenuAction(a)
	return nil
}

func (s *winShell) OpenSettingsItem() error {
	for _, a := range s.menuActions {
		if a.role == desktop.RoleSettings {
			s.invokeMenuAction(a)
			return nil
		}
	}
	return &desktop.Error{Code: desktop.CodeUnsupported, Message: "the app has no Settings menu item"}
}

func (s *winShell) ScriptPrompts(ds ...desktop.Decision) {
	s.promptMu.Lock()
	s.promptQueue = append(s.promptQueue, ds...)
	s.promptMu.Unlock()
}

func (s *winShell) ClickPrompt(button string) error {
	names := []string{button}
	switch strings.ToLower(button) {
	case "allow":
		names = append(names, "Yes")
	case "allow once":
		names = append(names, "No")
	case "deny":
		names = append(names, "Cancel")
	}
	for i := 0; i < 100; i++ {
		dlg := win32.FindWindow("#32770", "Permission required")
		if dlg != 0 {
			for _, name := range names {
				buttonName, _ := win32.UTF16Ptr(name)
				child := win32.FindWindowEx(dlg, "Button", buttonName)
				if child != 0 {
					win32.ClickWindow(child)
					return nil
				}
			}
		}
		waitWindows(50)
	}
	return fmt.Errorf("desktop/windows: permission dialog button %q did not appear", button)
}

func (s *winShell) PostDeepLink(rawURL string) error {
	if s.onDeepLink == nil {
		return &desktop.Error{Code: desktop.CodeUnsupported, Message: "the app has no deep link handler"}
	}
	go s.onDeepLink(rawURL)
	return nil
}

func (s *winShell) CloseWindowNative(id string) error {
	s.mu.RLock()
	w := s.windowsByID[id]
	s.mu.RUnlock()
	if w == nil {
		return fmt.Errorf("desktop/windows: window %q was not found", id)
	}
	return win32.PostMessage(w.hwnd, wmClose, 0, 0)
}

func (s *winShell) WindowState(id string) (desktop.WindowState, error) {
	s.mu.RLock()
	w := s.windowsByID[id]
	s.mu.RUnlock()
	if w == nil {
		return desktop.WindowState{}, errWindowClosed()
	}
	var state desktop.WindowState
	var nativeErr error
	err := s.Main(func() {
		if w.closed.Load() || !win32.IsWindow(w.hwnd) {
			nativeErr = errWindowClosed()
			return
		}
		frame := w.frame
		if !win32.IsIconic(w.hwnd) && !win32.IsZoomed(w.hwnd) {
			l, t, r, b, ok := win32.GetWindowRect(w.hwnd)
			if !ok {
				nativeErr = errWindowClosed()
				return
			}
			frame = frameFromWindowRect(w.hwnd, l, t, r, b)
		}
		state = desktop.WindowState{
			Title: w.title, Visible: win32.IsWindowVisible(w.hwnd),
			Key: win32.GetForegroundWindow() == w.hwnd, Class: "HWND", ContentClass: "WebView2",
			X: frame.X, Y: frame.Y, Width: frame.Width, Height: frame.Height,
			Material: w.material, SidebarWidth: w.sidebarWidth,
			TitlebarTransparent: w.titlebarTransparent,
		}
		if w.style.Chrome == desktop.ChromeHiddenTitle || w.style.Chrome == desktop.ChromeUnified {
			state.TitleVisibility = "visible"
			if win32.GetWindowText(w.hwnd) == "" {
				state.TitleVisibility = "hidden"
			}
		}
	})
	if err != nil {
		return desktop.WindowState{}, err
	}
	if nativeErr != nil {
		return desktop.WindowState{}, nativeErr
	}
	return state, nil
}

func (s *winShell) MakeKey(id string) error {
	s.mu.RLock()
	w := s.windowsByID[id]
	s.mu.RUnlock()
	if w == nil {
		return errWindowClosed()
	}
	return w.Focus()
}

func (s *winShell) NotificationLog() []desktop.Notification {
	s.notifyMu.Lock()
	defer s.notifyMu.Unlock()
	return append([]desktop.Notification(nil), s.notifyLog...)
}
