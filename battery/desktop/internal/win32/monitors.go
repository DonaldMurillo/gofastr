package win32

import (
	"math"

	"github.com/DonaldMurillo/gofastr/battery/desktop"
)

// WorkArea is a monitor rectangle in physical pixels.
type WorkArea struct{ Left, Top, Right, Bottom int32 }

// MonitorWorkArea pairs a physical work rectangle with its effective DPI.
type MonitorWorkArea struct {
	Bounds WorkArea
	DPI    uint32
}

// RestoreMonitorFrame checks visibility in physical pixels and returns saved-frame
// coordinates: system-DPI origins and monitor-DPI dimensions, primary first.
func RestoreMonitorFrame(frame *desktop.Frame, monitors []MonitorWorkArea, systemDPI uint32, defaultWidth, defaultHeight int) *desktop.Frame {
	if frame == nil {
		return nil
	}
	systemDPI = effectiveMonitorDPI(systemDPI, 96)
	if frame.Width > 0 && frame.Height > 0 && frame.Width <= 32767 && frame.Height <= 32767 {
		defaultWidth, defaultHeight = frame.Width, frame.Height
		left := math.Round(float64(frame.X) * float64(systemDPI) / 96)
		top := math.Round(float64(frame.Y) * float64(systemDPI) / 96)
		for _, monitor := range monitors {
			area := monitor.Bounds
			if area.Right <= area.Left || area.Bottom <= area.Top {
				continue
			}
			scale := float64(effectiveMonitorDPI(monitor.DPI, systemDPI)) / 96
			right := left + math.Round(float64(frame.Width)*scale)
			bottom := top + math.Round(float64(frame.Height)*scale)
			overlapWidth := math.Min(right, float64(area.Right)) - math.Max(left, float64(area.Left))
			overlapHeight := math.Min(bottom, float64(area.Bottom)) - math.Max(top, float64(area.Top))
			if overlapWidth >= math.Round(100*scale) && overlapHeight >= math.Round(50*scale) {
				copy := *frame
				return &copy
			}
		}
	}
	for _, monitor := range monitors {
		area := monitor.Bounds
		if area.Right <= area.Left || area.Bottom <= area.Top {
			continue
		}
		dpi := effectiveMonitorDPI(monitor.DPI, systemDPI)
		areaWidth := int64(area.Right) - int64(area.Left)
		areaHeight := int64(area.Bottom) - int64(area.Top)
		width := min(max(defaultWidth, 1), int(areaWidth*96/int64(dpi)), 32767)
		height := min(max(defaultHeight, 1), int(areaHeight*96/int64(dpi)), 32767)
		widthPixels := (int64(width)*int64(dpi) + 48) / 96
		heightPixels := (int64(height)*int64(dpi) + 48) / 96
		return &desktop.Frame{
			X:     int(monitorPixelsToPoints(int64(area.Left)+(areaWidth-widthPixels)/2, systemDPI)),
			Y:     int(monitorPixelsToPoints(int64(area.Top)+(areaHeight-heightPixels)/2, systemDPI)),
			Width: width, Height: height,
		}
	}
	return nil
}

func effectiveMonitorDPI(dpi, fallback uint32) uint32 {
	if dpi == 0 {
		return fallback
	}
	return dpi
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
