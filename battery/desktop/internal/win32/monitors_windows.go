//go:build windows && amd64

package win32

import (
	"runtime"
	"syscall"
	"unsafe"
)

var shcore = syscall.NewLazyDLL("shcore.dll")

var monitorWorkAreaCallback = syscall.NewCallback(func(monitor, device uintptr, bounds, state unsafe.Pointer) uintptr {
	var info struct {
		Size    uint32
		Monitor WorkArea
		Work    WorkArea
		Flags   uint32
	}
	info.Size = uint32(unsafe.Sizeof(info))
	if result, _, _ := call(user32, "GetMonitorInfoW", monitor, uintptr(unsafe.Pointer(&info))); result != 0 {
		areas := (*[]WorkArea)(state)
		area := monitorWorkAreaPoints(info.Work, monitorDPI(monitor), SystemDPI())
		if info.Flags&1 != 0 {
			*areas = append([]WorkArea{area}, (*areas)...)
		} else {
			*areas = append(*areas, area)
		}
	}
	return 1
})

func monitorDPI(monitor uintptr) uint32 {
	const effectiveDPI = 0
	var horizontal, vertical uint32
	result, _, _ := call(shcore, "GetDpiForMonitor", monitor, effectiveDPI,
		uintptr(unsafe.Pointer(&horizontal)), uintptr(unsafe.Pointer(&vertical)))
	if result != 0 || horizontal == 0 {
		return SystemDPI()
	}
	return horizontal
}

// MonitorWorkAreas enumerates usable rectangles in screen points, primary first.
func MonitorWorkAreas() []WorkArea {
	var areas []WorkArea
	var pin runtime.Pinner
	pin.Pin(&areas)
	defer pin.Unpin()
	result, _, _ := call(user32, "EnumDisplayMonitors", 0, 0, monitorWorkAreaCallback, uintptr(unsafe.Pointer(&areas)))
	if result == 0 {
		return nil
	}
	return areas
}
