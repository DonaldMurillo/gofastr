package appspec

import (
	"fmt"
	"math"
	"strings"

	"github.com/DonaldMurillo/gofastr/codegen"
	coreyaml "github.com/DonaldMurillo/gofastr/core/yaml"
)

func strictIntValue(node *coreyaml.Node, label string) (int, error) {
	if node == nil {
		return 0, nil
	}
	if node.Kind != coreyaml.Scalar {
		return 0, fmt.Errorf("%s must be a whole number, got a %s", label, yamlKindName(node.Kind))
	}
	switch v := node.Value.(type) {
	case int64:
		return int(v), nil
	case float64:
		if v != math.Trunc(v) {
			return 0, fmt.Errorf("%s must be a whole number, got %v", label, v)
		}
		return int(v), nil
	default:
		return 0, fmt.Errorf("%s must be a whole number, got %q", label, fmt.Sprint(node.Value))
	}
}

func stringValue(node *coreyaml.Node) string {
	if node == nil || node.Kind != coreyaml.Scalar || node.Value == nil {
		return ""
	}
	return fmt.Sprint(node.Value)
}

func boolValue(node *coreyaml.Node) bool {
	if node == nil || node.Kind != coreyaml.Scalar {
		return false
	}
	if v, ok := node.Value.(bool); ok {
		return v
	}
	return strings.EqualFold(fmt.Sprint(node.Value), "true")
}

func protectiveBool(m map[string]*coreyaml.Node, key, context string) (bool, error) {
	node, ok := m[key]
	if !ok || node == nil {
		return false, nil
	}
	v, err := strictBoolValue(node)
	if err != nil {
		return false, fmt.Errorf("%s.%s %w: this flag is not guessed (YAML 1.2 reads yes/on/y/1 as strings, not booleans)", context, key, err)
	}
	return v, nil
}

func strictBoolValue(node *coreyaml.Node) (bool, error) {
	if node != nil && node.Kind == coreyaml.Scalar {
		if v, ok := node.Value.(bool); ok {
			return v, nil
		}
		switch fmt.Sprint(node.Value) {
		case "true":
			return true, nil
		case "false":
			return false, nil
		}
	}
	got := "missing"
	line := 0
	if node != nil {
		got = fmt.Sprintf("%q", fmt.Sprint(node.Value))
		line = node.Line
	}
	return false, fmt.Errorf("must be true or false (got %s) at line %d", got, line)
}

func intValue(node *coreyaml.Node) int {
	if node == nil || node.Kind != coreyaml.Scalar {
		return 0
	}
	switch v := node.Value.(type) {
	case int64:
		return int(v)
	case float64:
		return int(v)
	default:
		return 0
	}
}

func floatValue(node *coreyaml.Node) float64 {
	if node == nil || node.Kind != coreyaml.Scalar {
		return 0
	}
	switch v := node.Value.(type) {
	case int64:
		return float64(v)
	case float64:
		return v
	default:
		return 0
	}
}

func scalarValue(node *coreyaml.Node) any {
	if node == nil || node.Kind != coreyaml.Scalar {
		return nil
	}
	return node.Value
}

func stringListValue(node *coreyaml.Node) []string {
	if node == nil || node.Kind != coreyaml.List {
		return nil
	}
	out := make([]string, 0, len(node.List))
	for _, item := range node.List {
		out = append(out, stringValue(item))
	}
	return out
}

func mapValue(node *coreyaml.Node) map[string]any {
	if node == nil || node.Kind != coreyaml.Map {
		return nil
	}
	out := make(map[string]any, len(node.Map))
	for key, child := range node.Map {
		out[key] = codegen.AnyValue(child)
	}
	return out
}

func stringStringMapValue(node *coreyaml.Node) map[string]string {
	if node == nil || node.Kind != coreyaml.Map {
		return nil
	}
	out := make(map[string]string, len(node.Map))
	for key, child := range node.Map {
		out[key] = stringValue(child)
	}
	return out
}
