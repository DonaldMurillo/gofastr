package appspec

import (
	"fmt"
	"strings"

	fwentity "github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/internal/piicheck"
)

// piiFinding is one entity flagged by lintUnscopedPII.
type piiFinding struct {
	Entity string
	Fields []string
}

// Message names the entity, the PII-shaped fields, and every remedy.
// Enabling auth is deliberately NOT listed: SessionMiddleware is
// pass-through, so auth alone leaves the rows world-readable.
func (f piiFinding) Message() string {
	return fmt.Sprintf(
		"entity %q exposes PII-shaped field(s) %s via auto-CRUD/MCP with no scoping. Set owner_field: <column> for per-user rows, add access: permissions (RBAC), or set multi_tenant: true",
		f.Entity, strings.Join(f.Fields, ", "))
}

// LintUnscopedPII returns one finding per auto-exposed entity with
// PII-shaped fields and no scoping.
func LintUnscopedPII(bp AppSpec) []piiFinding {
	var out []piiFinding
	for _, decl := range bp.Entities {
		exposure := EntityDeclarationExposure(decl)
		scope := EntityDeclarationScope(decl)
		if !EntityDeclarationCRUDEnabled(decl) && !exposure.MCP {
			continue
		}
		if piicheck.Scoped(scope.OwnerField, scope.MultiTenant, HasAccessGate(exposure.Access)) {
			continue
		}
		var pii []string
		for _, field := range decl.Fields {
			// FK columns typed `relation` reference PII rows, they don't
			// hold PII; the target entity is checked on its own.
			if strings.EqualFold(strings.TrimSpace(field.Type), "relation") {
				continue
			}
			if piicheck.Field(field.Name) {
				pii = append(pii, field.Name)
			}
		}
		if len(pii) > 0 {
			out = append(out, piiFinding{Entity: decl.Name, Fields: pii})
		}
	}
	return out
}

// HasAccessGate reports whether the access declaration gates at least one operation.
func HasAccessGate(a *fwentity.AccessDeclaration) bool {
	if a == nil {
		return false
	}
	for _, perm := range []string{a.Read, a.Create, a.Update, a.Delete} {
		if piicheck.GatesAccess(perm) {
			return true
		}
	}
	return false
}
