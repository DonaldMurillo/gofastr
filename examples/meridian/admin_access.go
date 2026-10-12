package main

import (
	"context"
	"database/sql"
	"log"

	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/access"
)

// meridianCapabilities are the permissions Meridian checks: the plan
// catalog's writes (entities/plans.go) and the state override on the
// two entities with a lifecycle. The admin's Roles page draws a row for
// each.
var meridianCapabilities = []access.Permission{
	"plans:write", "plans:admin",
	"invoices:override_state", "subscriptions:override_state",
}

// seedRoles declares the capabilities and the roles a billing team
// splits the back office into, beside admin's Wildcard. Grants made on
// the Roles page persist through adminGrantStore and win over these.
func seedRoles(p *access.RolePolicy) {
	p.Register(meridianCapabilities...)
	for role, perms := range map[string][]access.Permission{
		"billing": {"plans:write", "invoices:override_state", "subscriptions:override_state"},
		"support": {"subscriptions:override_state"},
	} {
		if err := p.Grant(role, perms...); err != nil {
			log.Fatalf("seed role %s: %v", role, err)
		}
	}
}

// adminGrantStore keeps the Roles page's grants and revokes in the app's
// database and loads them over the seeded roles at boot.
func adminGrantStore(db *sql.DB, p *access.RolePolicy) *access.GrantStore {
	if db == nil || p == nil {
		return nil
	}
	ctx := context.Background()
	s := framework.NewGrantStore(db, p)
	if err := s.EnsureSchema(ctx); err != nil {
		log.Fatalf("grant store: %v", err)
	}
	if err := s.LoadInto(ctx, p); err != nil {
		log.Fatalf("grant store: %v", err)
	}
	return s
}
