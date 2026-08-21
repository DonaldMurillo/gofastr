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

// group_by is the chart's LABEL. groupCounts reads rows raw, because the
// dashboard aggregates are meant to compute over stored values, and prints
// each distinct value as a bar or slice caption, so a masked column renders
// verbatim on the page while the API masks it. Not an oracle: the whole
// value set, on whatever route the screen sits on.
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
func TestSearchAcceptsFrameworkManagedColumns(t *testing.T) {
	for _, col := range []string{"id", "created_at", "updated_at", "label"} {
		err := r5Blueprint(t, `      - kind: entity_list
        entity: cards
        fields: [label]
        search: `+col+`
`)
		if err != nil {
			t.Errorf("search on %q should generate (it did before this validation existed): %v", col, err)
		}
	}
}

func TestSearchStillRefusesMaskedColumn(t *testing.T) {
	err := r5Blueprint(t, `      - kind: entity_list
        entity: cards
        fields: [label]
        search: number
`)
	if err == nil {
		t.Fatal("search on a no_query column was accepted")
	}
}

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
func TestSearchRejectsSystemColumnsTheEntityLacks(t *testing.T) {
	yaml := `
app:
  name: Demo
  module: example.com/demo
entities:
  - name: plain
    crud: true
    timestamps: false
    fields:
      - name: label
        type: string
screens:
  - name: dash
    route: /
    body:
      - kind: entity_list
        entity: plain
        fields: [label]
        search: created_at
`
	node, perr := coreyaml.Parse(yaml)
	if perr != nil {
		t.Fatalf("parse: %v", perr)
	}
	bp, err := decodeBlueprint(node)
	if err == nil {
		err = validateBlueprint(bp)
	}
	if err == nil {
		t.Fatal("search on created_at was accepted for an entity with timestamps: false; " +
			"the generated app then queries a column that does not exist")
	}
	if !strings.Contains(err.Error(), "created_at") {
		t.Fatalf("error should name the column: %v", err)
	}
}

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
func TestSearchAcceptsRelationsDeclaredForeignKey(t *testing.T) {
	yaml := `
app:
  name: Demo
  module: example.com/demo
entities:
  - name: authors
    crud: true
    fields:
      - name: name
        type: string
  - name: posts
    crud: true
    fields:
      - name: title
        type: string
    relations:
      - type: belongs_to
        name: author
        entity: authors
        foreign_key: author_id
screens:
  - name: dash
    route: /
    body:
      - kind: entity_list
        entity: posts
        fields: [title]
        search: author_id
`
	node, perr := coreyaml.Parse(yaml)
	if perr != nil {
		t.Fatalf("parse: %v", perr)
	}
	bp, err := decodeBlueprint(node)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if err := validateBlueprint(bp); err != nil {
		t.Fatalf("a relations-declared FK is a real column and was a working search target "+
			"before this validation existed: %v", err)
	}
}

// deleted_at exists only under soft_delete:, tenant_id only under
// multi_tenant:. Both arms of the system-column gate were unreachable from
// any test: replacing their bodies with a panic left the whole package
// green, so the round-6 bug was fixed for created_at and left live for its
// two siblings.
func TestScreenColumnsGateOnSoftDeleteAndTenancy(t *testing.T) {
	build := func(entityBody, search string) string {
		return `
app:
  name: Demo
  module: example.com/demo
entities:
  - name: things
    crud: true
    timestamps: false
` + entityBody + `    fields:
      - name: label
        type: string
screens:
  - name: dash
    route: /
    body:
      - kind: entity_list
        entity: things
        fields: [label]
        search: ` + search + `
`
	}
	run := func(t *testing.T, yaml string) error {
		t.Helper()
		node, err := coreyaml.Parse(yaml)
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		bp, err := decodeBlueprint(node)
		if err != nil {
			return err
		}
		return validateBlueprint(bp)
	}

	t.Run("deleted_at without soft_delete", func(t *testing.T) {
		if err := run(t, build("", "deleted_at")); err == nil {
			t.Fatal("search on deleted_at was accepted for an entity without soft_delete; the " +
				"generated app queries a column that does not exist")
		}
	})
	t.Run("deleted_at with soft_delete", func(t *testing.T) {
		if err := run(t, build("    soft_delete: true\n", "deleted_at")); err != nil {
			t.Fatalf("deleted_at is a real column under soft_delete: %v", err)
		}
	})
	t.Run("tenant_id without multi_tenant", func(t *testing.T) {
		if err := run(t, build("", "tenant_id")); err == nil {
			t.Fatal("search on tenant_id was accepted for an entity without multi_tenant")
		}
	})
}
