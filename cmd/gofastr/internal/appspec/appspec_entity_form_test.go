package appspec

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

// entityFormSpec is a tours entity with a required relation and no
// default, and one screen whose body is the given entity_form block.
// extra is appended to the entity (after its fields) and spec is
// appended at the top level.
func entityFormSpec(form, extra, spec string) string {
	return `
app:
  name: Demo
  module: ex.com/demo
entities:
  - name: properties
    crud: true
    fields:
      - name: title
        type: string
  - name: tours
    crud: true
    fields:
      - name: property_id
        type: relation
        to: properties
        required: true
      - name: visitor_name
        type: string
        required: true
      - name: duration_minutes
        type: int
        required: true
        default: 30
` + extra + `
screens:
  - name: schedule_tour
    route: /schedule-tour
    body:
` + form + spec
}

func loadEntityFormSpec(t *testing.T, src string) error {
	t.Helper()
	path := filepath.Join(t.TempDir(), "gofastr.yml")
	clitest.WriteTestFile(t, path, src)
	_, err := LoadAppSpec(path)
	return err
}

// A create form whose fields: leave out a required field with no default
// renders no control for it, so every submit answers 400. Validate
// refuses it and names the field.
func TestCreateFormMustCoverRequired(t *testing.T) {
	for name, form := range map[string]string{
		"create": `
      - kind: entity_form
        entity: tours
        mode: create
        fields: [visitor_name, duration_minutes]
`,
		"mode omitted": `
      - kind: entity_form
        entity: tours
        fields: [visitor_name]
`,
		"nested in a card": `
      - type: card
        children:
          - kind: entity_form
            entity: tours
            fields: [visitor_name]
`,
	} {
		t.Run(name, func(t *testing.T) {
			err := loadEntityFormSpec(t, entityFormSpec(form, "", ""))
			if err == nil {
				t.Fatal("a create form missing required property_id validated")
			}
			if !strings.Contains(err.Error(), `"property_id"`) {
				t.Fatalf("error should name property_id, got: %v", err)
			}
		})
	}
}

// The fields a create form may leave out: one with a default, one the
// server fills (owner, the state field, a before_create hook's
// entity), and every field of an edit form.
func TestCreateFormMayOmitServerSet(t *testing.T) {
	covered := `
      - kind: entity_form
        entity: tours
        fields: [property_id, visitor_name]
`
	omitsRelation := `
      - kind: entity_form
        entity: tours
        fields: [visitor_name]
`
	cases := map[string]struct{ form, extra, spec, route string }{
		"all required listed": {form: covered},
		"no fields list": {form: `
      - kind: entity_form
        entity: tours
`},
		"edit form": {form: `
      - kind: entity_form
        entity: tours
        mode: edit
        fields: [visitor_name]
`, route: "/tours/{id}/edit"},
		"before_create hook": {form: omitsRelation, spec: `
hooks:
  - id: pick_property
    entity: tours
    when: before_create
    handler: PickProperty
`},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			src := entityFormSpec(c.form, c.extra, c.spec)
			if c.route != "" {
				src = strings.Replace(src, "route: /schedule-tour", "route: "+c.route, 1)
			}
			if err := loadEntityFormSpec(t, src); err != nil {
				t.Fatalf("validate refused: %v", err)
			}
		})
	}

	// The owner column is stamped from the request; a required state
	// field takes its first initial value.
	serverSet := `
app:
  name: Demo
  module: ex.com/demo
  auth:
    enabled: true
entities:
  - name: notes
    crud: true
    owner_field: author_id
    fields:
      - name: author_id
        type: string
        required: true
      - name: body
        type: string
        required: true
      - name: status
        type: enum
        values: [draft, published]
        required: true
    states:
      field: status
      initial: [draft]
      transitions:
        - key: publish
          from: [draft]
          to: published
screens:
  - name: new_note
    route: /new-note
    body:
      - kind: entity_form
        entity: notes
        fields: [body]
`
	if err := loadEntityFormSpec(t, serverSet); err != nil {
		t.Fatalf("owner and state fields refused: %v", err)
	}
}
