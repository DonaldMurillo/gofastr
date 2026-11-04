package win32

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/battery/desktop"
)

func TestRestoreMonitorFrameFallback(t *testing.T) {
	for _, test := range []struct {
		name                  string
		pixels                WorkArea
		want                  desktop.Frame
		monitorDPI, systemDPI uint32
	}{
		{
			name: "primary 100 percent", pixels: WorkArea{0, 0, 1920, 1040},
			monitorDPI: 96, systemDPI: 96, want: desktop.Frame{X: 660, Y: 320, Width: 600, Height: 400},
		},
		{
			name: "secondary 200 percent", pixels: WorkArea{1920, 0, 5760, 2080},
			monitorDPI: 192, systemDPI: 96, want: desktop.Frame{X: 3240, Y: 640, Width: 600, Height: 400},
		},
		{
			name: "negative origin 150 percent", pixels: WorkArea{-3840, -360, 0, 1800},
			monitorDPI: 144, systemDPI: 96, want: desktop.Frame{X: -2370, Y: 420, Width: 600, Height: 400},
		},
		{
			name: "secondary below system DPI", pixels: WorkArea{3840, 0, 5760, 1040},
			monitorDPI: 96, systemDPI: 192, want: desktop.Frame{X: 2250, Y: 160, Width: 600, Height: 400},
		},
		{
			name: "fractional negative origin", pixels: WorkArea{-2401, -301, -1, 1199},
			monitorDPI: 144, systemDPI: 120, want: desktop.Frame{X: -1321, Y: 119, Width: 600, Height: 400},
		},
		{
			name: "missing monitor DPI", pixels: WorkArea{0, 0, 2880, 1560},
			systemDPI: 144, want: desktop.Frame{X: 660, Y: 320, Width: 600, Height: 400},
		},
		{
			name: "missing all DPI", pixels: WorkArea{-1920, 0, 0, 1040},
			want: desktop.Frame{X: -1260, Y: 320, Width: 600, Height: 400},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			frame := desktop.Frame{X: 30000, Y: 30000, Width: 600, Height: 400}
			got := RestoreMonitorFrame(&frame, []MonitorWorkArea{{Bounds: test.pixels, DPI: test.monitorDPI}}, test.systemDPI, 1024, 768)
			if got == nil || *got != test.want {
				t.Fatalf("restored frame = %+v, want %+v", got, test.want)
			}
		})
	}
}

func TestMonitorWorkAreaRestoreMixedDPI(t *testing.T) {
	for _, test := range []struct {
		name         string
		systemDPI    uint32
		secondaryDPI uint32
		frame, want  desktop.Frame
		physical     WorkArea
	}{
		{
			name: "recover offscreen low DPI secondary", systemDPI: 192, secondaryDPI: 96,
			frame:    desktop.Frame{X: 3200, Y: 600, Width: 600, Height: 400},
			want:     desktop.Frame{X: 660, Y: 320, Width: 600, Height: 400},
			physical: WorkArea{6400, 1200, 7000, 1600},
		},
		{
			name: "keep visible high DPI secondary", systemDPI: 96, secondaryDPI: 192,
			frame:    desktop.Frame{X: 4500, Y: 1400, Width: 600, Height: 400},
			want:     desktop.Frame{X: 4500, Y: 1400, Width: 600, Height: 400},
			physical: WorkArea{4500, 1400, 5700, 2200},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			primary := WorkArea{0, 0, int32(1920 * test.systemDPI / 96), int32(1040 * test.systemDPI / 96)}
			secondary := WorkArea{primary.Right, 0, primary.Right + 3840, 2080}
			if test.secondaryDPI == 96 {
				secondary.Right = primary.Right + 1920
				secondary.Bottom = 1040
			}
			monitors := []MonitorWorkArea{{Bounds: primary, DPI: test.systemDPI}, {Bounds: secondary, DPI: test.secondaryDPI}}
			if pixels := testFramePhysical(test.frame, test.systemDPI, test.secondaryDPI); pixels != test.physical {
				t.Fatalf("saved physical rectangle = %+v, want %+v", pixels, test.physical)
			}
			got := RestoreMonitorFrame(&test.frame, monitors, test.systemDPI, 600, 400)
			if got == nil || *got != test.want {
				t.Fatalf("restored frame = %+v, want %+v", got, test.want)
			}
			dpi, area := test.systemDPI, primary
			if test.want == test.frame {
				dpi, area = test.secondaryDPI, secondary
			}
			pixels := testFramePhysical(*got, test.systemDPI, dpi)
			if overlapWidth := min(pixels.Right, area.Right) - max(pixels.Left, area.Left); overlapWidth < int32(100*dpi/96) {
				t.Fatalf("restored physical rectangle %+v has insufficient horizontal overlap with %+v", pixels, area)
			}
			if overlapHeight := min(pixels.Bottom, area.Bottom) - max(pixels.Top, area.Top); overlapHeight < int32(50*dpi/96) {
				t.Fatalf("restored physical rectangle %+v has insufficient vertical overlap with %+v", pixels, area)
			}
			roundTrip := desktop.Frame{
				X: int(monitorPixelsToPoints(int64(pixels.Left), test.systemDPI)), Y: int(monitorPixelsToPoints(int64(pixels.Top), test.systemDPI)),
				Width: int(monitorPixelsToPoints(int64(pixels.Right-pixels.Left), dpi)), Height: int(monitorPixelsToPoints(int64(pixels.Bottom-pixels.Top), dpi)),
			}
			if roundTrip != *got {
				t.Fatalf("physical round trip = %+v, want %+v", roundTrip, *got)
			}
		})
	}
}

