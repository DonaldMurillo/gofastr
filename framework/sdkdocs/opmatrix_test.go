package sdkdocs

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/filter"
)

// The SDK docs' canonical example filter offers `<field>_gte` exactly
// when the picked column's type accepts a range comparison: a `_gte`
// snippet on a Bool or JSON column is a request the server answers
// 400, published as the example every reader copies. Over every field
// type, the operator offered and the column picked must agree with
// filter.OpSuitsType, the one predicate every filter surface applies.
func TestExampleFilterOperatorSuitsPickedField(t *testing.T) {
	for _, tpe := range []schema.FieldType{
		schema.String, schema.Text, schema.Int, schema.Float, schema.Decimal,
		schema.Bool, schema.Enum, schema.UUID, schema.Timestamp, schema.Date,
		schema.JSON,
	} {
		cfg := entity.EntityConfig{Fields: []schema.Field{{Name: "f", Type: tpe}}}
		field, op := exampleFilter(cfg)
		if field != "f" {
			t.Fatalf("type %d: example picked %q, want the probe field f", tpe, field)
		}
		wantGte := filter.OpSuitsType(filter.OpGte, tpe)
		if gotGte := op == "_gte"; gotGte != wantGte {
			t.Errorf("type %d: example offers _gte = %v, OpSuitsType says %v", tpe, gotGte, wantGte)
		}
	}
}

// An entity whose every queryable column refuses the range operators
// (a Bool and a JSON blob) still gets a working example: plain
// equality, which suits every type.
func TestExampleFilterFallsBackToEquality(t *testing.T) {
	cfg := entity.EntityConfig{Fields: []schema.Field{
		{Name: "flag", Type: schema.Bool},
		{Name: "meta", Type: schema.JSON},
	}}
	field, op := exampleFilter(cfg)
	if field != "flag" || op != "" {
		t.Fatalf("exampleFilter = (%q, %q), want flag with plain equality", field, op)
	}
}
