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
		// The app: blocks merge as YAML, key by key, before one decode.
		// A decoded AppSpecApp cannot tell an unset key from its default
		// (api_prefix "api", auth.dev_mode true), so merging decoded
		// structs would let a file that only sets the theme re-apply
		// those defaults over another file's choice.
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

// RootFileNames are the conventional app spec file names at a project
// root, in probe order. Callers that look for "the app spec" in a
// directory use this list so YAML and JSON specs are found alike.
var RootFileNames = []string{"gofastr.yml", "gofastr.yaml", "gofastr.json"}

func DecodeAppSpecFile(path string) (AppSpec, error) {
	node, err := parseAppSpecFile(path)
	if err != nil {
		return AppSpec{}, err
	}
	return DecodeAppSpec(node)
}

// fragmentError names the file of a directory spec a parse or decode
// error came from. Without it `gofastr validate specs/` answered
// "yaml:3:4: unexpected indentation" with every fragment a candidate.
type fragmentError struct {
	file string
	err  error
}

func (e fragmentError) Error() string {
	return "app spec: " + e.file + ": " + strings.TrimPrefix(e.err.Error(), "app spec: ")
}

func (e fragmentError) Unwrap() error { return e.err }

// parseAppSpecFile reads one .yml/.yaml/.json app spec into a YAML node.
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

// mergeAppNode folds one file's app: block into dst. Maps merge key by
// key (auth in one file, theme in another); a key that two files both
// set is an error naming both, the same way a duplicate entity or
// screen name is refused rather than one silently winning. seen maps a
// dotted key path to the file that set it.
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

// strictJSONValue decodes one JSON document, refusing duplicate mapping
// keys. encoding/json is silent last-wins on duplicates, and the YAML leg
// of this decoder already rejects them at the parser (core/yaml: "silent
// last-wins lets a stale or hostile copy-paste line override the value a
// reviewer believes is in force"); the .json leg must not be the seam
// where a merged or agent-transcribed app spec quietly takes the second
// copy. Numbers decode as float64, exactly as json.Unmarshal would, so
// downstream node values keep their type.
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

// decodeJSONValue walks json.Decoder tokens into the same any-shape
// json.Unmarshal produces (map[string]any, []any, string, float64, bool,
// nil), erroring on a repeated object key anywhere in the tree.
func decodeJSONValue(dec *json.Decoder) (any, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	delim, ok := tok.(json.Delim)
	if !ok {
		return tok, nil // string, float64, bool or nil
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
		if _, err := dec.Token(); err != nil { // consume '}'
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
		if _, err := dec.Token(); err != nil { // consume ']'
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

// MergeAppSpecs appends b's list sections to a's. It keeps a.App, or
// takes b.App when a has none: a decoded AppSpecApp cannot tell an unset
// key from a default, so a directory spec merges its app: blocks as YAML
// in LoadAppSpecPath instead. TestMergeAppSpecsAppendsEverySlice fails
// when a new list section is not appended here.
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
