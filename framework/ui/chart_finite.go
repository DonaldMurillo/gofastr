package ui

import "math"

// finiteRange returns the smallest and largest finite value in vs and
// how many finite values it holds. A NaN or Inf reading is one that
// never arrived: it widens nothing and counts for nothing, the rule
// Gauge, Heatmap and Scatter already follow through finite.
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
// when the span is empty. Two finite values can still be too far apart
// for a float64 (hi - lo overflows to Inf, and (v - lo) / span is then
// Inf / Inf = NaN), so a span that overflows is measured in halves.
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
