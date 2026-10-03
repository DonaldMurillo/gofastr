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
// redispatch), let any caller through an operation the entity does not
// gate, and refuse a judged caller without the operation's permission.
func TestMCPToolGateBranches(t *testing.T) {
	ent := entity.Define("notes", entity.EntityConfig{
		Name: "notes", Table: "notes",
		Fields: []schema.Field{{Name: "title", Type: schema.String}},
		Exposure: &entity.ExposureConfig{
			Access: entity.AccessControl{Read: "", Create: "notes:write"},
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
		deny bool
	}{
		{"unresolved caller is left to the route", context.Background(), opCreate, false},
		{"ungated operation", as("viewer"), opRead, false},
		{"granted", as("editor"), opCreate, false},
		{"missing permission", as("viewer"), opCreate, true},
		{"no policy on the context is left to the route", user, opCreate, false},
		{"a Decider alone is enough to judge", access.WithDecider(user, func(context.Context, []string, access.Permission, access.Ref) access.Decision {
			return access.DecisionDeny
		}), opCreate, true},
	}
	for _, tc := range cases {
		err := ch.mcpToolGate(tc.op)(tc.ctx)
		if tc.deny != (err != nil) {
			t.Errorf("SECURITY: [authz] %s: gate err = %v, want deny=%v", tc.name, err, tc.deny)
		}
		if err != nil && !errors.Is(err, errMCPToolForbidden) {
			t.Errorf("%s: err = %v, want errMCPToolForbidden", tc.name, err)
		}
	}
}
