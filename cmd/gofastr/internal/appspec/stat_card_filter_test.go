package appspec

import (
	"strings"
	"testing"
)

// A stat_card's source.filter is a full DSL predicate that entityui
// parses on every render. A literal its column cannot hold (featured =
// "yes" on a bool) made the card draw "—" forever, and an enum value
// outside the field's values counted nothing. The load checks every
// comparison's literal with the entity_list pin's type and enum rules.
func statCardFilterAppSpec(t *testing.T, filter string) error {
	t.Helper()
	yaml := `
app:
  name: Demo
  module: example.com/demo
entities:
  - name: cards
    crud: true
    fields:
      - name: label
        type: string
      - name: featured
        type: bool
      - name: stage
        type: enum
        values: [draft, open]
      - name: amount
        type: float
screens:
  - name: dash
    route: /
    body:
      - kind: stat_card
        props:
          label: Cards
          source:
            entity: cards
            agg: count
            filter: '` + filter + `'
`
	return r5YAMLAppSpec(t, yaml)
}

func TestStatCardFilterAcceptsTypedValues(t *testing.T) {
	for _, ok := range []string{
		"stage=open",
		"featured=true",
		`featured = true and amount > 12.5`,
		`stage in ["draft", "open"] or label contains "x"`,
		`(stage != "draft") and amount <= 3`,
		// Columns and operators the check cannot judge pass through.
		`nosuch = "x" and stage = "closed"`,
		`stage ~ "x"`,
	} {
		if err := statCardFilterAppSpec(t, ok); err != nil {
			t.Errorf("filter %q was refused: %v", ok, err)
		}
	}
}

func TestStatCardFilterRefusesMistypedValue(t *testing.T) {
	for _, tc := range []struct{ filter, value string }{
		{"featured=yes", "yes"},
		{"stage=closed", "closed"},
		{`label = "a" and amount > "abc"`, "abc"},
		{`stage != "closed"`, "closed"},
		{`stage in ["open", "closed"]`, "closed"},
		{`label = "x" or (featured = "maybe")`, "maybe"},
	} {
		err := statCardFilterAppSpec(t, tc.filter)
		if err == nil {
			t.Errorf("filter %q was accepted; its column cannot hold %q", tc.filter, tc.value)
			continue
		}
		if !strings.Contains(err.Error(), tc.value) || !strings.Contains(err.Error(), "stat_card") {
			t.Errorf("filter %q: error does not name the stat_card value %q: %v", tc.filter, tc.value, err)
		}
	}
}
