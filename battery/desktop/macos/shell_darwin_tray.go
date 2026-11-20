//go:build darwin && (arm64 || amd64)

package macos

import (
	"context"
	"runtime"
	"unsafe"

	"github.com/DonaldMurillo/gofastr/battery/desktop"
	"github.com/DonaldMurillo/gofastr/battery/desktop/internal/objc"
)

func (s *darwinShell) installTray(t *desktop.Tray, rows []desktop.MenuItemPlan) {
	bar := objc.ID(objc.Send(objc.Class("NSStatusBar"), objc.Sel("systemStatusBar")))
	if bar == 0 {
		s.logger.Warn("desktop: no system status bar; the tray is not installed")
		return
	}
	item := objc.ID(objc.SendF(bar, objc.Sel("statusItemWithLength:"), nil, []float64{-1.0}))
	if item == 0 {
		s.logger.Warn("desktop: statusItemWithLength returned nil; the tray is not installed")
		return
	}
	objc.Send(item, objc.Sel("retain"))
	button := objc.ID(objc.Send(item, objc.Sel("button")))
	if button == 0 {
		s.logger.Warn("desktop: status item has no button; the tray is not installed")
		return
	}
	if t.Title != "" {
		objc.Send(button, objc.Sel("setTitle:"), uintptr(objc.NSString(t.Title)))
	}
	if !t.Tooltip.IsZero() {
		s.mu.Lock()
		textCtx := s.textCtx
		s.mu.Unlock()
		if textCtx == nil {
			textCtx = context.Background()
		}
		objc.Send(button, objc.Sel("setToolTip:"), uintptr(objc.NSString(t.Tooltip.Text(textCtx))))
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

func templateImage(png []byte) objc.ID {
	buf := png
	data := objc.Send(objc.Class("NSData"), objc.Sel("dataWithBytes:length:"), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	runtime.KeepAlive(buf)
	if data == 0 {
		return 0
	}
	img := objc.ID(objc.Send(objc.ID(objc.Send(objc.Class("NSImage"), objc.Sel("alloc"))), objc.Sel("initWithData:"), data))
	if img == 0 {
		return 0
	}
	objc.Send(img, objc.Sel("setTemplate:"), 1)
	objc.SendF(img, objc.Sel("setSize:"), nil, []float64{18, 18}) // NSSize{18,18}
	return img
}

func (s *darwinShell) SetTrayTitle(title string) error {
	s.mu.Lock()
	item := s.statusItem
	s.trayTitle = title
	s.mu.Unlock()
	if item == 0 {
		return errNoTray()
	}
	err := s.onMain(func() {
		if button := objc.ID(objc.Send(item, objc.Sel("button"))); button != 0 {
			objc.Send(button, objc.Sel("setTitle:"), uintptr(objc.NSString(title)))
		}
	})
	if err != nil {
		return &desktop.Error{Code: desktop.CodeInternal, Message: desktop.InternalErrorMsg}
	}
	return nil
}

func errNoTray() *desktop.Error {
	return &desktop.Error{Code: desktop.CodeUnsupported, Message: "no tray is configured on this host"}
}

func (s *darwinShell) statusItemButtonAttr(attr string) string {
	s.mu.Lock()
	item := s.statusItem
	s.mu.Unlock()
	if item == 0 {
		return ""
	}
	var out string
	_ = s.onMain(func() {
		if button := objc.ID(objc.Send(item, objc.Sel("button"))); button != 0 {
			out = objc.GoString(objc.ID(objc.Send(button, objc.Sel(attr))))
		}
	})
	return out
}

func (s *darwinShell) statusItemButtonTitle() string {
	return s.statusItemButtonAttr("title")
}

func (s *darwinShell) showMainWindow() {
	if win := s.windowID(); win != 0 {
		objc.Send(win, objc.Sel("makeKeyAndOrderFront:"), 0)
	}
	objc.Send(s.appID(), objc.Sel("activateIgnoringOtherApps:"), 1)
}
