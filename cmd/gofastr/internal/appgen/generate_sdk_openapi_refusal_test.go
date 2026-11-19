package appgen

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/codegen"
)

func mustDecodeJSONMap(t *testing.T, doc string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(doc), &m); err != nil {
		t.Fatal(err)
	}
	return m
}

// oaSDKDoc wraps paths (and optional components) in a minimal document.
func oaSDKDoc(paths string, components ...string) string {
	doc := `{"openapi":"3.1.0","paths":` + paths
	if len(components) > 0 {
		doc += `,"components":` + components[0]
	}
	return doc + "}"
}

// The SDK refuses what the --from-openapi CLI refuses, through the same
// walk, and the names an SDK cannot declare. Nothing is renamed.
func TestOpenAPISDKRefusals(t *testing.T) {
	cases := []struct {
		name, doc, want string
	}{
		{"missing operationId", oaSDKDoc(`{"/x":{"get":{}}}`), "missing operationId"},
		{"duplicate Go name", oaSDKDoc(`{"/a":{"get":{"operationId":"list_1"}},"/b":{"get":{"operationId":"list1"}}}`), "both derive the Go name List1"},
		{"duplicate operationId", oaSDKDoc(`{"/a":{"get":{"operationId":"getPet"}},"/b":{"get":{"operationId":"getPet"}}}`), "operationIds must be unique"},
		{"unsupported style", oaSDKDoc(`{"/a":{"get":{"operationId":"a","parameters":[{"name":"q","in":"query","style":"deepObject","schema":{"type":"string"}}]}}}`), `style "deepObject"`},
		{"cookie parameter", oaSDKDoc(`{"/a":{"get":{"operationId":"a","parameters":[{"name":"s","in":"cookie","schema":{"type":"string"}}]}}}`), "cookie parameter"},
		{"external ref in a parameter", oaSDKDoc(`{"/a":{"get":{"operationId":"a","parameters":[{"$ref":"other.json#/p"}]}}}`), "external $ref"},
		{"external ref in a request body", oaSDKDoc(`{"/a":{"post":{"operationId":"a","requestBody":{"content":{"application/json":{"schema":{"$ref":"other.json#/S"}}}}}}}`), "external $ref"},
		{"external ref in a response", oaSDKDoc(`{"/a":{"get":{"operationId":"a","responses":{"200":{"description":"ok","content":{"application/json":{"schema":{"$ref":"https://x.example/s.json"}}}}}}}}`), "external $ref"},
		{"external ref in a property", oaSDKDoc(`{"/a":{"get":{"operationId":"a","responses":{"200":{"description":"ok","content":{"application/json":{"schema":{"type":"object","properties":{"p":{"$ref":"other.json#/P"}}}}}}}}}}`), "external $ref"},
		{"Go client member", oaSDKDoc(`{"/a":{"get":{"operationId":"do"}}}`), "Client.Do"},
		{"JS client member", oaSDKDoc(`{"/a":{"get":{"operationId":"_send"}}}`), "client.js method _send"},
		{"JS object member", oaSDKDoc(`{"/a":{"get":{"operationId":"toString"}}}`), "client.js method toString"},
		{"reserved type name", oaSDKDoc(`{"/a":{"get":{"operationId":"a","responses":{"200":{"description":"ok","content":{"application/json":{"schema":{"$ref":"#/components/schemas/Client"}}}}}}}}`,
			`{"schemas":{"Client":{"type":"object","properties":{"id":{"type":"string"}}}}}`), "type name Client"},
		{"type name taken twice", oaSDKDoc(`{"/a":{"get":{"operationId":"a","responses":{"200":{"description":"ok","content":{"application/json":{"schema":{"type":"object","properties":{"x":{"type":"string"}}}}}}}}},"/b":{"get":{"operationId":"b","responses":{"200":{"description":"ok","content":{"application/json":{"schema":{"$ref":"#/components/schemas/AResponse"}}}}}}}}`,
			`{"schemas":{"AResponse":{"type":"object","properties":{"y":{"type":"string"}}}}}`), "type name AResponse"},
		{"parameter fields collide", oaSDKDoc(`{"/a":{"get":{"operationId":"a","parameters":[{"name":"pet_id","in":"query","schema":{"type":"string"}},{"name":"petId","in":"header","schema":{"type":"string"}}]}}}`), "Go field PetId"},
		{"parameter without a field name", oaSDKDoc(`{"/a":{"get":{"operationId":"a","parameters":[{"name":"@q","in":"query","schema":{"type":"string"}}]}}}`), "does not derive a Go field name"},
		{"swagger 2", `{"swagger":"2.0","paths":{}}`, "Swagger"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := oaSDKSpecFor(t, tc.doc)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v, want an error containing %q", err, tc.want)
			}
		})
	}
}

