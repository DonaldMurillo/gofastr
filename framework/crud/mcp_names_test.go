package crud

import (
	"slices"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/mcp"
	"github.com/DonaldMurillo/gofastr/core/router"
)

// MCPToolNames must answer exactly the names RegisterEntityMCPTools
// registers, in registration order: the five CRUD actions, then one per
// routable move under its own key, the flat spelling without a namespace
// and the namespaced one with it. A drift would tell an agent tool names
// that do not exist.
func TestMCPToolNamesMatchRegistration(t *testing.T) {
	for _, tc := range []struct {
		name      string
		namespace string
	}{
		{"flat", ""},
		{"namespaced", "v1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ent, db, r := covSimpleEntity(t)
			st := ent.Config.States
			if st != nil {
				t.Fatal("setup: covSimpleEntity unexpectedly declares states; pick a states fixture instead")
			}
			_ = st
			ch := NewCrudHandler(ent, db).WithJSONCase(CaseSnake)
			ch.MCPNamespace = tc.namespace
			RegisterCrudRoutes(r, ch, "/widgets")

			var registered []string
			srv := mcp.NewServer()
			srv.SetRegisterHook(func(name string) { registered = append(registered, name) })
			if err := RegisterEntityMCPTools(srv, ch, r); err != nil {
				t.Fatalf("register mcp: %v", err)
			}
			want := []string{
				"widgets_list", "widgets_get", "widgets_create", "widgets_update", "widgets_delete",
			}
			if tc.namespace != "" {
				want = []string{
					"v1.widgets.list", "v1.widgets.get", "v1.widgets.create", "v1.widgets.update", "v1.widgets.delete",
				}
			}
			if !slices.Equal(registered, want) {
				t.Fatalf("registered %v, want %v", registered, want)
			}
			if got := MCPToolNames(ch); !slices.Equal(got, want) {
				t.Errorf("MCPToolNames = %v, want %v", got, want)
			}
		})
	}
}

// A states entity names one tool per routable move, in declared order;
// System moves register none.
func TestMCPToolNamesListMoves(t *testing.T) {
	ent := invoicesEntity(false)
	ch := NewCrudHandler(ent, nil)
	want := []string{"invoices_list", "invoices_get", "invoices_create", "invoices_update", "invoices_delete", "invoices_issue", "invoices_pay", "invoices_void"}
	if got := MCPToolNames(ch); !slices.Equal(got, want) {
		t.Errorf("MCPToolNames = %v, want %v", got, want)
	}

	var registered []string
	srv := mcp.NewServer()
	srv.SetRegisterHook(func(name string) { registered = append(registered, name) })
	r := router.New()
	if err := RegisterEntityMCPTools(srv, ch, r); err != nil {
		t.Fatalf("register mcp: %v", err)
	}
	if !slices.Equal(registered, want) {
		t.Errorf("registered %v, want %v", registered, want)
	}
}

// A nil handler answers nil, not a panic.
func TestMCPToolNamesNil(t *testing.T) {
	if got := MCPToolNames(nil); got != nil {
		t.Errorf("MCPToolNames(nil) = %v, want nil", got)
	}
}
