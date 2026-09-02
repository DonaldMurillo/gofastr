// Package b holds laxcoerce positives in code that never existed in the
// repo: different names, different package, same shape.
package b

import "fmt"

// parseThreshold reads the "threshold" knob from a tool-args map. A
// caller sending threshold as a string gets "no threshold supplied"
// semantics with a nil error.
func parseThreshold(args map[string]any) (float64, bool, error) {
	t, ok := args["threshold"].(float64) // want `type assertion on args treated as absence`
	if !ok {
		return 0, false, nil
	}
	return t, true, nil
}

// collectDisplayNames drops every record whose display_name is present
// but not a string: the continue reads as "nothing to show here".
func collectDisplayNames(records []map[string]any) []string {
	var out []string
	for _, rec := range records {
		name, ok := rec["display_name"].(string) // want `type assertion on rec treated as absence`
		if !ok {
			continue
		}
		out = append(out, name)
	}
	return out
}

// regionFor narrows a search by an optional region filter. Pre-fix
// shape via a local: the entry is read once, asserted through the
// local, and a wrong-typed region quietly widens the search.
func regionFor(q map[string]any) (string, error) {
	v := q["region"]
	s, ok := v.(string) // want `type assertion on q treated as absence`
	if !ok {
		return "", nil // no filter: search everywhere
	}
	return s, nil
}

// fixed spellings of the same three shapes, for the negative column.

func parseThresholdFixed(args map[string]any) (float64, bool, error) {
	v, present := args["threshold"]
	if !present {
		return 0, false, nil
	}
	t, ok := v.(float64)
	if !ok {
		return 0, false, fmt.Errorf("threshold: want float, got %T", v)
	}
	return t, true, nil
}

func collectDisplayNamesFixed(records []map[string]any) ([]string, error) {
	var out []string
	for _, rec := range records {
		v, present := rec["display_name"]
		if !present {
			continue
		}
		name, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("display_name: want string, got %T", v)
		}
		out = append(out, name)
	}
	return out, nil
}
