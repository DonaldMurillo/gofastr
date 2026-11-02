package generatecmd

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"sync"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/textsafe"
)

const cliValuesFixtureYAML = `app:
  name: myapp
entities:
  - name: widgets
    crud: true
    fields:
      - name: title
        type: string
      - name: meta
        type: json
`

// bigInt is 2^53+1: the first integer a float64 cannot hold.
const bigInt = "9007199254740993"

// valuesServer answers every call with one widget whose title carries a
// screen-clearing escape and whose meta holds bigInt, keeps the last
// request body, and fails a call under /fail with a raw escape in its error body.
type valuesServer struct {
	*httptest.Server
	mu   sync.Mutex
	body string
}

func newValuesServer(t *testing.T) *valuesServer {
	s := &valuesServer{}
	row := `{"id":"w1","title":"\u001b[2Jforged\u009b31m\u202eflipped\u200b\u007f","meta":{"n":` + bigInt + `}}`
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		s.mu.Lock()
		s.body = string(b)
		s.mu.Unlock()
		if strings.HasPrefix(r.URL.Path, "/fail") {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte("\x1b[2Jbad request"))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/widgets") {
			_, _ = w.Write([]byte(`{"data":[` + row + `],"page":1,"perPage":20,"total":1,"totalPages":1,"cursor":"c\u001b[2J"}`))
			return
		}
		_, _ = w.Write([]byte(`{"data":` + row + `}`))
	}))
	t.Cleanup(s.Close)
	return s
}

func (s *valuesServer) last() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.body
}

// runValuesCLI runs the CLI and returns its combined output whatever the
// exit code.
func runValuesCLI(bin string, env []string, args ...string) string {
	cmd := exec.Command(bin, args...)
	cmd.Env = env
	out, _ := cmd.CombinedOutput()
	return string(out)
}

// A JSON value the CLI sends or prints keeps every digit: --json and a
// json field's flag decoded into any, so 2^53+1 went out as ...992, and a
// record read back printed the rounded value too.
func TestCLIKeepsBigIntegers(t *testing.T) {
	bin, env := buildFixtureCLI(t, cliValuesFixtureYAML)
	srv := newValuesServer(t)
	runValuesCLI(bin, env, "widgets", "create", "--url", srv.URL, "--json", `{"meta":{"n":`+bigInt+`}}`)
	if got := srv.last(); !strings.Contains(got, bigInt) {
		t.Errorf("--json sent %s, want %s kept", got, bigInt)
	}
	runValuesCLI(bin, env, "widgets", "create", "--url", srv.URL, "--meta", `{"n":`+bigInt+`}`)
	if got := srv.last(); !strings.Contains(got, bigInt) {
		t.Errorf("--meta sent %s, want %s kept", got, bigInt)
	}
	if out := runValuesCLI(bin, env, "widgets", "get", "w1", "--url", srv.URL); !strings.Contains(out, bigInt) {
		t.Errorf("get printed %s, want %s kept", out, bigInt)
	}
}

// list prints the envelope the server sent: a keyset page has no total,
// but decoding it through a struct printed "total": 0, "page": 0 and
// "totalPages": 0, so `jq .total` read a count of zero, and a key the
// struct did not name was dropped.
func TestCLIListPrintsTheServerEnvelope(t *testing.T) {
	bin, env := buildFixtureCLI(t, cliValuesFixtureYAML)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"w1","meta":{"n":` + bigInt + `}}],"cursor":"c2","hasMore":true,"extra":{"k":1}}`))
	}))
	t.Cleanup(srv.Close)
	out := runValuesCLI(bin, env, "widgets", "list", "--url", srv.URL, "--cursor", "")
	for _, absent := range []string{`"total"`, `"page"`, `"totalPages"`, `"perPage"`} {
		if strings.Contains(out, absent) {
			t.Errorf("list printed %s, which the server never sent:\n%s", absent, out)
		}
	}
	for _, want := range []string{`"extra"`, `"cursor": "c2"`, bigInt} {
		if !strings.Contains(out, want) {
			t.Errorf("list lacks %s:\n%s", want, out)
		}
	}
	if out := runValuesCLI(bin, env, "widgets", "list", "--url", srv.URL, "-o", "table"); !strings.Contains(out, "1 rows, next cursor: c2") {
		t.Errorf("table lost the cursor line:\n%s", out)
	}
}

// A table cell spells a json field's object or array as JSON and keeps a
// multi-line or tabbed value's words apart on one line: fmt's %v printed
// map[a:[1 2] s:<b>], and scrubbing the newline out of "line1\nline2"
// printed line1line2.
func TestCLITableCellsReadAsJSON(t *testing.T) {
	bin, env := buildFixtureCLI(t, cliValuesFixtureYAML)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"w1","title":"line1\nline2\tend","meta":{"a":[1,2],"s":"<b>"}},` +
			`{"id":"w2","title":"x","meta":[` + bigInt + `,true]}],"page":1,"perPage":20,"total":2,"totalPages":1}`))
	}))
	t.Cleanup(srv.Close)
	out := runValuesCLI(bin, env, "widgets", "list", "--url", srv.URL, "-o", "table")
	for _, want := range []string{`{"a":[1,2],"s":"<b>"}`, "line1 line2 end", `[` + bigInt + `,true]`} {
		if !strings.Contains(out, want) {
			t.Errorf("table lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "map[") {
		t.Errorf("table prints a Go map:\n%s", out)
	}
	if lines := strings.Split(strings.TrimSpace(out), "\n"); len(lines) != 4 {
		t.Errorf("table has %d lines, want header, two rows and the page line:\n%s", len(lines), out)
	}
}

// Record text a table prints, and server text an error echoes, reach the
// terminal without control bytes: a stored title "\x1b[2J..." cleared the
// operator's screen on `list -o table`.
func TestCLITableStripsControlBytes(t *testing.T) {
	bin, env := buildFixtureCLI(t, cliValuesFixtureYAML)
	srv := newValuesServer(t)
	out := runValuesCLI(bin, env, "widgets", "list", "--url", srv.URL, "-o", "table")
	if !strings.Contains(out, "forged") {
		t.Fatalf("table lost the title:\n%q", out)
	}
	// Bidi overrides and zero-width runes too (owner decision, 2026-11):
	// the table printer drops what core/textsafe calls unsafe, the set the
	// gofastr CLI's own helpers scrub.
	for _, r := range out {
		if r != '\n' && r != '\t' && textsafe.IsUnsafe(r) {
			t.Errorf("table output carries %U live:\n%q", r, out)
			break
		}
	}
	out = runValuesCLI(bin, env, "widgets", "list", "--url", srv.URL+"/fail")
	if !strings.Contains(out, "bad request") || strings.Contains(out, "\x1b") {
		t.Errorf("error output = %q, want the server text without ESC", out)
	}
}
