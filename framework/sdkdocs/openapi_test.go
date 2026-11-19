package sdkdocs

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/DonaldMurillo/gofastr/framework/sdk"
)

const openAPIDoc = `{"openapi":"3.1.0","paths":{"/api/generate":{"post":{"operationId":"generateBarcode"}}}}`

// openAPIArtifacts is a dist directory `gofastr generate sdk
// --from-openapi` wrote from doc.
func openAPIArtifacts(t *testing.T, doc string) fstest.MapFS {
	t.Helper()
	hash, err := sdk.OpenAPIHash([]byte(doc))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(sdk.Manifest{
		SchemaVersion: sdk.SchemaVersion, App: "barc", SDKVersion: "1.0.0", GofastrVersion: "v0.97.0",
		Source: sdk.SourceOpenAPI, Entities: []string{}, SchemaHash: hash,
		Artifacts: map[string]sdk.Artifact{
			"go": {File: sdk.GoArtifact, SHA256: sdkSum("zip"), Bytes: 3, Module: "example.com/barc-sdk"},
			"js": {File: sdk.JSArtifact, SHA256: sdkSum("//js\n"), Bytes: 5},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return fstest.MapFS{
		sdk.ManifestFile: {Data: raw},
		sdk.GoArtifact:   {Data: []byte("zip")},
		sdk.JSArtifact:   {Data: []byte("//js\n")},
	}
}

// SDKs generated from an OpenAPI document serve only beside the document
// the host publishes, and drift is the hash of that document moving: a
// reformatted copy is no drift, a changed one shows the banner and still
// serves.
func TestOpenAPIManifestDownloads(t *testing.T) {
	reg := testRegistry()
	artifacts := openAPIArtifacts(t, openAPIDoc)

	srv := mountedServer(t, Config{Registry: reg, Artifacts: artifacts})
	for _, path := range []string{"/docs/api/sdk/go.zip", "/docs/api/sdk/client.js"} {
		if resp, _ := get(t, srv, path); resp.StatusCode != http.StatusNotFound {
			t.Errorf("without Config.OpenAPI, %s: status %d, want 404", path, resp.StatusCode)
		}
	}
	if _, body := get(t, srv, "/docs/api"); !strings.Contains(body, "sdkdocs.Config.OpenAPI") {
		t.Error("the index does not say why the downloads are withheld")
	}

	reformatted := strings.ReplaceAll(openAPIDoc, ",", ",\n  ")
	srv = mountedServer(t, Config{Registry: reg, Artifacts: artifacts, OpenAPI: []byte(reformatted)})
	if resp, _ := get(t, srv, "/docs/api/sdk/go.zip"); resp.StatusCode != http.StatusOK {
		t.Errorf("with the document, go.zip: status %d, want 200", resp.StatusCode)
	}
	_, body := get(t, srv, "/docs/api")
	if strings.Contains(body, "out of date") || strings.Contains(body, "Quickstart") {
		t.Error("an unchanged document shows drift, or the index shows the entity quickstart for an OpenAPI SDK")
	}

	changed := strings.Replace(openAPIDoc, "generateBarcode", "renderBarcode", 1)
	srv = mountedServer(t, Config{Registry: reg, Artifacts: artifacts, OpenAPI: []byte(changed)})
	if _, body := get(t, srv, "/docs/api"); !strings.Contains(body, "out of date") {
		t.Error("a changed document shows no drift banner")
	}
	if resp, _ := get(t, srv, "/docs/api/sdk/client.js"); resp.StatusCode != http.StatusOK {
		t.Errorf("a drifted SDK still downloads: status %d", resp.StatusCode)
	}
}
