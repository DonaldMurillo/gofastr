package appspec

import (
	"fmt"
	"strings"
	"unicode"

	fwentity "github.com/DonaldMurillo/gofastr/framework/entity"
)

// piiFieldTokens are field-name tokens that suggest a column holds
// personally identifiable or secret data. Matching is per-token (split on
// `_`, `-`, digits, and camelCase boundaries), not substring, so
// "cardinality" does not trip "card".
var piiFieldTokens = map[string]bool{
	"email": true, "phone": true, "mobile": true, "address": true,
	"street": true, "ssn": true, "password": true, "passwd": true,
	"token": true, "secret": true, "card": true, "iban": true,
	"dob": true, "birthday": true, "birthdate": true, "passport": true,
	"salary": true,
}

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
		if scope.OwnerField != "" || scope.MultiTenant || HasAccessGate(exposure.Access) {
			continue
		}
		var pii []string
		for _, field := range decl.Fields {
			// FK columns typed `relation` reference PII rows, they don't
			// hold PII; the target entity is checked on its own.
			if strings.EqualFold(strings.TrimSpace(field.Type), "relation") {
				continue
			}
			if fieldLooksPII(field.Name) {
				pii = append(pii, field.Name)
			}
		}
		if len(pii) > 0 {
			out = append(out, piiFinding{Entity: decl.Name, Fields: pii})
		}
	}
	return out
}

// HasAccessGate reports whether the access declaration actually gates at
// least one operation: an access: map with only blank or anyone entries
// gates nothing and must not count as a remedy. anyone opens its operation
// to every caller, and a blank read beside it stays open too.
func HasAccessGate(a *fwentity.AccessDeclaration) bool {
	if a == nil {
		return false
	}
	for _, perm := range []string{a.Read, a.Create, a.Update, a.Delete} {
		if p := strings.TrimSpace(perm); p != "" && p != fwentity.Anyone {
			return true
		}
	}
	return false
}

func fieldLooksPII(name string) bool {
	for _, tok := range splitFieldTokens(name) {
		if piiFieldTokens[tok] {
			return true
		}
	}
	return false
}

// splitFieldTokens lowercases and splits a column name on `_`, `-`, `.`,
// digits, and lower→upper camelCase boundaries. Consecutive uppercase runs
// stay one token, so "userSSN" yields ["user", "ssn"].
func splitFieldTokens(name string) []string {
	var tokens []string
	var cur []rune
	flush := func() {
		if len(cur) > 0 {
			tokens = append(tokens, strings.ToLower(string(cur)))
			cur = nil
		}
	}
	prevLower := false
	for _, r := range name {
		switch {
		case unicode.IsLetter(r):
			if unicode.IsUpper(r) && prevLower {
				flush()
			}
			cur = append(cur, r)
			prevLower = unicode.IsLower(r)
		default:
			flush()
			prevLower = false
		}
	}
	flush()
	return tokens
}