// The refusals that only a command line needs stay out of the SDK: a
// command word the CLI reserves, or a parameter named like a CLI flag,
// is an ordinary method and field.
func TestOpenAPISDKSkipsCLIOnlyRefusals(t *testing.T) {
	doc := oaSDKDoc(`{"/login":{"post":{"operationId":"login","parameters":[{"name":"token","in":"query","schema":{"type":"string"}},{"name":"url","in":"header","schema":{"type":"string"}}]}}}`)
	spec, err := oaSDKSpecFor(t, doc)
	if err != nil {
		t.Fatal(err)
	}
	if len(spec.Ops) != 1 || spec.Ops[0].GoName != "Login" || spec.Ops[0].QueryFields[0] != "Token" || spec.Ops[0].HeaderFields[0] != "Url" {
		t.Fatalf("ops: %+v", spec.Ops)
	}
	if _, err := BuildOpenAPICLISpec(mustDecodeJSONMap(t, doc), CliOptions{Binary: "x", FromOpenAPI: "openapi.json"}, "c"); err == nil {
		t.Fatal("the CLI accepted the reserved command login")
	}
}

// An object whose property names cannot all be Go fields is typed as raw
// JSON, in both SDKs, so no property is dropped or renamed.
func TestOpenAPISDKUnnameableObjectIsRaw(t *testing.T) {
	doc := oaSDKDoc(`{"/a":{"get":{"operationId":"getThing","responses":{"200":{"description":"ok","content":{"application/ld+json":{"schema":{"type":"object","properties":{"@type":{"type":"string"},"id":{"type":"string"}}}}}}}}}}`)
	spec, err := oaSDKSpecFor(t, doc)
	if err != nil {
		t.Fatal(err)
	}
	op := spec.Ops[0]
	if op.Result != "json" || op.ResultType.Kind != "raw" || op.ResultType.goExpr() != "json.RawMessage" || op.ResultType.tsExpr() != "unknown" {
		t.Fatalf("result %s %+v, want raw JSON", op.Result, op.ResultType)
	}
}

// A response that offers any non-JSON media type is binary; one whose
// media types are all JSON is typed; no 2xx content is no result.
func TestOpenAPISDKResponseKinds(t *testing.T) {
	spec, err := oaSDKSpecFor(t, oaSDKFixture)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"BatchZip": "binary", "DecodeImage": "json", "GenerateBarcode": "binary", "GetSpec": "json", "Ping": "", "ValidateData": "json"}
	for _, op := range spec.Ops {
		if op.Result != want[op.GoName] {
			t.Errorf("%s: result %q, want %q", op.GoName, op.Result, want[op.GoName])
		}
	}
}

// A settings entry's from_openapi names a file in the project; it takes
// no entity selection, and `gofastr generate --config` runs it through
// the registered sdk generator.
func TestOpenAPISDKSettingsEntry(t *testing.T) {
	for _, tc := range []struct {
		name string
		cfg  map[string]any
		want string
	}{
		{"url", map[string]any{"from_openapi": "https://x.example/openapi.json"}, "must be a file in the project"},
		{"only", map[string]any{"from_openapi": "api/openapi.json", "only": []any{"posts"}}, "only and exclude select entities"},
		{"empty", map[string]any{"from_openapi": ""}, "config.from_openapi must be a non-empty string"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := decodeSDKEntry(0, codegen.GeneratorConfig{Name: "sdk", Config: tc.cfg})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v, want %q", err, tc.want)
			}
		})
	}

	dir := oaTempModule(t)
	if err := os.MkdirAll(filepath.Join(dir, "api"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "api", "openapi.json"), []byte(oaSDKFixture), 0o644); err != nil {
		t.Fatal(err)
	}
	files, err := sdkGenerator{}.Generate(t.Context(), &codegen.Context{ProjectDir: dir}, codegen.GeneratorConfig{
		Name: "sdk", Config: map[string]any{"from_openapi": "api/openapi.json", "module": "github.com/me/app-sdk"},
	})
	if err != nil {
		t.Fatal(err)
	}
	var mod, manifest string
	for _, f := range files {
		switch f.Path {
		case "go/go.mod":
			mod = f.Content
		case "dist/manifest.json":
			manifest = f.Content
		}
	}
	if !strings.Contains(mod, "module github.com/me/app-sdk") || !strings.Contains(manifest, `"source": "openapi"`) {
		t.Fatalf("go.mod %q, manifest %q", mod, manifest)
	}

	settings := "codegen:\n  generators:\n    - name: sdk\n      output: apisdk\n      config:\n        from_openapi: api/openapi.json\n        module: github.com/me/app-sdk\n"
	if err := os.WriteFile(filepath.Join(dir, "gofastr.settings.yml"), []byte(settings), 0o644); err != nil {
		t.Fatal(err)
	}
	opts := SdkOptions{set: map[string]bool{}}
	if err := ApplySDKConfigDefaults(&opts); err != nil {
		t.Fatal(err)
	}
	if opts.fromOpenAPI != "api/openapi.json" || opts.module != "github.com/me/app-sdk" || opts.OutDir != "gen/apisdk" {
		t.Fatalf("settings defaults: %+v", opts)
	}
}
