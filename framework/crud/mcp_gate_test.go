package crud

import (
	"context"
	"errors"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/access"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// mcpToolGate answers each entity tool's listing and call precondition. It
// must leave to the route a caller it cannot judge (no user, or no policy
// and no Decider on the context: a group-scoped policy runs only on the
// redispatch; or an item tool with a Decider, which may answer per record),
// let any caller through an operation the entity does not gate, and refuse
// a judged caller without the operation's permission.
func TestMCPToolGateBranches(t *testing.T) {
	ent := entity.Define("notes", entity.EntityConfig{
		Name: "notes", Table: "notes",
		Fields: []schema.Field{{Name: "title", Type: schema.String}},
		Exposure: &entity.ExposureConfig{
			Access: entity.AccessControl{Read: "", Create: "notes:write", Update: "notes:write"},
		},
	})
	ch := NewCrudHandler(ent, nil)

	policy := access.NewRolePolicy()
	policy.Grant("editor", "notes:write")
	user := handler.SetUser(context.Background(), struct{ ID string }{ID: "u1"})
	as := func(role string) context.Context {
		return access.WithRoles(access.WithPolicy(user, policy), []string{role})
	}

	cases := []struct {
		name string
		ctx  context.Context
		op   crudOp
		item bool
		deny bool
	}{
		{"unresolved caller is left to the route", context.Background(), opCreate, false, false},
		{"a policy without a user is left to the route", access.WithRoles(access.WithPolicy(context.Background(), policy), []string{"viewer"}), opCreate, false, false},
		{"ungated operation", as("viewer"), opRead, false, false},
		{"granted", as("editor"), opCreate, false, false},
		{"missing permission", as("viewer"), opCreate, false, true},
		{"no policy on the context is left to the route", user, opCreate, false, false},
		{"a Decider alone is enough to judge", access.WithDecider(user, denyAll), opCreate, false, true},
		{"an item tool under a role policy is judged", as("viewer"), opUpdate, true, true},
		{"an item tool with a Decider is left to the route", access.WithDecider(as("viewer"), perRecord), opUpdate, true, false},
	}
	for _, tc := range cases {
		err := ch.mcpToolGate(tc.op, tc.item)(tc.ctx)
		if tc.deny != (err != nil) {
			t.Errorf("SECURITY: [authz] %s: gate err = %v, want deny=%v", tc.name, err, tc.deny)
		}
		if err != nil && !errors.Is(err, errMCPToolForbidden) {
			t.Errorf("%s: err = %v, want errMCPToolForbidden", tc.name, err)
		}
	}

	// A grouped entity's route runs the group's middleware, which may
	// install another policy: the same refused caller is left to the route.
	grouped := NewCrudHandler(ent, nil)
	grouped.MCPRouteScoped = true
	if err := grouped.mcpToolGate(opCreate, false)(as("viewer")); err != nil {
		t.Errorf("SECURITY: [authz] a route-scoped entity was judged on the /mcp policy: %v", err)
	}
}

func denyAll(context.Context, []string, access.Permission, access.Ref) access.Decision {
	return access.DecisionDeny
}

// perRecord refuses the entity as a whole and allows one record, as a
// Decider granting a shared document does; the route asks with the ID.
func perRecord(_ context.Context, _ []string, _ access.Permission, r access.Ref) access.Decision {
	if r.ID == "shared" {
		return access.DecisionAllow
	}
	return access.DecisionDeny
}
