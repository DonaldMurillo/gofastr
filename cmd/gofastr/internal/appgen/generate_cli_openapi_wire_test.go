package appgen

import (
	"io"
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

// oaRecorder is an API server that keeps each request's path and the
// value of one header.
type oaRecorder struct {
	*httptest.Server
	mu     sync.Mutex
	paths  []string
	header []string
}

func newOARecorder(t *testing.T, header string, h http.HandlerFunc) *oaRecorder {
	rec := &oaRecorder{}
	rec.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.mu.Lock()
		rec.paths = append(rec.paths, r.URL.Path)
		rec.header = append(rec.header, r.Header.Get(header))
		rec.mu.Unlock()
		if h != nil {
			h(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":7}`))
	}))
	t.Cleanup(rec.Close)
	return rec
}

func (r *oaRecorder) seen() (paths, header []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.paths...), append([]string(nil), r.header...)
}

// buildOAPets writes doc as openapi.json in dir, generates the pets CLI
// from it with args, and builds it.
func buildOAPets(t *testing.T, dir, doc string, args ...string) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "openapi.json"), []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	RunGenerateCLI(append([]string{"--from-openapi=openapi.json", "--binary=pets"}, args...))
	bin := clitest.TestExecutablePath(filepath.Join(dir, "pets-bin"))
	cmd := exec.Command("go", "build", "-o", bin, "./cmd/pets")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	return bin
}

// oaCLIEnv is an environment with an empty config directory.
func oaCLIEnv(t *testing.T, extra ...string) []string {
	home := t.TempDir()
	return append([]string{"PATH=" + os.Getenv("PATH"), "HOME=" + home, "XDG_CONFIG_HOME=" + filepath.Join(home, ".config")}, extra...)
}

// The --from-openapi CLI sends its token on every request, so its client
// refuses redirects the way the entity SDK's does. Go re-sends
// Authorization to a redirect on the same hostname at another port, and a
// custom apiKey header to any host at all.
func TestOpenAPICLIRefusesRedirect(t *testing.T) {
	dir := oaTempModule(t)
	cases := []struct {
		name, security, header, value string
		target                        func(*oaRecorder) string
	}{
		{
			name:   "bearer to another port",
			header: "Authorization", value: "Bearer tok-1",
			target: func(other *oaRecorder) string { return other.URL },
		},
		{
			name:     "apiKey to another host",
			security: `,"components":{"securitySchemes":{"key":{"type":"apiKey","in":"header","name":"X-Pets-Key"}}}`,
			header:   "X-Pets-Key", value: "tok-1",
			target: func(other *oaRecorder) string {
				return strings.Replace(other.URL, "127.0.0.1", "localhost", 1)
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doc := strings.TrimSuffix(strings.TrimSpace(oaPetDoc("/")), "}") + tc.security + "}"
			bin := buildOAPets(t, dir, doc)
			other := newOARecorder(t, tc.header, nil)
			origin := newOARecorder(t, tc.header, func(w http.ResponseWriter, r *http.Request) {
				http.Redirect(w, r, tc.target(other)+r.URL.Path, http.StatusTemporaryRedirect)
			})
			cmd := exec.Command(bin, "get-pet-by-id", "--url", origin.URL, "--pet-id", "7")
			cmd.Env = oaCLIEnv(t, "PETS_TOKEN=tok-1")
			out, _ := cmd.CombinedOutput()
			if _, got := origin.seen(); len(got) != 1 || got[0] != tc.value {
				t.Fatalf("the origin saw %s %q, want %q once:\n%s", tc.header, got, tc.value, out)
			}
			if paths, got := other.seen(); len(paths) != 0 {
				t.Errorf("the CLI followed the redirect to %s, sending %s %q:\n%s", tc.target(other), tc.header, got, out)
			}
		})
	}
}

// A login the previous generator stored carries the server URL's path
// (https://pets.example/api/v3): the CLI sends /api/v3/pet/7, not
// /api/v3/api/v3/pet/7.
func TestOpenAPIStoredURLWithPrefix(t *testing.T) {
	dir := oaTempModule(t)
	bin := buildOAPets(t, dir, oaPetDoc("https://pets.example/api/v3"))
	srv := newOARecorder(t, "Authorization", nil)
	env := oaCLIEnv(t)
	run := func(stdin string, args ...string) {
		t.Helper()
		cmd := exec.Command(bin, args...)
		cmd.Env, cmd.Stdin = env, strings.NewReader(stdin)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v: %v\n%s", args, err, out)
		}
	}
	run("tok-1\n", "login", "--url", srv.URL+"/api/v3/", "--with-token")
	run("", "get-pet-by-id", "--pet-id", "7")
	run("", "get-pet-by-id", "--pet-id", "7", "--url", srv.URL)
	paths, auth := srv.seen()
	want := []string{"/api/v3/pet/7", "/api/v3/pet/7"}
	if strings.Join(paths, " ") != strings.Join(want, " ") {
		t.Errorf("server saw %q, want %q", paths, want)
	}
	if len(auth) != 2 || auth[0] != "Bearer tok-1" || auth[1] != "Bearer tok-1" {
		t.Errorf("Authorization %q, want the stored token on both", auth)
	}
}

