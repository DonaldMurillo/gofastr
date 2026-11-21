package appgen

import (
	"fmt"
	"slices"
	"strings"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
	fwentity "github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/internal/piicheck"
)

// unscopedFinding is one entity flagged by lintUnscopedEntities.
type unscopedFinding struct {
	Entity        string
	AnonymousRead bool
}

// Message names who can read unscoped rows and how to limit read access.
func (f unscopedFinding) Message() string {
	readers := "every authenticated user"
	if f.AnonymousRead {
		readers = "anonymous callers"
	}
	return fmt.Sprintf(
		"entity %q exposes unscoped reads via auto-CRUD/MCP. %s can read every row. Set owner_field: <column> for per-user rows, require a permission in access.read (RBAC), or set multi_tenant: true",
		f.Entity, readers)
}

// publicFinding is one entity explicitly opting into open reads and writes.
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

// lintUnscopedEntities warns on unscoped reads regardless of field names.
func lintUnscopedEntities(bp appspec.AppSpec) []unscopedFinding {
	var out []unscopedFinding
	for _, decl := range bp.Entities {
		exposure := appspec.EntityDeclarationExposure(decl)
		scope := appspec.EntityDeclarationScope(decl)
		if !appspec.EntityDeclarationCRUDEnabled(decl) && !exposure.MCP {
			continue
		}
		// Explicitly public entities have their own warning.
		access := exposure.Access
		if exposure.Public || piicheck.Scoped(scope.OwnerField, scope.MultiTenant, access != nil && piicheck.GatesAccess(access.Read)) {
			continue
		}
		out = append(out, unscopedFinding{
			Entity:        decl.Name,
			AnonymousRead: access != nil && fwentity.AccessControl(*access).Declared(),
		})
	}
	return out
}

// signedOutFinding is an entity a screen draws in an app spec that wires
// no sign-in.
type signedOutFinding struct {
	Entity  string
	Screens []string
}

func (f signedOutFinding) Message() string {
	return fmt.Sprintf(
		"screen %s draws entity %q, which only a signed-in caller can read, but the app spec turns on no sign-in (app.auth.enabled): every visitor gets \"Not available\" in its place. Set app.auth.enabled: true, or public: true on the entity if anyone may read and write it",
		strings.Join(f.Screens, ", "), f.Entity)
}

// LintSignedOutScreens flags a data block (entity_list, entity_detail,
// entity_form) over an entity that needs a session in an app spec with
// app.auth off. Nobody can sign in to such an app, so the generated
// screen renders the "Not available" callout to every visitor: the
// shortest spec there is (one entity, one list screen) generated an app
// whose list never drew and whose own axe gate failed. A warning from
// `gofastr validate` and `gofastr generate`, never an error: owned Go
// can wire its own sign-in after generation. An entity with an access:
// gate is left alone, since a blank read: there already opens its reads.
// A fragment (generate app --add) is judged by the project's auth
// (AppSpec.RouteApp), since its own app section is empty.
func LintSignedOutScreens(bp appspec.AppSpec) []signedOutFinding {
	if bp.RouteApp().Auth.Enabled {
		return nil
	}
	gated := map[string]bool{}
	for _, decl := range bp.Entities {
		exposure := appspec.EntityDeclarationExposure(decl)
		if !exposure.Public && !appspec.HasAccessGate(exposure.Access) {
			gated[decl.Name] = true
		}
	}
	screens := map[string][]string{}
	var order []string
	var walk func(screen string, blocks []appspec.AppSpecBlock)
	walk = func(screen string, blocks []appspec.AppSpecBlock) {
		for _, b := range blocks {
			if gated[b.Entity] && !slices.Contains(screens[b.Entity], screen) {
				if len(screens[b.Entity]) == 0 {
					order = append(order, b.Entity)
				}
				screens[b.Entity] = append(screens[b.Entity], screen)
			}
			walk(screen, b.Children)
		}
	}
	for _, s := range bp.Screens {
		walk(fmt.Sprintf("%q", s.Name), s.Body)
	}
	var out []signedOutFinding
	for _, name := range order {
		out = append(out, signedOutFinding{Entity: name, Screens: screens[name]})
	}
	return out
}
