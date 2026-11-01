package entityui

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/dsl"
	"github.com/DonaldMurillo/gofastr/framework/filter"
)

// A value holding a backslash must survive both filter-text writers: a
// row term (rowTerms) and a rebuilt term (predicateText). Quoting only
// the double quote left `C:\reports` unparseable, and the list then
// showed unfiltered.
func TestFilterTextKeepsBackslash(t *testing.T) {
	fields := []schema.Field{{Name: "path", Type: schema.String}}
	m := &meta{byName: map[string]schema.Field{"path": fields[0]}}
	for _, v := range []string{`C:\reports`, `a"b`, `ends\`, `\"`, `x\\y`} {
		row := rowTerms(m, []string{"path"}, []string{"="}, []string{v})
		if len(row) != 1 {
			t.Fatalf("rowTerms dropped %q: %v", v, row)
		}
		for name, text := range map[string]string{
			"row":  row[0],
			"term": predicateText(&filter.Predicate{Field: "path", Op: filter.OpEq, Value: v}),
		} {
			p, err := dsl.ParsePredicate(text, fields)
			if err != nil {
				t.Errorf("%s %q: %s does not parse: %v", name, v, text, err)
				continue
			}
			if p.Value != v {
				t.Errorf("%s %q: parsed back as %q", name, v, p.Value)
			}
		}
	}
}
