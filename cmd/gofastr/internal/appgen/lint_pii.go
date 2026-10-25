package appgen

import (
	"fmt"
	"slices"
	"strings"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
)

// unscopedFinding is one entity flagged by lintUnscopedEntities.
type unscopedFinding struct {
	Entity string
}

// Message spells out the exposure and every remedy. Unlike the PII rule
// this is informational: genuinely public data (a blog's posts) is a
// legitimate shape, but letting every OTHER authenticated user read and
// overwrite it almost never is, so the warning fires until the entity
// says how it's governed. This entity already requires a session for
// every operation (auto-CRUD's secure-by-default gate); the exposure
// here is cross-user, not anonymous: any signed-in caller can read,
// create, update, and delete any row.
func (f unscopedFinding) Message() string {
	return fmt.Sprintf(
		"entity %q is exposed via auto-CRUD/MCP with no per-user scoping: every authenticated user can read, create, update, and delete every OTHER user's row (a session is already required to reach it: this is cross-user exposure). Set owner_field: <column> for per-user rows, access: permissions (RBAC) to gate by role, or multi_tenant: true",
		f.Entity)
}

// publicFinding is one entity flagged by lintPublicEntities, a
// declared `public: true` opt-out. Unlike unscopedFinding this
// IS the anonymous-access surface: Public is a deliberate, full bypass of
// the session requirement (issue #65), not an oversight, so the message
// confirms the declaration rather than prescribing a remedy.
type publicFinding struct {
	Entity string
}

// Message names the entity and spells out exactly what "public" grants,
// anonymous READ and WRITE rather than read alone, so `gofastr generate`'s
// warning can't be mistaken for "this entity is merely readable".
func (f publicFinding) Message() string {
	return fmt.Sprintf(
		"entity %q is public: true, anonymous callers can read, create, update, AND delete every row (not just read). Confirm this is intentional; entities that want public reads with gated writes should use access: (a blank read: + a real create: permission) instead",
		f.Entity)
}

// frameworkModulePath is the module every generated app imports. An app spec
// whose own app.module sits UNDER this path claims a package namespace the
// framework module also provides, and the Go toolchain refuses the ambiguity:
//
//	ambiguous import: found package <path> in multiple modules
//
// The in-repo example app specs legitimately declare such paths; they are
// generated inside this module, where the local package wins. Copying one out
// to learn from it (the documented way to start from an example) produces a
// build that cannot be repaired by any go.mod edit, and the generator used to
// print `go mod init <that same colliding path>` as the remedy, steering the
// reader further in.
const frameworkModulePath = "github.com/DonaldMurillo/gofastr"

// moduleCollisionFinding is an app spec whose app.module collides with the
// framework's own module path.
type moduleCollisionFinding struct {
	Module string
}

func (f moduleCollisionFinding) Message() string {
	return fmt.Sprintf(
		"app.module %q sits inside the framework's own module (%s), so the generated code and the framework both claim that package path: `go build` fails with \"ambiguous import\" and no go.mod edit fixes it. This works only inside the GoFastr repo itself. Set app.module to a path you own (for example \"local/%s\" while experimenting, or your repo's path).",
		f.Module, frameworkModulePath, lastPathSegment(f.Module))
}

// LintModuleCollision reports an app spec whose app.module is the framework
// module or a subpath of it. Returns nil for every other module path.
func LintModuleCollision(bp appspec.AppSpec) []moduleCollisionFinding {
	m := strings.TrimSpace(bp.App.Module)
	if m != frameworkModulePath && !strings.HasPrefix(m, frameworkModulePath+"/") {
		return nil
	}
	return []moduleCollisionFinding{{Module: m}}
}

func lastPathSegment(module string) string {
	if i := strings.LastIndex(module, "/"); i >= 0 && i+1 < len(module) {
		return module[i+1:]
	}
	return module
}

// ownerUniqueFinding is one uniqueness rule on an owner-scoped entity that
// leaves the owner column out: a unique field, or a unique index whose
// columns do not include owner_field.
type ownerUniqueFinding struct {
	Entity     string
	OwnerField string
	Field      string // set for a field-level unique
	Index      string // set for a unique index
	Columns    []string
}

