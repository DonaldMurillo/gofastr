package expr

import (
	"fmt"
	"math"
	"strings"
	"time"
)

// DefaultEnv returns the standard built-in function set.
func DefaultEnv() *Env {
	e := &Env{Functions: map[string]Func{}}
	e.Register("len", builtinLen)
	e.Register("lower", builtinLower)
	e.Register("upper", builtinUpper)
	e.Register("contains", builtinContains)
	e.Register("starts_with", stringPairBuiltin("starts_with", strings.HasPrefix))
	e.Register("ends_with", stringPairBuiltin("ends_with", strings.HasSuffix))
	e.Register("abs", builtinAbs)
	e.Register("min", builtinMin)
	e.Register("max", builtinMax)
	e.Register("now", builtinNow)
	return e
}

func builtinLen(args []any) (any, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("len: %w", ErrArity)
	}
	switch v := args[0].(type) {
	case string:
		return int64(len(v)), nil
	case []any:
		return int64(len(v)), nil
	case map[string]any:
		return int64(len(v)), nil
	}
	return nil, fmt.Errorf("len: cannot take length of %T: %w", args[0], ErrType)
}

func builtinLower(args []any) (any, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("lower: %w", ErrArity)
	}
	s, ok := args[0].(string)
	if !ok {
		return nil, fmt.Errorf("lower: %w", ErrType)
	}
	return strings.ToLower(s), nil
}

func builtinUpper(args []any) (any, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("upper: %w", ErrArity)
	}
	s, ok := args[0].(string)
	if !ok {
		return nil, fmt.Errorf("upper: %w", ErrType)
	}
	return strings.ToUpper(s), nil
}

func builtinContains(args []any) (any, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("contains: %w", ErrArity)
	}
	switch hay := args[0].(type) {
	case string:
		needle, ok := args[1].(string)
		if !ok {
			return nil, fmt.Errorf("contains: %w", ErrType)
		}
		return strings.Contains(hay, needle), nil
	case []any:
		for _, item := range hay {
			if equals(item, args[1]) {
				return true, nil
			}
		}
		return false, nil
	}
	return nil, fmt.Errorf("contains: %w", ErrType)
}

// stringPairBuiltin builds a two-string-argument builtin that applies
// pred after arity and type checks, prefixing errors with name. It
// replaces the former builtinStartsWith and builtinEndsWith, whose
// bodies were identical except for the name prefix and the strings
// predicate.
func stringPairBuiltin(name string, pred func(a, b string) bool) func(args []any) (any, error) {
	return func(args []any) (any, error) {
		if len(args) != 2 {
			return nil, fmt.Errorf("%s: %w", name, ErrArity)
		}
		a, ok1 := args[0].(string)
		b, ok2 := args[1].(string)
		if !ok1 || !ok2 {
			return nil, fmt.Errorf("%s: %w", name, ErrType)
		}
		return pred(a, b), nil
	}
}

func builtinAbs(args []any) (any, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("abs: %w", ErrArity)
	}
	switch v := args[0].(type) {
	case int:
		return builtinAbs([]any{int64(v)})
	case int64:
		if v < 0 {
			if v == math.MinInt64 {
				// |MinInt64| is MaxInt64+1, which does not fit; -v
				// wraps back to MinInt64 and abs would return a
				// negative. Saturate: the magnitude is honest to the
				// representable range and errs away from every
				// threshold guard that consumes abs() (abs(x) < N
				// fails closed on the saturated value).
				return int64(math.MaxInt64), nil
			}
			return -v, nil
		}
		return v, nil
	case float64:
		if v < 0 {
			return -v, nil
		}
		return v, nil
	}
	return nil, fmt.Errorf("abs: %w", ErrType)
}

func builtinMin(args []any) (any, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("min: %w", ErrArity)
	}
	return foldNumeric(args, true)
}

func builtinMax(args []any) (any, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("max: %w", ErrArity)
	}
	return foldNumeric(args, false)
}

// foldNumeric compares integer operands without rounding them through float64.
func foldNumeric(args []any, minimum bool) (any, error) {
	bestInt, bestFloat, ok := numerics(args[0])
	if !ok {
		return nil, fmt.Errorf("min/max: %w", ErrType)
	}
	allInt := bestInt != nil
	for _, arg := range args[1:] {
		i, f, ok := numerics(arg)
		if !ok {
			return nil, fmt.Errorf("min/max: %w", ErrType)
		}
		allInt = allInt && i != nil
		order := compareNumbers(i, f, bestInt, bestFloat)
		if (minimum && order <= 0) || (!minimum && order >= 0) {
			bestInt, bestFloat = i, f
		}
	}
	if bestInt != nil && (allInt || compareNumbers(bestInt, bestFloat, nil, bestFloat) != 0) {
		return *bestInt, nil
	}
	return bestFloat, nil
}

func builtinNow(args []any) (any, error) {
	if len(args) != 0 {
		return nil, fmt.Errorf("now: %w", ErrArity)
	}
	return time.Now().UTC().Format(time.RFC3339), nil
}
