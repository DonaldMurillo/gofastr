package harnesscmd

import (
	"io"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
	xharness "github.com/DonaldMurillo/gofastr/framework/experimental/harness"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/logging"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/profile"
)

func TestHarnessRefusesToBootWithoutACredstoreKey(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("GOFASTR_HARNESS_PASSPHRASE", "")
	t.Setenv("GOFASTR_HARNESS_MACHINE_KEY", "")
	var code int
	errOut := clitest.CovT_capStderr(t, func() {
		code = clitest.CovT_capExit(t, func() { RunHarness([]string{"--prompt", "hi"}) })
	})
	if code != 1 || !strings.Contains(errOut, noCredstoreKeyHelp) {
		t.Fatalf("exit %d, stderr %q: want exit 1 with the missing-key help", code, errOut)
	}
}

func TestWantWebSidecar(t *testing.T) {
	for _, tc := range []struct {
		name                  string
		listen, prompt        string
		web, noWeb, tty, want bool
	}{
		{name: "interactive terminal", tty: true, want: true},
		{name: "piped input", tty: false, want: false},
		{name: "--no-web", tty: true, noWeb: true, want: false},
		{name: "--prompt", tty: true, prompt: "hi", want: false},
		{name: "--web", web: true, prompt: "hi", want: true},
		{name: "--listen", listen: "127.0.0.1:0", noWeb: true, prompt: "hi", want: true},
	} {
		if got := wantWebSidecar(tc.listen, tc.web, tc.noWeb, tc.prompt, tc.tty); got != tc.want {
			t.Errorf("%s: wantWebSidecar = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestChatPageCarriesTheTokenInAMetaTag(t *testing.T) {
	out := string(renderChat("127.0.0.1:1", "sess_meta", "meta-token")) // not-a-secret: synthetic fixture
	for _, name := range []string{"harness-session", "harness-token"} {
		if !regexp.MustCompile(`<meta [^>]*name="` + name + `"[^>]*>`).MatchString(out) {
			t.Errorf("no meta tag named %s in the chat page", name)
		}
	}
	if !regexp.MustCompile(`<meta [^>]*content="meta-token"[^>]*>`).MatchString(out) {
		t.Error("the token is not carried in a meta tag")
	}
}

func TestChatJSEscapeDenies(t *testing.T) {
	if !strings.Contains(chatJS, "if (e.key === 'Escape' && !permModal.classList.contains('hidden')) {\n      answerPermission('deny', 'once');") {
		t.Fatal("Escape on an open permission modal must answer deny, once")
	}
}

// The listener's guards are pinned to the authority it bound: the chat page,
// the REST API and its Origin check all refuse another host.
func TestHTTPListenerPinsTheBoundAuthority(t *testing.T) {
	t.Setenv("GOFASTR_HARNESS_MACHINE_KEY", "")
	prof, err := profile.Embedded("default")
	if err != nil {
		t.Fatal(err)
	}
	h, err := xharness.New(xharness.Config{Profile: prof, WorkingDir: t.TempDir(), XDGConfig: t.TempDir(), XDGState: t.TempDir(),
		Logger: logging.New(nil, logging.LevelWarn), CredstorePass: "listener-pass"})
	if err != nil {
		t.Fatal(err)
	}
	defer h.Shutdown()
	sess, err := h.CreateSession(h.Providers[0], "m")
	if err != nil {
		t.Fatal(err)
	}
	url, token, shutdown, err := startHTTPListener(h, sess, "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer shutdown()
	bound := strings.TrimPrefix(url, "http://")
	get := func(path, host, origin string) (int, string) {
		t.Helper()
		req, err := http.NewRequest(http.MethodGet, url+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Host = host
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		req.Header.Set("Authorization", "Bearer "+token)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		return res.StatusCode, string(b)
	}
	if code, body := get("/", bound, ""); code != http.StatusOK || !strings.Contains(body, token) {
		t.Fatalf("chat page on the bound Host: %d", code)
	}
	if code, body := get("/", "rebound.example:"+strings.Split(bound, ":")[1], ""); code != http.StatusForbidden || strings.Contains(body, token) {
		t.Errorf("chat page on a rebound Host: %d, token leaked: %v", code, strings.Contains(body, token))
	}
	if code, _ := get("/v1/sessions", bound, ""); code != http.StatusOK {
		t.Errorf("REST on the bound Host: %d", code)
	}
	if code, _ := get("/v1/sessions", "rebound.example", ""); code != http.StatusForbidden {
		t.Errorf("REST on a rebound Host: %d, want 403", code)
	}
	if code, _ := get("/v1/sessions", bound, "http://evil.example"); code != http.StatusForbidden {
		t.Errorf("REST with a cross-site Origin: %d, want 403", code)
	}
}
