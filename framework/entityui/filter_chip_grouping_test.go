package entityui

import (
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/dsl"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/filter"
)

// Removing a chip rebuilds the filter from the terms it keeps, and the
// rebuilt text must parse to exactly those terms: a group keeps its
// parentheses, so removing one term never regroups the others.
func TestChipRemovalKeepsGrouping(t *testing.T) {
	fields := ordersConfig().Fields
	parse := func(text string) *filter.Predicate {
		t.Helper()
		p, err := dsl.ParsePredicate(text, fields)
		if err != nil {
			t.Fatalf("parse %q: %v", text, err)
		}
		return p
	}
	for _, text := range []string{
		`(status = "open" or status = "paid") and amount > "100" and name = "x"`,
		`name = "y" and (status = "open" or (amount > "100" and (name = "x" or name = "z")))`,
	} {
		orig := parse(text)
		terms := topLevelTerms(orig)
		if len(terms) != len(orig.Children) {
			t.Fatalf("%q: %d chips for %d terms", text, len(terms), len(orig.Children))
		}
		for i := range terms {
			var kept []string
			var want []filter.Predicate
			for j := range terms {
				if j != i {
					kept = append(kept, terms[j])
					want = append(want, orig.Children[j])
				}
			}
			got := parse(strings.Join(kept, " and "))
			exp := &filter.Predicate{Children: want}
			if len(want) == 1 {
				exp = &want[0]
			}
			if !reflect.DeepEqual(got, exp) {
				t.Fatalf("%q without chip %d: %q parses to %+v, want %+v", text, i, strings.Join(kept, " and "), got, exp)
			}
		}
	}
	// A top-level or is one term: its sides are not and-ed chips.
	if terms := topLevelTerms(parse(`status = "open" or amount > "100"`)); len(terms) != 1 {
		t.Fatalf("an or filter shows %d chips, want 1: %q", len(terms), terms)
	}
}

// The query box keeps the terms a row cannot say; joined with and, an
// or group among them keeps its parentheses.
func TestQueryBoxKeepsGroupParens(t *testing.T) {
	x := newTestUI(t,
		map[string]entity.EntityConfig{"orders": ordersConfig()},
		map[string][]map[string]any{"orders": ordersRows()},
	)
	text := `(status = "open" or name = "zeta") and status in ["open", "paid"]`
	h := listHTML(t, x.ui.List("orders").QueryBox(), x.ctx("/orders", "?filter="+url.QueryEscape(text)))
	want := `value="(status = &quot;open&quot; or name = &quot;zeta&quot;) and status in [&quot;open&quot;, &quot;paid&quot;]"`
	if !strings.Contains(h, want) {
		t.Fatalf("the box lost the or group's parentheses, want %s in:\n%s", want, h)
	}
}
