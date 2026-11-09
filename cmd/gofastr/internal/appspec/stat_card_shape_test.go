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
