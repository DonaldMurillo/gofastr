package appspec

import (
	"fmt"
	"math"
	"strings"

	"github.com/DonaldMurillo/gofastr/codegen"
	coreyaml "github.com/DonaldMurillo/gofastr/core/yaml"
)

// strictIntValue reads a whole-number scalar, refusing every other shape.
//
// intValue, the lax decoder, maps a string ("5"), a fractional float (3.7),
// or a list to 0 — and 0 is legal, so `count: "5"` silently generated no
// rows at all while the author asked for five. Same coercion on a weight
// silently zeroed it. This is the count/weights sibling of protectiveBool: a
// lax decoder guessing at a value the author demonstrably tried to set.
// Integral floats decode (mirroring seedValueRejection's toInt64, which
// accepts them, so the generate-time verdict never disagrees with boot).
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

// protectiveBool decodes a flag whose FALSE value is the PERMISSIVE state, so a
// coerced-to-false read is fail-open. Missing is fine (that is the documented
// default); present-but-unreadable is an error naming the key.
//
// boolValue, the lax decoder, maps anything that is not the bool true or the
// string "true" to false. core/yaml is YAML 1.2, so `yes` / `on` / `y` / `1` are
// STRINGS there, and they are how people, and agents transcribing a spec, most
// often write "true" in YAML. `access: {auth: yes}` therefore decoded to
// Auth=false and registered the screen with no policy at all: publicly
// reachable, no warning, no error.
//
// strictBoolValue already existed for this and was wired to exactly one key
// (app.auth.dev_mode). Its comment justified itself as "the one app spec bool
// whose default is true", and that framing is what let these through. The
// polarity that matters is not what the default is, it is which direction the
// coercion moves safety. Every key routed through here defaults to false, and
// false is the unsafe side.
//
// Deliberately NOT applied to flags whose false value is the safe/inert one
// (`exposure.public`, `exposure.mcp`, `exposure.crud`, `app.public_openapi`,
// `pwa.enabled`, `admin.enabled`, `llm_md`, `timestamps`, `many`, `create`):
// there, a coerced false turns a feature OFF, which is the direction we would
// want anyway. NOTE: `*.enabled` is NOT a blanket exception; app.auth.enabled
// IS routed here: false means the auth subsystem is off and the app is public.
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

// strictBoolValue accepts only a genuine bool node (or the literal
// strings "true"/"false") and errors on anything else, for keys where
// lax coercion would silently invert a safe default. See protectiveBool
// for the larger set of keys where the unsafe direction is FALSE.
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

// stringStringMapValue decodes a YAML map whose values are all scalars, e.g.
// an entity's `renames:` (old column -> new column).
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
