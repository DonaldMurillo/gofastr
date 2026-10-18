package analyzers_test

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/contracts"
)

// permApp is a main package that registers caps and runs body.
func permApp(register, body string) string {
	return `package main

import (
	"context"

	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/access"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

var _ = entity.EntityConfig{}
var _ = framework.AccessControl{}

func wire(ctx context.Context, p *access.RolePolicy) {
	` + register + `
	` + body + `
}
`
}

// A typo in a checked permission compiles and denies everyone but a
// wildcard holder, so an admin testing the screen sees it work.
func TestUnregisteredPermInAccessBlock(t *testing.T) {
	ds := fixture(t, map[string]string{"main.go": permApp(
		`p.Register("plans:write", "plans:admin")`,
		`_ = framework.AccessControl{Create: "plans:wirte", Delete: "plans:admin"}`,
	)})
	d := assertHas(t, ds, contracts.RuleUnregisteredPermission)
	if !strings.Contains(d.Message, `"plans:wirte"`) || !strings.Contains(d.Message, `"plans:write"`) {
		t.Errorf("message should name the typo and the nearest capability: %q", d.Message)
	}
	if n := len(countRule(t, ds, contracts.RuleUnregisteredPermission)); n != 1 {
		t.Errorf("findings = %d, want 1 (plans:admin is registered)", n)
	}
}

func TestUnregisteredPermInChecks(t *testing.T) {
	for _, check := range []string{
		`_ = access.RequirePermission("plans:wirte")`,
		`_ = access.Can(ctx, "plans:wirte")`,
		`_ = access.CanResource(ctx, "plans:wirte", access.Ref{})`,
		`_ = entity.Transition{Permission: "plans:wirte"}`,
		`_ = entity.ScopeConfig{CrossOwnerRead: "plans:wirte"}`,
		`_ = entity.ReadScopeConfig{Unrestricted: "plans:wirte"}`,
	} {
		t.Run(check, func(t *testing.T) {
			ds := fixture(t, map[string]string{"main.go": permApp(`p.Register("plans:write")`, check)})
			assertHas(t, ds, contracts.RuleUnregisteredPermission)
		})
	}
}

// Meridian registers through a package-level slice, and a constant is
// the spelling an app reaches for once it wants one name per capability.
func TestRegisteredPermViaVarAndConst(t *testing.T) {
	ds := fixture(t, map[string]string{
		"main.go": permApp(`p.Register(caps...)
	p.Register(PlansAdmin)`,
			`_ = framework.AccessControl{Create: "plans:write", Delete: PlansAdmin}
	_ = access.Can(ctx, PlansAdmin)`),
		"caps.go": `package main

import "github.com/DonaldMurillo/gofastr/framework/access"

const PlansAdmin access.Permission = "plans:admin"

var caps = []access.Permission{"plans:write"}
`,
	})
	assertNot(t, ds, contracts.RuleUnregisteredPermission, "every checked name is registered")
}

// A constant used at the check site resolves too, so a typo in the
// constant's value is still caught.
func TestUnregisteredPermViaConst(t *testing.T) {
	ds := fixture(t, map[string]string{
		"main.go": permApp(`p.Register("plans:write")`, `_ = access.Can(ctx, PlansWrite)`),
		"caps.go": `package main

const PlansWrite = "plans:wirte"
`,
	})
	assertHas(t, ds, contracts.RuleUnregisteredPermission)
}

// An app that never registers capabilities has not opted in: there is
// no list to check against.
func TestUnregisteredPermNeedsARegistry(t *testing.T) {
	ds := fixture(t, map[string]string{"main.go": permApp(``,
		`_ = framework.AccessControl{Create: "plans:wirte"}`)})
	assertNot(t, ds, contracts.RuleUnregisteredPermission, "no Register call anywhere")
}

