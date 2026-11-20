package crud

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/core/mcp"
	"github.com/DonaldMurillo/gofastr/core/router"
)

func TestEntityMCPToolsCarryHints(t *testing.T) {
	srv := mcp.NewServer()
	if err := RegisterEntityMCPTools(srv, NewCrudHandler(invoicesEntity(false), nil), router.New()); err != nil {
		t.Fatal(err)
	}
	type hints struct{ readOnly, destructive, idempotent bool }
	want := map[string]hints{
		"invoices_list":   {true, false, true},
		"invoices_get":    {true, false, true},
		"invoices_create": {false, false, false},
		"invoices_update": {false, true, true},
		"invoices_delete": {false, true, true},
		"invoices_issue":  {false, true, false},
		"invoices_pay":    {false, true, false},
		"invoices_void":   {false, true, false},
	}
	tools := srv.ListTools()
	if len(tools) != len(want) {
		t.Fatalf("registered %d tools, want %d", len(tools), len(want))
	}
	for _, tool := range tools {
		w, ok := want[tool.Name]
		a := tool.Annotations
		if !ok || a == nil || a.ReadOnlyHint == nil || a.DestructiveHint == nil || a.IdempotentHint == nil || a.OpenWorldHint == nil {
			t.Errorf("%s: annotations %+v, want every hint set", tool.Name, a)
			continue
		}
		got := hints{*a.ReadOnlyHint, *a.DestructiveHint, *a.IdempotentHint}
		if got != w || *a.OpenWorldHint {
			t.Errorf("%s: hints %+v openWorld %v, want %+v and openWorld false", tool.Name, got, *a.OpenWorldHint, w)
		}
	}
}
