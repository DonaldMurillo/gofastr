// Package b holds intwrap positives in code that never existed in the
// repo: different names, same shape. After the 2026-09-02 narrowing the
// rule fires only on any-boxed sources (type switches over any), so the
// param-based spellings here are the documented negatives.
package b

import "math"

// coerceSeq narrows a decoded JSON sequence number for storage.
func coerceSeq(v any) (int64, bool) {
	switch n := v.(type) {
	case uint64:
		return int64(n), true // want `conversion uint64 → int64 without a dominating bound check`
	case uint32:
		return int64(n), true // fits: silent by width
	}
	return 0, false
}

// narrowChannel squeezes a boxed channel id into a signed field.
func narrowChannel(v any) (int32, bool) {
	switch n := v.(type) {
	case uint:
		return int32(n), true // want `conversion uint → int32 without a dominating bound check`
	case uint16:
		return int32(n), true // fits: silent by width
	}
	return 0, false
}

// absPayload mirrors the kiln shape: negate a boxed int64.
func absPayload(args []any) (any, error) {
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
	return nil, errRange
}

// coerceSeqBounded is the fix posture.
func coerceSeqBounded(v any) (int64, bool) {
	switch n := v.(type) {
	case uint64:
		if n > math.MaxInt64 {
			return 0, false
		}
		return int64(n), true
	}
	return 0, false
}

// absPayloadFixed saturates at the magnitude limit.
func absPayloadFixed(args []any) (any, error) {
	switch v := args[0].(type) {
	case int64:
		if v == math.MinInt64 {
			return math.MaxInt64, nil
		}
		if v < 0 {
			return -v, nil
		}
		return v, nil
	}
	return nil, errRange
}

// eventSeq takes a plain uint64 parameter: semantically bounded by its
// caller (a counter), and silent here since the 2026-09-02 narrowing.
func eventSeq(seq uint64) int64 {
	return int64(seq)
}

// absDiff on plain ints: bounded by its callers (float exponents,
// deltas), silent here for the same reason.
func absDiff(delta int) int {
	if delta < 0 {
		return -delta
	}
	return delta
}

// constantSource is compile-time checked.
const floorSeq uint64 = 1 << 40

func constantSource() int64 {
	return int64(floorSeq)
}

// literalGuard uses a bound-sized literal guard.
func literalGuard(hash uint64) int32 {
	if hash > 9223372036854775807 {
		return 0
	}
	return int32(hash)
}
