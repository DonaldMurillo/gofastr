package main

// Blueprint-level lint for CLAUDE.md hard rule #6: an entity holding
// per-user data must set OwnerField (or another scoping mechanism) before
// it is exposed via auto-CRUD/MCP. The blueprint generator cannot prove a
// field is per-user, so this is a heuristic over field NAMES: when an
// entity is auto-exposed (crud defaults on, or mcp: true), declares
// PII-shaped fields, and has no owner_field / multi_tenant / non-blank
// access, every row is readable and writable by every OTHER authenticated
// user on the generated API — cross-user exposure, not anonymous access.
// (Auto-CRUD itself is secure-by-default: an entity with none of
// owner_field/access/public already requires a session for every
// operation — see framework/crud's requireAuthenticated and
// EntityConfig.Public, issue #65. This lint's remaining concern is the
// narrower "logged-in user A can read/write user B's row" gap that only
// owner_field/access/multi_tenant close.) Blueprint auth alone does NOT
// close that gap: enabling auth only mounts pass-through
// SessionMiddleware — it authenticates the caller but does not scope rows
// to them.
//
// Severity by surface:
//   - `gofastr validate`   → error (exit 1)
//   - `gofastr generate`   → prominent warning, never blocks
//   - `gofastr audit lint` → finding (rule "unscoped-pii"), exit 1 like
//     the Go-source rules
//
// A SEPARATE lint (lintPublicEntities, same file) flags `public: true`
// entities — the actual anonymous-access surface post-#65, since Public
// is a deliberate full opt-out of the session requirement.

import (
	"fmt"
	"path/filepath"
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
		"entity %q exposes PII-shaped field(s) %s via auto-CRUD/MCP with no scoping — set owner_field: <column> for per-user rows, add access: permissions (RBAC), or set multi_tenant: true",
		f.Entity, strings.Join(f.Fields, ", "))
}

// lintUnscopedPII returns one finding per auto-exposed entity with
// PII-shaped fields and no scoping.
func lintUnscopedPII(bp Blueprint) []piiFinding {
	var out []piiFinding
	for _, decl := range bp.Entities {
		crudOn := decl.CRUD == nil || *decl.CRUD // blueprint CRUD defaults on
		if !crudOn && !decl.MCP {
			continue
		}
		if decl.OwnerField != "" || decl.MultiTenant || hasAccessGate(decl.Access) {
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

// unscopedFinding is one entity flagged by lintUnscopedEntities.
type unscopedFinding struct {
	Entity string
}

// Message spells out the exposure and every remedy. Unlike the PII rule
// this is informational: genuinely public data (a blog's posts) is a
// legitimate shape — but letting every OTHER authenticated user read and
// overwrite it almost never is, so the warning fires until the entity
// says how it's governed. This entity already requires a session for
// every operation (auto-CRUD's secure-by-default gate) — the exposure
// here is cross-user, not anonymous: any signed-in caller can read,
// create, update, and delete any row.
func (f unscopedFinding) Message() string {
	return fmt.Sprintf(
		"entity %q is exposed via auto-CRUD/MCP with no per-user scoping — every authenticated user can read, create, update, and delete every OTHER user's row (a session is already required to reach it — this is cross-user exposure). Set owner_field: <column> for per-user rows, access: permissions (RBAC) to gate by role, or multi_tenant: true",
		f.Entity)
}

// publicFinding is one entity flagged by lintPublicEntities — a
// blueprint-declared `public: true` opt-out. Unlike unscopedFinding this
// IS the anonymous-access surface: Public is a deliberate, full bypass of
// the session requirement (issue #65), not an oversight, so the message
// confirms the declaration rather than prescribing a remedy.
type publicFinding struct {
	Entity string
}

// Message names the entity and spells out exactly what "public" grants —
// anonymous READ and WRITE, not just read — so `gofastr generate`'s
// warning can't be mistaken for "this entity is merely readable".
func (f publicFinding) Message() string {
	return fmt.Sprintf(
		"entity %q is public: true — anonymous callers can read, create, update, AND delete every row (not just read). Confirm this is intentional; entities that want public reads with gated writes should use access: (a blank read: + a real create: permission) instead",
		f.Entity)
}

// lintPublicEntities returns one finding per blueprint entity declaring
// `public: true` — the full, deliberate opt-out from auto-CRUD's
// secure-by-default session requirement. Every one of these is genuinely
// reachable by an anonymous caller, so `gofastr generate` always surfaces
// the list (never blocks — Public is an intentional declaration, not a
// mistake to error on).
func lintPublicEntities(bp Blueprint) []publicFinding {
	var out []publicFinding
	for _, decl := range bp.Entities {
		if decl.Public {
			out = append(out, publicFinding{Entity: decl.Name})
		}
	}
	return out
}

// lintUnscopedEntities returns one finding per auto-exposed entity with NO
// scoping mechanism at all — the superset of lintUnscopedPII that doesn't
// depend on field names. Warned at generate time; never blocks.
func lintUnscopedEntities(bp Blueprint) []unscopedFinding {
	var out []unscopedFinding
	for _, decl := range bp.Entities {
		crudOn := decl.CRUD == nil || *decl.CRUD // blueprint CRUD defaults on
		if !crudOn && !decl.MCP {
			continue
		}
		// Public: true already carries its own, more accurate warning
		// (lintPublicEntities) — this entity requires no session at all,
		// so unscopedFinding.Message()'s "a session is already required"
		// claim would be false for it.
		if decl.OwnerField != "" || decl.MultiTenant || hasAccessGate(decl.Access) || decl.Public {
			continue
		}
		out = append(out, unscopedFinding{Entity: decl.Name})
	}
	return out
}

// hasAccessGate reports whether the access declaration actually gates at
// least one operation — an access: map with only blank entries gates
// nothing and must not count as a remedy.
func hasAccessGate(a *fwentity.AccessDeclaration) bool {
	if a == nil {
		return false
	}
	for _, perm := range []string{a.Read, a.Create, a.Update, a.Delete} {
		if strings.TrimSpace(perm) != "" {
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

// blueprintRootCandidates are the conventional blueprint file names probed
// by `gofastr audit lint` at the audited root. Arbitrary *.yml files are
// NOT decoded — a project root full of CI configs must not break the lint
// walk or masquerade as a blueprint.
var blueprintRootCandidates = []string{"gofastr.yml", "gofastr.yaml", "gofastr.json"}

// lintBlueprintPIIRoot adapts lintUnscopedPII to the audit-lint surface:
// it decodes the conventional blueprint file(s) at root (silently skipping
// files that do not parse — `gofastr validate` owns those errors), merges
// them so the lint sees the whole declared app, and attributes each
// finding to the file declaring the entity.
func lintBlueprintPIIRoot(root string) []LintFinding {
	var merged Blueprint
	fileOf := map[string]string{}
	found := false
	for _, name := range blueprintRootCandidates {
		bp, err := decodeBlueprintFile(filepath.Join(root, name))
		if err != nil {
			continue
		}
		found = true
		for _, decl := range bp.Entities {
			if fileOf[decl.Name] == "" {
				fileOf[decl.Name] = name
			}
		}
		merged = mergeBlueprints(merged, bp)
	}
	if !found {
		return nil
	}
	var out []LintFinding
	for _, f := range lintUnscopedPII(merged) {
		file := fileOf[f.Entity]
		if file == "" {
			file = blueprintRootCandidates[0]
		}
		out = append(out, LintFinding{
			File:    file,
			Line:    1,
			Rule:    "unscoped-pii",
			Message: f.Message(),
		})
	}
	return out
}
