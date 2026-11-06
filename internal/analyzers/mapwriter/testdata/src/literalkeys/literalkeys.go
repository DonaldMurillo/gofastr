// Package literalkeys pins the map-literal return arm (round 8,
// R8-RTS-01): a required-field check that ranges a map literal of
// field names returns on the first miss, and Go randomizes map order,
// so an entry with two bad fields names a different one per run.
package literalkeys

import "fmt"

type endpoint struct{ Name, Path string }

// decode is core/settings DecodeEndpoints before the fix.
func decode(item map[string]string) (endpoint, error) {
	var e endpoint
	for key, dst := range map[string]*string{"name": &e.Name, "path": &e.Path} {
		v, ok := item[key]
		if !ok {
			return e, fmt.Errorf("%s is required", key) // want `returning an error built from a map range's entry`
		}
		*dst = v
	}
	return e, nil
}

// decodeBound ranges a local bound to a literal: the same fixed set.
func decodeBound(item map[string]string) (endpoint, error) {
	var e endpoint
	fields := map[string]*string{"name": &e.Name, "path": &e.Path}
	for key, dst := range fields {
		v, ok := item[key]
		if !ok {
			return e, fmt.Errorf("%s is required", key) // want `returning an error built from a map range's entry`
		}
		*dst = v
	}
	return e, nil
}

// decodeOrdered is the fix: a slice of names keeps the order.
func decodeOrdered(item map[string]string) (endpoint, error) {
	var e endpoint
	for _, f := range []struct {
		key string
		dst *string
	}{{"name", &e.Name}, {"path", &e.Path}} {
		v, ok := item[f.key]
		if !ok {
			return e, fmt.Errorf("%s is required", f.key)
		}
		*f.dst = v
	}
	return e, nil
}

// typedConfig ranges a typed configuration map that is not a literal:
// the boot-validation shape the return arm leaves alone.
func typedConfig(routes map[string]endpoint) error {
	for name, r := range routes {
		if r.Path == "" {
			return fmt.Errorf("route %s has no path", name)
		}
	}
	return nil
}

// literalNoEntry returns an error that names no entry: quiet.
func literalNoEntry(item map[string]string) error {
	for key := range map[string]bool{"name": true, "path": true} {
		if item[key] == "" {
			return fmt.Errorf("a required field is missing")
		}
	}
	return nil
}

// builtFromEmpty starts from an empty literal and fills it at run time:
// a built map, not a fixed key set, so the arm leaves it to the typed
// configuration posture.
func builtFromEmpty(names []string) error {
	seen := map[string]bool{}
	for _, n := range names {
		seen[n] = true
	}
	for n := range seen {
		if n == "" {
			return fmt.Errorf("empty name after %q", n)
		}
	}
	return nil
}
