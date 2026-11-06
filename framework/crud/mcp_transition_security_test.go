package crud

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/mcp"
	"github.com/DonaldMurillo/gofastr/framework/access"
)

func TestMCPMovesRequireOwnPermission(t *testing.T) {
	crud, db := statesWorld(t)
	crud.Entity.Config.Exposure.Access.Update = "invoices:update"
	for index := range crud.Entity.Config.States.Transitions {
		move := &crud.Entity.Config.States.Transitions[index]
		if move.Key == "void" || move.Key == "pay" {
			move.Permission = "invoices:" + move.Key
		}
	}
	policy := access.NewRolePolicy()
	for role, permissions := range map[string][]access.Permission{
		"editor": {"invoices:update"}, "voider": {"invoices:update", "invoices:void"},
		"payer": {"invoices:update", "invoices:pay"}, "wildcard": {access.Wildcard},
		"no_update": {"invoices:void"}, "all": {access.Wildcard, "invoices:void", "invoices:pay"},
	} {
		if err := policy.Grant(role, permissions...); err != nil {
			t.Fatal(err)
		}
	}
	server := mcp.NewServer()
	router := statesRouter(crud)
	dispatched := 0
	if err := RegisterEntityMCPTools(server, crud, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		dispatched++
		router.ServeHTTP(writer, request)
	})); err != nil {
		t.Fatal(err)
	}
	for index, entry := range []struct {
		role   string
		scopes []string
		moves  []string
	}{
		{"editor", nil, nil}, {"voider", nil, []string{"void"}},
		{"payer", nil, []string{"pay"}}, {"wildcard", nil, nil},
		{"no_update", nil, nil}, {"all", nil, []string{"void", "pay"}},
		{"all", []string{"invoices:update"}, nil},
		{"all", []string{"invoices:update", "invoices:void"}, []string{"void"}},
	} {
		ctx := handler.SetUser(context.Background(), struct{ ID string }{ID: "caller"})
		ctx = access.WithRoles(access.WithPolicy(ctx, policy), []string{entry.role})
		if entry.scopes != nil {
			ctx = access.WithHeldScopes(ctx, entry.scopes)
		}
		tools, err := server.ListToolsFor(ctx)
		if err != nil {
			t.Fatal(err)
		}
		visible := map[string]bool{}
		for _, tool := range tools {
			visible[tool.Name] = true
		}
		for _, move := range []string{"void", "pay"} {
			want := slices.Contains(entry.moves, move)
			if visible["invoices_"+move] != want {
				t.Errorf("role %s scopes %v: %s visibility = %t, want %t", entry.role, entry.scopes, move, visible["invoices_"+move], want)
			}
			id := fmt.Sprintf("%s-%s-%d", entry.role, move, index)
			seedStateInvoice(t, db, id, "open", nil)
			before := dispatched
			_, err = server.CallTool(ctx, "invoices_"+move, map[string]any{"id": id})
			if want != (err == nil) {
				t.Errorf("role %s scopes %v: %s call error = %v, want allowed=%t", entry.role, entry.scopes, move, err, want)
			}
			if !want && dispatched != before {
				t.Errorf("role %s scopes %v: forbidden %s reached the route", entry.role, entry.scopes, move)
			}
			state, _ := readStateInvoice(t, db, id)
			if !want && state != "open" {
				t.Errorf("forbidden move changed state to %s", state)
			}
			if want && state == "open" {
				t.Error("allowed move did not change the stored state")
			}
		}
	}
}

func TestMCPMoveDefersRecordPolicy(t *testing.T) {
	crud, db := statesWorld(t)
	crud.Entity.Config.Exposure.Access.Update = "invoices:update"
	for index := range crud.Entity.Config.States.Transitions {
		crud.Entity.Config.States.Transitions[index].Permission = "invoices:move"
	}
	ctx := handler.SetUser(context.Background(), struct{ ID string }{ID: "caller"})
	ctx = access.WithDecider(ctx, perRecord)
	server := mcp.NewServer()
	if err := RegisterEntityMCPTools(server, crud, statesRouter(crud)); err != nil {
		t.Fatal(err)
	}
	tools, err := server.ListToolsFor(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(tools, func(tool mcp.Tool) bool { return tool.Name == "invoices_void" }) {
		t.Fatal("per-record permission hid a potentially allowed move")
	}
	for _, id := range []string{"shared", "private"} {
		seedStateInvoice(t, db, id, "open", nil)
		_, err := server.CallTool(ctx, "invoices_void", map[string]any{"id": id})
		if (id == "shared") != (err == nil) {
			t.Errorf("record %s: move error = %v", id, err)
		}
	}
}
