package appspec

import (
	"strings"
	"testing"

	coreyaml "github.com/DonaldMurillo/gofastr/core/yaml"
)

// The entity_list pinned filter (R8-EX3-01) carries stat_card's spelling,
// `filter: field=value`, and gets the same load-time column guard: the
// pin reaches entityui's ListBuilder.Where, which pins field = value on
// every read inside the caller's scope, so a Hidden or no_query column
// would turn the rendered list into the one-bit oracle the stat_card
// guard closes.

func entityListFilterAppSpec(t *testing.T, filter string) error {
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
      - name: secret
        type: string
        hidden: true
      - name: amount
        type: float
screens:
  - name: dash
    route: /
    body:
      - kind: entity_list
        entity: cards
        fields: [label, amount]
        filter: ` + filter + `
`
	return r5YAMLAppSpec(t, yaml)
}

// r5YAMLAppSpec decodes and validates a full YAML document, so a fixture
// can carry its own entity fields.
func r5YAMLAppSpec(t *testing.T, yaml string) error {
	t.Helper()
	node, err := coreyaml.Parse(yaml)
	if err != nil {
		t.Fatalf("parse: %v\n%s", err, yaml)
	}
	bp, err := DecodeAppSpec(node)
	if err != nil {
		return err
	}
	return ValidateAppSpec(bp)
}

func TestEntityListFilterAcceptsQueryableColumn(t *testing.T) {
	if err := entityListFilterAppSpec(t, "label=hello"); err != nil {
		t.Fatalf("a pinned filter on an ordinary column was refused: %v", err)
	}
}

func TestEntityListFilterRefusesNoQueryColumn(t *testing.T) {
	err := entityListFilterAppSpec(t, "number=42")
	if err == nil || !strings.Contains(err.Error(), "no_query") {
		t.Fatalf("filter on a no_query column: %v", err)
	}
}

func TestEntityListFilterRefusesHiddenColumn(t *testing.T) {
	err := entityListFilterAppSpec(t, "secret=x")
	if err == nil || !strings.Contains(err.Error(), "hidden") {
		t.Fatalf("filter on a hidden column: %v", err)
	}
}

func TestEntityListFilterRefusesUnknownColumn(t *testing.T) {
	err := entityListFilterAppSpec(t, "nonesuch=x")
	if err == nil || !strings.Contains(err.Error(), "nonesuch") {
		t.Fatalf("filter on an unknown column: %v", err)
	}
}

func TestEntityListFilterRefusesMalformed(t *testing.T) {
	for _, bad := range []string{"label", "a=b=c", "=x", "label=", "label = x"} {
		if err := entityListFilterAppSpec(t, bad); err == nil {
			t.Errorf("filter %q was accepted; the spelling is exactly field=value", bad)
		}
	}
}

// The pin binds through crud's filter coercion at request time, so a
// value its column cannot hold (featured=yes on a bool) built an app
// whose list answered 400 on every load. The load refuses it instead,
// and an enum value outside the field's values, which would match no
// row, is refused the same way.
func entityListTypedFilterAppSpec(t *testing.T, filter string) error {
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
      - kind: entity_list
        entity: cards
        fields: [label]
        filter: ` + filter + `
`
	return r5YAMLAppSpec(t, yaml)
}

func TestEntityListFilterAcceptsTypedValues(t *testing.T) {
	for _, ok := range []string{"featured=true", "stage=open", "amount=12"} {
		if err := entityListTypedFilterAppSpec(t, ok); err != nil {
			t.Errorf("filter %q was refused: %v", ok, err)
		}
	}
}

func TestEntityListFilterRefusesMistypedValue(t *testing.T) {
	for _, bad := range []string{"featured=yes", "amount=abc", "stage=closed"} {
		err := entityListTypedFilterAppSpec(t, bad)
		if err == nil {
			t.Errorf("filter %q was accepted; its column cannot hold the value", bad)
			continue
		}
		_, value, _ := strings.Cut(bad, "=")
		if !strings.Contains(err.Error(), value) {
			t.Errorf("filter %q: error does not name the value: %v", bad, err)
		}
	}
}
