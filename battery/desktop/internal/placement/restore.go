package placement

import (
	"math"

	"github.com/DonaldMurillo/gofastr/battery/desktop"
)

// Restore chooses the initial remembered frame in top-left screen points.
func Restore(frame *desktop.Frame, screens []desktop.Frame, defaultWidth, defaultHeight int) *desktop.Frame {
	if frame == nil {
		return nil
	}
	if frame.Width > 0 && frame.Height > 0 && frame.Width <= 32767 && frame.Height <= 32767 {
		for _, screen := range screens {
			if screen.Width <= 0 || screen.Height <= 0 {
				continue
			}
			overlapWidth := math.Min(float64(frame.X)+float64(frame.Width), float64(screen.X)+float64(screen.Width)) - math.Max(float64(frame.X), float64(screen.X))
			overlapHeight := math.Min(float64(frame.Y)+float64(frame.Height), float64(screen.Y)+float64(screen.Height)) - math.Max(float64(frame.Y), float64(screen.Y))
			if overlapWidth >= 100 && overlapHeight >= 50 {
				copy := *frame
				return &copy
			}
		}
	}
	for _, screen := range screens {
		if screen.Width <= 0 || screen.Height <= 0 {
			continue
		}
		width := min(max(defaultWidth, 1), screen.Width, 32767)
		height := min(max(defaultHeight, 1), screen.Height, 32767)
		return &desktop.Frame{
			X: screen.X + (screen.Width-width)/2, Y: screen.Y + (screen.Height-height)/2,
			Width: width, Height: height,
		}
	}
	return nil
}