// An explicit --from-openapi naming the settings entry's document takes
// the entry's api_prefix, the same CLI a settings run builds.
func TestOpenAPIFlagTakesEntryPrefix(t *testing.T) {
	dir := oaTempModule(t)
	if err := os.WriteFile(filepath.Join(dir, "openapi.json"), []byte(oaPetDoc("/api/v3")), 0o644); err != nil {
		t.Fatal(err)
	}
	clitest.WriteSettings(t, dir, "codegen:\n  generators:\n    - name: cli\n      config:\n        binary: pets\n        from_openapi: openapi.json\n        api_prefix: /v9\n")
	prefix := func(args []string) string {
		t.Helper()
		opts, err := parseCLIOptions(args)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := ApplyCLIConfigDefaults(&opts); err != nil {
			t.Fatal(err)
		}
		return cliAPIPrefix(opts.ApiPrefix)
	}
	fromSettings, explicit := prefix(nil), prefix([]string{"--from-openapi=openapi.json"})
	if fromSettings != "/v9" || explicit != "/v9" {
		t.Errorf("settings run prefix %q, --from-openapi run %q; want /v9 for both", fromSettings, explicit)
	}
	// Another document is not the entry's: its prefix does not carry over.
	if got := prefix([]string{"--from-openapi=other.json"}); got == "/v9" {
		t.Errorf("--from-openapi=other.json took the openapi.json entry's prefix")
	}
}

// The token goes under the scheme the document's security requires, not
// the first scheme by name, and an operation's own security overrides
// the document's.
func TestOpenAPIAuthFollowsSecurity(t *testing.T) {
	dir := oaTempModule(t)
	bin := buildOAPets(t, dir, `{"openapi":"3.0.3","security":[{"B":[]}],
	  "components":{"securitySchemes":{"A":{"type":"apiKey","in":"header","name":"X-A"},"B":{"type":"apiKey","in":"header","name":"X-B"}}},
	  "paths":{"/pet/{petId}":{"get":{"operationId":"getPetById","parameters":[{"name":"petId","in":"path","required":true,"schema":{"type":"integer"}}]}},
	    "/pets":{"get":{"operationId":"listPets","security":[{"A":[]}]}}}}`)
	var mu sync.Mutex
	var b []string
	srv := newOARecorder(t, "X-A", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		b = append(b, r.Header.Get("X-B"))
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	})
	for _, args := range [][]string{{"get-pet-by-id", "--pet-id", "7"}, {"list-pets"}} {
		cmd := exec.Command(bin, append(args, "--url", srv.URL)...)
		cmd.Env = oaCLIEnv(t, "PETS_TOKEN=tok-1")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v: %v\n%s", args, err, out)
		}
	}
	_, a := srv.seen()
	mu.Lock()
	defer mu.Unlock()
	if strings.Join(a, ",") != ",tok-1" || strings.Join(b, ",") != "tok-1," {
		t.Errorf("X-A %q, X-B %q; want the token under X-B for get-pet-by-id and under X-A for list-pets", a, b)
	}
}

// An optional requestBody may be left out: the CLI sends no body. The
// schema's required properties bind only a body built from field flags,
// and an optional binary body does not demand --file.
func TestOpenAPIOptionalBodyMayBeOmitted(t *testing.T) {
	dir := oaTempModule(t)
	bin := buildOAPets(t, dir, `{"openapi":"3.0.3","paths":{
	  "/notes":{"post":{"operationId":"createNote","requestBody":{"content":{"application/json":{"schema":{
	    "type":"object","required":["text"],"properties":{"text":{"type":"string"},"title":{"type":"string"}}}}}}}},
	  "/blob":{"put":{"operationId":"putBlob","requestBody":{"content":{"application/octet-stream":{"schema":{"type":"string","format":"binary"}}}}}}}}`)
	var mu sync.Mutex
	var bodies []string
	srv := newOARecorder(t, "Content-Type", func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, string(b))
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	})
	run := func(args ...string) (string, error) {
		cmd := exec.Command(bin, append(args, "--url", srv.URL)...)
		cmd.Env = oaCLIEnv(t)
		out, err := cmd.CombinedOutput()
		return string(out), err
	}
	for _, op := range []string{"create-note", "put-blob"} {
		if out, err := run(op); err != nil {
			t.Errorf("%s with no body: %v\n%s", op, err, out)
		}
	}
	if out, err := run("create-note", "--title", "x"); err == nil || !strings.Contains(out, "--text is required") {
		t.Errorf("a body built without its required --text was sent: %v\n%s", err, out)
	}
	paths, ct := srv.seen()
	mu.Lock()
	defer mu.Unlock()
	if strings.Join(paths, " ") != "/notes /blob" || strings.Join(bodies, "") != "" || strings.Join(ct, "") != "" {
		t.Errorf("server saw paths %q, bodies %q, content types %q; want /notes and /blob with no body", paths, bodies, ct)
	}
}
