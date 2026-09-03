//go:build red

package main

// RED TEST — open finding, 2026-09-03 adversarial pass round 8 (tests-only; no fix applied).
// Property family: a security surface the tooling COUNTS as part of the artifact
// must be either implemented in the artifact or refused — never silently
// dropped (CWE-1078 silent omission / inherited "validate says yes" trust).
// Surface: blueprint `hooks:`. decodeBlueprintHooks (blueprint.go:2042-2065)
// decodes id/entity/when/handler/description and checks ONLY unknown keys —
// no field validation at all. validateBlueprint (blueprint.go:2149+) has no
// hooks loop: a hook may name a nonexistent entity, a garbage lifecycle point,
// or an empty handler and still pass. renderBlueprintFiles (blueprint.go:3233)
// never reads bp.Hooks — grep for Hooks over the emitter returns nothing — so
// generate ships an app with no hook stub, no registration, no TODO. Meanwhile:
//   - validate.go:69-70 prints "... N hook(s)" in the success line, counting
//     the surface the app will not have;
//   - pack.go:126-137 re-emits bp.Hooks, so a pack round-trip hides the gap;
//   - kiln.md promises hooks "graduate as owned-Go handler stubs".
// The kiln/freeze stage of this same property is pinned
// (kiln/freeze/blueprint_hooks_security_test.go TestFreezeHookNotSilentlyDropped)
// and the exhaustive emitter audit (emitter_quoting_audit_test.go) enumerates
// every IR struct except BlueprintHook. This pin is the generate/validate
// stage, one layer later — not re-derived from the freeze pin.
// Exploitable path: an operator (or a foreign/malicious blueprint, the
// product's core input) declares a before_create validation hook, watches
// `gofastr validate` bless it ("1 hook(s)"), generates, and ships an app
// with no such validation — a control the operator believes is enforced.
// Severity: MEDIUM — silent drop of a declared security control in the
// generate pipeline; no data exposure by itself, but it removes a defense
// the tooling affirms exists.
// Fix direction: either (a) validateBlueprint refuses blueprints with hooks
// until the emitter exists ("hooks are not yet implemented; remove the
// block"), or (b) renderBlueprintFiles emits an owned-Go handler stub plus
// HookRegistry registration per hook (the kiln.md contract), and
// validateBlueprint validates entity/when/handler like every other block.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestBlueprintHooksRedValidatedOrRefused drives the exact load-validate-render
// path `gofastr validate` and `gofastr generate` share (loadBlueprint with
// validation on, then renderBlueprintFiles) against a minimal blueprint
// carrying one before_create hook. The contract: a decoded hook is either
// REFUSED somewhere on that path, or fully validated and RENDERED into the
// file set. Counted-and-dropped is the failure.
func TestBlueprintHooksRedValidatedOrRefused(t *testing.T) {
	dir := t.TempDir()
	yml := filepath.Join(dir, "gofastr.yml")
	const blueprint = `app:
  name: HookGate
  module: example.com/hookgate
entities:
  - name: orders
    fields:
      - name: title
        type: string
hooks:
  - id: reject_bad_rows
    entity: orders
    when: before_create
    handler: RejectBadRows
    description: before_create row validation
`
	if err := os.WriteFile(yml, []byte(blueprint), 0o644); err != nil {
		t.Fatalf("write blueprint: %v", err)
	}

	// Leg 1: the load path the CLI uses (decode + validateBlueprint).
	bp, err := loadBlueprint(yml)
	if err != nil {
		// Refused — the contract is satisfied; nothing survives to render.
		return
	}
	if len(bp.Hooks) == 0 {
		t.Fatal("decoder dropped the hooks block entirely — a different defect than the one pinned here, but the surface is still not consumable; investigate decodeBlueprintHooks")
	}
	if len(bp.Entities) == 0 {
		t.Fatal("fixture blueprint lost its entity — fixture bug, not a product finding")
	}

	// Leg 2: the render pass runValidate runs to catch codegen-only failures.
	files, err := renderBlueprintFiles(bp)
	if err != nil {
		// Refused at render — contract satisfied.
		return
	}

	// Blessed by both legs: the hook must survive into the generated app as
	// a stub, registration, or reference (by ID or handler name).
	rendered := false
	for _, f := range files {
		if strings.Contains(f.content, "reject_bad_rows") || strings.Contains(f.content, "RejectBadRows") {
			rendered = true
			break
		}
	}
	if !rendered {
		t.Errorf("blueprint with %d hook(s) passed validation but renderBlueprintFiles emitted %d files carrying no hook stub or registration: `gofastr validate` counts the surface in its success line (validate.go:69-70) while `gofastr generate` silently drops it (renderBlueprintFiles never reads bp.Hooks). Refuse the blueprint or emit the hook — counted-and-dropped is the failure",
			len(bp.Hooks), len(files))
	}

	// The "fully validated" half of the contract: a hook naming an entity the
	// blueprint does not declare is a hard error for every other reference
	// family (screens, endpoints, relations). For hooks there is no loop, so
	// it sails through validation.
	const ghostBlueprint = `app:
  name: HookGhost
  module: example.com/hookghost
entities:
  - name: orders
    fields:
      - name: title
        type: string
hooks:
  - id: ghost_hook
    entity: nonexistent_entity
    when: not_a_lifecycle_point
    handler: ""
    description: ""
`
	ghostYML := filepath.Join(dir, "ghost-gofastr.yml")
	if err := os.WriteFile(ghostYML, []byte(ghostBlueprint), 0o644); err != nil {
		t.Fatalf("write ghost blueprint: %v", err)
	}
	if _, err := loadBlueprint(ghostYML); err == nil {
		t.Error("a hook referencing entity \"nonexistent_entity\" with when \"not_a_lifecycle_point\" and an empty handler passed validation: validateBlueprint has no hooks loop, so hook fields (entity existence, lifecycle point, handler) are never checked — unlike every other reference family")
	}
}