func testFramePhysical(frame desktop.Frame, systemDPI, monitorDPI uint32) WorkArea {
	left := int32(int64(frame.X) * int64(systemDPI) / 96)
	top := int32(int64(frame.Y) * int64(systemDPI) / 96)
	return WorkArea{Left: left, Top: top, Right: left + int32(int64(frame.Width)*int64(monitorDPI)/96), Bottom: top + int32(int64(frame.Height)*int64(monitorDPI)/96)}
}

func TestRestoreMonitorFramePhysicalOverlapThreshold(t *testing.T) {
	monitors := []MonitorWorkArea{{Bounds: WorkArea{0, 0, 3840, 2080}, DPI: 192}}
	for _, test := range []struct {
		name  string
		frame desktop.Frame
		keep  bool
	}{
		{name: "wide enough", frame: desktop.Frame{X: 3640, Y: 100, Width: 600, Height: 400}, keep: true},
		{name: "too narrow", frame: desktop.Frame{X: 3641, Y: 100, Width: 600, Height: 400}},
		{name: "tall enough", frame: desktop.Frame{X: 100, Y: 1980, Width: 600, Height: 400}, keep: true},
		{name: "too short", frame: desktop.Frame{X: 100, Y: 1981, Width: 600, Height: 400}},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := RestoreMonitorFrame(&test.frame, monitors, 96, 600, 400)
			if got == nil || (*got == test.frame) != test.keep {
				t.Fatalf("restored frame = %+v, keep = %v", got, test.keep)
			}
		})
	}
}

func TestRestoreMonitorFrameMissingScreensAndInvalidSize(t *testing.T) {
	frame := desktop.Frame{Width: -1, Height: 400}
	monitor := MonitorWorkArea{Bounds: WorkArea{-1920, 0, 0, 1040}, DPI: 96}
	if got := RestoreMonitorFrame(nil, []MonitorWorkArea{monitor}, 96, 600, 400); got != nil {
		t.Fatalf("nil frame restored as %+v", got)
	}
	if got := RestoreMonitorFrame(&frame, []MonitorWorkArea{{}}, 96, 600, 400); got != nil {
		t.Fatalf("invalid work area restored as %+v", got)
	}
	got := RestoreMonitorFrame(&frame, []MonitorWorkArea{{}, monitor}, 96, 4000, 4000)
	want := desktop.Frame{X: -1920, Width: 1920, Height: 1040}
	if got == nil || *got != want {
		t.Fatalf("bounded fallback = %+v, want %+v", got, want)
	}
}
