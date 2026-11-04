//go:build windows && amd64

package win32

import (
	"runtime"
	"syscall"
	"unsafe"
)

// WorkArea is a monitor's usable rectangle in virtual-desktop pixels.
type WorkArea struct{ Left, Top, Right, Bottom int32 }

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
		if info.Flags&1 != 0 {
			*areas = append([]WorkArea{info.Work}, (*areas)...)
		} else {
			*areas = append(*areas, info.Work)
		}
	}
	return 1
})

// MonitorWorkAreas enumerates usable rectangles, with the primary first.
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
