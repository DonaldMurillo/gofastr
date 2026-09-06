//go:build darwin && arm64

package desktop

import (
	"runtime"
	"unsafe"

	"github.com/DonaldMurillo/gofastr/battery/desktop/internal/objc"
)

// The macOS tray: one NSStatusItem on the system status bar, with the
// planned menu (shared action-id table with the menu bar) and the
// shell's own selectors for the roles the battery routes here
// (RoleShow) and the app menu (RoleSettings, via settingsAction:).
//
// Every call here runs on the main thread (installTray from Run,
// SetTrayTitle through onMain).

// installTray puts the status item in the menu bar. Call on the main
// thread, before the run loop starts. rows are the planned tray rows
// (planTrayMenu, sharing the menu bar's action-id table).
func (s *darwinShell) installTray(t *Tray, rows []menuItemPlan) {
	bar := objc.ID(objc.Send(objc.Class("NSStatusBar"), objc.Sel("systemStatusBar")))
	if bar == 0 {
		s.logger.Warn("desktop: no system status bar; the tray is not installed")
		return
	}
	// NSVariableStatusItemLength is a CGFloat (-1.0), so the argument
	// travels through SendF's float registers.
	item := objc.ID(objc.SendF(bar, objc.Sel("statusItemWithLength:"), nil, []float64{-1.0}))
	if item == 0 {
		s.logger.Warn("desktop: statusItemWithLength returned nil; the tray is not installed")
		return
	}
	// Retain: a released status item vanishes from the bar (the
	// process-lifetime autorelease pool is no guarantee here).
	objc.Send(item, objc.Sel("retain"))
	button := objc.ID(objc.Send(item, objc.Sel("button")))
	if button == 0 {
		s.logger.Warn("desktop: status item has no button; the tray is not installed")
		return
	}
	if t.Title != "" {
		objc.Send(button, objc.Sel("setTitle:"), uintptr(objc.NSString(t.Title)))
	}
	if t.Tooltip != "" {
		objc.Send(button, objc.Sel("setToolTip:"), uintptr(objc.NSString(t.Tooltip)))
	}
	if len(t.Icon) > 0 {
		if img := templateImage(t.Icon); img != 0 {
			objc.Send(button, objc.Sel("setImage:"), uintptr(img))
		}
	}
	if len(rows) > 0 {
		objc.Send(item, objc.Sel("setMenu:"), uintptr(s.buildMenu(rows)))
	}
	s.mu.Lock()
	s.statusItem = item
	s.trayTitle = t.Title
	s.mu.Unlock()
}

// templateImage builds the 18x18 template NSImage from PNG bytes.
// NSData copies the buffer during the call, so the Go slice only has
// to stay alive across it (runtime.KeepAlive, the NSString pattern).
func templateImage(png []byte) objc.ID {
	buf := png
	data := objc.Send(objc.Class("NSData"), objc.Sel("dataWithBytes:length:"), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	runtime.KeepAlive(buf)
	if data == 0 {
		return 0
	}
	// alloc first: initWithData: is an instance initializer, and sending
	// it to the class raises NSInvalidArgumentException (the first
	// bundled run with a tray icon died on exactly that at startup).
	img := objc.ID(objc.Send(objc.ID(objc.Send(objc.Class("NSImage"), objc.Sel("alloc"))), objc.Sel("initWithData:"), data))
	if img == 0 {
		return 0
	}
	objc.Send(img, objc.Sel("setTemplate:"), 1)
	objc.SendF(img, objc.Sel("setSize:"), nil, []float64{18, 18}) // NSSize{18,18}
	return img
}

// SetTrayTitle changes the tray button's title.
func (s *darwinShell) SetTrayTitle(title string) error {
	s.mu.Lock()
	item := s.statusItem
	s.trayTitle = title
	s.mu.Unlock()
	if item == 0 {
		// No tray configured (or not installed yet): unsupported, the
		// named answer the battery logs.
		return errNoTray()
	}
	err := s.onMain(func() {
		if button := objc.ID(objc.Send(item, objc.Sel("button"))); button != 0 {
			objc.Send(button, objc.Sel("setTitle:"), uintptr(objc.NSString(title)))
		}
	})
	if err != nil {
		return &Error{Code: CodeInternal, Message: internalErrorMsg}
	}
	return nil
}

// errNoTray is the fixed answer for tray calls without a tray.
func errNoTray() *Error {
	return &Error{Code: CodeUnsupported, Message: "no tray is configured on this host"}
}

// statusItemButtonTitle reads the tray button's title natively: the
// e2e seam for "the status item exists with the configured title" and
// for tray.setTitle round-trips.
func (s *darwinShell) statusItemButtonTitle() string {
	s.mu.Lock()
	item := s.statusItem
	s.mu.Unlock()
	if item == 0 {
		return ""
	}
	var title string
	_ = s.onMain(func() {
		if button := objc.ID(objc.Send(item, objc.Sel("button"))); button != 0 {
			title = objc.GoString(objc.ID(objc.Send(button, objc.Sel("title"))))
		}
	})
	return title
}

// showMainWindow is RoleShow: order the main window front and
// activate the app. Main thread.
func (s *darwinShell) showMainWindow() {
	if win := s.windowID(); win != 0 {
		objc.Send(win, objc.Sel("makeKeyAndOrderFront:"), 0)
	}
	objc.Send(s.appID(), objc.Sel("activateIgnoringOtherApps:"), 1)
}
