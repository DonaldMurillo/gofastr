package ui

import "math"

// finiteRange returns the smallest and largest finite value in vs and
// how many finite values it holds.
func finiteRange(vs []float64) (lo, hi float64, n int) {
	for _, v := range vs {
		if !finite(v) {
			continue
		}
		if n == 0 || v < lo {
			lo = v
		}
		if n == 0 || v > hi {
			hi = v
		}
		n++
	}
	return lo, hi, n
}

// chartFrac places a finite v on [lo, hi] as a fraction of the span, 0
// when the span is empty.
func chartFrac(v, lo, hi float64) float64 {
	span := hi - lo
	if span == 0 {
		return 0
	}
	if math.IsInf(span, 0) {
		return (v/2 - lo/2) / (hi/2 - lo/2)
	}
	return (v - lo) / span
}

// chartPoints projects the finite values onto a plot of width plotW as
// "x,y" pairs and reports the first and last finite X. slots is the number
// of intervals on the X axis, so series on the same axis use the same
// spacing. Y runs from topPad + plotH, scaled by chartFrac.
func chartPoints(values []float64, slots int, plotW, topPad, plotH, lo, hi float64) (pts []string, firstX, lastX float64) {
	pts = make([]string, 0, len(values))
	firstX, lastX = -1.0, 0.0
	for i, v := range values {
		if !finite(v) {
			continue
		}
		x := float64(i) * plotW / float64(slots)
		y := topPad + plotH - chartFrac(v, lo, hi)*plotH
		pts = append(pts, ftoa(x)+","+ftoa(y))
		if firstX < 0 {
			firstX = x
		}
		lastX = x
	}
	return pts, firstX, lastX
}
