package framework

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/query"
)

// Property: one module, one schema. The §7 isolation model derives a
// dedicated Postgres schema + login role from the module's name; the REVOKE
// fence bounds each role to "its own" schema. That bound only isolates if
// DISTINCT module names derive DISTINCT schemas: two modules colliding onto
// one schema share one role, and either module's DDL (authenticated as that
// role) can read, alter, or drop the other's objects. Re-provisioning either
// also rotates the shared role's password.
//
// Reachability: module names are third-party-supplied (descriptor) and only
// validated against moduleIdentPattern `^[A-Za-z][A-Za-z0-9_-]*$`, which
// admits both '-' and '_'. moduleSchemaRole sanitizes every non-[a-z0-9]
// rune (including '-' after lowercasing) to '_', so "billing-1" and
// "billing_1" are different modules to the store and the SAME schema+role
// to Postgres. No collision check exists anywhere on the provisioning path.
//
// RED: distinct operator-approved module names collapse onto one schema/role.
func TestModuleSchemaNamesDoNotCollide(t *testing.T) {
	pairs := [][2]string{
		{"billing-1", "billing_1"},
		{"Widget", "widget"}, // case folds after lowercase
		{"A_b-c", "A-b_c"},   // hyphen/underscore mix
	}
	for _, pair := range pairs {
		sa, ra := moduleSchemaRole(pair[0])
		sb, rb := moduleSchemaRole(pair[1])
		if sa == sb || ra == rb {
			t.Errorf("modules %q and %q share schema %q / role %q — two distinct operator-approved modules land in one Postgres schema behind one role, and the §7 REVOKE fence then bounds them to EACH OTHER's objects",
				pair[0], pair[1], sa, ra)
		}
	}
}

// Property: provisioning grants nothing outside the module's own schema. The
// GRANT statements emitted for any module may only name the derived schema;
// every other privilege statement is a REVOKE. This is the GREEN anchor the
// collision test above depends on: for non-colliding names the fence holds,
// because no GRANT ever reaches another schema.
func TestModuleProvisionGrantsOnlyOwnSchema(t *testing.T) {
	for _, name := range []string{"demo", "Demo-App_2", "z"} {
		schema, role := moduleSchemaRole(name)
		stmts := moduleSchemaRoleStmts(schema, role, "feedface")
		for _, s := range stmts {
			if !strings.Contains(s, "GRANT") {
				continue
			}
			other := query.QuoteIdent("public")
			if strings.Contains(s, other) {
				t.Errorf("module %q: provisioning grants on a schema other than its own: %s", name, s)
			}
			if !strings.Contains(s, query.QuoteIdent(schema)) {
				t.Errorf("module %q: GRANT does not name the module's own schema: %s", name, s)
			}
		}
	}
}
