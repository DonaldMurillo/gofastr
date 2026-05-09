package expr

import (
	"fmt"
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
	e.Register("starts_with", builtinStartsWith)
	e.Register("ends_with", builtinEndsWith)
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

func builtinStartsWith(args []any) (any, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("starts_with: %w", ErrArity)
	}
	a, ok1 := args[0].(string)
	b, ok2 := args[1].(string)
	if !ok1 || !ok2 {
		return nil, fmt.Errorf("starts_with: %w", ErrType)
	}
	return strings.HasPrefix(a, b), nil
}

func builtinEndsWith(args []any) (any, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("ends_with: %w", ErrArity)
	}
	a, ok1 := args[0].(string)
	b, ok2 := args[1].(string)
	if !ok1 || !ok2 {
		return nil, fmt.Errorf("ends_with: %w", ErrType)
	}
	return strings.HasSuffix(a, b), nil
}

func builtinAbs(args []any) (any, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("abs: %w", ErrArity)
	}
	switch v := args[0].(type) {
	case int64:
		if v < 0 {
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
	return foldNumeric(args, func(a, b float64) float64 {
		if a < b {
			return a
		}
		return b
	})
}

func builtinMax(args []any) (any, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("max: %w", ErrArity)
	}
	return foldNumeric(args, func(a, b float64) float64 {
		if a > b {
			return a
		}
		return b
	})
}

func foldNumeric(args []any, op func(a, b float64) float64) (any, error) {
	allInt := true
	values := make([]float64, len(args))
	intValues := make([]int64, len(args))
	for i, a := range args {
		switch v := a.(type) {
		case int64:
			intValues[i] = v
			values[i] = float64(v)
		case int:
			intValues[i] = int64(v)
			values[i] = float64(v)
			allInt = true
		case float64:
			values[i] = v
			allInt = false
		default:
			return nil, fmt.Errorf("min/max: %w", ErrType)
		}
	}
	if allInt {
		result := intValues[0]
		for i := 1; i < len(intValues); i++ {
			if op(float64(result), float64(intValues[i])) == float64(intValues[i]) {
				result = intValues[i]
			}
		}
		return result, nil
	}
	result := values[0]
	for i := 1; i < len(values); i++ {
		result = op(result, values[i])
	}
	return result, nil
}

func builtinNow(args []any) (any, error) {
	if len(args) != 0 {
		return nil, fmt.Errorf("now: %w", ErrArity)
	}
	return time.Now().UTC().Format(time.RFC3339), nil
}
