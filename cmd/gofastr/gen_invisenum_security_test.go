package main

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/framework"
)

// Pins the invisible-character hole in validateBlueprint's enum-value
// gate, found by the 2026-09-05 red-probe round (round 4); fixed by
// extending the gate (blueprint.go) with a textsafe refusal: C1 controls
// (IsC1) and the invisible/bidi set (ContainsInvisible), the same choke
// point the literal breakers already use.
// Family: F25 Bidi, invisible, and confusable characters
// Property: a blueprint value carrying a terminal-invisible character — a
// C1 control, a bidi override, a zero-width joiner, or a BOM — must not
// pass validateBlueprint into the generated app, whose served surfaces
// (llm.md field notes, the OpenAPI enum array) interpolate it raw.
// Surfaces: cmd/gofastr/blueprint.go::validateBlueprint (the enum-value
// gate) and the downstream raw sinks the value reaches at runtime:
// framework/crud/llmmd.go's `values: `+Join(Values,"|") note and
// framework/openapi's enum array; the generated Go itself is inert (%q
// escapes the rune), so the character only materializes in served text.
// Threat: U+202E and U+200B/U+FEFF reorder or hide the value from the
// human reviewing what the agent authored — the documented threat model
// for blueprint strings (blueprint_emitter_injection_test.go: "an AGENT
// authoring gofastr.yml"); C1 CSI (U+009B) is additionally
// terminal-interpretable.
func TestBlueprintRejectsInvisibleEnumChars(t *testing.T) {
	shapes := map[string]string{
		"c1_csi":     "open\u009b31;31mRED",
		"bidi_rlo":   "open\u202eevades",
		"zero_width": "open\u200bsneak",
		"bom":        "open\ufeffbom",
	}
	for label, shape := range shapes {
		bp := Blueprint{
			App: BlueprintApp{Name: "app"},
			Entities: []framework.EntityDeclaration{{
				Name: "tickets",
				Fields: []framework.FieldDeclaration{
					{Name: "title", Type: "string"},
					{Name: "status", Type: "string", Values: []string{shape}},
				},
			}},
			Screens: []BlueprintScreen{{Name: "home", Route: "/", Type: "page"}},
		}
		if err := validateBlueprint(bp); err != nil {
			continue // rejected at the boundary: the property holds
		}
		if _, err := renderBlueprintFiles(bp); err != nil {
			continue // the emitter refused: also fine
		}
		t.Errorf("SECURITY: [invisible-enum] %s: enum value %q carries a terminal-invisible character and was accepted into the generated app (validateBlueprint and the emitter both waved it through)", label, shape)
	}
}
