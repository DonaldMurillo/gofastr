package appgen

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

// oaPetDoc is a petstore-shaped document whose server URL is server.
func oaPetDoc(server string) string {
	return `{"openapi":"3.0.3","servers":[{"url":` + string(mustJSON(server)) + `}],
	  "paths":{"/pet/{petId}":{"get":{"operationId":"getPetById",
	    "parameters":[{"name":"petId","in":"path","required":true,"schema":{"type":"integer"}}]}}}}`
}

// The path part of servers[0].url is the API prefix, whether the URL is
// relative or absolute; --api-prefix replaces it.
func TestOpenAPIServerBasePath(t *testing.T) {
	cases := []struct {
		name, server, prefixFlag string
		prefixSet                bool
		wantURL, wantPrefix      string
	}{
		{name: "relative", server: "/api/v3", wantPrefix: "/api/v3"},
		{name: "relative slash", server: "/api/v3/", wantPrefix: "/api/v3"},
		{name: "absolute", server: "https://pets.example.com/api/v3", wantURL: "https://pets.example.com", wantPrefix: "/api/v3"},
		{name: "absolute bare", server: "https://pets.example.com/", wantURL: "https://pets.example.com"},
		{name: "templated", server: "https://{host}/api"},
		{name: "flag wins", server: "/api/v3", prefixFlag: "v9/", prefixSet: true, wantPrefix: "/v9"},
		{name: "flag empties", server: "https://pets.example.com/api/v3", prefixFlag: "", prefixSet: true, wantURL: "https://pets.example.com"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var m map[string]any
			if err := json.Unmarshal([]byte(oaPetDoc(tc.server)), &m); err != nil {
				t.Fatal(err)
			}
			opts := CliOptions{Binary: "pets", FromOpenAPI: "openapi.json", ApiPrefix: tc.prefixFlag, apiPrefixSet: tc.prefixSet}
			spec, err := BuildOpenAPICLISpec(m, opts, "c")
			if err != nil {
				t.Fatal(err)
			}
			if spec.DefaultURL != tc.wantURL || spec.APIPrefix != tc.wantPrefix {
				t.Errorf("server %q: DefaultURL %q APIPrefix %q, want %q %q", tc.server, spec.DefaultURL, spec.APIPrefix, tc.wantURL, tc.wantPrefix)
			}
		})
	}
}

// A body offering several binary media types takes application/
// octet-stream, else the first by name: the generated CLI is the same on
// every run, not whichever key map order reached first.
func TestOpenAPIBinaryMediaDeterministic(t *testing.T) {
	cases := []struct{ content, want string }{
		{`"image/png":{"schema":{"format":"binary"}},"application/octet-stream":{},"application/gzip":{"schema":{"format":"binary"}}`, "application/octet-stream"},
		{`"image/png":{"schema":{"format":"binary"}},"image/jpeg":{"schema":{"format":"binary"}},"image/gif":{"schema":{"format":"binary"}}`, "image/gif"},
	}
	for _, tc := range cases {
		doc := `{"openapi":"3.0.3","paths":{"/up":{"put":{"operationId":"upload","requestBody":{"content":{` + tc.content + `}}}}}}`
		for range 20 {
			var m map[string]any
			if err := json.Unmarshal([]byte(doc), &m); err != nil {
				t.Fatal(err)
			}
			spec, err := BuildOpenAPICLISpec(m, CliOptions{Binary: "pets", FromOpenAPI: "o"}, "c")
			if err != nil {
				t.Fatal(err)
			}
			if got := spec.Ops[0].BodyContentType; got != tc.want {
				t.Fatalf("binary body content type %q, want %q", got, tc.want)
			}
		}
	}
}

func TestOpenAPIPrefixControlByteFails(t *testing.T) {
	var m map[string]any
	if err := json.Unmarshal([]byte(oaPetDoc("/api\n/v3")), &m); err != nil {
		t.Fatal(err)
	}
	if _, err := BuildOpenAPICLISpec(m, CliOptions{Binary: "pets", FromOpenAPI: "o"}, "c"); err == nil {
		t.Fatal("a server path with a control byte must be refused")
	}
}

