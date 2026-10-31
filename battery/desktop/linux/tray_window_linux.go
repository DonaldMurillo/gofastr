//go:build linux && (amd64 || arm64)

package linux

import (
	"sync"
	"sync/atomic"

	"github.com/DonaldMurillo/gofastr/battery/desktop"
	"github.com/DonaldMurillo/gofastr/battery/desktop/internal/ffi"
	"github.com/DonaldMurillo/gofastr/battery/desktop/internal/gtk"
)

type trayWindowState struct {
	broker *trayBroker
	window *linuxWindow
	hidden atomic.Bool
	about  uintptr // GTK thread only; reset by its destroy signal
}

var trayShells sync.Map // *linuxShell -> *trayWindowState, removed before window destruction

func startLinuxTray(w *linuxWindow) error {
	s := w.shell
	if s.cfg.Tray == nil {
		return nil
	}
	model, err := newTrayModel(s.text(), s.cfg)
	if err != nil {
		return err
	}
	state := &trayWindowState{window: w}
	state.broker = newTrayBroker(model, state.action, func() bool {
		// Test hidden INSIDE GTK dispatch. A watcher may disappear between the
		// close callback's availability check and its gtk_widget_hide call.
		err := s.Main(func() {
			if state.hidden.Load() && !w.closed.Load() {
				state.showMain()
			}
		})
		return err == nil || !s.running.Load() || w.closed.Load()
	})
	if err := state.broker.start(); err != nil {
		return err
	}
	trayShells.Store(s, state)
	connect(w.handle, "delete-event", trayDeleteCallback, 0)
	return nil
}

func (state *trayWindowState) action(action trayAction) {
	w, s := state.window, state.window.shell
	if w.closed.Load() {
		return
	}
	switch action.plan.Role {
	case desktop.RoleQuit:
		// Quit is deliberately not timeout-cancellable GTK dispatch. A busy
		// main loop must retain the user's explicit shutdown request.
		s.Quit()
	case desktop.RoleShow:
		state.show()
	case desktop.RoleSettings:
		if s.cfg.OnSettings != nil {
			s.cfg.OnSettings(s.text())
		}
	case desktop.RoleAbout:
		_ = w.withMain(state.showAboutMain)
	default:
		if action.id != "" {
			if s.cfg.OnTrayMenu != nil {
				s.cfg.OnTrayMenu(action.id)
			} else if s.cfg.OnMenu != nil {
				s.cfg.OnMenu(action.id)
			}
		}
	}
}

func (state *trayWindowState) showAboutMain() {
	if state.about != 0 {
		call(gtk.Sym.GtkWindowPresent, state.about)
		return
	}
	title, body := desktop.AboutText(state.window.shell.text())
	withString("%s", func(format uintptr) {
		withString(body, func(text uintptr) {
			state.about = call(gtk.Sym.GtkMessageDialogNew, state.window.handle, 2, 0, 2, format, text)
		})
	})
	withString(title, func(p uintptr) { call(gtk.Sym.GtkWindowSetTitle, state.about, p) })
	connect(state.about, "response", aboutResponse, 0)
	connect(state.about, "destroy", trayAboutDestroyed, state.window.handle)
	call(gtk.Sym.GtkWidgetShowAll, state.about)
}

var trayAboutDestroyed = ffi.NewCallback(func(a *ffi.Args) uintptr {
	if value, ok := nativeWindows.Load(a.Int[1]); ok {
		if value, ok := trayShells.Load(value.(*linuxWindow).shell); ok {
			state := value.(*trayWindowState)
			if state.about == a.Int[0] {
				state.about = 0
			}
		}
	}
	return 0
})

func stopLinuxTray(s *linuxShell) {
	if value, ok := trayShells.LoadAndDelete(s); ok {
		value.(*trayWindowState).broker.close()
	}
}
func setLinuxTrayTitle(s *linuxShell, title string) error {
	if value, ok := trayShells.Load(s); ok {
		return value.(*trayWindowState).broker.setTitle(title)
	}
	return &desktop.Error{Code: desktop.CodeUnsupported, Message: "Linux status notifier tray is unavailable; the window remains visible"}
}
func (s *trayWindowState) show() { _ = s.window.withMain(func() { s.showMain() }) }
func (s *trayWindowState) showMain() {
	if s.window.closed.Load() {
		return
	}
	s.hidden.Store(false)
	trayCall("gtk_widget_show", s.window.handle)
	call(gtk.Sym.GtkWindowPresent, s.window.handle)
}

var trayDeleteCallback = ffi.NewCallback(func(a *ffi.Args) uintptr {
	value, ok := nativeWindows.Load(a.Int[0])
	if !ok {
		return 0
	}
	w := value.(*linuxWindow)
	value, ok = trayShells.Load(w.shell)
	if !ok {
		return 0
	}
	state := value.(*trayWindowState)
	if w.id != desktop.MainWindowID || w.shell.quitting.Load() || !w.shell.cfg.Tray.CloseHidesWindow || !state.broker.available.Load() {
		return 0
	}
	trayCall("gtk_widget_hide", w.handle)
	state.hidden.Store(true)
	return 1 // GDK_EVENT_STOP: the window can be recovered via the live host.
})
