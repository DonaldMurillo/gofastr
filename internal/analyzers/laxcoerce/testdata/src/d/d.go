// Package d holds the laxcoerce spellings the 2026-09-02 adversarial
// review showed were missing or mis-scoped: the fall-through after a
// bare `if ok`, a zero-value assignment in the !ok branch, switch-case
// guards, closures judged under their own error-result flag, and
// presence separated on one key of a map silencing an assert on
// another.
package d

import "errors"

// fallThrough: with no else, the statements after `if ok` ARE the
// not-ok path; a wrong-typed value lands in the nil-error return and
// reads as "not supplied".
func fallThrough(m map[string]any, k string) (string, bool, error) {
	s, ok := m[k].(string) // want `type assertion on m treated as absence`
	if ok {
		return s, true, nil
	}
	return "", false, nil
}

// zeroAssign: the !ok branch rebinds the asserted variable to its zero
// value and falls through — in a function with an error result, the
// wrong type is erased before the nil error goes out.
func zeroAssign(m map[string]any, k string) (string, error) {
	s, ok := m[k].(string) // want `type assertion on m treated as absence`
	if !ok {
		s = ""
	}
	return process(s), nil
}

func process(s string) string { return s + "!" }

// switchCase: `switch { case !ok: }` is the same not-ok branch.
func switchCase(m map[string]any, k string) (string, bool, error) {
	s, ok := m[k].(string) // want `type assertion on m treated as absence`
	switch {
	case !ok:
		return "", false, nil
	}
	return s, true, nil
}

// switchTagFalse: `switch ok { case false: }` likewise.
func switchTagFalse(m map[string]any, k string) (string, bool, error) {
	s, ok := m[k].(string) // want `type assertion on m treated as absence`
	switch ok {
	case false:
		return "", false, nil
	}
	return s, true, nil
}

// outerNoChannel: the closure has no error result of its own, and the
// outer function's error flag must not be applied to it (documented
// silence: zero returns in functions with no error result).
func outerNoChannel(m map[string]any, k string) (int, error) {
	apply := func(k string) int {
		n, ok := m[k].(int)
		if !ok {
			return 0 // closure has no error result: silent
		}
		return n
	}
	return apply(k), nil
}

// closureOwnError: the closure has its own error result and its own
// lax branch — judged by the closure's own pass, exactly once.
func closureOwnError(m map[string]any, k string) (int, error) {
	apply := func(k string) (int, error) {
		n, ok := m[k].(int) // want `type assertion on m treated as absence`
		if !ok {
			return 0, nil
		}
		return n, nil
	}
	return apply(k)
}

// presentOtherKey: presence was separated for "fmt", never for
// "region" — the presence silence is per key, not per map, when the
// key is a literal.
func presentOtherKey(m map[string]any) (string, error) {
	_, present := m["fmt"]
	if !present {
		return "", nil
	}
	region, ok := m["region"].(string) // want `type assertion on m treated as absence`
	if !ok {
		return "", nil // wrong-typed region reads as "no filter"
	}
	return region, nil
}

// presentSameKey: the assert's own key was presence-checked, so
// presence and type are genuinely separated for it: silent.
func presentSameKey(m map[string]any) (string, error) {
	_, present := m["region"]
	if !present {
		return "", nil
	}
	region, ok := m["region"].(string)
	if !ok {
		return "", nil // wrong type surfaces as absence-by-design here
	}
	return region, nil
}

// ---- silent counterparts of the not-ok forms ----------------------------

// fallThroughFixed: the same fall-through shape, but the not-ok path
// returns an error — the wrong type is surfaced, not swallowed.
func fallThroughFixed(m map[string]any, k string) (string, bool, error) {
	s, ok := m[k].(string)
	if ok {
		return s, true, nil
	}
	return "", false, errors.New("entry has wrong type")
}

// zeroAssignFixed: the zero-rebind still ends in an error return.
func zeroAssignFixed(m map[string]any, k string) (string, error) {
	s, ok := m[k].(string)
	if !ok {
		s = ""
		return process(s), errors.New("entry has wrong type")
	}
	return process(s), nil
}

// switchCaseFixed: `case !ok:` returning an error.
func switchCaseFixed(m map[string]any, k string) (string, bool, error) {
	s, ok := m[k].(string)
	switch {
	case !ok:
		return "", false, errors.New("entry has wrong type")
	}
	return s, true, nil
}

// switchTagFalseFixed: `case false:` returning an error.
func switchTagFalseFixed(m map[string]any, k string) (string, bool, error) {
	s, ok := m[k].(string)
	switch ok {
	case false:
		return "", false, errors.New("entry has wrong type")
	}
	return s, true, nil
}
