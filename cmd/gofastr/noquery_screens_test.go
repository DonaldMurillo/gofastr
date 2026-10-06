package main

import (
	"strings"
	"testing"

	coreyaml "github.com/DonaldMurillo/gofastr/core/yaml"
)

// The screen-level column guards are the last line before a generated page
// prints a value the API masks. Each case here names a column in a different
// YAML position; a guard that only covers one of them is the shape that
// shipped three times already.

func r5Blueprint(t *testing.T, block string) error {
	t.Helper()
	yaml := `
app:
  name: Demo
  module: example.com/demo
entities:
  - name: cards
    crud: true
    timestamps: true
    fields:
      - name: label
        type: string
      - name: number
        type: string
        no_query: true
      - name: amount
        type: float
screens:
  - name: dash
    route: /
    body:
` + block
	node, err := coreyaml.Parse(yaml)
	if err != nil {
		t.Fatalf("parse: %v\n%s", err, yaml)
	}
	bp, err := decodeBlueprint(node)
	if err != nil {
		return err
	}
	return validateBlueprint(bp)
}

// group_by is the chart's LABEL. groupCounts prints each distinct stored
// value as a bar or slice caption, so a masked column renders verbatim on
// the page while the API masks it. Not an oracle: the whole value set, on
// whatever route the screen sits on.
func TestChartGroupByRefusesMaskedColumn(t *testing.T) {
	for _, kind := range []string{"bar_chart", "pie_chart", "line_chart"} {
		err := r5Blueprint(t, `      - kind: `+kind+`
        props:
          source:
            entity: cards
            group_by: number
`)
		if err == nil {
			t.Errorf("%s group_by on a no_query column was accepted; the chart renders each "+
				"stored value as a label", kind)
			continue
		}
		if !strings.Contains(err.Error(), "no_query") {
			t.Errorf("%s: error should name no_query, got: %v", kind, err)
		}
	}
}

// group_by was not checked as a COLUMN at all, so a typo produced a chart that
// silently grouped everything under one empty bucket.
func TestChartGroupByRefusesUnknownColumn(t *testing.T) {
	err := r5Blueprint(t, `      - kind: bar_chart
        props:
          source:
            entity: cards
            group_by: no_such_column
`)
	if err == nil {
		t.Fatal("bar_chart group_by accepted a column that does not exist on the entity")
	}
}

// agg: sum over a masked numeric renders its total. At one row the total IS
// the stored value.
func TestStatCardSumRefusesMaskedColumn(t *testing.T) {
	err := r5Blueprint(t, `      - kind: stat_card
        props:
          source:
            entity: cards
            agg: sum
            field: number
`)
	if err == nil {
		t.Fatal("stat_card agg:sum over a no_query column was accepted")
	}
}

// agg is StatValue's exact spelling, and a sum totals a numeric field:
// anything else renders "—" at runtime, so generation refuses it.
func TestStatCardAggIsExact(t *testing.T) {
	stat := func(agg, field string) error {
		return r5Blueprint(t, `      - kind: stat_card
        props:
          source:
            entity: cards
            agg: "`+agg+`"
            field: `+field+`
`)
	}
	for _, c := range [][2]string{{"SUM", "amount"}, {"avg", "amount"}, {" sum", "amount"}, {"sum", "label"}, {"sum", "created_at"}} {
		if err := stat(c[0], c[1]); err == nil {
			t.Errorf("stat_card agg %q field %q was accepted", c[0], c[1])
		}
	}
	for _, c := range [][2]string{{"sum", "amount"}, {"count", "label"}, {"", "label"}} {
		if err := stat(c[0], c[1]); err != nil {
			t.Errorf("stat_card agg %q field %q: %v", c[0], c[1], err)
		}
	}
}

// A queryable column has to keep working, or the guard is just breakage.
func TestChartGroupByAcceptsOrdinaryColumn(t *testing.T) {
	if err := r5Blueprint(t, `      - kind: bar_chart
        props:
          source:
            entity: cards
            group_by: label
`); err != nil {
		t.Fatalf("bar_chart on an ordinary column should generate: %v", err)
	}
}

// timestamps: true adds created_at/updated_at, which are therefore never in
// decl.Fields. The search guard rejected them as "not defined", which was
// both a regression and a false statement: these were working search columns.

