// Package c is a third intwrap positive with a different layout: an
// abs method over boxed values, and a conversion guarded only by a
// sibling branch (which guards nothing).
package c

import "math"

type delta struct {
	v int64
}

// absFromBoxed negates a boxed delta magnitude.
func (d delta) absFromBoxed(box any) (int64, error) {
	switch v := box.(type) {
	case int64:
		if v < 0 {
			return -v, nil // want `negation of v in an abs without a MinInt check`
		}
		return v, nil
	}
	return 0, errNoInt
}

// absFromBoxedFixed saturates.
func (d delta) absFromBoxedFixed(box any) (int64, error) {
	switch v := box.(type) {
	case int64:
		if v == math.MinInt64 {
			return math.MaxInt64, nil
		}
		if v < 0 {
			return -v, nil
		}
		return v, nil
	}
	return 0, errNoInt
}

// absoluteValue on a plain receiver field: bounded by its callers since
// the 2026-09-02 narrowing; kept as the documented negative.
func (d delta) absoluteValue() int64 {
	if d.v < 0 {
		return -d.v
	}
	return d.v
}

// clampDays converts a user-supplied day offset. The uint64 arm has the
// check; the uint arm does not — sibling arms guard nothing.
func clampDays(v any) (int64, bool) {
	switch n := v.(type) {
	case uint:
		return int64(n), true // want `conversion uint → int64 without a dominating bound check`
	case uint64:
		if n > math.MaxInt64 {
			return 0, false
		}
		return int64(n), true
	}
	return 0, false
}

// clampDaysFixed checks both arms.
func clampDaysFixed(v any) (int64, bool) {
	switch n := v.(type) {
	case uint:
		if n > math.MaxInt64 {
			return 0, false
		}
		return int64(n), true
	case uint64:
		if n > math.MaxInt64 {
			return 0, false
		}
		return int64(n), true
	}
	return 0, false
}

// negate flips a sign outside any abs context: not this rule's shape.
func negate(v int64) int64 {
	return -v
}
