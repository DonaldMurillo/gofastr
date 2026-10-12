package access

import (
	"context"
	"testing"
)

// CanResourceExact: the Decider decides first; on abstain only a grant of
// the capability itself passes, never the Wildcard.
func TestCanResourceExactSkipsWildcard(t *testing.T) {
	policy := NewRolePolicy()
	if err := policy.Grant("root", Wildcard); err != nil {
		t.Fatal(err)
	}
	if err := policy.Grant("payer", "invoices:pay"); err != nil {
		t.Fatal(err)
	}
	ref := Ref{Type: "invoices", ID: "i1"}
	root := WithRoles(WithPolicy(context.Background(), policy), []string{"root"})
	payer := WithRoles(WithPolicy(context.Background(), policy), []string{"payer"})

	if !CanResource(root, "invoices:pay", ref) {
		t.Fatal("CanResource: the Wildcard should pass")
	}
	if CanResourceExact(root, "invoices:pay", ref) {
		t.Error("CanResourceExact: the Wildcard passed")
	}
	if !CanResourceExact(payer, "invoices:pay", ref) {
		t.Error("CanResourceExact: a named grant was refused")
	}
	allow := WithDecider(root, func(context.Context, []string, Permission, Ref) Decision { return DecisionAllow })
	if !CanResourceExact(allow, "invoices:pay", ref) {
		t.Error("CanResourceExact: a Decider allow was refused")
	}
	deny := WithDecider(payer, func(context.Context, []string, Permission, Ref) Decision { return DecisionDeny })
	if CanResourceExact(deny, "invoices:pay", ref) {
		t.Error("CanResourceExact: a Decider deny was overridden by the grant")
	}
	var nilCtx context.Context
	if CanResourceExact(nilCtx, "invoices:pay", ref) {
		t.Error("CanResourceExact: a nil context passed")
	}
}
