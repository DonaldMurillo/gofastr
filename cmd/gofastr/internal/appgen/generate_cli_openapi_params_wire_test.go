package appgen

import (
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"testing"
)

// An array header parameter is OpenAPI's simple style, the one style a
// header has: one header line, items joined by commas (X-Tags: a,b). Two
// X-Tags lines read as "a" to a server that takes the header's value
// (Go's Header.Get), so the second item never arrived.
func TestOpenAPIArrayHeaderIsOneLine(t *testing.T) {
	dir := oaTempModule(t)
	doc := `{"openapi":"3.0.3","paths":{"/pets":{"get":{"operationId":"listPets",
	  "parameters":[{"name":"X-Tags","in":"header","schema":{"type":"array","items":{"type":"string"}}}]}}}}`
	bin := buildOAPets(t, dir, doc)
	var mu sync.Mutex
	var got []string
	rec := newOARecorder(t, "X-Tags", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		got = append([]string(nil), r.Header.Values("X-Tags")...)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	})
	cmd := exec.Command(bin, "list-pets", "--url", rec.URL, "--x-tags", "a", "--x-tags", "b")
	cmd.Env = oaCLIEnv(t)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("list-pets: %v\n%s", err, out)
	}
	mu.Lock()
	defer mu.Unlock()
	if strings.Join(got, "|") != "a,b" {
		t.Errorf("X-Tags lines = %q, want the one line \"a,b\"", got)
	}
}

// A path naming one parameter twice (/users/{id}/files/{id}) fills both
// placeholders: the runner replaced the first only, and the second went
// out as a literal %7Bid%7D segment.
func TestOpenAPIPathPlaceholderTwice(t *testing.T) {
	dir := oaTempModule(t)
	doc := `{"openapi":"3.0.3","paths":{"/users/{id}/files/{id}":{"get":{"operationId":"getOwnFile",
	  "parameters":[{"name":"id","in":"path","required":true,"schema":{"type":"string"}}]}}}}`
	bin := buildOAPets(t, dir, doc)
	rec := newOARecorder(t, "Authorization", nil)
	cmd := exec.Command(bin, "get-own-file", "--url", rec.URL, "--id", "7")
	cmd.Env = oaCLIEnv(t)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("get-own-file: %v\n%s", err, out)
	}
	if paths, _ := rec.seen(); len(paths) != 1 || paths[0] != "/users/7/files/7" {
		t.Errorf("paths = %q, want /users/7/files/7", paths)
	}
}
