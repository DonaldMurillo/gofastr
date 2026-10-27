package appgen

import (
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// oaStyleDoc is one GET /pets whose array query parameter carries the
// given style/explode members (a JSON fragment, possibly empty).
func oaStyleDoc(in, style string) string {
	return `{"openapi":"3.0.3","paths":{"/pets":{"get":{"operationId":"listPets",
	  "parameters":[{"name":"tags","in":"` + in + `"` + style + `,"schema":{"type":"array","items":{"type":"string"}}}]}}}}`
}

// An array query parameter is sent the way its style and explode say:
// form with explode false is one tags=a,b. The CLI always repeated
// tags=a&tags=b, which such a server reads as the one tag "b" (or "a").
func TestOpenAPIArrayQueryStyle(t *testing.T) {
	dir := oaTempModule(t)
	for _, tc := range []struct{ name, style, want string }{
		{"default", ``, "tags=a+b&tags=c%2Cd"},
		{"form explode", `,"style":"form","explode":true`, "tags=a+b&tags=c%2Cd"},
		{"form joined", `,"style":"form","explode":false`, "tags=a+b,c%2Cd"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bin := buildOAPets(t, dir, oaStyleDoc("query", tc.style))
			var mu sync.Mutex
			var raw string
			srv := newOARecorder(t, "", func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				raw = r.URL.RawQuery
				mu.Unlock()
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{}`))
			})
			cmd := exec.Command(bin, "list-pets", "--url", srv.URL, "--tags", "a b", "--tags", "c,d")
			cmd.Env = oaCLIEnv(t)
			out, err := cmd.CombinedOutput()
			mu.Lock()
			defer mu.Unlock()
			if err != nil || raw != tc.want {
				t.Errorf("query = %q, %v; want %q\n%s", raw, err, tc.want, out)
			}
		})
	}
}

// A style the CLI cannot send is refused when the CLI is generated, not
// guessed at: spaceDelimited, deepObject, a header or path parameter in
// anything but simple, and a style or explode that is not a string or
// bool.
func TestOpenAPIRefusesUnsentStyle(t *testing.T) {
	for _, tc := range []struct{ in, style, want string }{
		{"query", `,"style":"spaceDelimited","explode":false`, "spaceDelimited"},
		{"query", `,"style":"pipeDelimited","explode":false`, "pipeDelimited"},
		{"query", `,"style":"deepObject"`, "deepObject"},
		{"header", `,"style":"form"`, "form"},
		{"query", `,"style":7`, "style is not a string"},
		{"query", `,"explode":"no"`, "explode is not a boolean"},
	} {
		m := mustDecodeDoc(t, oaStyleDoc(tc.in, tc.style))
		_, err := BuildOpenAPICLISpec(m, CliOptions{Binary: "pets", FromOpenAPI: "openapi.json"}, "c")
		if err == nil || !strings.Contains(err.Error(), "tags") || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s %s: err = %v, want tags refused naming %s", tc.in, tc.style, err, tc.want)
		}
	}
	pathDoc := `{"openapi":"3.0.3","paths":{"/pets/{id}":{"get":{"operationId":"getPet",
	  "parameters":[{"name":"id","in":"path","required":true,"style":"label","schema":{"type":"string"}}]}}}}`
	if _, err := BuildOpenAPICLISpec(mustDecodeDoc(t, pathDoc), CliOptions{Binary: "pets", FromOpenAPI: "openapi.json"}, "c"); err == nil || !strings.Contains(err.Error(), "id") {
		t.Errorf("label path parameter: err = %v, want it refused", err)
	}
}

// The OpenAPI CLI prints a JSON response with every digit, and an error
// body without the control bytes a terminal acts on.
func TestOpenAPIOutputExactAndScrubbed(t *testing.T) {
	dir := oaTempModule(t)
	bin := buildOAPets(t, dir, oaStyleDoc("query", ""))
	srv := newOARecorder(t, "", func(w http.ResponseWriter, r *http.Request) {
		if len(r.URL.Query()["tags"]) > 0 {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte("\x1b[2Jbad tag"))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"n":9007199254740993}`))
	})
	cmd := exec.Command(bin, "list-pets", "--url", srv.URL)
	cmd.Env = oaCLIEnv(t)
	out, _ := cmd.CombinedOutput()
	if !strings.Contains(string(out), "9007199254740993") {
		t.Errorf("printed %s, want 9007199254740993 kept", out)
	}
	cmd = exec.Command(bin, "list-pets", "--url", srv.URL, "--tags", "x")
	cmd.Env = oaCLIEnv(t)
	out, _ = cmd.CombinedOutput()
	if !strings.Contains(string(out), "bad tag") || strings.Contains(string(out), "\x1b") {
		t.Errorf("error output = %q, want the server text without ESC", out)
	}

	// Client.Do, the custom.go entry point, decodes as exactly.
	doTest := `package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDoKeepsDigits(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(` + "`" + `{"n":9007199254740993}` + "`" + `))
	}))
	defer srv.Close()
	var got map[string]any
	if err := NewClient(srv.URL, nil).Do(context.Background(), "GET", "/", nil, &got); err != nil {
		t.Fatal(err)
	}
	if b, _ := json.Marshal(got); string(b) != ` + "`" + `{"n":9007199254740993}` + "`" + ` {
		t.Errorf("Do re-marshals to %s", b)
	}
}
`
	if err := os.WriteFile(filepath.Join(dir, "cmd", "pets", "internal", "client", "do_test.go"), []byte(doTest), 0o644); err != nil {
		t.Fatal(err)
	}
	gt := exec.Command("go", "test", "./cmd/pets/internal/client/")
	gt.Dir = dir
	if out, err := gt.CombinedOutput(); err != nil {
		t.Errorf("Client.Do: %v\n%s", err, out)
	}
}

func mustDecodeDoc(t *testing.T, doc string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(doc), &m); err != nil {
		t.Fatal(err)
	}
	return m
}