// One binary's registry says nothing about another binary's checks.
func TestUnregisteredPermPerBinary(t *testing.T) {
	ds := fixture(t, map[string]string{
		"cmd/appa/main.go": permApp(`p.Register("plans:write")`, ``),
		"cmd/appb/main.go": permApp(`p.Register("orders:read")`,
			`_ = framework.AccessControl{Create: "plans:write"}`),
		"cmd/appc/main.go": permApp(``, `_ = framework.AccessControl{Create: "plans:wirte"}`),
	})
	if n := len(countRule(t, ds, contracts.RuleUnregisteredPermission)); n != 1 {
		t.Errorf("findings = %d, want 1: appc registers nothing", n)
	}
	d := assertHas(t, ds, contracts.RuleUnregisteredPermission)
	if !strings.Contains(d.File, "appb") {
		t.Errorf("finding at %s, want appb", d.File)
	}
}

// Register is a common method name: contracts.Register and a desktop
// capability registry take no permission strings and must not count.
func TestUnregisteredPermIgnoresOtherRegister(t *testing.T) {
	ds := fixture(t, map[string]string{"main.go": permApp(`d.Register("orders", "plans")`,
		`_ = framework.AccessControl{Create: "plans:wirte"}`)})
	assertNot(t, ds, contracts.RuleUnregisteredPermission, "no permission registry")
}

// A Register call the pass cannot read leaves the registry incomplete;
// judging against the part it can read would flag correct names.
func TestUnregisteredPermUnreadRegistry(t *testing.T) {
	ds := fixture(t, map[string]string{"main.go": permApp(`p.Register("plans:write")
	p.Register(loadCaps()...)`,
		`_ = framework.AccessControl{Delete: "plans:admin"}`)})
	assertNot(t, ds, contracts.RuleUnregisteredPermission, "loadCaps may register plans:admin")
}

// Read and Create are common field names; only an AccessControl block's
// are permissions.
func TestUnregisteredPermOnlyAccessFields(t *testing.T) {
	ds := fixture(t, map[string]string{"main.go": permApp(`p.Register("plans:write")`,
		`_ = cache.Options{Read: "plans:wirte"}`)})
	assertNot(t, ds, contracts.RuleUnregisteredPermission, "not an AccessControl")
}

// A constants package is the one-spelling-per-capability layout.
func TestUnregisteredPermPackageConst(t *testing.T) {
	ds := fixture(t, map[string]string{
		"main.go": `package main

import (
	"github.com/DonaldMurillo/gofastr/framework/access"

	_ "example.com/app/entities"
)

func wire(p *access.RolePolicy) { p.Register("plans:write") }
`,
		"perms/perms.go": `package perms

const PlansWrite = "plans:wirte"
`,
		"entities/plans.go": `package entities

import (
	"example.com/app/perms"
	"github.com/DonaldMurillo/gofastr/framework"
)

var Plans = framework.AccessControl{Create: perms.PlansWrite}
`,
	})
	d := assertHas(t, ds, contracts.RuleUnregisteredPermission)
	if !strings.Contains(d.File, "entities") {
		t.Errorf("finding at %s, want the check in entities", d.File)
	}
}

// Names registered through a spread slice are the whole registry here,
// so a typo against them is still caught.
func TestUnregisteredPermViaSpread(t *testing.T) {
	ds := fixture(t, map[string]string{
		"main.go": permApp(`p.Register(caps...)`, `_ = access.Can(ctx, "plans:wirte")`),
		"caps.go": `package main

import "github.com/DonaldMurillo/gofastr/framework/access"

var caps = []access.Permission{"plans:write"}
`,
	})
	assertHas(t, ds, contracts.RuleUnregisteredPermission)
}

