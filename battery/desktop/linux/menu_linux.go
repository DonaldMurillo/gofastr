//go:build linux && (amd64 || arm64)

package linux

import (
	"strings"
	"sync"
	"sync/atomic"

	"github.com/DonaldMurillo/gofastr/battery/desktop"
	"github.com/DonaldMurillo/gofastr/battery/desktop/internal/ffi"
	"github.com/DonaldMurillo/gofastr/battery/desktop/internal/gtk"
)

var menuSequence atomic.Uint64
var menuActions sync.Map

type menuAction struct {
	window *linuxWindow
	plan   desktop.MenuItemPlan
	id     string
}

var menuCallback = ffi.NewCallback(func(a *ffi.Args) uintptr {
	if value, ok := menuActions.Load(a.Int[1]); ok {
		value.(menuAction).activate()
	}
	return 0
})

func (w *linuxWindow) installMenus(box uintptr) {
	plans, ids := desktop.PlanMenuBar(w.Title(), w.shell.cfg.Menu, w.shell.cfg.Settings != nil)
	bar := call(gtk.Sym.GtkMenuBarNew)
	group := call(gtk.Sym.GtkAccelGroupNew)
	call(gtk.Sym.GtkWindowAddAccelGroup, w.handle, group)
	defer call(gtk.Sym.GObjectUnref, group)
	for _, plan := range plans {
		var item uintptr
		withString(plan.Title, func(p uintptr) { item = call(gtk.Sym.GtkMenuItemNewWithLabel, p) })
		submenu := w.menuRows(plan.Items, ids, group)
		call(gtk.Sym.GtkMenuItemSetSubmenu, item, submenu)
		call(gtk.Sym.GtkMenuShellAppend, bar, item)
	}
	call(gtk.Sym.GtkBoxPackStart, box, bar, 0, 0, 0)
}
func (w *linuxWindow) menuRows(rows []desktop.MenuItemPlan, ids []string, group uintptr) uintptr {
	menu := call(gtk.Sym.GtkMenuNew)
	for _, row := range rows {
		var item uintptr
		if row.Sep {
			item = call(gtk.Sym.GtkSeparatorMenuItemNew)
		} else {
			withString(row.Title, func(p uintptr) { item = call(gtk.Sym.GtkMenuItemNewWithLabel, p) })
			if len(row.Submenu) > 0 {
				call(gtk.Sym.GtkMenuItemSetSubmenu, item, w.menuRows(row.Submenu, ids, group))
			} else {
				action := menuAction{window: w, plan: row}
				if row.Action && row.Tag >= 0 && row.Tag < len(ids) {
					action.id = ids[row.Tag]
				}
				if action.id != "" || row.Role != "" || row.FirstResponder != "" {
					token := uintptr(menuSequence.Add(1))
					menuActions.Store(token, action)
					w.menuTokens = append(w.menuTokens, token)
					connect(item, "activate", menuCallback, token)
					if row.Key != "" {
						var key uintptr
						if runes := []rune(row.Key); len(runes) == 1 {
							key = uintptr(runes[0])
							if key > 0xff {
								key |= 0x01000000
							}
						} else {
							withString(row.Key, func(p uintptr) { key = call(gtk.Sym.GdkKeyvalFromName, p) })
						}
						if key != 0 && key != 0xffffff {
							withString("activate", func(p uintptr) { call(gtk.Sym.GtkWidgetAddAccelerator, item, p, group, key, gtkModifiers(row.Mask), 1) })
						}
					}
				} else {
					call(gtk.Sym.GtkWidgetSetSensitive, item, 0)
				}
			}
		}
		call(gtk.Sym.GtkMenuShellAppend, menu, item)
	}
	return menu
}
func gtkModifiers(mask uintptr) uintptr {
	var out uintptr
	if mask&(1<<20|1<<18) != 0 {
		out |= 1 << 2
	} // Command and Control -> Control
	if mask&(1<<17) != 0 {
		out |= 1
	} // Shift
	if mask&(1<<19) != 0 {
		out |= 1 << 3
	} // Alt
	return out
}
func (a menuAction) activate() {
	w := a.window
	s := w.shell
	if w.closed.Load() {
		return
	}
	switch a.plan.Role {
	case desktop.RoleQuit:
		s.Quit()
	case desktop.RoleSettings:
		if s.cfg.OnSettings != nil {
			go s.cfg.OnSettings()
		}
	case desktop.RoleShow:
		_ = w.Focus()
	case desktop.RoleAbout:
		// A plain informational GTK dialog; no application capability is invoked.
		var dialog uintptr
		withString("%s", func(format uintptr) {
			withString(s.cfg.Title, func(title uintptr) { dialog = call(gtk.Sym.GtkMessageDialogNew, w.handle, 2, 0, 2, format, title) })
		})
		connect(dialog, "response", aboutResponse, 0)
		call(gtk.Sym.GtkWidgetShowAll, dialog)
	default:
		if a.id != "" && s.cfg.OnMenu != nil {
			go s.cfg.OnMenu(a.id)
		}
		if a.plan.FirstResponder != "" {
			command := strings.TrimSuffix(a.plan.FirstResponder, ":")
			command = strings.ToUpper(command[:1]) + command[1:]
			withString(command, func(p uintptr) { call(gtk.Sym.WebkitWebViewExecuteEditingCommand, w.webview, p) })
		}
	}
}

var aboutResponse = ffi.NewCallback(func(a *ffi.Args) uintptr { call(gtk.Sym.GtkWidgetDestroy, a.Int[0]); return 0 })

func (w *linuxWindow) releaseMenuActions() {
	for _, id := range w.menuTokens {
		menuActions.Delete(id)
	}
	w.menuTokens = nil
}
