// Package h holds the 2026-11-03 round fixtures: shape E with the
// operands flipped (1+limit), a 64-bit bound guarding a 32-bit target,
// a guard gone stale through a reassignment, and the same bodies
// spelled as package-level function literals.
package h

import (
	"io"
	"math"
	"net/http"
	"time"
)

// flippedSentinel: 1+limit is limit+1 spelled the other way around.
func flippedSentinel(body io.Reader, limit int64) []byte {
	b, _ := io.ReadAll(io.LimitReader(body, 1+limit)) // want `1 \+ limit to io.LimitReader with no upper bound on limit`
	return b
}

// keptSentinel: the guard spelling, either order.
func keptSentinel(body io.Reader, limit int64) []byte {
	if limit >= math.MaxInt64 {
		return nil
	}
	b, _ := io.ReadAll(io.LimitReader(body, limit+1))
	_ = b
	b2, _ := io.ReadAll(io.LimitReader(body, 1+limit))
	return b2
}

// wideGuardNarrowTarget: MaxInt64 does not bound an int32 conversion —
// every u in (2^31, 2^63] still wraps.
func wideGuardNarrowTarget(box any) int32 {
	if u, ok := box.(uint64); ok {
		if u > math.MaxInt64 {
			return 0
		}
		return int32(u) // want `conversion uint64 → int32 without a dominating bound check`
	}
	return 0
}

// wideLiteralNarrowTarget: a 64-bit-sized literal guard, same gap.
func wideLiteralNarrowTarget(box any) int32 {
	if u, ok := box.(uint64); ok {
		if u > 5000000000 {
			return 0
		}
		return int32(u) // want `conversion uint64 → int32 without a dominating bound check`
	}
	return 0
}

// narrowGuardNarrowTarget: MaxInt32 bounds the int32 target.
func narrowGuardNarrowTarget(box any) int32 {
	if u, ok := box.(uint64); ok {
		if u > math.MaxInt32 {
			return 0
		}
		return int32(u)
	}
	return 0
}

// wideGuardWideTarget: MaxInt64 bounds the int64 target as before.
func wideGuardWideTarget(box any) int64 {
	if u, ok := box.(uint64); ok {
		if u > math.MaxInt64 {
			return 0
		}
		return int64(u)
	}
	return 0
}

// staleGuard: the guard is invalidated by the reassignment of u before
// the conversion.
func staleGuard(box any) int64 {
	if u, ok := box.(uint64); ok {
		if u > math.MaxInt64 {
			return 0
		}
		u = box.(uint64)
		return int64(u) // want `conversion uint64 → int64 without a dominating bound check`
	}
	return 0
}

// freshGuard: the rebind happens BEFORE the guard, so the guard speaks
// for the value that reaches the conversion.
func freshGuard(box any) int64 {
	if u, ok := box.(uint64); ok {
		u = box.(uint64)
		if u > math.MaxInt64 {
			return 0
		}
		return int64(u)
	}
	return 0
}

// litSentinel: shape E in a package-level function literal.
var litSentinel = func(r *http.Request, limit int64) ([]byte, error) {
	return io.ReadAll(io.LimitReader(r.Body, limit+1)) // want `limit \+ 1 to io.LimitReader with no upper bound on limit`
}

// litDuration: shape D in a package-level function literal.
var litDuration = func(seconds int64) time.Duration {
	return time.Duration(seconds) * time.Second // want `time.Duration\(seconds\) \* time.Second with no upper bound on seconds`
}

// litConv: shape A in a package-level function literal.
var litConv = func(box any) int64 {
	if u, ok := box.(uint64); ok {
		return int64(u) // want `conversion uint64 → int64 without a dominating bound check`
	}
	return 0
}
