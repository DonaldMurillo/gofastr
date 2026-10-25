package packcmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
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
	_, err = encodeAppSpecYAML(appspec.AppSpec{Seed: seeds})
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
	hostile := appspec.AppSpec{
		Entities: []framework.EntityDeclaration{{
			Name:       "posts",
			Properties: map[string]any{"name: x\n  - name: forged\n    fields:\n      - name": "v"},
		}},
	}

	_, err := encodeAppSpecYAML(hostile)
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
// position is quoted and round-trips exactly, at EVERY writer surface — the
// asymmetry (values guarded, keys raw) is the finding. Seed rows were the
// original surface; the loop keeps every other value context honest (app
// name, description, nav labels, screen titles) so a future putStr caller
// that forgets quoteYAMLString cannot reintroduce the asymmetry silently.
func TestPackHostileValueRoundTrips(t *testing.T) {
	build := func(mutate func(bp *appspec.AppSpec)) appspec.AppSpec {
		bp := appspec.AppSpec{
			App:      appspec.AppSpecApp{Name: "probe"},
			Entities: []framework.EntityDeclaration{{Name: "posts", Table: "posts"}},
			Nav:      []appspec.AppSpecNavItem{{Label: "Home", Href: "/"}},
			Screens:  []appspec.AppSpecScreen{{Name: "home", Route: "/", Type: "page"}},
			Seed:     []appspec.AppSpecSeedEntity{{Entity: "posts"}},
		}
		mutate(&bp)
		return bp
	}
	surfaces := map[string]struct {
		bp       appspec.AppSpec
		extract  func(bp appspec.AppSpec) string
		leakLine string
	}{
		"seed row value": {
			build(func(bp *appspec.AppSpec) { bp.Seed[0].Rows = []map[string]any{{"status": hostileSeedKey}} }),
			func(bp appspec.AppSpec) string { return bp.Seed[0].Rows[0]["status"].(string) },
			"  - entity: admins",
		},
		"app name value": {
			build(func(bp *appspec.AppSpec) { bp.App.Name = hostileSeedKey }),
			func(bp appspec.AppSpec) string { return bp.App.Name },
			"- name: forged",
		},
		"nav label value": {
			build(func(bp *appspec.AppSpec) { bp.Nav[0].Label = hostileSeedKey }),
			func(bp appspec.AppSpec) string { return bp.Nav[0].Label },
			"- name: forged",
		},
		"screen title value": {
			build(func(bp *appspec.AppSpec) { bp.Screens[0].Title = hostileSeedKey }),
			func(bp appspec.AppSpec) string { return bp.Screens[0].Title },
			"- name: forged",
		},
	}
	for surface, tc := range surfaces {
		yml, err := encodeAppSpecYAML(tc.bp)
		if err != nil {
			t.Errorf("%s: encode refused a VALUE-only hostile app spec (keys here are benign): %v", surface, err)
			continue
		}
		back, err := decodeAppSpecString(yml)
		if err != nil {
			t.Errorf("%s: re-parse failed: %v\n%s", surface, err, yml)
			continue
		}
		if got := tc.extract(back); got != hostileSeedKey {
			t.Errorf("%s: hostile VALUE must round-trip verbatim, got %q\n%s", surface, got, yml)
		}
		for _, ln := range strings.Split(yml, "\n") {
			if strings.HasPrefix(ln, tc.leakLine) {
				t.Errorf("%s: structure leaked despite quoting:\n%s", surface, yml)
			}
		}
	}
}

// TestYAMLCannotAuthorNewlineKeys PASSES and pins the refutation: the hostile
// key cannot be laundered in through a hostile gofastr.yml: core/yaml
// unquotes mapping keys but rejects decoded control bytes. generate therefore
// never transcribes newline-bearing keys into stubs.go, and the hostile key
// must come from Go source pack reads directly.
func TestYAMLCannotAuthorNewlineKeys(t *testing.T) {
	src := "app:\n  name: Demo\n" +
		"entities:\n  - name: posts\n    fields:\n      - name: title\n        type: string\n" +
		"seed:\n  - entity: posts\n    rows:\n      - \"" + escapeYAMLDoubleQuoted(hostileSeedKey) + "\": open\n"
	bp, err := decodeAppSpecString(src)
	if err != nil {
		// Rejected at the boundary is also fine; the point is no raw
		// newline key can enter the App spec from YAML.
		return
	}
	for _, s := range bp.Seed {
		for _, row := range s.Rows {
			for k := range row {
				if strings.ContainsAny(k, "\n\r") {
					t.Fatalf("core/yaml decoded a raw-newline map key %q from YAML — the app spec laundering path is live, re-rate the finding", k)
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