// A slice with a constant element registers that element too; one with
// an element the pass cannot read is an unread registry, not a short one.
func TestRegisteredPermPartialSlice(t *testing.T) {
	for name, caps := range map[string]string{
		"const element":  `var caps = []access.Permission{"plans:write", PlansAdmin}`,
		"unread element": `var caps = []access.Permission{"plans:write", loadOne()}`,
	} {
		t.Run(name, func(t *testing.T) {
			ds := fixture(t, map[string]string{
				"main.go": permApp(`p.Register(caps...)`, `_ = access.Can(ctx, "plans:admin")`),
				"caps.go": `package main

import "github.com/DonaldMurillo/gofastr/framework/access"

const PlansAdmin = "plans:admin"

` + caps + `
`,
			})
			assertNot(t, ds, contracts.RuleUnregisteredPermission, "plans:admin may be registered")
		})
	}
}

// The framework facade's policy is a policy too: an unread Register on
// it leaves the registry incomplete.
func TestUnregisteredPermFacadeUnread(t *testing.T) {
	ds := fixture(t, map[string]string{"main.go": `package main

import (
	"context"

	"github.com/DonaldMurillo/gofastr/framework"
)

func wire(ctx context.Context) {
	pol := framework.NewRolePolicy()
	pol.Register("plans:write")
	pol.Register(loadCaps()...)
	_ = framework.Can(ctx, "plans:admin")
}
`})
	assertNot(t, ds, contracts.RuleUnregisteredPermission, "loadCaps may register plans:admin")
}

// A local of the same name hides the package constant.
func TestUnregisteredPermLocalShadow(t *testing.T) {
	ds := fixture(t, map[string]string{
		"main.go": permApp(`p.Register("plans:write")`, `PlansWrite := os.Getenv("CAP")
	_ = access.Can(ctx, PlansWrite)`),
		"caps.go": `package main

const PlansWrite = "plans:wirte"
`,
	})
	assertNot(t, ds, contracts.RuleUnregisteredPermission, "the local is not the constant")
}

// Another type's Register is not a capability registry, even with a
// colon in its argument.
func TestUnregisteredPermOtherRegisterShaped(t *testing.T) {
	ds := fixture(t, map[string]string{"main.go": permApp(`events.Register("jobs:run")`,
		`_ = access.Can(ctx, "plans:write")`)})
	assertNot(t, ds, contracts.RuleUnregisteredPermission, "no policy registers anything")
}

// A package two binaries share is judged against each binary's own
// registry, not their union.
func TestUnregisteredPermSharedPackage(t *testing.T) {
	bin := func(caps string) string {
		return `package main

import (
	"github.com/DonaldMurillo/gofastr/framework/access"

	_ "example.com/app/entities"
)

func wire(p *access.RolePolicy) { p.Register(` + caps + `) }
`
	}
	ds := fixture(t, map[string]string{
		"cmd/appa/main.go": bin(`"plans:write", "orders:read"`),
		"cmd/appb/main.go": bin(`"plans:write"`),
		"entities/orders.go": `package entities

import "github.com/DonaldMurillo/gofastr/framework"

var Orders = framework.AccessControl{Read: "orders:read"}
`,
	})
	d := assertHas(t, ds, contracts.RuleUnregisteredPermission)
	if !strings.Contains(d.Evidence["binary"], "appb") {
		t.Errorf("binary = %q, want appb", d.Evidence["binary"])
	}
}

// A check call's permission argument needs no colon to be judged.
func TestUnregisteredPermColonless(t *testing.T) {
	ds := fixture(t, map[string]string{"main.go": permApp(`p.Register("admin")`,
		`_ = access.RequirePermission("admn")`)})
	assertHas(t, ds, contracts.RuleUnregisteredPermission)
}

// A name built from constants is still a constant.
func TestUnregisteredPermConstExpr(t *testing.T) {
	ds := fixture(t, map[string]string{
		"main.go": permApp(`p.Register(plans + ":write")`, `_ = access.Can(ctx, (plans + ":wirte"))`),
		"caps.go": `package main

const plans = "plans"
`,
	})
	assertHas(t, ds, contracts.RuleUnregisteredPermission)
}

