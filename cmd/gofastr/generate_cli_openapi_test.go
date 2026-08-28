package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const oaFixture = `{
  "openapi": "3.0.3",
  "info": {"title": "barc", "version": "1.0.0"},
  "servers": [{"url": "https://barc.example.com"}],
  "components": {
    "securitySchemes": {"bearer": {"type": "http", "scheme": "bearer"}},
    "schemas": {
      "GenerateRequest": {
        "type": "object",
        "required": ["text"],
        "properties": {
          "text": {"type": "string"},
          "scale": {"type": "integer"},
          "transparent": {"type": "boolean"}
        }
      }
    }
  },
  "paths": {
    "/api/spec/{symbology}": {
      "get": {
        "operationId": "getSpec",
        "summary": "describe one symbology",
        "parameters": [
          {"name": "symbology", "in": "path", "required": true, "schema": {"type": "string"}},
          {"name": "verbose", "in": "query", "schema": {"type": "boolean"}},
          {"name": "tag", "in": "query", "schema": {"type": "array", "items": {"type": "string"}}}
        ]
      }
    },
    "/api/generate": {
      "post": {
        "operationId": "generateCode",
        "requestBody": {
          "content": {"application/json": {"schema": {"$ref": "#/components/schemas/GenerateRequest"}}}
        }
      }
    },
    "/api/decode": {
      "post": {
        "operationId": "decodeImage",
        "requestBody": {
          "content": {"application/octet-stream": {"schema": {"type": "string", "format": "binary"}}}
        }
      }
    }
  }
}`

func oaSpecFromFixture(t *testing.T, doc string) cliSpec {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(doc), &m); err != nil {
		t.Fatal(err)
	}
	spec, err := buildOpenAPICLISpec(m, cliOptions{binary: "oapp", fromOpenAPI: "openapi.json"}, "example.com/oapp/cmd/oapp/internal/client")
	if err != nil {
		t.Fatal(err)
	}
	return spec
}

func TestOpenAPICLI_SpecModel(t *testing.T) {
	spec := oaSpecFromFixture(t, oaFixture)
	if !spec.SelfClient || spec.DefaultURL != "https://barc.example.com" {
		t.Fatalf("self-client + servers[0] expected: %+v", spec)
	}
	if spec.TokenHeader != "Authorization" || spec.TokenPrefix != "Bearer " {
		t.Errorf("bearer scheme expected: %q %q", spec.TokenHeader, spec.TokenPrefix)
	}
	if len(spec.Ops) != 3 {
		t.Fatalf("3 ops expected, got %d", len(spec.Ops))
	}
	// Sorted by command: decode-image, generate-code, get-spec.
	if spec.Ops[0].Command != "decode-image" || spec.Ops[1].Command != "generate-code" || spec.Ops[2].Command != "get-spec" {
		t.Fatalf("commands wrong: %s %s %s", spec.Ops[0].Command, spec.Ops[1].Command, spec.Ops[2].Command)
	}
	getSpec := spec.Ops[2]
	if len(getSpec.PathParams) != 1 || getSpec.PathParams[0].Flag != "symbology" {
		t.Errorf("path param missing: %+v", getSpec.PathParams)
	}
	if len(getSpec.QueryParams) != 2 || getSpec.QueryParams[0].GoType != "bool" || !getSpec.QueryParams[1].Repeated {
		t.Errorf("query params wrong: %+v", getSpec.QueryParams)
	}
	gen := spec.Ops[1]
	if gen.BodyKind != "json" || len(gen.BodyFields) != 3 {
		t.Fatalf("json body with 3 fields expected: %+v", gen)
	}
	if !gen.BodyFields[1].Required || gen.BodyFields[1].Wire != "text" {
		t.Errorf("required text field expected (alphabetical order): %+v", gen.BodyFields)
	}
	if spec.Ops[0].BodyKind != "binary" {
		t.Errorf("binary body expected: %+v", spec.Ops[0])
	}
}

func TestOpenAPICLI_MissingOperationIDFails(t *testing.T) {
	doc := `{"openapi":"3.0.3","paths":{"/x":{"get":{"summary":"no id"}}}}`
	var m map[string]any
	_ = json.Unmarshal([]byte(doc), &m)
	_, err := buildOpenAPICLISpec(m, cliOptions{binary: "x"}, "c")
	if err == nil || !strings.Contains(err.Error(), "operationId") {
		t.Fatalf("missing operationId must fail, got %v", err)
	}
}

func TestOpenAPICLI_DuplicateCommandFails(t *testing.T) {
	doc := `{"openapi":"3.0.3","paths":{
	  "/a":{"get":{"operationId":"doThing"}},
	  "/b":{"get":{"operationId":"do_thing"}}}}`
	var m map[string]any
	_ = json.Unmarshal([]byte(doc), &m)
	_, err := buildOpenAPICLISpec(m, cliOptions{binary: "x"}, "c")
	if err == nil || !strings.Contains(err.Error(), "already used") {
		t.Fatalf("colliding commands must fail, got %v", err)
	}
}

func TestOpenAPICLI_ReservedFlagFails(t *testing.T) {
	doc := `{"openapi":"3.0.3","paths":{"/a":{"get":{"operationId":"listA",
	  "parameters":[{"name":"token","in":"query","schema":{"type":"string"}}]}}}}`
	var m map[string]any
	_ = json.Unmarshal([]byte(doc), &m)
	_, err := buildOpenAPICLISpec(m, cliOptions{binary: "x"}, "c")
	if err == nil || !strings.Contains(err.Error(), "reserves") {
		t.Fatalf("reserved flag must fail, got %v", err)
	}
}

