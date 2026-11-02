// Package returned pins the return arm (2026-11): a map range over
// decoded wire input whose body returns an error built from the entry it
// reached names a random entry per call.
package returned

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"mime/multipart"
	"net/http"
	"slices"
	"strings"
)

type handler struct{}

func (handler) wireKeyColumn(k string) string { return strings.ToLower(k) }

// itemKeyFolds is framework/crud itemKeyFoldsUnambiguous before
// a2b42f4d: the refusal names a column derived from the key.
func (h handler) itemKeyFolds(item map[string]any) error {
	seen := make(map[string]struct{}, len(item))
	for k := range item {
		col := h.wireKeyColumn(k)
		if _, dup := seen[col]; dup {
			return fmt.Errorf("keys folding onto the same field %q (sent %q and another spelling)", col, k) // want `returning an error built from a map range's entry`
		}
		seen[col] = struct{}{}
	}
	return nil
}

// itemKeyFoldsSorted is the fix.
func (h handler) itemKeyFoldsSorted(item map[string]any) error {
	seen := make(map[string]string, len(item))
	for _, k := range slices.Sorted(maps.Keys(item)) {
		col := h.wireKeyColumn(k)
		if prev, dup := seen[col]; dup {
			return fmt.Errorf("keys %q and %q fold onto %q", prev, k, col)
		}
		seen[col] = k
	}
	return nil
}

type fileFields map[string]bool

// parseMultipart is framework/crud parseMultipartBody before a2b42f4d:
// both form maps refused whichever duplicate key came first.
func parseMultipart(r *http.Request, files fileFields) (map[string]any, error) {
	body := map[string]any{}
	for key, vals := range r.MultipartForm.Value {
		if len(vals) == 0 {
			continue
		}
		if len(vals) == 1 {
			body[key] = vals[0]
			continue
		}
		return nil, fmt.Errorf("duplicate form key %q for a single-valued field", key) // want `returning an error built from a map range's entry`
	}
	for key, headers := range r.MultipartForm.File {
		if !files[key] {
			continue
		}
		if len(headers) > 1 {
			return nil, fmt.Errorf("duplicate file part %q: an Image/File field holds one upload", key) // want `returning an error built from a map range's entry`
		}
	}
	return body, nil
}

// fieldError is a constructor-built error.
type fieldError struct{ Field string }

func (e *fieldError) Error() string { return "bad field " + e.Field }

func composite(m map[string]json.RawMessage) error {
	for k, v := range m {
		if len(v) == 0 {
			return &fieldError{Field: k} // want `returning an error built from a map range's entry`
		}
	}
	return nil
}

func newFieldError(name string) error { return &fieldError{Field: name} }

func constructor(h http.Header) error {
	for name := range maps.Keys(h) {
		if strings.HasPrefix(name, "X-Bad") {
			return newFieldError(name) // want `returning an error built from a map range's entry`
		}
	}
	return nil
}

// ---- quiet ----

var errBad = errors.New("bad body")

// constant: the refusal names no entry, so order cannot show.
func constant(m map[string]any) error {
	for _, v := range m {
		if v == nil {
			return errBad
		}
		if s, ok := v.(string); ok && s == "" {
			return errors.New("empty value")
		}
	}
	return nil
}

func check(v any) error {
	if v == nil {
		return errBad
	}
	return nil
}

// propagated: the range hands on an error a callee built; the rule
// reads construction at the return, not every call on the entry.
func propagated(m map[string]any) error {
	for _, v := range m {
		if err := check(v); err != nil {
			return err
		}
		if err := check(v); err != nil {
			return fmt.Errorf("checking: %w", err)
		}
	}
	return nil
}

// lookup returns the entry, not a refusal: "find the one match".
func lookup(m map[string]any, want string) (string, error) {
	for k, v := range m {
		if v == want {
			return k, nil
		}
	}
	return "", errBad
}

// lookupBuilt builds its answer from the entry: a call, but not an error.
func lookupBuilt(m map[string]any, want string) (string, error) {
	for k, v := range m {
		if v == want {
			return strings.ToUpper(k), nil
		}
	}
	return "", errBad
}

// single is iterated once inside the len(m) == 1 guard.
func single(m map[string]any) error {
	if len(m) == 1 {
		for k := range m {
			return fmt.Errorf("only %q", k)
		}
	}
	return nil
}

// literal: the return leaves a nested func literal, not the range.
func literal(m map[string]any) []func() error {
	var out []func() error
	for k := range m {
		out = append(out, func() error { return fmt.Errorf("%s", k) })
	}
	return out
}

type route struct{ Path string }

// config is a typed configuration map, not decoded wire input: a boot
// error naming one bad entry is one fix away from the next.
func config(routes map[string]route) error {
	for name, r := range routes {
		if r.Path == "" {
			return fmt.Errorf("route %q has no path", name)
		}
	}
	return nil
}

// sortedSlice ranges a slice, ordered by construction.
func sortedSlice(names []string, m map[string]any) error {
	for _, n := range names {
		if _, ok := m[n]; !ok {
			return fmt.Errorf("missing %q", n)
		}
	}
	return nil
}

var _ = multipart.FileHeader{}
