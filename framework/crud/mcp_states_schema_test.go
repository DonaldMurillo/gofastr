package crud

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// invoicesEntity is the fixture: status is the guarded enum, paid_on the
// stamp the pay move writes, mark_overdue a System move.
func invoicesEntity(advisory bool) *entity.Entity {
	return entity.Define("invoices", entity.EntityConfig{
		Name:  "invoices",
		Table: "invoices",
		Fields: []schema.Field{
			{Name: "number", Type: schema.String, Required: true},
			{Name: "amount", Type: schema.Decimal},
			{Name: "status", Type: schema.Enum, Values: []string{"draft", "open", "paid", "void"}, Default: "draft"},
			{Name: "paid_on", Type: schema.Date},
		},
		States: &entity.StatesConfig{
			Field:    "status",
			Initial:  []string{"draft", "open"},
			Advisory: advisory,
			Transitions: []entity.Transition{
				{Key: "issue", From: []string{"draft"}, To: "open"},
				{Key: "pay", From: []string{"open"}, To: "paid", Stamp: "paid_on"},
				{Key: "void", From: []string{"draft", "open"}, To: "void"},
				{Key: "mark_overdue", From: []string{"open"}, To: "open", System: true},
			},
		},
	})
}

func schemaProps(t *testing.T, s map[string]any) map[string]any {
	t.Helper()
	props, ok := s["properties"].(map[string]any)
	if !ok {
		t.Fatalf("tool schema properties is %T, want map[string]any", s["properties"])
	}
	return props
}

// The create tool keeps the state field but narrowed to the values a
// create may set, and offers no stamp; the update tool offers neither.
// An agent told it can write status will burn a 422 otherwise.
func TestWriteToolSchemaNarrowsStates(t *testing.T) {
	create := writeToolSchema(invoicesEntity(false))
	props := schemaProps(t, create)
	status, ok := props["status"].(map[string]any)
	if !ok {
		t.Fatal("create tool dropped the state field entirely; a create may start at an initial value")
	}
	enum, _ := status["enum"].([]string)
	if len(enum) != 2 || enum[0] != "draft" || enum[1] != "open" {
		t.Errorf("create tool status enum = %v, want [draft open]", enum)
	}
	if _, has := props["paid_on"]; has {
		t.Error("create tool offers the paid_on stamp; the pay move sets it, never a create")
	}

	update := updateToolSchema(invoicesEntity(false))
	uprops := schemaProps(t, update)
	if _, has := uprops["status"]; has {
		t.Error("update tool offers the state field; it changes only through the move tools")
	}
	if _, has := uprops["paid_on"]; has {
		t.Error("update tool offers the paid_on stamp")
	}
	if _, has := uprops["id"]; !has {
		t.Error("update tool lost its id parameter")
	}
}

// Without enforced states the schemas are unchanged: Advisory keeps the
// field writable, and an entity with no states never narrows.
func TestWriteToolSchemaUnchangedWithoutEnforcement(t *testing.T) {
	advisory := writeToolSchema(invoicesEntity(true))
	props := schemaProps(t, advisory)
	status, _ := props["status"].(map[string]any)
	enum, _ := status["enum"].([]string)
	if len(enum) != 4 {
		t.Errorf("advisory create status enum = %v, want the full value set", enum)
	}

	plain := entity.Define("posts", entity.EntityConfig{
		Name:  "posts",
		Table: "posts",
		Fields: []schema.Field{
			{Name: "title", Type: schema.String, Required: true},
			{Name: "status", Type: schema.Enum, Values: []string{"draft", "live"}},
		},
	})
	plainProps := schemaProps(t, writeToolSchema(plain))
	status, _ = plainProps["status"].(map[string]any)
	enum, _ = status["enum"].([]string)
	if len(enum) != 2 {
		t.Errorf("no-states status enum = %v, want untouched [draft live]", enum)
	}
}
