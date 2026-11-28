package appspec

import (
	"fmt"
	"strings"

	fwentity "github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/internal/piicheck"
)

type piiFinding struct {
	Entity string
	Fields []string
}

// Message names the entity, its PII-shaped fields and every remedy.
func (f piiFinding) Message() string {
	return fmt.Sprintf(
		"entity %q exposes PII-shaped field(s) %s via auto-CRUD/MCP with no scoping. Set owner_field: <column> for per-user rows, require a permission in access.read (RBAC), or set multi_tenant: true",
		f.Entity, strings.Join(f.Fields, ", "))
}

// LintUnscopedPII returns one finding per auto-exposed entity with PII-shaped fields and no scoping.
func LintUnscopedPII(bp AppSpec) []piiFinding {
	var out []piiFinding
	for _, decl := range bp.Entities {
		exposure := EntityDeclarationExposure(decl)
		scope := EntityDeclarationScope(decl)
		if !EntityDeclarationCRUDEnabled(decl) && !exposure.MCP {
			continue
		}
		access := exposure.Access
		if piicheck.Scoped(scope.OwnerField, scope.MultiTenant, access != nil && piicheck.GatesAccess(access.Read)) {
			continue
		}
		var pii []string
		for _, field := range decl.Fields {
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
