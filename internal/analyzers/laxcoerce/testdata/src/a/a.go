// Package a holds the laxcoerce fixture reduced from the real bug site:
// battery/log mcp.go timeParam as it was before fix 4b7a25d2 (probe
// TestFilterTimestampTypeConfusionErr), plus the fixed spelling and the
// neighboring postures the rule must stay quiet on.
package a

import (
	"fmt"
	"time"
)

// timeParam parses an RFC3339 (with or without sub-second precision)
// timestamp argument. Pre-fix: a numeric `since_ts` landed in the !ok
// branch, which read as "no filter supplied" and returned the
// unfiltered window with a nil error.
func timeParam(params map[string]any, name string) (time.Time, bool, error) {
	s, ok := params[name].(string) // want `type assertion on params treated as absence`
	if !ok || s == "" {
		return time.Time{}, false, nil
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		t, err = time.Parse(time.RFC3339, s)
	}
	if err != nil {
		return time.Time{}, false, fmt.Errorf("%s: %w", name, err)
	}
	return t, true, nil
}

// timeParamFixed is the fix posture: presence and type are separated,
// and a present value of the wrong type is an error, not an absence.
func timeParamFixed(params map[string]any, name string) (time.Time, bool, error) {
	v, present := params[name]
	if !present {
		return time.Time{}, false, nil
	}
	s, ok := v.(string)
	if !ok {
		return time.Time{}, false, fmt.Errorf("%s: want RFC3339 timestamp string, got %T", name, v)
	}
	if s == "" {
		return time.Time{}, false, nil
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}, false, fmt.Errorf("%s: %w", name, err)
	}
	return t, true, nil
}

// errorSurfacingBranch: the !ok branch returns an error, so the wrong
// type is not swallowed even without a presence split.
func errorSurfacingBranch(opts map[string]any) (int, error) {
	n, ok := opts["retries"].(int)
	if !ok {
		return 0, fmt.Errorf("retries: want int, got %T", opts["retries"])
	}
	return n, nil
}

// typedMap: the element type is int, not any — a wrong type cannot be
// stored, so the comma-ok index genuinely means absent. (Also not a
// type assertion at all; kept as a neighboring posture.)
func typedMap(scores map[string]int) int {
	v, ok := scores["level"]
	if !ok {
		return 0
	}
	return v * 2
}

// assertOnNonMap: the asserted value is not a map entry; whatever the
// branch does with !ok is outside this rule's shape.
func assertOnNonMap(v any) (string, error) {
	s, ok := v.(string)
	if !ok {
		return "", nil
	}
	return s, nil
}
