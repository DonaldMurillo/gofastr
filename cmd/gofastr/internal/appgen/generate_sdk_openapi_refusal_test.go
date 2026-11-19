package appgen

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
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
		{"object parameter", oaSDKDoc(`{"/a":{"get":{"operationId":"a","parameters":[{"name":"q","in":"query","schema":{"type":"object","properties":{"x":{"type":"string"}}}}]}}}`), "only scalars and arrays of strings are sent as parameters"},
		{"array of integers", oaSDKDoc(`{"/a":{"get":{"operationId":"a","parameters":[{"name":"q","in":"query","schema":{"type":"array","items":{"type":"integer"}}}]}}}`), "only arrays of strings are sent as repeated parameters"},
		{"unknown in", oaSDKDoc(`{"/a":{"get":{"operationId":"a","parameters":[{"name":"q","in":"body","schema":{"type":"string"}}]}}}`), "the SDK sends path, query or header parameters"},
		{"form body", oaSDKDoc(`{"/a":{"post":{"operationId":"a","requestBody":{"content":{"application/x-www-form-urlencoded":{"schema":{"type":"object"}}}}}}}`), "the SDK sends application/json and binary"},
		{"text body", oaSDKDoc(`{"/a":{"post":{"operationId":"a","requestBody":{"content":{"text/plain":{"schema":{"type":"string"}}}}}}}`), "the SDK sends application/json and binary"},
		{"header name", oaSDKDoc(`{"/a":{"get":{"operationId":"a","parameters":[{"name":"X(1)","in":"header","schema":{"type":"string"}}]}}}`), "is not a plain header name"},
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
// command word the CLI reserves, a parameter named like a CLI flag, and
// terminal-control bytes in a summary or description (the SDK folds them
// out of its comments; the CLI prints them in --help) are fine.
func TestOpenAPISDKSkipsCLIOnlyRefusals(t *testing.T) {
	doc := oaSDKDoc(`{"/login":{"post":{"operationId":"login","summary":"sign in\u001b[2J now","parameters":[{"name":"token","in":"query","description":"the\u001b]0;x\u0007 token","schema":{"type":"string"}},{"name":"url","in":"header","schema":{"type":"string"}}]}}}`)
	spec, err := oaSDKSpecFor(t, doc)
	if err != nil {
		t.Fatal(err)
	}
	files, err := renderOASDKGoFiles(spec)
	if err != nil {
		t.Fatal(err)
	}
	js := renderOASDKJSFiles(spec)
	for _, f := range append(files, js...) {
		if strings.ContainsAny(f.Content, "\x1b\x07") {
			t.Errorf("%s carries the summary's or description's control bytes", f.Name)
		}
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

// A response that offers a JSON media type is typed from it, whatever
// else it offers, and asks for JSON; one with none is binary and asks for
// its types; no 2xx content is no result and sends no Accept.
func TestOpenAPISDKResponseKinds(t *testing.T) {
	spec, err := oaSDKSpecFor(t, oaSDKFixture)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][2]string{
		"BatchZip": {"binary", "application/zip"}, "DecodeImage": {"json", "application/json"},
		"GenerateBarcode": {"binary", "image/png, image/svg+xml"}, "GetSpec": {"json", "application/json"},
		"Ping": {"", ""}, "PostOpt": {"", ""}, "UploadOpt": {"json", "application/json"},
		"ValidateData": {"json", "application/json"},
	}
	for _, op := range spec.Ops {
		if got := [2]string{op.Result, op.Accept}; got != want[op.GoName] {
			t.Errorf("%s: result and Accept %q, want %q", op.GoName, got, want[op.GoName])
		}
	}
	if v := spec.Ops[slices.IndexFunc(spec.Ops, func(op oaSDKOp) bool { return op.GoName == "ValidateData" })]; v.ResultType.Name != "ValidateResult" {
		t.Errorf("ValidateData beside text/plain: result %+v, want the typed ValidateResult", v.ResultType)
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

// oaRecursiveDoc refers to components inside themselves: a map of
// itself, a list of itself, an object through an unnameable component,
// and a struct through its own property.
const oaRecursiveDoc = `{"openapi":"3.1.0",
  "components":{"schemas":{
    "Json":{"type":"object","additionalProperties":{"$ref":"#/components/schemas/Json"}},
    "Node":{"type":"array","items":{"$ref":"#/components/schemas/Node"}},
    "Odd[Name]":{"type":"object","properties":{"next":{"$ref":"#/components/schemas/Odd[Name]"}}},
    "Tree":{"type":"object","properties":{"kids":{"type":"array","items":{"$ref":"#/components/schemas/Tree"}}}}
  }},
  "paths":{
    "/json":{"post":{"operationId":"putJson","requestBody":{"content":{"application/json":{"schema":{"$ref":"#/components/schemas/Json"}}}},
      "responses":{"200":{"description":"ok","content":{"application/json":{"schema":{"$ref":"#/components/schemas/Node"}}}}}}},
    "/odd":{"get":{"operationId":"getOdd","parameters":[{"name":"q","in":"query","schema":{"$ref":"#/components/schemas/Node"}}],
      "responses":{"200":{"description":"ok","content":{"application/json":{"schema":{"$ref":"#/components/schemas/Odd[Name]"}}}}}}},
    "/tree":{"get":{"operationId":"getTree","responses":{"200":{"description":"ok","content":{"application/json":{"schema":{"$ref":"#/components/schemas/Tree"}}}}}}}
  }}`

// A component met again inside itself is typed once and then as raw JSON
// (a named struct refers to itself by name), so a recursive document
// generates instead of overflowing the stack. The CLI's walk reads the
// same document without recursing.
func TestOpenAPISDKRecursiveSchemas(t *testing.T) {
	spec, err := oaSDKSpecFor(t, strings.Replace(oaRecursiveDoc, `,"parameters":[{"name":"q","in":"query","schema":{"$ref":"#/components/schemas/Node"}}]`, "", 1))
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, op := range spec.Ops {
		if op.Body != nil {
			got[op.GoName+" body"] = op.Body.goExpr()
		}
		got[op.GoName] = op.ResultType.goExpr()
	}
	want := map[string]string{
		"PutJson body": "map[string]json.RawMessage",
		"PutJson":      "[]json.RawMessage",
		"GetOdd":       "GetOddResponse",
		"GetTree":      "Tree",
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s: %s, want %s", k, got[k], v)
		}
	}
	if fields := spec.Types.structs["GetOddResponse"].Fields; len(fields) != 1 || fields[0].Type.Kind != "raw" {
		t.Errorf("GetOddResponse fields: %+v", fields)
	}
	if fields := spec.Types.structs["Tree"].Fields; len(fields) != 1 || fields[0].Type.goExpr() != "[]Tree" {
		t.Errorf("Tree fields: %+v", fields)
	}
	if _, err := renderOASDKGoFiles(spec); err != nil {
		t.Fatal(err)
	}
	if _, err := BuildOpenAPICLISpec(mustDecodeJSONMap(t, oaRecursiveDoc), CliOptions{Binary: "x", FromOpenAPI: "openapi.json"}, "c"); err == nil || !strings.Contains(err.Error(), "non-scalar schema") {
		t.Fatalf("the CLI over a recursive parameter schema: %v, want the non-scalar refusal", err)
	}
	if _, err := BuildOpenAPICLISpec(mustDecodeJSONMap(t, strings.Replace(oaRecursiveDoc, `,"parameters":[{"name":"q","in":"query","schema":{"$ref":"#/components/schemas/Node"}}]`, "", 1)), CliOptions{Binary: "x", FromOpenAPI: "openapi.json"}, "c"); err != nil {
		t.Fatalf("the CLI over a recursive document: %v", err)
	}
}

// oaUnionSettings is the union app's settings file, read before a test
// changes directory.
var oaUnionSettings, _ = filepath.Abs(filepath.Join("testdata", "union", "gofastr.settings.yml"))

// With an entity sdk entry and an OpenAPI one (the union app's layout), a
// run takes one entry's settings: --from-openapi or --out picks it, or
// none when it names no entry, and a plain run refuses to guess instead
// of reading the document from one entry and the output directory from
// the other. No run writes into another entry's output directory.
func TestOpenAPISDKEntrySelection(t *testing.T) {
	union, err := os.ReadFile(oaUnionSettings)
	if err != nil {
		t.Fatal(err)
	}
	dir := oaTempModule(t)
	if err := os.WriteFile(filepath.Join(dir, "gofastr.settings.yml"), union, 0o644); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name             string
		opts             SdkOptions
		wantOut, wantDoc string
		wantErr          string
	}{
		{name: "plain", wantErr: "lists 2 sdk entries"},
		{name: "from-openapi", opts: SdkOptions{fromOpenAPI: "api/openapi.json"}, wantOut: "gen/apisdk", wantDoc: "api/openapi.json"},
		{name: "out entity", opts: SdkOptions{OutDir: "gen/sdk"}, wantOut: "gen/sdk"},
		{name: "out openapi", opts: SdkOptions{OutDir: "gen/apisdk"}, wantOut: "gen/apisdk", wantDoc: "api/openapi.json"},
		{name: "another document", opts: SdkOptions{fromOpenAPI: "other.json"}, wantErr: "gen/sdk is where the sdk entry for the entity set"},
		{name: "another document elsewhere", opts: SdkOptions{fromOpenAPI: "other.json", OutDir: "gen/other"}, wantOut: "gen/other", wantDoc: "other.json"},
		{name: "another document over an entry", opts: SdkOptions{fromOpenAPI: "other.json", OutDir: "gen/apisdk"}, wantErr: "the OpenAPI document api/openapi.json"},
		{name: "a document over the entity entry", opts: SdkOptions{fromOpenAPI: "api/openapi.json", OutDir: "gen/sdk"}, wantErr: "gen/sdk is where the sdk entry for the entity set"},
		{name: "out unnamed", opts: SdkOptions{OutDir: "gen/x"}, wantOut: "gen/x"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			opts := tc.opts
			opts.set = map[string]bool{}
			err := applySDKConfigDefaults(&opts)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("got %v, want %q", err, tc.wantErr)
				}
				return
			}
			if err != nil || opts.OutDir != tc.wantOut || opts.fromOpenAPI != tc.wantDoc {
				t.Fatalf("out %q doc %q err %v, want out %q doc %q", opts.OutDir, opts.fromOpenAPI, err, tc.wantOut, tc.wantDoc)
			}
		})
	}
}
