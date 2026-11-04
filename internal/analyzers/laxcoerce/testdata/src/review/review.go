package review

import "errors"

func sharedError(values map[string]any) (int, error) {
	count, ok := values["count"].(int)
	if !ok {
		goto invalid
	}
	return count, nil
invalid:
	return 0, errors.New("invalid count")
}

func chainedLabels(values map[string]any) (int, error) {
	count, ok := values["count"].(int)
	if !ok {
		goto invalid
	}
	return count, nil
invalid:
	goto failure
failure:
	{
		return 0, errors.New("invalid count")
	}
}

func labelFallthrough(values map[string]any) (int, error) {
	count, ok := values["count"].(int)
	if !ok {
		goto invalid
	}
	return count, nil
invalid:
	println("invalid count")
	return 0, errors.New("invalid count")
}

func conditionalError(values map[string]any, detail bool) (int, error) {
	count, ok := values["count"].(int)
	if !ok {
		goto invalid
	}
	return count, nil
invalid:
	if detail {
		return 0, errors.New("invalid count")
	}
	return 0, errors.New("invalid input")
}

func nilLabel(values map[string]any) (int, error) {
	count, ok := values["count"].(int) // want `type assertion .* treated as absence`
	if !ok {
		goto invalid
	}
	return count, nil
invalid:
	return 0, nil
}

func sometimesNil(values map[string]any, detail bool) (int, error) {
	count, ok := values["count"].(int) // want `type assertion .* treated as absence`
	if !ok {
		goto invalid
	}
	return count, nil
invalid:
	if detail {
		return 0, errors.New("invalid count")
	}
	return 0, nil
}

func cyclicLabels(values map[string]any) (int, error) {
	count, ok := values["count"].(int) // want `type assertion .* treated as absence`
	if !ok {
		goto invalid
	}
	return count, nil
invalid:
	goto invalid
}
