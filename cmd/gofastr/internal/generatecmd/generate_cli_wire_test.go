package generatecmd

import (
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appgen"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

const cliWireFixtureYAML = `app:
  name: myapp
entities:
  - name: widgets
    crud: true
    fields:
      - name: title
        type: string
`

// wireRecorder is an API server that keeps the last request's
// Authorization header and raw query.
type wireRecorder struct {
	*httptest.Server
	mu          sync.Mutex
	auth, query string
	hits        int
}

func newWireRecorder(t *testing.T) *wireRecorder {
	rec := &wireRecorder{}
	rec.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.mu.Lock()
		rec.auth, rec.query = r.Header.Get("Authorization"), r.URL.RawQuery
		rec.hits++
		rec.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[],"page":1,"perPage":20,"total":0,"totalPages":0}`))
	}))
	t.Cleanup(rec.Close)
	return rec
}

func (r *wireRecorder) last() (auth, query string, hits int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.auth, r.query, r.hits
}

// buildFixtureCLI generates and builds the CLI for the spec in yaml,
// returning the binary and an environment whose config directory is
// empty.
func buildFixtureCLI(t *testing.T, yaml string) (bin string, env []string) {
	t.Helper()
	dir := cliModuleFromYAML(t, yaml)
	appgen.RunGenerateCLI([]string{"--binary=myapp"})
	bin = clitest.TestExecutablePath(filepath.Join(dir, "myapp"))
	build := exec.Command("go", "build", "-o", bin, "./cmd/myapp")
	build.Dir = dir
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build CLI: %v\n%s", err, out)
	}
	home := t.TempDir()
	env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + home, "XDG_CONFIG_HOME=" + filepath.Join(home, ".config")}
	return bin, env
}

func runWireCLI(t *testing.T, bin string, env []string, stdin string, args ...string) string {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Env = env
	cmd.Stdin = strings.NewReader(stdin)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%v: %v\n%s", args, err, out)
	}
	return string(out)
}

// The stored login token belongs to the origin it was stored for: a --url
// or $MYAPP_URL naming another host must not receive it.
func TestCLITokenOnlyToItsOrigin(t *testing.T) {
	bin, env := buildFixtureCLI(t, cliWireFixtureYAML)
	prod, other := newWireRecorder(t), newWireRecorder(t)
	runWireCLI(t, bin, env, "tok-prod\n", "login", "--url", prod.URL, "--with-token")

	out := runWireCLI(t, bin, env, "", "widgets", "list", "--url", other.URL)
	if auth, _, hits := other.last(); hits != 1 || auth != "" {
		t.Errorf("--url to another origin sent Authorization %q (hits %d)", auth, hits)
	}
	if !strings.Contains(out, "not sending the stored token") {
		t.Errorf("no stderr note on the withheld token:\n%s", out)
	}
	runWireCLI(t, bin, append(env, "MYAPP_URL="+other.URL), "", "widgets", "list")
	if auth, _, _ := other.last(); auth != "" {
		t.Errorf("$MYAPP_URL to another origin sent Authorization %q", auth)
	}

	runWireCLI(t, bin, env, "", "widgets", "list", "--url", prod.URL+"/")
	if auth, _, _ := prod.last(); auth != "Bearer tok-prod" {
		t.Errorf("same origin got Authorization %q, want the stored token", auth)
	}
	runWireCLI(t, bin, env, "", "widgets", "list")
	if auth, _, _ := prod.last(); auth != "Bearer tok-prod" {
		t.Errorf("stored URL got Authorization %q, want the stored token", auth)
	}

	runWireCLI(t, bin, append(env, "MYAPP_TOKEN=tok-env"), "", "widgets", "list", "--url", other.URL)
	if auth, _, _ := other.last(); auth != "Bearer tok-env" {
		t.Errorf("$MYAPP_TOKEN got Authorization %q, want it sent anywhere", auth)
	}
}

// The server enters keyset mode on the cursor key's presence, so an
// empty --cursor must still send cursor=.
func TestCLIEmptyCursorStartsKeyset(t *testing.T) {
	bin, env := buildFixtureCLI(t, cliWireFixtureYAML)
	srv := newWireRecorder(t)
	env = append(env, "MYAPP_URL="+srv.URL)

	runWireCLI(t, bin, env, "", "widgets", "list", "--cursor", "")
	if _, query, _ := srv.last(); query != "cursor=" {
		t.Errorf(`--cursor "" sent query %q, want "cursor="`, query)
	}
	runWireCLI(t, bin, env, "", "widgets", "list")
	if _, query, _ := srv.last(); query != "" {
		t.Errorf("no --cursor sent query %q, want none", query)
	}
	runWireCLI(t, bin, env, "", "widgets", "list", "--cursor", "abc")
	if _, query, _ := srv.last(); query != "cursor=abc" {
		t.Errorf("--cursor abc sent query %q", query)
	}
}

// The equality flag's help promises "a comma list sends title_in, IN",
// and the flag keeps it: the server reads ?title=a,b as equality on the
// literal "a,b", so a value holding a comma goes to the IN key. One
// value stays equality.
func TestCLIEqualityFlagCommaListSendsIN(t *testing.T) {
	bin, env := buildFixtureCLI(t, cliWireFixtureYAML)
	srv := newWireRecorder(t)
	env = append(env, "MYAPP_URL="+srv.URL)

	runWireCLI(t, bin, env, "", "widgets", "list", "--title", "a,b")
	if _, query, _ := srv.last(); query != "title_in=a%2Cb" {
		t.Errorf("--title a,b sent query %q, want title_in=a%%2Cb", query)
	}
	runWireCLI(t, bin, env, "", "widgets", "list", "--title", "a")
	if _, query, _ := srv.last(); query != "title=a" {
		t.Errorf("--title a sent query %q, want title=a", query)
	}
	help := runWireCLI(t, bin, env, "", "widgets", "list", "--help")
	if !strings.Contains(help, "a comma list sends title_in, IN") {
		t.Errorf("list --help does not say where a comma list goes:\n%s", help)
	}
}
