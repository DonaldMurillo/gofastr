package openapi

import (
	"math"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
)

func TestOpenAPIHugeMaxLength(t *testing.T) {
	huge := 1e30
	for _, typ := range []schema.FieldType{schema.String, schema.Text} {
		prop := FieldToSchema(schema.Field{Name: "s", Type: typ, Min: &huge, Max: &huge})
		if prop["maxLength"] != math.MaxInt32 || prop["minLength"] != math.MaxInt32 {
			t.Errorf("%v: minLength %v maxLength %v, want both clamped to MaxInt32", typ, prop["minLength"], prop["maxLength"])
		}
	}
}
