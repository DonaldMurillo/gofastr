package win32

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/battery/desktop"
	"github.com/DonaldMurillo/gofastr/battery/desktop/internal/placement"
)

func TestMonitorWorkAreaPointsUsesMonitorDPI(t *testing.T) {
	for _, test := range []struct {
		name                  string
		pixels, want          WorkArea
		monitorDPI, systemDPI uint32
	}{
		{
			name: "primary 100 percent", pixels: WorkArea{0, 0, 1920, 1040},
			monitorDPI: 96, systemDPI: 96, want: WorkArea{0, 0, 1920, 1040},
		},
		{
			name: "secondary 200 percent", pixels: WorkArea{1920, 0, 5760, 2080},
			monitorDPI: 192, systemDPI: 96, want: WorkArea{1920, 0, 3840, 1040},
		},
		{
			name: "negative origin 150 percent", pixels: WorkArea{-3840, -360, 0, 1800},
			monitorDPI: 144, systemDPI: 96, want: WorkArea{-3840, -360, -1280, 1080},
		},
		{
			name: "secondary below system DPI", pixels: WorkArea{3840, 0, 5760, 1040},
			monitorDPI: 96, systemDPI: 192, want: WorkArea{1920, 0, 3840, 1040},
		},
		{
			name: "fractional negative origin", pixels: WorkArea{-2401, -301, -1, 1199},
			monitorDPI: 144, systemDPI: 120, want: WorkArea{-1921, -241, -321, 759},
		},
		{
			name: "missing monitor DPI", pixels: WorkArea{0, 0, 2880, 1560},
			systemDPI: 144, want: WorkArea{0, 0, 1920, 1040},
		},
		{
			name: "missing all DPI", pixels: WorkArea{-1920, 0, 0, 1040},
			want: WorkArea{-1920, 0, 0, 1040},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := monitorWorkAreaPoints(test.pixels, test.monitorDPI, test.systemDPI); got != test.want {
				t.Fatalf("monitor points = %+v, want %+v", got, test.want)
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
	}{
		{
			name: "keep visible low DPI secondary", systemDPI: 192, secondaryDPI: 96,
			frame: desktop.Frame{X: 3200, Y: 600, Width: 600, Height: 400},
			want:  desktop.Frame{X: 3200, Y: 600, Width: 600, Height: 400},
		},
		{
			name: "reject phantom high DPI secondary area", systemDPI: 96, secondaryDPI: 192,
			frame: desktop.Frame{X: 4500, Y: 1400, Width: 600, Height: 400},
			want:  desktop.Frame{X: 660, Y: 320, Width: 600, Height: 400},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			primary := WorkArea{0, 0, int32(1920 * test.systemDPI / 96), int32(1040 * test.systemDPI / 96)}
			secondary := WorkArea{primary.Right, 0, primary.Right + 3840, 2080}
			if test.secondaryDPI == 96 {
				secondary.Right = primary.Right + 1920
				secondary.Bottom = 1040
			}
			var screens []desktop.Frame
			for index, pixels := range []WorkArea{primary, secondary} {
				dpi := test.systemDPI
				if index == 1 {
					dpi = test.secondaryDPI
				}
				area := monitorWorkAreaPoints(pixels, dpi, test.systemDPI)
				screens = append(screens, desktop.Frame{
					X: int(area.Left), Y: int(area.Top), Width: int(area.Right - area.Left), Height: int(area.Bottom - area.Top),
				})
			}
			if got := placement.Restore(&test.frame, screens, 600, 400); got == nil || *got != test.want {
				t.Fatalf("restored frame = %+v, want %+v", got, test.want)
			}
		})
	}
}