// The built CLI sends the server URL's base path: the request the server
// sees is /api/v3/pet/7, not /pet/7.
func TestOpenAPIBasePathOnTheWire(t *testing.T) {
	dir := oaTempModule(t)
	if err := os.WriteFile(filepath.Join(dir, "openapi.json"), []byte(oaPetDoc("/api/v3")), 0o644); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths = append(paths, r.URL.Path)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":7}`))
	}))
	defer srv.Close()
	run := func(bin string) string {
		t.Helper()
		out, err := exec.Command(bin, "get-pet-by-id", "--url", srv.URL, "--pet-id", "7").CombinedOutput()
		if err != nil {
			t.Fatalf("get-pet-by-id: %v\n%s", err, out)
		}
		mu.Lock()
		defer mu.Unlock()
		return paths[len(paths)-1]
	}
	build := func(args ...string) string {
		t.Helper()
		RunGenerateCLI(append([]string{"--from-openapi=openapi.json", "--binary=pets"}, args...))
		bin := clitest.TestExecutablePath(filepath.Join(dir, "pets-bin"))
		cmd := exec.Command("go", "build", "-o", bin, "./cmd/pets")
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("build: %v\n%s", err, out)
		}
		return bin
	}

	if got := run(build()); got != "/api/v3/pet/7" {
		t.Errorf("server saw %q, want /api/v3/pet/7", got)
	}
	if got := run(build("--api-prefix=/v9")); got != "/v9/pet/7" {
		t.Errorf("--api-prefix=/v9: server saw %q, want /v9/pet/7", got)
	}
}

// An operation parameter overrides the path-level one with the same name
// and location (OpenAPI 3 "Path Item Object"): one flag, the operation's
// description.
func TestOpenAPIOpParamOverridesPath(t *testing.T) {
	doc := `{"openapi":"3.0.3","paths":{"/pet/{petId}":{
	  "parameters":[{"name":"petId","in":"path","required":true,"description":"path-level id","schema":{"type":"string"}}],
	  "get":{"operationId":"getPetById","parameters":[
	    {"name":"petId","in":"path","required":true,"description":"ID of pet to return","schema":{"type":"integer"}},
	    {"name":"petId","in":"query","description":"a query twin","schema":{"type":"string"}}]}}}}`
	var m map[string]any
	if err := json.Unmarshal([]byte(doc), &m); err != nil {
		t.Fatal(err)
	}
	_, err := BuildOpenAPICLISpec(m, CliOptions{Binary: "pets"}, "c")
	if err == nil || !strings.Contains(err.Error(), "already taken") {
		t.Fatalf("a path and a query param sharing --pet-id must still collide, got %v", err)
	}

	doc = strings.Replace(doc, `,
	    {"name":"petId","in":"query","description":"a query twin","schema":{"type":"string"}}`, "", 1)
	m = nil
	if err := json.Unmarshal([]byte(doc), &m); err != nil {
		t.Fatal(err)
	}
	spec, err := BuildOpenAPICLISpec(m, CliOptions{Binary: "pets"}, "c")
	if err != nil {
		t.Fatalf("operation param overriding the path-level one: %v", err)
	}
	op := spec.Ops[0]
	if len(op.PathParams) != 1 || op.PathParams[0].GoType != "int" || op.PathParams[0].Description != "ID of pet to return" {
		t.Fatalf("want one int --pet-id with the operation's description, got %+v", op.PathParams)
	}
	var sb strings.Builder
	renderCLIOpRunner(&sb, spec, op)
	if !strings.Contains(sb.String(), "ID of pet to return") || strings.Contains(sb.String(), "path-level id") {
		t.Errorf("--pet-id usage should carry the operation's description:\n%s", sb.String())
	}
}

// A description is help text: line breaks fold, a terminal escape is refused.
func TestOpenAPIParamDescription(t *testing.T) {
	build := func(desc string) (cliSpec, error) {
		doc := `{"openapi":"3.0.3","paths":{"/pet":{"get":{"operationId":"findPets","parameters":[
		  {"name":"tag","in":"query","description":` + string(mustJSON(desc)) + `,"schema":{"type":"string"}}]}}}}`
		var m map[string]any
		if err := json.Unmarshal([]byte(doc), &m); err != nil {
			t.Fatal(err)
		}
		return BuildOpenAPICLISpec(m, CliOptions{Binary: "pets"}, "c")
	}
	spec, err := build("tags to\n  filter by\r\n")
	if err != nil {
		t.Fatal(err)
	}
	if got := spec.Ops[0].QueryParams[0].Description; got != "tags to filter by" {
		t.Errorf("description %q, want it folded to one line", got)
	}
	if _, err := build("tags \x1b]0;pwned\x07"); err == nil {
		t.Error("a description carrying ESC/BEL must be refused")
	}
}
