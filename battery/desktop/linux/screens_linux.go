//go:build linux && (amd64 || arm64)

package linux

import (
	"runtime"
	"unsafe"

	"github.com/DonaldMurillo/gofastr/battery/desktop"
	"github.com/DonaldMurillo/gofastr/battery/desktop/internal/gtk"
)

func screenWorkAreas(window uintptr) []desktop.Frame {
	screen := call(gtk.Sym.GtkWidgetGetScreen, window)
	if screen == 0 {
		return nil
	}
	count := int(call(gtk.Sym.GdkScreenGetNMonitors, screen))
	primary := int(call(gtk.Sym.GdkScreenGetPrimaryMonitor, screen))
	var areas []desktop.Frame
	var bounds [4]int32
	var pin runtime.Pinner
	pin.Pin(&bounds)
	defer pin.Unpin()
	for index := range count {
		call(gtk.Sym.GdkScreenGetMonitorWorkarea, screen, uintptr(index), uintptr(unsafe.Pointer(&bounds)))
		area := desktop.Frame{X: int(bounds[0]), Y: int(bounds[1]), Width: int(bounds[2]), Height: int(bounds[3])}
		if index == primary {
			areas = append([]desktop.Frame{area}, areas...)
		} else {
			areas = append(areas, area)
		}
	}
	return areas
}
