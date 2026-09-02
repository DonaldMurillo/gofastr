// Package a holds the intwrap fixture reduced from the real bug sites:
// core/schema validate.go toInt64's unsigned arms as they were before
// fix b79942f7 (probe TestIntUintWrapBypassesMaxBound) and
// kiln/expr env.go builtinAbs before fix f06f4412 (probe
// TestAbsNeverReturnsNegative).
package a

import "math"

// toInt64, reduced: the uint64 arm has the overflow check, the uint arm
// — the same width on 64-bit — had none, so uint(MaxUint64) read as -1
// and slipped past every Max-only validator.
func toInt64(v any) (int64, bool) {
	switch n := v.(type) {
	case int:
		return int64(n), true
	case int8, int16, int32:
		return 0, true // reduced: signed arms cannot wrap negative upward
	case uint:
		return int64(n), true // want `conversion uint → int64 without a dominating bound check`
	case uint8, uint16:
		return 0, true // reduced: they fit
	case uint32:
		return int64(n), true // fits: silent by width
	case uint64:
		if n > math.MaxInt64 {
			return 0, false
		}
		return int64(n), true
	}
	return 0, false
}

// toInt64Fixed carries the fix into the uint arm: the same overflow
// check its uint64 sibling already had.
func toInt64Fixed(v any) (int64, bool) {
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

// builtinAbs, reduced: |MinInt64| does not fit, -v wrapped back to
// MinInt64 and abs returned a negative. The float arm cannot wrap.
func builtinAbs(args []any) (any, error) {
	switch v := args[0].(type) {
	case int64:
		if v < 0 {
			return -v, nil // want `negation of v in an abs without a MinInt check`
		}
		return v, nil
	case float64:
		if v < 0 {
			return -v, nil // float negation: silent
		}
		return v, nil
	}
	return nil, errAbsType
}

// builtinAbsFixed saturates at the magnitude limit.
func builtinAbsFixed(args []any) (any, error) {
	switch v := args[0].(type) {
	case int64:
		if v == math.MinInt64 {
			return int64(math.MaxInt64), nil
		}
		if v < 0 {
			return -v, nil
		}
		return v, nil
	}
	return nil, errAbsType
}
