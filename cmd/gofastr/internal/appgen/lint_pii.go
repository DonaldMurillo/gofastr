package appgen

import (
	"fmt"
	"slices"
	"strings"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
	fwentity "github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/internal/piicheck"
)

type unscopedFinding struct {
	Entity        string
	AnonymousRead bool
}

// Message names who can read the unscoped rows and the remedies that limit it.
func (f unscopedFinding) Message() string {
	readers := "every authenticated user"
	if f.AnonymousRead {
		readers = "anonymous callers"
	}
	return fmt.Sprintf(
		"entity %q exposes unscoped reads via auto-CRUD/MCP. %s can read every row. Set owner_field: <column> for per-user rows or require a permission in access.read (RBAC)",
		f.Entity, readers)
}

type publicFinding struct {
	Entity string
}

// Message names the entity and says that public grants anonymous reads and writes.
func (f publicFinding) Message() string {
	return fmt.Sprintf(
		"entity %q is public: true, anonymous callers can read, create, update, AND delete every row (not just read). Confirm this is intentional; entities that want public reads with gated writes should use access: (a blank read: + a real create: permission) instead",
		f.Entity)
}

const frameworkModulePath = "github.com/DonaldMurillo/gofastr"

type moduleCollisionFinding struct {
	Module string
}

func (f moduleCollisionFinding) Message() string {
	return fmt.Sprintf(
		"app.module %q sits inside the framework's own module (%s), so the generated code and the framework both claim that package path: `go build` fails with \"ambiguous import\" and no go.mod edit fixes it. This works only inside the GoFastr repo itself. Set app.module to a path you own (for example \"local/%s\" while experimenting, or your repo's path).",
		f.Module, frameworkModulePath, lastPathSegment(f.Module))
}

// LintModuleCollision reports an app spec whose app.module is the framework module or a subpath of it, and returns nil for any other module path.
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

type ownerUniqueFinding struct {
	Entity     string
	OwnerField string
	Field      string
	Index      string
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

// LintOwnerScopedUnique flags a writable unique field or unique index that spans owners on an owner-scoped entity.
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

func lintPublicEntities(bp appspec.AppSpec) []publicFinding {
	var out []publicFinding
	for _, decl := range bp.Entities {
		if appspec.EntityDeclarationExposure(decl).Public {
			out = append(out, publicFinding{Entity: decl.Name})
		}
	}
	return out
}

func lintUnscopedEntities(bp appspec.AppSpec) []unscopedFinding {
	var out []unscopedFinding
	for _, decl := range bp.Entities {
		exposure := appspec.EntityDeclarationExposure(decl)
		scope := appspec.EntityDeclarationScope(decl)
		if !appspec.EntityDeclarationCRUDEnabled(decl) && !exposure.MCP {
			continue
		}
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

type signedOutFinding struct {
	Entity  string
	Screens []string
}

func (f signedOutFinding) Message() string {
	return fmt.Sprintf(
		"screen %s draws entity %q, which only a signed-in caller can read, but the app spec turns on no sign-in (app.auth.enabled): every visitor gets \"Not available\" in its place. Set app.auth.enabled: true, or public: true on the entity if anyone may read and write it",
		strings.Join(f.Screens, ", "), f.Entity)
}

// LintSignedOutScreens flags a data block over an entity that needs a session in an app spec with app.auth off.
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
