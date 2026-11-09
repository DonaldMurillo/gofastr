package appspec

import (
	"strings"
	"testing"
)

// The generated app runs entityui.CheckStat on every sourced stat_card
// at boot, and CheckStat refuses a count given a field or a format and a
// sum whose format is not "money". StatValue ignores those props, so the
// load let them through and the card drew a figure that was not the one
// asked for (a plain count where "money" or a sum was written). The load
// now refuses the same shapes, so a spec it passes never panics at boot.
func statCardShapeAppSpec(t *testing.T, source, format string) error {
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
`
	for _, kv := range strings.Split(source, ",") {
		yaml += "            " + strings.TrimSpace(kv) + "\n"
	}
	if format != "" {
		yaml += "          format: " + format + "\n"
	}
	return r5YAMLAppSpec(t, yaml)
}

func TestStatCardShapeMatchesBootCheck(t *testing.T) {
	for _, ok := range []struct{ source, format string }{
		{"entity: cards", ""},
		{"entity: cards, agg: count", ""},
		{"entity: cards, agg: sum, field: amount", ""},
		{"entity: cards, agg: sum, field: amount", "money"},
	} {
		if err := statCardShapeAppSpec(t, ok.source, ok.format); err != nil {
			t.Errorf("source %s format %q was refused: %v", ok.source, ok.format, err)
		}
	}
	for _, bad := range []struct{ source, format, want string }{
		{"entity: cards, agg: count, field: amount", "", "amount"},
		{"entity: cards, field: amount", "", "amount"},
		{"entity: cards, agg: count", "money", "money"},
		{"entity: cards, agg: sum, field: amount", "percent", "percent"},
	} {
		err := statCardShapeAppSpec(t, bad.source, bad.format)
		if err == nil {
			t.Errorf("source %s format %q was accepted; entityui.CheckStat refuses it at boot", bad.source, bad.format)
			continue
		}
		if !strings.Contains(err.Error(), "stat_card") || !strings.Contains(err.Error(), bad.want) {
			t.Errorf("source %s format %q: error does not name the stat_card and %q: %v", bad.source, bad.format, bad.want, err)
		}
	}
}

// The generated app runs entityui.CheckChart on every sourced chart at
// boot, and CheckChart refuses a group_by the database cannot group (a
// json field, a many-to-many relation) and an entity it does not know
// (one written as a route, "/cards"). The chart drew nothing for each,
// so the load refuses the same shapes.
func chartShapeAppSpec(t *testing.T, kind, entity, groupBy string) error {
	t.Helper()
	return r5YAMLAppSpec(t, `
app:
  name: Demo
  module: example.com/demo
entities:
  - name: tags
    crud: true
    fields:
      - name: label
        type: string
  - name: cards
    crud: true
    fields:
      - name: label
        type: string
      - name: status
        type: enum
        values: [draft, open]
      - name: meta
        type: json
      - name: tags
        type: relation
        to: tags
        many: true
screens:
  - name: dash
    route: /
    body:
      - kind: `+kind+`
        props:
          title: Cards
          source:
            entity: `+entity+`
            group_by: `+groupBy+`
`)
}

func TestChartShapeMatchesBootCheck(t *testing.T) {
	for _, kind := range []string{"bar_chart", "pie_chart", "line_chart"} {
		for _, groupBy := range []string{"label", "status", "created_at"} {
			if err := chartShapeAppSpec(t, kind, "cards", groupBy); err != nil {
				t.Errorf("%s group_by %q was refused: %v", kind, groupBy, err)
			}
		}
		for _, bad := range []struct{ entity, groupBy, want string }{
			{"cards", "meta", "meta"},
			{"cards", "tags", "tags"},
			{"/cards", "label", "/cards"},
		} {
			err := chartShapeAppSpec(t, kind, bad.entity, bad.groupBy)
			if err == nil {
				t.Errorf("%s entity %q group_by %q was accepted; entityui.CheckChart refuses it at boot", kind, bad.entity, bad.groupBy)
				continue
			}
			if !strings.Contains(err.Error(), kind) || !strings.Contains(err.Error(), bad.want) {
				t.Errorf("%s entity %q group_by %q: error does not name the chart and %q: %v", kind, bad.entity, bad.groupBy, bad.want, err)
			}
		}
	}
}

// A stat_card source entity written as a route is the same unknown
// entity to entityui.CheckStat.
func TestStatCardEntityIsAName(t *testing.T) {
	err := statCardShapeAppSpec(t, "entity: /cards", "")
	if err == nil || !strings.Contains(err.Error(), "/cards") {
		t.Fatalf("stat_card source entity /cards: err = %v, want a refusal naming it", err)
	}
}
