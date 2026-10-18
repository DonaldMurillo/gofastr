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
