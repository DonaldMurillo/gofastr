//go:build red

package main

import (
	"strings"
	"testing"
)

// RED TEST — open finding, 2026-09-06/07 adversarial round 5, phase 2
// (family enumeration; tier T2).
// Family: the requireScalarString doctrine, blueprint.go:1358-1366 —
// "stringValue answers '' for a list or a map, which is fine for a cosmetic
// field and wrong for a security posture ... so the shape is an error rather
// than a default". Pinned sibling (green today):
// TestDBConfigRejectsNonScalarTypes (blueprint_decode_security_test.go:113)
// moved app.db.driver/url onto requireScalarString (blueprint.go:550,554);
// decodeEntityReadScope did the same for unrestricted (:1464) and every
// predicate operand (:1419-1427).
// Property: a blueprint YAML value naming a security posture is REFUSED when
// its node is a list or a map — never silently decoded to "", because every
// one of these postures reads "" as its WEAKEST setting, not an error.
// Surfaces (blueprint.go, all still on plain stringValue):
//   - decodeEntityAccess :1344-1349 — access perms; `read: [posts:review]`
//     decodes Read="" and the CRUD layer enforces access.Can only for a
//     non-empty permission (entity/declaration.go:223-227), i.e. the
//     lms-lessons ungated-write shape, reached by shape drift instead of
//     omission;
//   - mergeEntityScopeDeclaration :934-945 (flat keys) and
//     decodeEntityScope :1105-1106 (scope: map) — owner_field/tenant_field
//     as a list decodes to "" and owner scoping silently no-ops: no hidden
//     owner column is injected (:791-798), every row is exposed to every
//     caller;
//   - decodeBlueprintScreens :1677 — screens[].access.role as a map decodes
//     Role="" with Auth=false, so blueprintScreenMountStmt takes its default
//     arm (:5487-5488) and mounts the screen via bare site.Register — no
//     policy, world-readable — where the scalar control emits
//     WithPolicy(authPolicy(...)).
// Finding (verified below by decode): all three arms decode today with
// zeroed postures and nil errors.
// Fix direction: route all three surfaces through requireScalarString with
// their YAML contexts (entities[i].access.<op>, entities[i].owner_field /
// .scope.owner_field and tenant_field twins, screens[i].access.role),
// exactly as app.db and read_scope already are.

// TestBlueprintRedPostureScalarsRefuseShapes: three arms, each asserting a
// shape-drifted posture value is a decode ERROR, with a scalar control
// proving the only delta is the node shape (and, for screens, that the
// scalar control still emits the auth policy at the mount site).
func TestBlueprintRedPostureScalarsRefuseShapes(t *testing.T) {
	// Arm (a): entity access perms — one list-valued perm.
	t.Run("entity access perm as list", func(t *testing.T) {
		bp, err := decodeBlueprintString(`
app:
  name: ShapeApp
entities:
  - name: posts
    fields:
      - name: title
        type: string
    access:
      read: ["posts:review"]
`)
		if err == nil {
			if len(bp.Entities) != 1 {
				t.Fatalf("setup broken: decoded %d entities, want 1", len(bp.Entities))
			}
			read := "<nil exposure/access>"
			if e := bp.Entities[0].Exposure; e != nil && e.Access != nil {
				read = e.Access.Read
			}
			t.Errorf("SECURITY: [blueprint-posture-shape-drift] entities[0].access.read as a list decoded without error to Read=%q — an empty read permission is anonymous read; the requireScalarString doctrine (blueprint.go:1358-1366) demands this shape be an error", read)
		}
		// Control: the scalar twin decodes and lands in the declaration.
		bp, err = decodeBlueprintString(`
app:
  name: ShapeApp
entities:
  - name: posts
    fields:
      - name: title
        type: string
    access:
      read: posts:review
`)
		if err != nil {
			t.Fatalf("setup broken: scalar access.read must decode: %v", err)
		}
		if got := bp.Entities[0].Exposure.Access.Read; got != "posts:review" {
			t.Fatalf("setup broken: scalar access.read lost: %q", got)
		}
	})

	// Arm (b): owner_field as a list — both declaration shapes (flat key and
	// scope: map) ride stringValue and both must refuse.
	t.Run("owner_field as list", func(t *testing.T) {
		for name, yml := range map[string]string{
			"flat key": `
app:
  name: ShapeApp
entities:
  - name: posts
    owner_field: ["user_id"]
    fields:
      - name: title
        type: string
`,
			"scope map": `
app:
  name: ShapeApp
entities:
  - name: posts
    fields:
      - name: title
        type: string
    scope:
      owner_field: ["user_id"]
`,
		} {
			bp, err := decodeBlueprintString(yml)
			if err == nil {
				if len(bp.Entities) != 1 {
					t.Fatalf("setup broken: decoded %d entities, want 1", len(bp.Entities))
				}
				owner := "<nil scope>"
				if s := bp.Entities[0].Scope; s != nil {
					owner = s.OwnerField
				}
				t.Errorf("SECURITY: [blueprint-posture-shape-drift] %s owner_field as a list decoded without error to OwnerField=%q — owner scoping silently no-ops and every row is exposed to every caller; the requireScalarString doctrine demands this shape be an error", name, owner)
			}
		}
		// Control: the scalar twin decodes, names the owner column, and gets
		// the hidden column injected (:791-798).
		bp, err := decodeBlueprintString(`
app:
  name: ShapeApp
entities:
  - name: posts
    owner_field: user_id
    fields:
      - name: title
        type: string
`)
		if err != nil {
			t.Fatalf("setup broken: scalar owner_field must decode: %v", err)
		}
		if got := bp.Entities[0].Scope.OwnerField; got != "user_id" {
			t.Fatalf("setup broken: scalar owner_field lost: %q", got)
		}
	})

	// Arm (c): screens[].access.role as a map.
	t.Run("screen access role as map", func(t *testing.T) {
		bp, err := decodeBlueprintString(`
app:
  name: ShapeApp
screens:
  - name: adminconsole
    route: /admin
    access:
      role:
        any: [admin]
`)
		if err == nil {
			if len(bp.Screens) != 1 {
				t.Fatalf("setup broken: decoded %d screens, want 1", len(bp.Screens))
			}
			mount := blueprintScreenMountStmt(bp.Screens[0], bp)
			t.Errorf("SECURITY: [blueprint-posture-shape-drift] screens[0].access.role as a map decoded without error to Auth=%v Role=%q, and the mount site is the ungated %q — a world-readable screen where the author wrote an access block; the requireScalarString doctrine demands this shape be an error",
				bp.Screens[0].Access.Auth, bp.Screens[0].Access.Role, strings.TrimSpace(mount))
		}
		// Control: the scalar twin decodes to a gated screen whose mount
		// emits the auth policy.
		bp, err = decodeBlueprintString(`
app:
  name: ShapeApp
screens:
  - name: adminconsole
    route: /admin
    access:
      role: admin
`)
		if err != nil {
			t.Fatalf("setup broken: scalar access.role must decode: %v", err)
		}
		scr := bp.Screens[0]
		if !scr.Access.Auth || scr.Access.Role != "admin" {
			t.Fatalf("setup broken: scalar access.role decoded wrong: Auth=%v Role=%q", scr.Access.Auth, scr.Access.Role)
		}
		if mount := blueprintScreenMountStmt(scr, bp); !strings.Contains(mount, ".WithPolicy(authPolicy(") {
			t.Fatalf("setup broken: scalar access.role must emit WithPolicy(authPolicy(...)) at the mount site, got %q", mount)
		}
	})
}
