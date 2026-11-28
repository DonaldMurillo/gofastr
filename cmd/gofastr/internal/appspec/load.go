package appspec

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"

	coreyaml "github.com/DonaldMurillo/gofastr/core/yaml"
)

func LoadAppSpec(path string) (AppSpec, error) {
	return LoadAppSpecPath(path, true)
}

func LoadAppSpecPath(path string, validate bool) (AppSpec, error) {
	info, err := os.Stat(path)
	if err != nil {
		return AppSpec{}, err
	}
	if info.IsDir() {
		entries, err := os.ReadDir(path)
		if err != nil {
			return AppSpec{}, err
		}
		var merged AppSpec
		foundAppSpec := false
		appNode := &coreyaml.Node{Kind: coreyaml.Map, Map: map[string]*coreyaml.Node{}}
		appKeyFile := map[string]string{}
		for _, entry := range entries {
			if entry.IsDir() || !isAppSpecFile(entry.Name()) {
				continue
			}
			foundAppSpec = true
			node, err := parseAppSpecFile(filepath.Join(path, entry.Name()))
			if err != nil {
				return AppSpec{}, fragmentError{entry.Name(), err}
			}
			next, err := DecodeAppSpec(node)
			if err != nil {
				return AppSpec{}, fragmentError{entry.Name(), err}
			}
			if node.Kind == coreyaml.Map && node.Map["app"] != nil {
				if err := mergeAppNode(appNode, node.Map["app"], "app", entry.Name(), appKeyFile); err != nil {
					return AppSpec{}, fmt.Errorf("app spec directory %q: %w", path, err)
				}
			}
			merged = MergeAppSpecs(merged, next)
		}
		if !foundAppSpec {
			return AppSpec{}, fmt.Errorf("app spec directory %q does not contain any app spec files", path)
		}
		if len(appKeyFile) > 0 {
			app, err := decodeAppSpecApp(appNode)
			if err != nil {
				return AppSpec{}, fmt.Errorf("app spec directory %q: %w", path, err)
			}
			merged.App = app
		}
		merged.appless = len(appKeyFile) == 0
		if validate {
			if err := ValidateAppSpec(merged); err != nil {
				return AppSpec{}, err
			}
		}
		return merged, nil
	}
	bp, err := DecodeAppSpecFile(path)
	if err != nil {
		return AppSpec{}, err
	}
	if validate {
		if err := ValidateAppSpec(bp); err != nil {
			return AppSpec{}, err
		}
	}
	return bp, nil
}

// RootFileNames are the app spec file names probed at a project root, in order.
var RootFileNames = []string{"gofastr.yml", "gofastr.yaml", "gofastr.json"}

func DecodeAppSpecFile(path string) (AppSpec, error) {
	node, err := parseAppSpecFile(path)
	if err != nil {
		return AppSpec{}, err
	}
	return DecodeAppSpec(node)
}

type fragmentError struct {
	file string
	err  error
}

func (e fragmentError) Error() string {
	return "app spec: " + e.file + ": " + strings.TrimPrefix(e.err.Error(), "app spec: ")
}

func (e fragmentError) Unwrap() error { return e.err }

