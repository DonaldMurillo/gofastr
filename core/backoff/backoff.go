// Package backoff holds the retry-delay policies shared by the outbox
// relay, the queue and webhook batteries, and the log webhook sink. It is
// dependency-free and low-level so the framework tree and the batteries
// (which sit above it but not above each other) reuse one implementation
// instead of maintaining copies.
package backoff

import "time"

// Exponential returns the delay before the next retry for the attempt
// that just failed (attempts >= 1): base*2^(attempts-1), clamped to max
// when max is positive. attempts <= 1 yields base. A zero base yields
// zero. Callers gate on base > 0 before applying backoff. Doubling
// stops at the largest value whose next doubling could overflow int64,
// so any attempt count is safe.
func Exponential(base, max time.Duration, attempts int) time.Duration {
	exp := attempts - 1
	if exp < 0 {
		exp = 0
	}
	d := base
	for range exp {
		if max > 0 && d >= max {
			return max
		}
		if d > (1<<62)/2 {
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

// At returns the wait after the given attempt from a positional
// schedule: table[attempts-1], reusing the final entry once attempts
// runs past the table. attempts < 1 reads as the first attempt; an
// empty table yields zero.
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
