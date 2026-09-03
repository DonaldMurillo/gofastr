// Package d holds the intwrap spellings the 2026-09-02 adversarial
// review showed were missing or mis-scoped: comma-ok assertions from
// an any box (as unbounded as a type-switch case variable), and
// bounds that sit inside a nested conditional body of an earlier
// statement.
package d

import "math"

// coerceComma: a comma-ok assertion from an any is the same unbounded
// JSON-box value the type-switch arm is.
func coerceComma(box any) (int64, bool) {
	if u, ok := box.(uint64); ok {
		return int64(u), true // want `conversion uint64 → int64 without a dominating bound check`
	}
	return 0, false
}

// coerceCommaFixed bounds before converting.
func coerceCommaFixed(box any) (int64, bool) {
	if u, ok := box.(uint64); ok && u <= math.MaxInt64 {
		return int64(u), true
	}
	return 0, false
}

// absComma negates a comma-ok asserted int64: MinInt64 wraps.
func absComma(box any) int64 {
	if v, ok := box.(int64); ok && v < 0 {
		return -v // want `negation of v in an abs without a MinInt check`
	}
	if v, ok := box.(int64); ok {
		return v
	}
	return 0
}

// absCommaFixed saturates at the magnitude limit.
func absCommaFixed(box any) int64 {
	if v, ok := box.(int64); ok {
		if v == math.MinInt64 {
			return math.MaxInt64
		}
		if v < 0 {
			return -v
		}
		return v
	}
	return 0
}

// nestedGuard: the bound check runs only when strict is set — the
// conversion still wraps with strict=false.
func nestedGuard(box any, strict bool) (int64, bool) {
	switch n := box.(type) {
	case uint64:
		if strict {
			if n > math.MaxInt64 {
				return 0, false
			}
		}
		return int64(n), true // want `conversion uint64 → int64 without a dominating bound check`
	}
	return 0, false
}

// nestedGuardFixed hoists the bound out of the flag-gated branch: now
// it dominates every path to the conversion.
func nestedGuardFixed(box any, strict bool) (int64, bool) {
	switch n := box.(type) {
	case uint64:
		if n > math.MaxInt64 {
			return 0, false
		}
		if strict {
			return int64(n), true
		}
		return 0, true
	}
	return 0, false
}

// wrongDirectionBound: the then-branch holds exactly the values that
// WRAP — u > math.MaxInt64 selects them — so a comparison that does
// not establish the safe range dominates nothing. Only u < / u <= /
// u == the bound (mirrored when the subject is on the right) count in
// a branch the conversion sits in.
func wrongDirectionBound(box any) int64 {
	if u, ok := box.(uint64); ok && u > math.MaxInt64 {
		return int64(u) // want `conversion uint64 → int64 without a dominating bound check`
	}
	return 0
}

// postBoundBreaks: the bound comparison sits in the loop's Post — it
// runs only after a normal iteration, and the immediate break skips
// it, so the conversion after the loop sees the unbounded value.
func postBoundBreaks(box any, flags map[bool]uint64) int64 {
	u, ok := box.(uint64)
	if !ok {
		return 0
	}
	for i := 0; i < 3; i, u = i+1, flags[u <= math.MaxInt64] {
		if i == 0 {
			break
		}
	}
	return int64(u) // want `conversion uint64 → int64 without a dominating bound check`
}

// conjDiverging: the failure of `u > math.MaxInt64 && strict` can be
// strict alone — ¬(A && B) is ¬A ∨ ¬B, and which operand failed is
// unknown — so BOTH operands' failure must bound the subject for the
// diverging guard to count. strict's failure bounds nothing, and the
// out-of-range u still reaches the conversion.
func conjDiverging(box any, strict bool) int64 {
	if u, ok := box.(uint64); ok {
		if u > math.MaxInt64 && strict {
			return 0
		}
		return int64(u) // want `conversion uint64 → int64 without a dominating bound check`
	}
	return 0
}

// conjHeld: the conjoined enclosing condition held means every operand
// held, so any one operand establishing the safe range suffices.
func conjHeld(box any, strict bool) int64 {
	if u, ok := box.(uint64); ok {
		if u <= math.MaxInt64 && strict {
			return int64(u)
		}
		return 0
	}
	return 0
}
