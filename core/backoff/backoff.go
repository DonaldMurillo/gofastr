// Package backoff holds the retry-delay policies shared by the outbox, queue, webhook and log batteries.
package backoff

import (
	"math"
	"time"
)

// Exponential returns base*2^(attempts-1) for a base of zero or more, clamped to a positive max and never overflowing.
func Exponential(base, max time.Duration, attempts int) time.Duration {
	if base == 0 {
		return 0
	}
	if attempts < 1 {
		attempts = 1
	}
	exp := attempts - 1
	d := base
	for range exp {
		if max > 0 && d >= max {
			return max
		}
		if d < math.MinInt64/2 {
			return d
		}
		if d > math.MaxInt64/2 {
			// The next doubling would overflow; clamp to max (or this
			// value when uncapped).
			if max > 0 {
				return max
			}
			return d
		}
		d *= 2
	}
	if max > 0 && d > max {
		d = max
	}
	return d
}

// At returns table[attempts-1], reusing the last entry past the end; an empty table yields zero.
func At(table []time.Duration, attempts int) time.Duration {
	if len(table) == 0 {
		return 0
	}
	if attempts < 1 {
		attempts = 1
	}
	if attempts > len(table) {
		attempts = len(table)
	}
	return table[attempts-1]
}

// Jitter maps r in [0, 1) onto d×(1-fraction) to d×(1+fraction); r=0.5 returns d.
func Jitter(d time.Duration, fraction, r float64) time.Duration {
	if d <= 0 || !(fraction > 0) {
		return d
	}
	fraction = min(fraction, 1)
	if !(r >= 0) {
		r = 0
	}
	r = min(r, 1)
	f := float64(d) * (1 + fraction*(2*r-1))
	if f >= float64(1<<63-1) {
		return time.Duration(1<<63 - 1)
	}
	if f <= 0 {
		return 0
	}
	return time.Duration(f)
}
