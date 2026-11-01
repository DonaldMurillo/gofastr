package sdkdocs

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/sdk"
)

// Property: the download routes serve only artifacts whose manifest names
// entities this site documents. `gofastr generate sdk` covers every entity
// unless --only narrows it, so a manifest naming the gated "invoices" used
// to hand anonymous callers its table and fields in client.d.ts while the
// reference page for it 404ed.
func TestArtifactsRefuseGatedScope(t *testing.T) {
	reg := testRegistry() // public posts + gated invoices
	dist := testArtifacts(t, reg, true)
	var m sdk.Manifest
	if err := json.Unmarshal(dist[sdk.ManifestFile].Data, &m); err != nil {
		t.Fatal(err)
	}
	m.Entities = []string{"invoices", "posts"}
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	dist[sdk.ManifestFile].Data = raw

	srv := mountedServer(t, Config{Registry: reg, Artifacts: dist})
	for _, p := range []string{"go.zip", "client.js", "client.d.ts", "manifest.json"} {
		resp, body := get(t, srv, "/docs/api/sdk/"+p)
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("%s: status %d, want 404 for artifacts covering a gated entity", p, resp.StatusCode)
		}
		if strings.Contains(body, "invoices") {
			t.Errorf("%s: refusal names the gated entity: %q", p, body)
		}
	}
	_, page := get(t, srv, "/docs/api")
	if strings.Contains(page, `href="/docs/api/sdk/client.d.ts"`) {
		t.Error("index links downloads the routes refuse")
	}
	if !strings.Contains(page, "withheld") {
		t.Error("index does not say the downloads are withheld")
	}

	// The same artifacts serve once the site documents what they cover.
	open := mountedServer(t, Config{Registry: reg, Artifacts: dist, IncludeGated: true})
	if resp, _ := get(t, open, "/docs/api/sdk/client.d.ts"); resp.StatusCode != http.StatusOK {
		t.Errorf("IncludeGated: status %d, want 200", resp.StatusCode)
	}
}

// Property: artifacts with no readable manifest declare no scope, so they
// refuse instead of serving whatever the dist directory holds.
func TestArtifactsRefuseWithoutManifest(t *testing.T) {
	reg := testRegistry()
	dist := testArtifacts(t, reg, true)
	delete(dist, sdk.ManifestFile)
	srv := mountedServer(t, Config{Registry: reg, Artifacts: dist})
	if resp, _ := get(t, srv, "/docs/api/sdk/client.d.ts"); resp.StatusCode != http.StatusNotFound {
		t.Errorf("status %d, want 404 with no manifest", resp.StatusCode)
	}
}

// Property: a download serves only the file the manifest records. A run
// with fewer targets (--target=js --only=posts) rewrites the manifest but
// leaves the earlier all-entity sdk-go.zip in dist; serving it would hand
// out the gated schema the manifest no longer names.
func TestArtifactsRefuseFileNotInManifest(t *testing.T) {
	reg := testRegistry()
	dist := testArtifacts(t, reg, true)
	var m sdk.Manifest
	if err := json.Unmarshal(dist[sdk.ManifestFile].Data, &m); err != nil {
		t.Fatal(err)
	}
	delete(m.Artifacts, "go")
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	dist[sdk.ManifestFile].Data = raw
	dist[sdk.JSArtifact].Data = []byte("//older client covering invoices\n")

	srv := mountedServer(t, Config{Registry: reg, Artifacts: dist})
	if resp, _ := get(t, srv, "/docs/api/sdk/go.zip"); resp.StatusCode != http.StatusNotFound {
		t.Errorf("go.zip absent from the manifest: status %d, want 404", resp.StatusCode)
	}
	if resp, _ := get(t, srv, "/docs/api/sdk/client.js"); resp.StatusCode != http.StatusNotFound {
		t.Errorf("client.js not matching its recorded hash: status %d, want 404", resp.StatusCode)
	}
	if resp, _ := get(t, srv, "/docs/api/sdk/client.d.ts"); resp.StatusCode != http.StatusOK {
		t.Errorf("client.d.ts matching the manifest: status %d, want 200", resp.StatusCode)
	}
}