// A policy is recognised by how it is made or typed, not by its name.
func TestUnregisteredPermPolicyReceivers(t *testing.T) {
	for name, src := range map[string]string{
		"facade ctor": `func wire(ctx context.Context) {
	pol := framework.NewRolePolicy()
	pol.Register("plans:write")
	_ = framework.Can(ctx, "plans:wirte")
}`,
		"struct field": `type server struct{ policy *framework.RolePolicy }

func (s *server) wire(ctx context.Context) {
	s.policy.Register("plans:write")
	_ = framework.Can(ctx, "plans:wirte")
}`,
	} {
		t.Run(name, func(t *testing.T) {
			ds := fixture(t, map[string]string{"main.go": `package main

import (
	"context"

	"github.com/DonaldMurillo/gofastr/framework"
)

` + src + `
`})
			assertHas(t, ds, contracts.RuleUnregisteredPermission)
		})
	}
}

// A permission-shaped Register on a receiver the pass cannot place may
// be the policy: the registry is then unknown.
func TestUnregisteredPermUnplacedRegister(t *testing.T) {
	ds := fixture(t, map[string]string{"main.go": permApp(`p.Register("plans:write")
	policyFor(ctx).Register("plans:admin")`, `_ = access.Can(ctx, "plans:admin")`)})
	assertNot(t, ds, contracts.RuleUnregisteredPermission, "policyFor may return the policy")
}

// Can and a bare Permission field are common names; only the access
// package's calls, and a permission-shaped value, are checks.
func TestUnregisteredPermOtherCanAndField(t *testing.T) {
	ds := fixture(t, map[string]string{"main.go": permApp(`p.Register("plans:write")`,
		`_ = acl.Can(ctx, "plans:wirte")
	_ = ui.Button{Permission: "edit"}`)})
	assertNot(t, ds, contracts.RuleUnregisteredPermission, "neither is a permission check")
}

// Permission is a common field name: an app's own struct holding one is
// not a check, however the value is spelt.
func TestUnregisteredPermAppStructField(t *testing.T) {
	ds := fixture(t, map[string]string{"main.go": permApp(`p.Register("plans:write")`,
		`_ = cron.Job{Permission: "cron:run"}
	_ = []cron.Job{{Permission: "cron:run"}}`)})
	assertNot(t, ds, contracts.RuleUnregisteredPermission, "cron.Job is not a transition")
}

// A slice or map literal elides its element type, so a transition or
// action written there is judged by the container's element type.
func TestUnregisteredPermElidedElement(t *testing.T) {
	for _, check := range []string{
		`_ = []entity.Transition{{Key: "go", Permission: "plans:wirte"}}`,
		`_ = []*entity.Transition{{Permission: "plans:wirte"}}`,
		`_ = map[string]entityui.Action{"go": {Permission: "plans:wirte"}}`,
	} {
		t.Run(check, func(t *testing.T) {
			ds := fixture(t, map[string]string{"main.go": permApp(`p.Register("plans:write")`, check)})
			assertHas(t, ds, contracts.RuleUnregisteredPermission)
		})
	}
}

// Without a module path every package is one node: literal names are
// still judged against the one registry.
func TestUnregisteredPermWithoutAGoModule(t *testing.T) {
	ds := fixture(t, map[string]string{
		"go.mod":  "",
		"main.go": permApp(`p.Register("plans:write")`, `_ = access.Can(ctx, "plans:wirte")`),
	})
	assertHas(t, ds, contracts.RuleUnregisteredPermission)
}

