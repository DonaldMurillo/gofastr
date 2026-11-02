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
