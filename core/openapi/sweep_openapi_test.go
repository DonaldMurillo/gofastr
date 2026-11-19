package openapi

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
)

func TestSweepOpenAPIFieldSchemaAgreesWithJSONSchema(t *testing.T) {
	lo, hi := 2.5, 9.5
	for ft := schema.String; ft <= schema.File; ft++ {
		f := schema.Field{Name: "f", Type: ft, Min: &lo, Max: &hi, Values: []string{"a", "b"}, Default: "a", To: "t", Many: true}
		if ft == schema.String || ft == schema.Text {
			f.Pattern = "^a+$"
		}
		oa := FieldToSchema(f)
		js := schema.JSONSchema([]schema.Field{f})["properties"].(map[string]any)["f"].(map[string]any)
		keys := []string{"type", "minLength", "maxLength", "enum", "default", "x-relation", "x-many"}
		if ft != schema.UUID {
			keys = append(keys, "pattern")
		}
		if ft != schema.Decimal {
			keys = append(keys, "minimum", "maximum")
		}
		for _, k := range keys {
			if !reflect.DeepEqual(oa[k], js[k]) {
				t.Errorf("type %d: %s = %v in OpenAPI, %v in JSONSchema", ft, k, oa[k], js[k])
			}
		}
	}
	for _, c := range []struct {
		values   []string
		wantJSON string
	}{{nil, "null"}, {[]string{}, "[]"}} {
		values := c.values
		f := schema.Field{Name: "f", Type: schema.Enum, Values: values}
		oa := FieldToSchema(f)
		js := schema.JSONSchema([]schema.Field{f})["properties"].(map[string]any)["f"].(map[string]any)
		if _, ok := oa["enum"]; ok {
			t.Errorf("Values %#v: OpenAPI enum = %v, want none", values, oa["enum"])
		}
		if e, ok := js["enum"].([]string); !ok || len(e) != 0 {
			t.Errorf("Values %#v: JSONSchema enum = %#v, want an empty list", values, js["enum"])
		}
		if b, err := json.Marshal(js["enum"]); err != nil || string(b) != c.wantJSON {
			t.Errorf("Values %#v: JSONSchema enum encodes as %s (%v), want %s", values, b, err, c.wantJSON)
		}
		if oa["type"] != js["type"] {
			t.Errorf("Values %#v: type %v in OpenAPI, %v in JSONSchema", values, oa["type"], js["type"])
		}
	}
}