// A receiver whose declared type is not a policy registers nothing and
// leaves the registry known: the framework's own a.Batteries.Register(b,
// deps...) once silenced the rule for every binary that links it.
func TestUnregisteredPermTypedOtherRegister(t *testing.T) {
	ds := fixture(t, map[string]string{
		"main.go": permApp(`p.Register("plans:write")
	var reg *battery.Registry
	reg.Register(b, deps...)`, `_ = access.Can(ctx, "plans:wirte")`),
		"app.go": `package main

import "github.com/DonaldMurillo/gofastr/framework/access"

var _ access.Permission

type App struct{ Batteries *battery.Registry }

func (a *App) mount(b any, deps []string) { a.Batteries.Register(b, deps...) }
`,
	})
	assertHas(t, ds, contracts.RuleUnregisteredPermission)
}

// An app package that happens to be named framework is not the facade.
func TestUnregisteredPermOwnFrameworkPkg(t *testing.T) {
	ds := fixture(t, map[string]string{"main.go": `package main

import (
	"context"

	"github.com/DonaldMurillo/gofastr/framework/access"
	framework "example.com/app/framework"
)

func wire(ctx context.Context, p *access.RolePolicy) {
	p.Register("plans:write")
	_ = framework.Can(ctx, "billing:run")
}
`})
	assertNot(t, ds, contracts.RuleUnregisteredPermission, "example.com/app/framework is the app's own")
}

// The same name declared as a policy in one function and as something
// else in another is still a policy where it is one.
func TestUnregisteredPermNameReusedLater(t *testing.T) {
	ds := fixture(t, map[string]string{"main.go": permApp(`p.Register("plans:write")`,
		`_ = access.Can(ctx, "plans:wirte")
}

func other(p *strings.Builder) {
	p.Reset()`)})
	assertHas(t, ds, contracts.RuleUnregisteredPermission)
}

// Registrations the pass reads wrongly, or not at all, must not leave a
// registry that looks complete.
func TestUnregisteredPermRegistryShapes(t *testing.T) {
	for name, files := range map[string]map[string]string{
		"local in spread literal": {"main.go": permApp(`C := access.Permission("plans:admin")
	p.Register([]access.Permission{"plans:write", C}...)`, `_ = access.Can(ctx, "plans:admin")`),
			"caps.go": "package main\n\nconst C = \"plans:write\"\n"},
		"appended in init": {"main.go": permApp(`p.Register(caps...)`, `_ = access.Can(ctx, "plans:admin")`),
			"caps.go": `package main

import "github.com/DonaldMurillo/gofastr/framework/access"

var caps = []access.Permission{"plans:write"}

func init() { caps = append(caps, "plans:admin") }
`},
		"import-free file": {"main.go": `package main

import (
	"context"

	"github.com/DonaldMurillo/gofastr/framework/access"
)

var p = access.NewRolePolicy()

func wire(ctx context.Context) {
	p.Register("plans:write")
	_ = access.Can(ctx, "plans:admin")
}
`, "extra.go": "package main\n\nfunc init() { p.Register(\"plans:admin\") }\n"},
		"method value": {"main.go": permApp(`p.Register("plans:write")
	register := p.Register
	register("plans:admin")`, `_ = access.Can(ctx, "plans:admin")`)},
		"interface receiver": {"main.go": permApp(`p.Register("plans:write")
	var r interface{ Register(...access.Permission) } = p
	r.Register("plans:admin")`, `_ = access.Can(ctx, "plans:admin")`)},
		"embedded policy": {"main.go": permApp(`p.Register("plans:write")
	w := Wrapped{p}
	w.Register("plans:admin")`, `_ = access.Can(ctx, "plans:admin")`),
			"wrap.go": "package main\n\nimport \"github.com/DonaldMurillo/gofastr/framework/access\"\n\ntype Wrapped struct{ *access.RolePolicy }\n"},
		"build-tag twins": {"main.go": permApp(`p.Register("plans:write", C)`, `_ = access.Can(ctx, "plans:admin")`),
			"a.go": "//go:build !tools\n\npackage main\n\nconst C = \"plans:admin\"\n",
			"z.go": "//go:build tools\n\npackage main\n\nconst C = \"tools:read\"\n"},
	} {
		t.Run(name, func(t *testing.T) {
			assertNot(t, fixture(t, files), contracts.RuleUnregisteredPermission, "plans:admin is registered")
		})
	}
}

