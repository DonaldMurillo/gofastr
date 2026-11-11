package packcmd

import (
	"reflect"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appgen"
)

// An entity form names each control twice: the wire key (camelCase,
// launchedOn) and the field id (field-launched_on). Pack read the wire
// key, so every multi-word field came back under a name the entity does
// not declare and the regenerated form dropped it. Each field type the
// form draws gets a multi-word field here, so a control kind that spells
// its name differently fails on its own.
const formFieldsSpec = `app:
  name: Forms
  module: example.com/forms
entities:
  - name: suppliers
    crud: true
    fields:
      - name: name
        type: string
  - name: stock_items
    crud: true
    fields:
      - name: display_name
        type: string
      - name: long_description
        type: text
      - name: stock_state
        type: enum
        values: [in_stock, sold_out]
      - name: main_supplier
        type: relation
        to: suppliers
      - name: is_featured
        type: bool
      - name: units_left
        type: int
      - name: shipping_weight
        type: float
      - name: unit_price
        type: decimal
      - name: launched_on
        type: date
      - name: restocked_at
        type: timestamp
screens:
  - name: home
    route: /
    title: Home
    body:
      - kind: entity_form
        entity: stock_items
        fields: [display_name, long_description, stock_state, main_supplier, is_featured, units_left, shipping_weight, unit_price, launched_on, restocked_at]
  - name: all_fields
    route: /items/new
    title: New item
    body:
      - kind: entity_form
        entity: stock_items
`

func TestPackFormKeepsMultiWordFields(t *testing.T) {
	spec, err := decodeAppSpecString(formFieldsSpec)
	if err != nil {
		t.Fatal(err)
	}
	packed, err := packAppSpec(materializeAppSpec(t, spec))
	if err != nil {
		t.Fatal(err)
	}
	want := spec.Screens[0].Body[0].Fields
	var got []string
	for _, screen := range packed.Screens {
		if screen.Name == "home" && len(screen.Body) == 1 {
			got = screen.Body[0].Fields
		}
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("packed form fields = %v, want %v", got, want)
	}

	yml, err := encodeAppSpecYAML(packed)
	if err != nil {
		t.Fatal(err)
	}
	back, err := decodeAppSpecString(yml)
	if err != nil {
		t.Fatal(err)
	}
	before, err := appgen.RenderAppSpecCode(spec)
	if err != nil {
		t.Fatal(err)
	}
	after, err := appgen.RenderAppSpecCode(back)
	if err != nil {
		t.Fatal(err)
	}
	rendered := map[string]string{}
	for _, f := range after {
		rendered[f.Name] = f.Content
	}
	for _, f := range before {
		if !strings.Contains(f.Name, "screen_") {
			continue
		}
		if rendered[f.Name] != f.Content {
			t.Errorf("%s differs after generate, pack, generate", f.Name)
		}
	}
}
