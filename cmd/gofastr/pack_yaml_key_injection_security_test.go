package main

// SECURITY DEMONSTRATION (issue #136 audit, audit/generators worktree).
//
// Finding: pack.go's YAML writer emits MAP KEYS raw. Values go through
// quoteYAMLString/needsQuote (covered by emitter_output_context_security_test.go),
// but writeYAMLEntry writes `pad + key + ": "` with no escaping, and three maps
// in the pack path carry keys chosen by whoever controls the packed directory:
//
//   - seed row keys        (packReadSeed -> astAny -> blueprintToMap -> rows)
//   - entity properties    (packEntityDeclFromCall -> astAny -> m["properties"])
//   - theme dark overrides (packReadTheme -> appToMap -> theme.dark)
//
// A key containing newlines detonates at pack time: `gofastr pack` writes a
// gofastr.yml that re-parses to a DIFFERENT blueprint than the app being
// packed — below, a second seed entity carrying credentials the source never
// declared. That forges the snapshot pack is trusted to produce (pack.go's
// banner claims encodeBlueprintYAML is the exact inverse of decodeBlueprint).
//
// Threat model, after refuting the obvious stronger one: core/yaml NEVER
// unquotes map keys (parseMap stores the raw bytes before the first ":"),
// so a hostile *blueprint* cannot author a newline-bearing key, and the
// generate -> pack laundering chain is dead — TestYAMLCannotAuthorNewlineKeys
// pins that. The reachable input is the packed Go source itself: a
// hand-written or third-party app dir whose stubs.go/entities/app.go carry
// hostile string-literal keys. `gofastr pack` output is then reviewed or
// committed as "what the app declares" and regenerated; the forged YAML lies
// about the app. No validation bypass: regeneration runs validateBlueprint
// on the forged file like any other.
//
// Mitigations that do NOT stop it (checked): assertBlueprintGoParses (only
// gates generate's own output); the serializer round-trip test (friendly
// fixture only); core/yaml's depth cap (nesting is shallow here).
//
// Fix: pack REFUSES keys that cannot round-trip. core/yaml never unquotes
// map keys, so quoting would trade a forged snapshot for a mangled one
// (`"a: b": v` re-parses as the key `"a`); writeYAMLEntry instead rejects
// any key that would re-parse changed — empty, edge whitespace, ':', '#',
// line breaks, tabs, flow indicators, a leading "- " — and
// encodeBlueprintYAML returns an error naming the key and its map. Tests 1-2
// pin the refusal (and FAIL if the guard is removed), test 3 is the value
// control (hostile values still round-trip verbatim), test 4 pins the
// laundering refutation.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework"
)

// hostileSeedKey is a Go string-literal map key in hostile stubs.go. astAny
// (strconv.Unquote) recovers it verbatim; the writer emits it raw.
const hostileSeedKey = "status: open\n" +
	"  - entity: admins\n" +
	"    rows:\n" +
	"      - email: evil@example.com\n" +
	"        password: Passw0rd!\n" +
	"        note"

// TestPackSeedRowKeyInjection pins the sink end to end at pack level:
func TestPackSeedRowKeyInjection(t *testing.T) {
	dir := t.TempDir()
	stubs := "package app\n\n" +
		"type seedEntity struct {\n\tEntity string\n\tRows   []map[string]any\n}\n\n" +
		"func seedData() []seedEntity {\n" +
		"\treturn []seedEntity{\n" +
		"\t\t{Entity: \"posts\", Rows: []map[string]any{\n" +
		"\t\t\t{" + quoteGoString(hostileSeedKey) + ": \"open\"},\n" +
		"\t\t}},\n" +
		"\t}\n" +
		"}\n"
	if err := os.WriteFile(filepath.Join(dir, "stubs.go"), []byte(stubs), 0o644); err != nil {
		t.Fatal(err)
	}

	seeds, err := packReadSeed(dir)
	if err != nil {
		t.Fatalf("packReadSeed: %v", err)
	}
	if len(seeds) != 1 || seeds[0].Entity != "posts" {
		t.Fatalf("fixture misread: %+v", seeds)
	}

	// pack must REFUSE. Emitting this key raw writes a gofastr.yml that
	// re-parses to a seed entity "admins" (with credentials) that the packed
	// app never declared; quoting it mangles the key instead (core/yaml
	// never unquotes keys). A loud failure naming the key is the honest
	// snapshot.
	_, err = encodeBlueprintYAML(Blueprint{Seed: seeds})
	if err == nil {
		t.Fatal("SECURITY: [yaml-key-injection] pack emitted a hostile seed-row key instead of refusing")
	}
	if !strings.Contains(err.Error(), "seed.rows") {
		t.Fatalf("SECURITY: [yaml-key-injection] refusal must name the map the key came from (seed.rows), got: %v", err)
	}
	if !strings.Contains(err.Error(), "status: open") {
		t.Fatalf("SECURITY: [yaml-key-injection] refusal must name the offending key so the operator can find it in the source, got: %v", err)
	}
}