// Assigning to a variable does not change its declared type, so an
// unrelated registry stays unrelated and the binary is still judged.
func TestUnregisteredPermReassignedOther(t *testing.T) {
	ds := fixture(t, map[string]string{"main.go": permApp(`p.Register("plans:write")
	var r *events.Bus
	r = events.New()
	r.Register("events:ready")`, `_ = access.Can(ctx, "plans:wirte")`)})
	assertHas(t, ds, contracts.RuleUnregisteredPermission)
}

// new(T) declares its type as plainly as a var does.
func TestUnregisteredPermNewOtherRegister(t *testing.T) {
	ds := fixture(t, map[string]string{"main.go": permApp(`p.Register("plans:write")
	r := new(events.Bus)
	r.Register("events:ready")`, `_ = access.Can(ctx, "plans:wirte")`)})
	assertHas(t, ds, contracts.RuleUnregisteredPermission)
}

// A file that imports no framework package declares its own
// AccessControl, not the entity one.
func TestUnregisteredPermOwnAccessControl(t *testing.T) {
	ds := fixture(t, map[string]string{
		"main.go": permApp(`p.Register("plans:write")`, ``),
		"acl.go": `package main

type AccessControl struct{ Read string }

var _ = AccessControl{Read: "doors:open"}
`,
	})
	assertNot(t, ds, contracts.RuleUnregisteredPermission, "acl.go's AccessControl is the app's own")
}

// A package variable some function reassigns is not its initializer.
func TestUnregisteredPermMutatedVar(t *testing.T) {
	ds := fixture(t, map[string]string{
		"main.go": permApp(`p.Register("plans:write", extra)`, `_ = access.Can(ctx, "plans:admin")`),
		"caps.go": "package main\n\nvar extra = \"plans:read\"\n\nfunc init() { extra = \"plans:admin\" }\n",
	})
	assertNot(t, ds, contracts.RuleUnregisteredPermission, "init registers plans:admin")
}

// A policy held in another package registers through its import alias
// from a file that imports no framework package.
func TestRegisteredPermViaImportedPolicy(t *testing.T) {
	ds := fixture(t, map[string]string{
		"main.go": `package main

import (
	"context"

	"github.com/DonaldMurillo/gofastr/framework/access"

	_ "example.com/app/plugin"
)

func check(ctx context.Context) { _ = access.Can(ctx, "plans:admin") }
`,
		"auth/auth.go": `package auth

import "github.com/DonaldMurillo/gofastr/framework/access"

var Policy = access.NewRolePolicy()

func init() { Policy.Register("plans:write") }
`,
		"plugin/plugin.go": `package plugin

import "example.com/app/auth"

func init() { auth.Policy.Register("plans:admin") }
`,
	})
	assertNot(t, ds, contracts.RuleUnregisteredPermission, "plugin registers plans:admin through auth.Policy")
}

// A package variable another package extends through its import alias
// is not its initializer any more.
func TestRegisteredPermImportedVarMutated(t *testing.T) {
	ds := fixture(t, map[string]string{
		"main.go": `package main

import (
	"context"

	"example.com/app/perms"
	"github.com/DonaldMurillo/gofastr/framework/access"

	_ "example.com/app/plugin"
)

func wire(ctx context.Context, p *access.RolePolicy) {
	p.Register(perms.All...)
	_ = access.Can(ctx, "plans:admin")
}
`,
		"perms/perms.go": "package perms\n\nvar All = []string{\"plans:write\"}\n",
		"plugin/plugin.go": `package plugin

import "example.com/app/perms"

func init() { perms.All = append(perms.All, "plans:admin") }
`,
	})
	assertNot(t, ds, contracts.RuleUnregisteredPermission, "plugin extends perms.All with plans:admin")
}
