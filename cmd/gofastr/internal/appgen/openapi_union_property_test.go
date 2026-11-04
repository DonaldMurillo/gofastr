package appgen

import (
	"fmt"
	"testing"
)

func TestUnionPropertiesUseRawJSON(t *testing.T) {
	for _, union := range []string{"oneOf", "anyOf"} {
		for _, wrapped := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/ref=%t", union, wrapped), func(t *testing.T) {
				property := fmt.Sprintf(`{"%s":[{"type":"integer"},{"type":"boolean"}]}`, union)
				if wrapped {
					property = `{"allOf":[{"$ref":"#/components/schemas/Value"}]}`
				}
				doc := mustDecodeDoc(t, fmt.Sprintf(`{"openapi":"3.1.0","components":{"schemas":{"Value":{"%s":[{"type":"integer"},{"type":"boolean"}]}}},
				"paths":{"/items":{"post":{"operationId":"create","requestBody":{"content":{"application/json":{"schema":{
				"type":"object","properties":{"count":%s,"label":{"type":"string"}}}}}}}}}}`, union, property))
				spec, err := BuildOpenAPICLISpec(doc, CliOptions{Binary: "union"}, "client")
				if err != nil {
					t.Fatal(err)
				}
				if len(spec.Ops) != 1 || spec.Ops[0].BodyKind != "json" {
					t.Fatalf("raw JSON unavailable: %+v", spec.Ops)
				}
				fields := spec.Ops[0].BodyFields
				if len(fields) != 1 || fields[0].Wire != "label" {
					t.Errorf("union emitted scalar flags: %+v", fields)
				}
			})
		}
	}
}
