package openapi

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// invoicesStates is the fixture: status is the guarded enum, paid_on the
// stamp the pay move writes, mark_overdue a System move that appears
// nowhere.
func invoicesStates(advisory bool) *entity.Entity {
	return entity.Define("invoices", entity.EntityConfig{
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
				{Key: "pay", Label: "Pay", From: []string{"open"}, To: "paid", Stamp: "paid_on", Permission: "invoices:pay"},
				{Key: "void", From: []string{"draft", "open"}, To: "void", Variant: "danger"},
				{Key: "mark_overdue", From: []string{"open"}, To: "open", System: true},
			},
		},
	}.WithTimestamps(false))
}

// requestBodyProps digs the application/json request-body properties out
// of an operation's built map.
func requestBodyProps(t *testing.T, op map[string]any) map[string]any {
	t.Helper()
	body := getMap(t, op, "requestBody")
	content := getMap(t, body, "content")
	jsonMedia := getMap(t, content, "application/json")
	return getMap(t, jsonMedia, "schema")
}

func propEnum(v any) []string {
	m, ok := v.(map[string]any)
	if !ok {
		return nil
	}
	enum, _ := m["enum"].([]string)
	return enum
}

// The create body keeps the state field but narrowed to the initial
// values, and drops the stamp; update bodies drop both. A state field a
// create cannot even start at must not be offered.
func TestStatesNarrowWriteRequestSchemas(t *testing.T) {
	doc := EntityOpenAPI(reg(invoicesStates(false)), "Test", "1.0.0", nil).Build()
	paths := getMap(t, doc, "paths")

	create := getMap(t, paths, "/invoices")
	createSchema := requestBodyProps(t, getMap(t, create, "post"))
	if got := propEnum(createSchema["properties"].(map[string]any)["status"]); len(got) != 2 || got[0] != "draft" || got[1] != "open" {
		t.Errorf("create body status enum = %v, want [draft open]", got)
	}
	if _, has := createSchema["properties"].(map[string]any)["paidOn"]; has {
		t.Error("create body still offers the paid_on stamp; the move sets it, never a create")
	}

	for _, verb := range []string{"put", "patch"} {
		item := getMap(t, paths, "/invoices/{id}")
		s := requestBodyProps(t, getMap(t, item, verb))
		props := s["properties"].(map[string]any)
		if _, has := props["status"]; has {
			t.Errorf("%s body still offers the state field; it changes only through a move's route", verb)
		}
		if _, has := props["paidOn"]; has {
			t.Errorf("%s body still offers the paid_on stamp", verb)
		}
	}

	// The response schema is untouched: full enum, stamp readable.
	comps := getMap(t, doc, "components")
	schemas := getMap(t, comps, "schemas")
	inv := getMap(t, schemas, "invoices")
	respProps := getMap(t, inv, "properties")
	if got := propEnum(respProps["status"]); len(got) != 4 {
		t.Errorf("response status enum = %v, want all four values (responses keep the field readable)", got)
	}
	if _, has := respProps["paidOn"]; !has {
		t.Error("response schema lost the paid_on stamp; it stays a readable field")
	}
}

// One literal path per non-system move (never a {key} wildcard), with
// the operationId convention the CRUD verbs use, From → To and the stamp
// in the description, the content-type note, and the 409/415 contract.
// The System move appears nowhere.
func TestStatesDocumentTransitionOperations(t *testing.T) {
	doc := EntityOpenAPI(reg(invoicesStates(false)), "Test", "1.0.0", nil).Build()
	paths := getMap(t, doc, "paths")

	pay := getMap(t, paths, "/invoices/{id}/transitions/pay")
	post := getMap(t, pay, "post")
	if post["operationId"] != "pay_invoices" {
		t.Errorf("pay operationId = %v, want pay_invoices (verb-first, like create_invoices)", post["operationId"])
	}
	if post["summary"] != "Pay invoices" {
		t.Errorf("pay summary = %v, want the move's Label", post["summary"])
	}
	desc, _ := post["description"].(string)
	for _, want := range []string{"status", "open", "paid", "paid_on", "Content-Type: application/json"} {
		if !strings.Contains(desc, want) {
			t.Errorf("pay description %q misses %q", desc, want)
		}
	}
	responses, ok := post["responses"].(map[int]map[string]any)
	if !ok {
		t.Fatalf("pay responses is %T, want map[int]map[string]any", post["responses"])
	}
	for _, code := range []int{200, 403, 404, 409, 415} {
		if _, has := responses[code]; !has {
			t.Errorf("pay is missing documented response %d (have %d responses)", code, len(responses))
		}
	}
	if _, has := responses[422]; has {
		t.Error("pay documents 422; the route never answers it")
	}
	if _, has := paths["/invoices/{id}/transitions/{key}"]; has {
		t.Error("spec documents a {key} wildcard; each move is its own literal path")
	}
	if _, has := paths["/invoices/{id}/transitions/issue"]; !has {
		t.Errorf("issue move lost its path; emitted %v", mapKeys(paths))
	}
	if _, has := paths["/invoices/{id}/transitions/mark_overdue"]; has {
		t.Error("System move mark_overdue has a path; system moves appear nowhere")
	}

	for _, p := range []struct{ path, verb string }{
		{"/invoices", "post"},
		{"/invoices/{id}", "put"},
		{"/invoices/{id}", "patch"},
	} {
		op := getMap(t, getMap(t, paths, p.path), p.verb)
		resps, ok := op["responses"].(map[int]map[string]any)
		if !ok {
			t.Fatalf("%s %s responses is %T", p.verb, p.path, op["responses"])
		}
		if _, has := resps[422]; !has {
			t.Errorf("%s %s does not document the 422 StateError", p.verb, p.path)
		}
	}
}

// An Advisory entity keeps the field writable in every body (today's
// behaviour) but still documents the moves: the route is mounted for
// them too.
func TestStatesAdvisoryKeepsFieldListsMoves(t *testing.T) {
	doc := EntityOpenAPI(reg(invoicesStates(true)), "Test", "1.0.0", nil).Build()
	paths := getMap(t, doc, "paths")

	create := getMap(t, paths, "/invoices")
	createSchema := requestBodyProps(t, getMap(t, create, "post"))
	if got := propEnum(createSchema["properties"].(map[string]any)["status"]); len(got) != 4 {
		t.Errorf("advisory create status enum = %v, want the full value set", got)
	}
	item := getMap(t, paths, "/invoices/{id}")
	put := requestBodyProps(t, getMap(t, item, "put"))
	if _, has := put["properties"].(map[string]any)["status"]; !has {
		t.Error("advisory update body dropped the state field; Advisory keeps it writable")
	}
	if _, has := paths["/invoices/{id}/transitions/pay"]; !has {
		t.Error("advisory entity lost the pay move's path; the route is mounted for it")
	}
	op := getMap(t, getMap(t, paths, "/invoices"), "post")
	resps, ok := op["responses"].(map[int]map[string]any)
	if !ok {
		t.Fatalf("advisory create responses is %T", op["responses"])
	}
	if _, has := resps[422]; has {
		t.Error("advisory create documents 422; nothing refuses the write")
	}
}
