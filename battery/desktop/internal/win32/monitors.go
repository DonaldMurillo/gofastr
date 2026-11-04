package win32

// WorkArea is a monitor rectangle; MonitorWorkAreas returns it in screen points.
type WorkArea struct{ Left, Top, Right, Bottom int32 }

func monitorWorkAreaPoints(area WorkArea, monitorDPI, systemDPI uint32) WorkArea {
	if monitorDPI == 0 {
		monitorDPI = systemDPI
	}
	left := monitorPixelsToPoints(int64(area.Left), systemDPI)
	top := monitorPixelsToPoints(int64(area.Top), systemDPI)
	return WorkArea{
		Left: left, Top: top,
		Right:  left + monitorPixelsToPoints(int64(area.Right)-int64(area.Left), monitorDPI),
		Bottom: top + monitorPixelsToPoints(int64(area.Bottom)-int64(area.Top), monitorDPI),
	}
}

func monitorPixelsToPoints(pixels int64, dpi uint32) int32 {
	if dpi == 0 {
		dpi = 96
	}
	value := pixels * 96
	if value < 0 {
		value -= int64(dpi) / 2
	} else {
		value += int64(dpi) / 2
	}
	return int32(value / int64(dpi))
}