func (f ownerUniqueFinding) Message() string {
	what := fmt.Sprintf("field %q is unique: true", f.Field)
	cols := append([]string{f.OwnerField}, f.Field)
	if f.Index != "" {
		what = fmt.Sprintf("unique index %q on (%s) leaves out %s", f.Index, strings.Join(f.Columns, ", "), f.OwnerField)
		cols = append([]string{f.OwnerField}, f.Columns...)
	}
	return fmt.Sprintf(
		"entity %q is owner-scoped (owner_field: %s) but %s, so uniqueness spans every account: one user's row blocks another user's create, and the 409 tells them the value exists in someone else's rows. To scope it per owner, drop it and declare a unique index under indices: with columns [%s]. Keep it only if the value is meant to be unique across all accounts",
		f.Entity, f.OwnerField, what, strings.Join(cols, ", "))
}

// LintOwnerScopedUnique flags uniqueness that spans owners on an
// owner-scoped entity: a unique field other than owner_field itself, or a
// unique index whose columns do not include owner_field. Only values a
// caller can write count: a read_only field is server-assigned (an
// auto_generate: uuid order number), so no caller can collide with it or
// probe it, and an index whose columns are all read_only or undeclared
// (created_at) stays quiet too. A warning from `gofastr validate` and
// `gofastr generate`, never an error: a value unique across all accounts
// (a public handle) can be deliberate.
func LintOwnerScopedUnique(bp appspec.AppSpec) []ownerUniqueFinding {
	var out []ownerUniqueFinding
	for _, decl := range bp.Entities {
		owner := appspec.EntityDeclarationScope(decl).OwnerField
		if owner == "" {
			continue
		}
		writable := map[string]bool{}
		for _, f := range decl.Fields {
			if !f.ReadOnly && f.Name != owner {
				writable[f.Name] = true
			}
		}
		for _, f := range decl.Fields {
			if f.Unique && writable[f.Name] {
				out = append(out, ownerUniqueFinding{Entity: decl.Name, OwnerField: owner, Field: f.Name})
			}
		}
		for _, idx := range decl.Indices {
			if idx.Unique && !slices.Contains(idx.Columns, owner) &&
				slices.ContainsFunc(idx.Columns, func(c string) bool { return writable[c] }) {
				out = append(out, ownerUniqueFinding{Entity: decl.Name, OwnerField: owner, Index: idx.Name, Columns: idx.Columns})
			}
		}
	}
	return out
}

// lintPublicEntities returns one finding per app spec entity declaring
// `public: true`, the full, deliberate opt-out from auto-CRUD's
// secure-by-default session requirement. Every one of these is genuinely
// reachable by an anonymous caller, so `gofastr generate` always surfaces
// the list (never blocks: Public is an intentional declaration, not a
// mistake to error on).
func lintPublicEntities(bp appspec.AppSpec) []publicFinding {
	var out []publicFinding
	for _, decl := range bp.Entities {
		if appspec.EntityDeclarationExposure(decl).Public {
			out = append(out, publicFinding{Entity: decl.Name})
		}
	}
	return out
}

// lintUnscopedEntities returns one finding per auto-exposed entity with NO
// scoping mechanism at all: the superset of lintUnscopedPII that doesn't
// depend on field names. Warned at generate time; never blocks.
func lintUnscopedEntities(bp appspec.AppSpec) []unscopedFinding {
	var out []unscopedFinding
	for _, decl := range bp.Entities {
		exposure := appspec.EntityDeclarationExposure(decl)
		scope := appspec.EntityDeclarationScope(decl)
		if !appspec.EntityDeclarationCRUDEnabled(decl) && !exposure.MCP {
			continue
		}
		// Public: true already carries its own, more accurate warning
		// (lintPublicEntities): this entity requires no session at all,
		// so unscopedFinding.Message()'s "a session is already required"
		// claim would be false for it.
		if scope.OwnerField != "" || scope.MultiTenant || appspec.HasAccessGate(exposure.Access) || exposure.Public {
			continue
		}
		out = append(out, unscopedFinding{Entity: decl.Name})
	}
	return out
}
