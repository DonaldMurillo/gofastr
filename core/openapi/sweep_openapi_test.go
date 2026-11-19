package openapi

import (
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
}
