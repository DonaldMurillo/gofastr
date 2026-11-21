package sdkdocs

import (
	"net/url"
	"strings"
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
		cfg := entity.EntityConfig{Fields: []schema.Field{
			{Name: "id", Type: schema.UUID, AutoGenerate: schema.AutoUUID},
			{Name: "f", Type: tpe, Values: []string{"example"}},
		}}
		sample := exampleFilter(cfg)
		if !filter.OpSuitsType(filter.OpEq, tpe) {
			// A JSON column takes no filter: the example falls back to
			// the primary key rather than publish a request that 400s.
			if sample.field != "id" {
				t.Errorf("type %d: example picked %q, want the id fallback", tpe, sample.field)
			}
			continue
		}
		if sample.field != "f" {
			t.Fatalf("type %d: example picked %q, want the probe field f", tpe, sample.field)
		}
		wantGte := filter.OpSuitsType(filter.OpGte, tpe)
		if gotGte := sample.op == filter.OpGte; gotGte != wantGte {
			t.Errorf("type %d: example offers _gte = %v, OpSuitsType says %v", tpe, gotGte, wantGte)
		}
	}
}

// An entity whose every queryable column refuses the range operators
// (a Bool and a JSON blob) still gets a working example: plain
// equality on the Bool. The JSON blob comes first and is skipped, since
// it takes no filter at all.
func TestExampleFilterFallsBackToEquality(t *testing.T) {
	cfg := entity.EntityConfig{Fields: []schema.Field{
		{Name: "meta", Type: schema.JSON},
		{Name: "flag", Type: schema.Bool},
	}}
	sample := exampleFilter(cfg)
	if sample.field != "flag" || sample.op != filter.OpEq {
		t.Fatalf("exampleFilter = %+v, want flag with plain equality", sample)
	}
}

func TestExampleTabsUseValuesTheServerAccepts(t *testing.T) {
	sc := &entityScreen{site: &site{}}
	for _, tc := range []struct {
		name string
		t    schema.FieldType
		key  string
		raw  string
		js   string
	}{
		{"bool", schema.Bool, "f", "true", "true"},
		{"date", schema.Date, "f_gte", "2026-01-01", `"2026-01-01"`},
		{"timestamp", schema.Timestamp, "f_gte", "2026-01-01T00:00:00Z", `"2026-01-01T00:00:00Z"`},
		{"uuid", schema.UUID, "f_gte", "00000000-0000-4000-8000-000000000001", `"00000000-0000-4000-8000-000000000001"`},
		{"enum", schema.Enum, "f_gte", "in progress", `"in progress"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := entity.Define("posts", entity.EntityConfig{Fields: []schema.Field{{
				Name: "f", Type: tc.t, Values: []string{"in progress"},
			}}})
			parsed, err := filter.ParseFiltersValues(url.Values{tc.key: {tc.raw}}, e.Config.Fields)
			if err != nil || len(parsed) != 1 || parsed[0].Field != "f" {
				t.Fatalf("invalid example filter: %v, %v", parsed, err)
			}
			examples := renderedText(t, sc.exampleTabs(e).String())
			for _, want := range []string{
				`params.Set("` + tc.key + `", "` + tc.raw + `")`,
				`filters: { "` + tc.key + `": ` + tc.js + " }",
				tc.key + "=" + url.QueryEscape(tc.raw),
			} {
				if !strings.Contains(examples, want) {
					t.Errorf("examples lack %q", want)
				}
			}
		})
	}
	t.Run("no queryable field", func(t *testing.T) {
		e := &entity.Entity{Config: entity.EntityConfig{
			Name: "Posts", Table: "posts", PrimaryKey: []string{"code"},
			Fields: []schema.Field{{Name: "code", Type: schema.String, NoQuery: true}},
		}}
		examples := renderedText(t, sc.exampleTabs(e).String())
		for _, unwanted := range []string{`params.Set(`, "filters:", "/posts?"} {
			if strings.Contains(examples, unwanted) {
				t.Errorf("examples contain %q without a queryable field", unwanted)
			}
		}
	})
	t.Run("list control name", func(t *testing.T) {
		e := entity.Define("posts", entity.EntityConfig{Fields: []schema.Field{{Name: "sort", Type: schema.Bool}}})
		examples := renderedText(t, sc.exampleTabs(e).String())
		if !strings.Contains(examples, "sort%5Beq%5D=true") || strings.Contains(examples, "sort=") {
			t.Error("the sort field example must use an explicit filter key without a sort parameter")
		}
	})
}
