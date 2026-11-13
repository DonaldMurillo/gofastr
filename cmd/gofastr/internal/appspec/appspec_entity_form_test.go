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

// The form generator draws a control only for a fields: entry that names
// a drawable field exactly, so an entry it would drop without a word is
// refused at load, naming the entry.
func TestFormFieldsEntryMustDraw(t *testing.T) {
	hidden := `
      - name: id
        type: string
      - name: notes
        type: string
        hidden: true
      - name: code
        type: string
        read_only: true
      - name: ref
        type: uuid
        auto_generate: uuid`
	for name, c := range map[string]struct{ form, entry string }{
		"typo": {form: `
      - kind: entity_form
        entity: tours
        fields: [property_id, visitor_name, visitor_emial]
`, entry: "visitor_emial"},
		"typo on edit": {form: `
      - kind: entity_form
        entity: tours
        mode: edit
        fields: [visitor_nmae]
`, entry: "visitor_nmae"},
		"hidden": {form: `
      - kind: entity_form
        entity: tours
        fields: [property_id, visitor_name, notes]
`, entry: "notes"},
		"read_only on create": {form: `
      - kind: entity_form
        entity: tours
        fields: [property_id, visitor_name, code]
`, entry: "code"},
		"auto_generate on create": {form: `
      - kind: entity_form
        entity: tours
        fields: [property_id, visitor_name, ref]
`, entry: "ref"},
		"system column on create": {form: `
      - kind: entity_form
        entity: tours
        fields: [property_id, visitor_name, id]
`, entry: "id"},
		"listed twice": {form: `
      - kind: entity_form
        entity: tours
        fields: [property_id, visitor_name, visitor_name]
`, entry: "visitor_name"},
	} {
		t.Run(name, func(t *testing.T) {
			src := entityFormSpec(c.form, hidden, "")
			if strings.Contains(c.form, "mode: edit") {
				src = strings.Replace(src, "route: /schedule-tour", "route: /tours/{id}/edit", 1)
			}
			err := loadEntityFormSpec(t, src)
			if err == nil {
				t.Fatalf("fields entry %q validated", c.entry)
			}
			if !strings.Contains(err.Error(), "entity_form field "+`"`+c.entry+`"`) {
				t.Fatalf("error should name entry %q, got: %v", c.entry, err)
			}
		})
	}
}

// A node block hands its subtree to the node renderer, which draws an
// entity screen or an auth form as an "unknown kind" comment. Validate
// refuses each one under a node block, at any depth.
func TestNodeBlockRefusesEntityAndAuth(t *testing.T) {
	for name, form := range map[string]string{
		"entity_form in div": `
      - type: div
        children:
          - kind: entity_form
            entity: tours
`,
		"entity_list in header": `
      - type: header
        children:
          - kind: entity_list
            entity: tours
            fields: [visitor_name]
`,
		"entity_detail in type section": `
      - type: section
        children:
          - kind: entity_detail
            entity: tours
`,
		"login_form deep in article": `
      - type: article
        children:
          - type: div
            children:
              - kind: login_form
`,
		"signup_form spelled type": `
      - type: div
        children:
          - type: signup_form
`,
	} {
		t.Run(name, func(t *testing.T) {
			src := entityFormSpec(form, "", "")
			if strings.Contains(form, "entity_detail") {
				src = strings.Replace(src, "route: /schedule-tour", "route: /tours/{id}", 1)
			}
			err := loadEntityFormSpec(t, src)
			if err == nil {
				t.Fatal("an entity or auth block under a node block validated")
			}
			if !strings.Contains(err.Error(), "node blocks draw only node kinds") {
				t.Fatalf("want the node-block refusal, got: %v", err)
			}
		})
	}
}
