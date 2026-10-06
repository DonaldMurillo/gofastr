package openapi

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/filter"
)

// allFieldTypes is every schema.FieldType, so the parity test below
// cannot go stale when a type is added: the new type lands in the loop
// (and AllFieldTypes fails first if the list itself drifts).
func allFieldTypes() []schema.FieldType {
	return []schema.FieldType{
		schema.String, schema.Text, schema.Int, schema.Float,
		schema.Decimal, schema.Bool, schema.Enum, schema.UUID,
		schema.Timestamp, schema.Date, schema.JSON, schema.Relation,
		schema.Image, schema.File,
	}
}

// The spec's filter parameters and the runtime's filter parser answer
// the same question — does this operator suit this column type — so the
// spec must list <field>_<op> exactly when filter.CheckOpType accepts
// the pair. This is the drift gate: fieldSupportsLike once advertised
// _like on Int, Float, Date and Decimal columns ?field_like= answers
// 400 on, generating SDK methods that always fail. Derived from
// filter.OpSuitsType on both sides, the only way to fail it is to reintroduce
// a local type decision in the spec builder.
func TestSpecFilterOpsMatchCheckOpType(t *testing.T) {
	for _, ft := range allFieldTypes() {
		f := schema.Field{Name: "col", Type: ft}
		if ft == schema.Enum {
			// An Enum needs its value set to be a valid declaration.
			f.Values = []string{"a", "b"}
		}
		e := entity.Define("parity_"+tableSuffix(ft), entity.EntityConfig{
			Table:  "parity_" + tableSuffix(ft),
			Fields: []schema.Field{f},
		}.WithTimestamps(false))
		doc := EntityOpenAPI(reg(e), "Test", "1.0.0", nil).Build()
		get := getMap(t, getMap(t, getMap(t, doc, "paths"), "/"+e.GetTable()), "get")
		params := get["parameters"]

		for _, o := range advertisedFilterOps {
			name := "col" + o.suffix
			advertised := findParam(params, name) != nil
			accepted := filter.CheckOpType("col", o.op, ft) == nil
			if advertised && !accepted {
				t.Errorf("%s: spec advertises %q but CheckOpType refuses it (an SDK call that always 400s)", typeName(ft), name)
			}
			if accepted && !advertised {
				t.Errorf("%s: CheckOpType accepts %q but the spec hides it", typeName(ft), name)
			}
		}
	}
}

func tableSuffix(ft schema.FieldType) string { return typeNames[ft] }

var typeNames = map[schema.FieldType]string{
	schema.String: "string", schema.Text: "text", schema.Int: "int",
	schema.Float: "float", schema.Decimal: "decimal", schema.Bool: "bool",
	schema.Enum: "enum", schema.UUID: "uuid", schema.Timestamp: "timestamp",
	schema.Date: "date", schema.JSON: "json", schema.Relation: "relation",
	schema.Image: "image", schema.File: "file",
}

func typeName(ft schema.FieldType) string { return typeNames[ft] }
