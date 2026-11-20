package appgen

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenAPISDKNullableBody(t *testing.T) {
	for _, schema := range []string{`{"type":"string","nullable":true}`, `{"type":["string","null"]}`, `{"$ref":"#/components/schemas/Maybe"}`} {
		t.Run(schema, func(t *testing.T) {
			version := "3.0.3"
			if strings.Contains(schema, `"type":[`) {
				version = "3.1.0"
			}
			doc := fmt.Sprintf(`{"openapi":%q,"info":{"title":"Null","version":"1"},"components":{"schemas":{"Maybe":{"type":"string","nullable":true}}},"paths":{`, version)
			for i, op := range []string{"required", "optional", "plainRequired", "plainOptional"} {
				if i > 0 {
					doc += ","
				}
				s := schema
				if i >= 2 {
					s = `{"type":"string"}`
				}
				doc += fmt.Sprintf(`"/%s":{"post":{"operationId":"%s","requestBody":{"required":%t,"content":{"application/json":{"schema":%s}}},"responses":{"204":{"description":"ok"}}}}`, op, op, i%2 == 0, s)
			}
			doc += "}}"
			spec, err := oaSDKSpecFor(t, doc)
			if err != nil {
				t.Fatal(err)
			}
			files := renderOASDKJSFiles(spec)
			t.Run("go", func(t *testing.T) {
				for _, op := range spec.Ops {
					var generated strings.Builder
					renderOASDKMethod(&generated, op)
					bodyType := "string"
					if !op.BodyRequired {
						bodyType = "*string"
					}
					want := "body " + bodyType + ") error"
					if !strings.Contains(generated.String(), want) {
						t.Errorf("%s: missing unchanged Go signature %q", op.GoName, want)
					}
				}
			})
			t.Run("types", func(t *testing.T) {
				for _, want := range []string{"required(body: string | null)", "optional(body?: string | null)"} {
					if !strings.Contains(files[1].Content, want) {
						t.Errorf("missing %s", want)
					}
				}
			})
			t.Run("wire", func(t *testing.T) {
				node, err := exec.LookPath("node")
				if err != nil {
					t.Skip("node is not installed")
				}
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					body, _ := io.ReadAll(r.Body)
					fmt.Fprintf(w, `{"body":%q,"contentType":%q}`, body, r.Header.Get("Content-Type"))
				}))
				defer server.Close()
				dir := t.TempDir()
				probe := fmt.Sprintf(`import {Client} from './client.mjs';
import assert from 'node:assert/strict';
const client = new Client({baseURL: %q});
const seen = [];
const fetchImpl = client._fetch;
client._fetch = async (...args) => { const r = await fetchImpl(...args); seen.push(await r.clone().json()); return r; };
for (const method of ['required', 'optional']) {
  await client[method](null);
  assert.deepEqual(seen.at(-1), {body:'null', contentType:'application/json'});
  await client[method]('hello');
  assert.deepEqual(seen.at(-1), {body:'"hello"', contentType:'application/json'});
}
await client.optional(undefined);
assert.deepEqual(seen.at(-1), {body:'', contentType:''});
await assert.rejects(() => client.required(undefined), /request body is required/);
await client.plainOptional(null);
assert.deepEqual(seen.at(-1), {body:'', contentType:''});
await assert.rejects(() => client.plainRequired(null), /request body is required/);
`, server.URL)
				for name, content := range map[string]string{"client.mjs": files[0].Content, "probe.mjs": probe} {
					if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
						t.Fatal(err)
					}
				}
				cmd := exec.Command(node, "probe.mjs")
				cmd.Dir = dir
				if out, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("node: %v\n%s", err, out)
				}
			})
		})
	}
}
