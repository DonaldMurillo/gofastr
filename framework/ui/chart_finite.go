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
