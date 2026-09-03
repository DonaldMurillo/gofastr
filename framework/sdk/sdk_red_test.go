//go:build red

package sdk

import (
	"testing"
	"testing/fstest"
)

// ---------------------------------------------------------------------------
// CONTRACT-QUESTION red: ReadManifest validates schemaVersion and artifacts
// but accepts a manifest covering ZERO entities. Delete or promote per
// maintainer decision.
// Property: a manifest that passes ReadManifest describes an SDK whose
// drift check can actually see the live schema.
// Surfaces: ReadManifest (manifest.go:57-78) checks SchemaVersion != 0 and
// len(Artifacts) > 0, never len(Entities) > 0. Downstream, sdkdocs.resolved
// (framework/sdkdocs/sdkdocs.go:270) computes the live hash as
// SchemaHash(RegistryNamedConfigs(reg, mf.Entities)) — restricted to the
// empty set, a constant independent of the registry — and compares it to
// mf.SchemaHash.
// Finding: an entities:[] (or entities-omitted) manifest validates, and a
// drift check over an empty entity set is vacuous: the "live" hash is the
// same constant no matter what the registry holds, so a manifest whose
// generation run lost its entity list (--only typo, empty registry at
// generate time) reports the artifacts as current while covering nothing.
// Severity: LOW — robustness pin, not an attack surface. The generator
// writes the list it was given; this guards a corrupted or hand-edited
// manifest against silently disabling the drift check. Note the tension:
// TestReadManifestValidates' "good" fixture (sdk_test.go:119) omits
// entities entirely, so promoting this fix means teaching that fixture an
// entities list (or this red gets deleted and nil-entities manifests stay
// valid by contract).
// Fix direction: ReadManifest rejects len(Entities) == 0 with the same
// "structurally empty" error class it already uses for missing artifacts
// — the doc comment on ReadManifest already promises "structurally empty"
// manifests error out, entities included. Alternatively, keep accepting
// and make sdkdocs surface a zero-coverage manifest as unknown provenance
// instead of a drift pass. Delete or promote per maintainer decision.
// ---------------------------------------------------------------------------
func TestReadManifestRedRejectsEmptyEntities(t *testing.T) {
	for name, mf := range map[string]string{
		"empty entities":   `{"schemaVersion":2,"app":"x","entities":[],"schemaHash":"sha256:ab","artifacts":{"go":{"file":"sdk-go.zip","sha256":"ab","bytes":2}}}`,
		"omitted entities": `{"schemaVersion":2,"app":"x","schemaHash":"sha256:ab","artifacts":{"go":{"file":"sdk-go.zip","sha256":"ab","bytes":2}}}`,
	} {
		if _, err := ReadManifest(fstest.MapFS{ManifestFile: {Data: []byte(mf)}}); err == nil {
			t.Errorf("CONTRACT: [sdk-manifest] %s manifest passed ReadManifest: schemaVersion and artifacts are validated but Entities is not (manifest.go:66-76). "+
				"A zero-entity manifest makes the drift check vacuous — sdkdocs.resolved hashes the registry restricted to an empty set, a constant, so the SDK docs page "+
				"serves downloads as \"current\" no matter what the live schema holds. Severity LOW (robustness); promote the rejection or delete this pin per the contract decision.",
				name)
		}
	}
}