func TestOpenAPICLI_ExternalRefFails(t *testing.T) {
	doc := `{"openapi":"3.0.3","paths":{"/a":{"post":{"operationId":"mk",
	  "requestBody":{"content":{"application/json":{"schema":{"$ref":"other.json#/X"}}}}}}}}`
	var m map[string]any
	_ = json.Unmarshal([]byte(doc), &m)
	_, err := buildOpenAPICLISpec(m, cliOptions{binary: "x"}, "c")
	if err == nil || !strings.Contains(err.Error(), "external $ref") {
		t.Fatalf("external ref must fail, got %v", err)
	}
}

func TestOpenAPICLI_YAMLDocumentParses(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "spec.yaml")
	if err := os.WriteFile(path, []byte(`openapi: 3.0.3
paths:
  /ping:
    get:
      operationId: ping
`), 0o644); err != nil {
		t.Fatal(err)
	}
	doc, err := loadOpenAPIDoc(path)
	if err != nil {
		t.Fatal(err)
	}
	spec, err := buildOpenAPICLISpec(doc, cliOptions{binary: "x"}, "c")
	if err != nil || len(spec.Ops) != 1 || spec.Ops[0].Command != "ping" {
		t.Fatalf("yaml spec should yield the ping op: %v %+v", err, spec.Ops)
	}
}

// oaTempModule writes a bare module (the generated tree is stdlib-only)
// plus the fixture spec, and chdirs into it.
func oaTempModule(t *testing.T) string {
	t.Helper()
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	goVersion, err := repoGoVersion(repoRoot)
	if err != nil {
		t.Fatalf("repoGoVersion: %v", err)
	}
	dir := t.TempDir()
	goMod := "module example.com/oapp\n\ngo " + goVersion + "\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(goMod), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "openapi.json"), []byte(oaFixture), 0o644); err != nil {
		t.Fatal(err)
	}
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(oldWD) })
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestOpenAPICLI_BuildsCleanly(t *testing.T) {
	dir := oaTempModule(t)
	runGenerateCLI([]string{"--from-openapi=openapi.json", "--binary=oapp"})

	for _, f := range []string{"main.go", "config.go", "auth.go", "output.go", "custom.go", "operations.go", "internal/client/client.go"} {
		if _, err := os.Stat(filepath.Join(dir, "cmd", "oapp", f)); err != nil {
			t.Fatalf("cmd/oapp/%s not written: %v", f, err)
		}
	}
	cmd := exec.Command("go", "build", "-o", filepath.Join(dir, "oapp-bin"), "./cmd/oapp")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated CLI did not build: %v\n%s", err, out)
	}
}

func TestOpenAPICLI_RoundTripLiveServer(t *testing.T) {
	dir := oaTempModule(t)
	runGenerateCLI([]string{"--from-openapi=openapi.json", "--binary=oapp"})
	bin := filepath.Join(dir, "oapp-bin")
	build := exec.Command("go", "build", "-o", bin, "./cmd/oapp")
	build.Dir = dir
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}

	type seen struct {
		method, path, query, auth, body, contentType string
	}
	var last seen
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b := make([]byte, 1<<16)
		n, _ := r.Body.Read(b)
		last = seen{
			method: r.Method, path: r.URL.Path, query: r.URL.RawQuery,
			auth: r.Header.Get("Authorization"), body: string(b[:n]),
			contentType: r.Header.Get("Content-Type"),
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	// GET with path + repeated query params, bearer token.
	out, err := exec.Command(bin, "get-spec", "--url", srv.URL, "--token", "tok",
		"--symbology", "qr code", "--verbose", "--tag", "a", "--tag", "b").CombinedOutput()
	if err != nil {
		t.Fatalf("get-spec: %v\n%s", err, out)
	}
	if last.method != "GET" || last.path != "/api/spec/qr code" {
		t.Errorf("path param not routed: %+v", last)
	}
	if !strings.Contains(last.query, "verbose=true") || !strings.Contains(last.query, "tag=a") || !strings.Contains(last.query, "tag=b") {
		t.Errorf("query params wrong: %q", last.query)
	}
	if last.auth != "Bearer tok" {
		t.Errorf("bearer auth expected, got %q", last.auth)
	}
	if !strings.Contains(string(out), `"ok": true`) {
		t.Errorf("json response should pretty-print: %s", out)
	}

	// POST with json body from field flags.
	if out, err := exec.Command(bin, "generate-code", "--url", srv.URL,
		"--text", "hello", "--scale", "3").CombinedOutput(); err != nil {
		t.Fatalf("generate-code: %v\n%s", err, out)
	}
	if last.method != "POST" || last.path != "/api/generate" || last.contentType != "application/json" {
		t.Errorf("json post wrong: %+v", last)
	}
	if !strings.Contains(last.body, `"text":"hello"`) || !strings.Contains(last.body, `"scale":3`) {
		t.Errorf("body wrong: %q", last.body)
	}

	// Required body field enforced.
	if out, err := exec.Command(bin, "generate-code", "--url", srv.URL, "--scale", "2").CombinedOutput(); err == nil {
		t.Errorf("missing required --text must fail:\n%s", out)
	}

	// Binary body from a file.
	img := filepath.Join(dir, "img.bin")
	if err := os.WriteFile(img, []byte{0x89, 0x50, 0x4e, 0x47}, 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(bin, "decode-image", "--url", srv.URL, "--file", img).CombinedOutput(); err != nil {
		t.Fatalf("decode-image: %v\n%s", err, out)
	}
	if last.contentType != "application/octet-stream" || len(last.body) != 4 {
		t.Errorf("binary body wrong: ct=%q len=%d", last.contentType, len(last.body))
	}
}
