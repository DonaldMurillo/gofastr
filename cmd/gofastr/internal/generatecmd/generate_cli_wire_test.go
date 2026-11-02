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

// A server URL with no http(s) scheme is refused where it enters: login
// stored `127.0.0.1:8080` as given, and every later call then failed on a
// URL parse error, the stored token withheld under an empty origin.
func TestCLIRefusesSchemelessServerURL(t *testing.T) {
	bin, env := buildFixtureCLI(t, cliWireFixtureYAML)
	for _, bad := range []string{"127.0.0.1:8080", "app.example.com", "ftp://app.example.com", "https://"} {
		cmd := exec.Command(bin, "login", "--url", bad, "--with-token")
		cmd.Env = env
		cmd.Stdin = strings.NewReader("gfsk_x\n")
		out, err := cmd.CombinedOutput()
		if exitErr, ok := err.(*exec.ExitError); !ok || exitErr.ExitCode() != 2 || !strings.Contains(string(out), "http(s)") {
			t.Errorf("login --url %s: %v, want exit 2 naming http(s)\n%s", bad, err, out)
		}
		cmd = exec.Command(bin, "widgets", "list")
		cmd.Env = append(env, "MYAPP_URL="+bad, "MYAPP_TOKEN=t")
		out, err = cmd.CombinedOutput()
		if exitErr, ok := err.(*exec.ExitError); !ok || exitErr.ExitCode() != 2 || !strings.Contains(string(out), "http(s)") {
			t.Errorf("MYAPP_URL=%s list: %v, want exit 2 naming http(s)\n%s", bad, err, out)
		}
	}
	rec := newWireRecorder(t)
	runWireCLI(t, bin, env, "gfsk_x\n", "login", "--url", strings.Replace(rec.URL, "http://", "HTTP://", 1)+"/", "--with-token")
	if out := runWireCLI(t, bin, env, "", "widgets", "list"); !strings.Contains(out, `"data"`) {
		t.Errorf("list after login printed %s", out)
	}
	if auth, _, _ := rec.last(); auth != "Bearer gfsk_x" {
		t.Errorf("the stored token did not reach its own origin: %q", auth)
	}
}

// A corrupt config file names its way out, and the way works: the refusal
// says "delete it or run 'myapp login' again", but login read the old
// file first and refused the same way, and so did logout.
func TestCLILoginReplacesCorruptConfig(t *testing.T) {
	bin, env := buildFixtureCLI(t, cliWireFixtureYAML)
	var cfgDir string
	for _, kv := range env {
		if v, ok := strings.CutPrefix(kv, "XDG_CONFIG_HOME="); ok {
			cfgDir = filepath.Join(v, "myapp")
		}
	}
	cfgPath := filepath.Join(cfgDir, "config.json")
	corrupt := func() {
		t.Helper()
		if err := os.MkdirAll(cfgDir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(cfgPath, []byte(`{"url":"http://a.test","token":"gfsk_old"`), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	rec := newWireRecorder(t)

	corrupt()
	runWireCLI(t, bin, env, "gfsk_new\n", "login", "--url", rec.URL, "--with-token")
	runWireCLI(t, bin, env, "", "widgets", "list")
	if auth, _, _ := rec.last(); auth != "Bearer gfsk_new" {
		t.Errorf("after login over a corrupt config the call carried %q", auth)
	}

	// With no --url there is no URL to keep: still refused, exit 1.
	corrupt()
	cmd := exec.Command(bin, "login", "--with-token")
	cmd.Env = env
	cmd.Stdin = strings.NewReader("gfsk_new\n")
	if out, err := cmd.CombinedOutput(); err == nil || !strings.Contains(string(out), "corrupt") {
		t.Errorf("login without --url over a corrupt config: %v\n%s", err, out)
	}

	// logout removes the token a corrupt file still holds.
	corrupt()
	runWireCLI(t, bin, env, "", "logout")
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "gfsk_old") {
		t.Errorf("logout left the token in the config file: %s", data)
	}
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
