package appgen

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework"
)

// Under enforced States the state field moves only through transitions:
// create keeps --status (a new record may start at an initial value),
// but update and patch must not offer a flag the server always refuses.
// The change table (<entity>ChangeFields) is where it drops, beside the
// key columns.
func TestRenderCLI_EnforcedStateLeavesChangeFlags(t *testing.T) {
	decl := framework.EntityDeclaration{
		Name: "invoices",
		Fields: []framework.FieldDeclaration{
			{Name: "number", Type: "string", Required: true},
			{Name: "status", Type: "enum", Values: []string{"draft", "open", "paid"}, Default: "draft"},
		},
		States: &framework.StatesConfig{Field: "status", Transitions: []framework.Transition{
			{Key: "issue", From: []string{"draft"}, To: "open"},
			{Key: "mark_paid", From: []string{"open"}, To: "paid"},
		}},
	}
	spec, err := buildCLISpec([]framework.EntityDeclaration{decl}, defaultCLIOptions(), "example.com/app/entities/client")
	if err != nil {
		t.Fatalf("buildCLISpec: %v", err)
	}
	out := map[string]string{}
	for _, f := range RenderCLIFiles(spec) {
		out[f.Name] = f.Content
	}
	inv := out["invoices.gen.go"]
	if inv == "" {
		t.Fatal("invoices.gen.go not rendered")
	}
	// Create keeps the flag: a new record may start at an initial value.
	if !strings.Contains(inv, `runCreateVerb("invoices create", "/invoices", invoicesMutationFields, args)`) {
		t.Errorf("create wiring missing:\n%s", inv)
	}
	if !strings.Contains(inv, `{flag: "status", wire: "status", kind: fieldString, usage: "status (enum) [draft|open|paid]"}`) {
		t.Errorf("the create table lost --status:\n%s", inv)
	}
	// Update and patch address the change table, which drops it.
	for _, w := range []string{
		`runUpdateVerb("invoices update", "/invoices", []string{"id"}, invoicesChangeFields, args)`,
		`runPatchVerb("invoices patch", "/invoices", []string{"id"}, invoicesChangeFields, args)`,
	} {
		if !strings.Contains(inv, w) {
			t.Errorf("change-table wiring missing %q:\n%s", w, inv)
		}
	}
	change := between(t, inv, "var invoicesChangeFields = []mutationField{", "}")
	if strings.Contains(change, `{flag: "status"`) {
		t.Errorf("the change table offers --status, a flag the enforced lifecycle always refuses:\n%s", change)
	}
	if !strings.Contains(change, `{flag: "number"`) {
		t.Errorf("the change table lost a writable field:\n%s", change)
	}
}
