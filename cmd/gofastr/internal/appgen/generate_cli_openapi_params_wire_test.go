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
