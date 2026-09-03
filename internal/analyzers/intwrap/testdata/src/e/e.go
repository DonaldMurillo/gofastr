// Package e holds the intwrap spellings review 6 added: a preceding
// bound check dominates only when its failure branch leaves the
// subject inside the conversion-safe range — the branch that CONTINUES
// is the one the conversion sees, and it must actually leave (return /
// panic), not fall through.
package e

import "math"

// safeRangeReturn returns on the in-range values: only out-of-range
// values reach the conversion, exactly the ones that wrap.
func safeRangeReturn(box any) (int64, bool) {
	if u, ok := box.(uint64); ok {
		if u <= math.MaxInt64 {
			return 0, false
		}
		return int64(u), true // want `conversion uint64 → int64 without a dominating bound check`
	}
	return 0, false
}

// unsafeRangeReturn refuses the wrapping values: the continuing path
// stays in range (the fix posture).
func unsafeRangeReturn(box any) (int64, bool) {
	if u, ok := box.(uint64); ok {
		if u > math.MaxInt64 {
			return 0, false
		}
		return int64(u), true
	}
	return 0, false
}

// nonDivergingBound flags the out-of-range values and falls through:
// they still reach the conversion.
func nonDivergingBound(box any, flagged *bool) int64 {
	if u, ok := box.(uint64); ok {
		if u > math.MaxInt64 {
			*flagged = true
		}
		return int64(u) // want `conversion uint64 → int64 without a dominating bound check`
	}
	return 0
}

// encloseBound: the condition held on the branch holding the
// conversion proves the value in range.
func encloseBound(box any) (int64, bool) {
	if u, ok := box.(uint64); ok {
		if u <= math.MaxInt64 {
			return int64(u), true
		}
		return 0, false
	}
	return 0, false
}

// throwBound: a throwing failure branch is a divergence spelling.
func throwBound(box any) (int64, bool) {
	if u, ok := box.(uint64); ok {
		if u > math.MaxInt64 {
			panic("uint64 overflows int64")
		}
		return int64(u), true
	}
	return 0, false
}