// TestPackEntityPropertiesKeyInjection is the same sink via entity
// `properties:`. The hostile key closes the entity's map and injects a
// sibling entity into the packed snapshot; pack must refuse it too.
func TestPackEntityPropertiesKeyInjection(t *testing.T) {
	hostile := Blueprint{
		Entities: []framework.EntityDeclaration{{
			Name:       "posts",
			Properties: map[string]any{"name: x\n  - name: forged\n    fields:\n      - name": "v"},
		}},
	}

	_, err := encodeBlueprintYAML(hostile)
	if err == nil {
		t.Fatal("SECURITY: [yaml-key-injection] pack emitted a hostile properties key instead of refusing")
	}
	if !strings.Contains(err.Error(), "entities.properties") {
		t.Fatalf("SECURITY: [yaml-key-injection] refusal must name the map the key came from (entities.properties), got: %v", err)
	}
	if !strings.Contains(err.Error(), "name: x") {
		t.Fatalf("SECURITY: [yaml-key-injection] refusal must name the offending key so the operator can find it in the source, got: %v", err)
	}
}

// TestPackHostileValueRoundTrips is the control: the same payload in VALUE
// position is quoted and round-trips exactly. The asymmetry (values guarded,
// keys raw) is the finding.
func TestPackHostileValueRoundTrips(t *testing.T) {
	orig := Blueprint{Seed: []BlueprintSeedEntity{{
		Entity: "posts",
		Rows:   []map[string]any{{"status": hostileSeedKey}},
	}}}
	yml, err := encodeBlueprintYAML(orig)
	if err != nil {
		t.Fatalf("value control: encode refused a VALUE-only hostile blueprint (keys here are benign): %v", err)
	}
	back, err := decodeBlueprintString(yml)
	if err != nil {
		t.Fatalf("value control: re-parse failed: %v\n%s", err, yml)
	}
	if len(back.Seed) != 1 || back.Seed[0].Entity != "posts" ||
		len(back.Seed[0].Rows) != 1 || back.Seed[0].Rows[0]["status"] != hostileSeedKey {
		t.Fatalf("value control: hostile VALUE must round-trip verbatim, got %+v\n%s", back.Seed, yml)
	}
	for _, ln := range strings.Split(yml, "\n") {
		if strings.HasPrefix(ln, "  - entity: admins") {
			t.Fatalf("value control: structure leaked despite quoting:\n%s", yml)
		}
	}
}

// TestYAMLCannotAuthorNewlineKeys PASSES and pins the refutation: the hostile
// key cannot be laundered in through a hostile gofastr.yml, because core/yaml
// stores map keys raw (never unquoting them). generate therefore only ever
// transcribes YAML-safe keys into stubs.go, and the hostile key must come
// from Go source pack reads directly.
func TestYAMLCannotAuthorNewlineKeys(t *testing.T) {
	src := "app:\n  name: Demo\n" +
		"entities:\n  - name: posts\n    fields:\n      - name: title\n        type: string\n" +
		"seed:\n  - entity: posts\n    rows:\n      - \"" + escapeYAMLDoubleQuoted(hostileSeedKey) + "\": open\n"
	bp, err := decodeBlueprintString(src)
	if err != nil {
		// Rejected at the boundary is also fine; the point is no raw
		// newline key can enter the Blueprint from YAML.
		return
	}
	for _, s := range bp.Seed {
		for _, row := range s.Rows {
			for k := range row {
				if strings.ContainsAny(k, "\n\r") {
					t.Fatalf("core/yaml decoded a raw-newline map key %q from YAML — the blueprint laundering path is live, re-rate the finding", k)
				}
			}
		}
	}
}

// --- helpers ---------------------------------------------------------------

func quoteGoString(s string) string {
	// mirrors fmt's %q closely enough for the fixture (ASCII payload)
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '\\':
			b.WriteString(`\\`)
		case '"':
			b.WriteString(`\"`)
		case '\n':
			b.WriteString(`\n`)
		case '\t':
			b.WriteString(`\t`)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// escapeYAMLDoubleQuoted renders s for a double-quoted YAML scalar; core/yaml
// unquotes such scalars with strconv.Unquote (values only — see the
// refutation test).
func escapeYAMLDoubleQuoted(s string) string {
	return strings.ReplaceAll(quoteGoString(s), `"`, `\"`)
}
