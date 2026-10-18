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