// entity.Define panics on a Hidden or no_query keyset column. That is
// correct, since the cursor token carries its value. But a generated app
// that dies at boot is a far worse diagnostic than the error search_fields
// gets at decode time.
func TestCursorFieldRefusesMaskedColumnAtDecode(t *testing.T) {
	yaml := `
app:
  name: Demo
  module: example.com/demo
entities:
  - name: cards
    crud: true
    cursor_field: number
    fields:
      - name: number
        type: string
        no_query: true
`
	node, perr := coreyaml.Parse(yaml)
	if perr != nil {
		t.Fatalf("parse: %v", perr)
	}
	_, err := decodeBlueprint(node)
	if err == nil {
		t.Fatal("cursor_field naming a no_query column was accepted at decode; the failure " +
			"lands as a panic when the generated app boots")
	}
	if !strings.Contains(err.Error(), "cursor") {
		t.Fatalf("error should name the cursor field, got: %v", err)
	}
}

func TestCursorFieldsRefusesUnknownColumn(t *testing.T) {
	yaml := `
app:
  name: Demo
  module: example.com/demo
entities:
  - name: cards
    crud: true
    cursor_fields: [nope]
    fields:
      - name: label
        type: string
`
	node, perr := coreyaml.Parse(yaml)
	if perr != nil {
		t.Fatalf("parse: %v", perr)
	}
	if _, err := decodeBlueprint(node); err == nil {
		t.Fatal("cursor_fields accepted a column that does not exist")
	}
}

// blueprintFieldSystem answers "is this a system column NAME"; validating a
// screen needs "does this entity HAVE that column". created_at/updated_at
// exist only under timestamps:, deleted_at under soft_delete:, tenant_id under
// multi_tenant:, so accepting the name unconditionally let a screen reference
// a column the table lacks, turning a named generate-time error into a runtime
// SQL failure.

// The paired hidden branch of each round-5 guard: every existing test uses
// no_query, so hidden went unexercised.
func TestScreenGuardsRejectHiddenColumns(t *testing.T) {
	base := `
app:
  name: Demo
  module: example.com/demo
entities:
  - name: cards
    crud: true
    timestamps: true
    fields:
      - name: label
        type: string
      - name: secret
        type: string
        hidden: true
`
	cases := map[string]string{
		"chart group_by": `
screens:
  - name: dash
    route: /
    body:
      - kind: bar_chart
        props:
          source:
            entity: cards
            group_by: secret
`,
		"stat_card sum field": `
screens:
  - name: dash
    route: /
    body:
      - kind: stat_card
        props:
          source:
            entity: cards
            agg: sum
            field: secret
`,
	}
	for name, screens := range cases {
		t.Run(name, func(t *testing.T) {
			node, perr := coreyaml.Parse(base + screens)
			if perr != nil {
				t.Fatalf("parse: %v", perr)
			}
			bp, err := decodeBlueprint(node)
			if err == nil {
				err = validateBlueprint(bp)
			}
			if err == nil {
				t.Fatalf("%s on a hidden column was accepted", name)
			}
			if !strings.Contains(err.Error(), "hidden") {
				t.Fatalf("error should name hidden: %v", err)
			}
		})
	}
}

// The cursor decode guard's hidden branch.
func TestCursorFieldRejectsHiddenColumn(t *testing.T) {
	yaml := `
app:
  name: Demo
  module: example.com/demo
entities:
  - name: cards
    crud: true
    cursor_field: secret
    fields:
      - name: secret
        type: string
        hidden: true
`
	node, perr := coreyaml.Parse(yaml)
	if perr != nil {
		t.Fatalf("parse: %v", perr)
	}
	if _, err := decodeBlueprint(node); err == nil {
		t.Fatal("cursor_field naming a hidden column was accepted; it is not in the projection, " +
			"so paging cannot read it")
	}
}

// The third framework-managed class blueprintColumn's own comment names: a FK
// declared by a top-level relations: block rather than a type: relation field.

// deleted_at exists only under soft_delete:, tenant_id only under
// multi_tenant:. Both arms of the system-column gate were unreachable from
// any test: replacing their bodies with a panic left the whole package
// green, so the round-6 bug was fixed for created_at and left live for its
// two siblings.

// The entity-level search_fields guard is the screen search key's
// replacement: it names declared string/text columns only, so a column the
// entity does not have is refused at decode, not queried at runtime.
func TestSearchFieldsRefuseUndeclaredColumn(t *testing.T) {
	yaml := `
app:
  name: Demo
  module: example.com/demo
entities:
  - name: plain
    crud: true
    timestamps: false
    search_fields: [created_at]
    fields:
      - name: label
        type: string
`
	node, perr := coreyaml.Parse(yaml)
	if perr != nil {
		t.Fatalf("parse: %v", perr)
	}
	if _, err := decodeBlueprint(node); err == nil {
		t.Fatal("search_fields on created_at was accepted for an entity with timestamps: false; " +
			"the generated app then queries a column that does not exist")
	} else if !strings.Contains(err.Error(), "created_at") {
		t.Fatalf("error should name the column: %v", err)
	}
}