func parseAppSpecFile(path string) (*coreyaml.Node, error) {
	if !isAppSpecFile(path) {
		return nil, fmt.Errorf("app spec %q must be .yml, .yaml, or .json", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if strings.EqualFold(filepath.Ext(path), ".json") {
		raw, err := strictJSONValue(data)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", path, err)
		}
		return yamlNodeFromJSON(raw), nil
	}
	return coreyaml.Parse(string(data))
}

func mergeAppNode(dst, src *coreyaml.Node, keyPath, file string, seen map[string]string) error {
	if src.Kind != coreyaml.Map {
		return fmt.Errorf("%s: %s must be a map at line %d", file, keyPath, src.Line)
	}
	for _, key := range slices.Sorted(maps.Keys(src.Map)) {
		child := src.Map[key]
		childPath := keyPath + "." + key
		existing, ok := dst.Map[key]
		if !ok {
			if child != nil && child.Kind == coreyaml.Map {
				existing = &coreyaml.Node{Kind: coreyaml.Map, Map: map[string]*coreyaml.Node{}, Line: child.Line, Column: child.Column}
				dst.Map[key] = existing
				seen[childPath] = file
				if err := mergeAppNode(existing, child, childPath, file, seen); err != nil {
					return err
				}
				continue
			}
			dst.Map[key] = child
			seen[childPath] = file
			continue
		}
		if existing != nil && existing.Kind == coreyaml.Map && child != nil && child.Kind == coreyaml.Map {
			if err := mergeAppNode(existing, child, childPath, file, seen); err != nil {
				return err
			}
			continue
		}
		return fmt.Errorf("%s is set in both %s and %s; set it in one file", childPath, seen[childPath], file)
	}
	return nil
}

func yamlNodeFromJSON(value any) *coreyaml.Node {
	switch v := value.(type) {
	case map[string]any:
		out := &coreyaml.Node{Kind: coreyaml.Map, Map: map[string]*coreyaml.Node{}, Line: 1, Column: 1}
		for key, child := range v {
			out.Map[key] = yamlNodeFromJSON(child)
		}
		return out
	case []any:
		out := &coreyaml.Node{Kind: coreyaml.List, Line: 1, Column: 1}
		for _, child := range v {
			out.List = append(out.List, yamlNodeFromJSON(child))
		}
		return out
	default:
		return &coreyaml.Node{Kind: coreyaml.Scalar, Value: v, Line: 1, Column: 1}
	}
}

func strictJSONValue(data []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	v, err := decodeJSONValue(dec)
	if err != nil {
		return nil, err
	}
	if _, err := dec.Token(); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("unexpected trailing data after JSON value")
		}
		return nil, err
	}
	return v, nil
}

func decodeJSONValue(dec *json.Decoder) (any, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	delim, ok := tok.(json.Delim)
	if !ok {
		return tok, nil
	}
	switch delim {
	case '{':
		obj := map[string]any{}
		for dec.More() {
			keyTok, err := dec.Token()
			if err != nil {
				return nil, err
			}
			key, ok := keyTok.(string)
			if !ok {
				return nil, fmt.Errorf("object key is not a string")
			}
			if _, dup := obj[key]; dup {
				return nil, fmt.Errorf("duplicate object key %q (silent last-wins would override the value a reviewer believes is in force)", key)
			}
			val, err := decodeJSONValue(dec)
			if err != nil {
				return nil, err
			}
			obj[key] = val
		}
		if _, err := dec.Token(); err != nil {
			return nil, err
		}
		return obj, nil
	case '[':
		arr := []any{}
		for dec.More() {
			val, err := decodeJSONValue(dec)
			if err != nil {
				return nil, err
			}
			arr = append(arr, val)
		}
		if _, err := dec.Token(); err != nil {
			return nil, err
		}
		return arr, nil
	default:
		return nil, fmt.Errorf("unexpected delimiter %v", delim)
	}
}

func isAppSpecFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".yml" || ext == ".yaml" || ext == ".json"
}

// MergeAppSpecs appends b's list sections to a's and keeps a.App, or b.App when a's is zero.
func MergeAppSpecs(a, b AppSpec) AppSpec {
	if reflect.ValueOf(a.App).IsZero() {
		a.App = b.App
	}
	a.Entities = append(a.Entities, b.Entities...)
	a.Screens = append(a.Screens, b.Screens...)
	a.Nav = append(a.Nav, b.Nav...)
	a.Seed = append(a.Seed, b.Seed...)
	a.Endpoints = append(a.Endpoints, b.Endpoints...)
	a.Hooks = append(a.Hooks, b.Hooks...)
	a.Middleware = append(a.Middleware, b.Middleware...)
	a.Plugins = append(a.Plugins, b.Plugins...)
	a.Helpers = append(a.Helpers, b.Helpers...)
	return a
}
