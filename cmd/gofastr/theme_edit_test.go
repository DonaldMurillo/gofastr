package main

import (
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/harnesscmd"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/themecmd"
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	uitheme "github.com/DonaldMurillo/gofastr/framework/ui/theme"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

// newTestServer builds a themeEditServer backed by a real in-process UIHost
// (the same wiring runThemeEdit uses, minus the network listener). No DB,
// no framework.App: UIHost.New takes a core-ui/app.App directly.
func newTestServer(t *testing.T) *themecmd.ThemeEditServer {
	t.Helper()
	outDir := filepath.Join(t.TempDir(), "theme")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatal(err)
	}
	base := uitheme.Default()
	a := app.NewApp("theme-edit-test").WithTheme(base)
	a.Register("/preview", &themecmd.GalleryPreviewScreen{}, nil)
	host := uihost.New(a)
	return &themecmd.ThemeEditServer{
		Host:    host,
		Base:    base,
		Working: base,
		OutPath: filepath.Join(outDir, "theme.go"),
		Hosts:   []string{"127.0.0.1:0"},
		Origins: []string{"http://127.0.0.1:0"},
		Token:   "test-token", // not-a-secret: fixture bearer for the in-process test server
	}
}

// The reflection walk has to actually reach the fields it claims to. A walk
// that silently visited nothing would make the injection test vacuous, the
// exact failure mode the hand-written list had, just harder to see.
func TestInjectEveryThemeStringReachesNestedFields(t *testing.T) {
	th := uitheme.Default()
	clitest.InjectEveryThemeString(reflect.ValueOf(&th).Elem(), "SENTINEL")
	for name, got := range map[string]string{
		"Colors.Primary.Value":  th.Colors.Primary.Value,
		"Fonts.Body.Value":      th.Fonts.Body.Value,
		"Shadows.MD.Value":      th.Shadows.MD.Value,
		"Easings.Spring.Value":  th.Easings.Spring.Value,
		"Typography.Base.Value": th.Typography.Base.Value,
		"Code.KW.Value":         th.Code.KW.Value,
		"Name":                  th.Name,
	} {
		if got != "SENTINEL" {
			t.Errorf("%s = %q — the walk did not reach it", name, got)
		}
	}
}

func TestThemeHelpRoutesLocally(t *testing.T) {
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"theme", "--help"}, "Usage: gofastr theme <subcommand>"},
		{[]string{"theme", "edit", "--help"}, "Usage: gofastr theme edit"},
		{[]string{"semantic", "--help"}, "semantic search for the project at cwd"},
	}
	for _, tc := range cases {
		out := clitest.CovT_capStdout(t, func() { dispatch(tc.args) })
		if !strings.Contains(out, tc.want) {
			t.Errorf("%v printed the wrong help:\n%s", tc.args, out)
		}
		if strings.Contains(out, "Start dev server with auto-restart") {
			t.Errorf("%v fell back to global help:\n%s", tc.args, out)
		}
	}

	var code int
	out := clitest.CovT_capStdout(t, func() {
		code = clitest.CovT_capExit(t, func() { dispatch([]string{"theme", "edit", "help"}) })
	})
	if code != -1 {
		t.Errorf("theme edit help exited %d, want a normal return", code)
	}
	if !strings.Contains(out, "Usage: gofastr theme edit") || strings.Contains(out, "Unknown flag") {
		t.Errorf("theme edit help printed the wrong output:\n%s", out)
	}
}

// The bearer token must not be the harness's HMAC signing key. The controls
// page is served with no authentication, and it is where the token comes
// from, so publishing a machine-persistent signing key there hands it to any
// local process, any other user on the box, and any screenshot in a bug report.
//
// This pins the property by reading the token the SERVER ACTUALLY SERVES in
// its <meta name="theme-edit-token"> tag, the same bytes any unauthenticated
// localhost reader can lift off the page, and asserting those bytes are not
// the harness signing key. The previous test only compared two values it
// generated itself and so passed even when runThemeEdit published the signing
// key verbatim (proven by mutation: replacing newThemeEditToken with
// hex.EncodeToString(deriveListenerSecret()) left the suite green).
func TestThemeEditTokenIsNotTheHarnessSigningKey(t *testing.T) {
	// Pin the signing key to a known deterministic value: with the machine
	// key set, deriveListenerSecret returns sha256("harness-http:" || key),
	// which is the HMAC key signing every harness control-plane token.
	t.Setenv("GOFASTR_HARNESS_MACHINE_KEY", strings.Repeat("ab", 32))
	signingKey := hex.EncodeToString(harnesscmd.DeriveListenerSecret())

	srv := newTestServer(t)
	tok, err := themecmd.NewThemeEditToken()
	if err != nil {
		t.Fatalf("newThemeEditToken: %v", err)
	}
	srv.Token = tok

	rec := httptest.NewRecorder()
	srv.ServeControlsPage(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	body := rec.Body.String()

	// The page must serve the server's own freshly generated token. Attribute
	// order in the meta tag is not guaranteed (render.writeAttrs sorts keys
	// alphabetically), so assert the name= and content= pair separately.
	if !strings.Contains(body, `name="theme-edit-token"`) {
		t.Fatalf("server did not emit <meta name=\"theme-edit-token\">; served:\n%s", clitest.Truncate(body, 400))
	}
	if !strings.Contains(body, `content="`+tok+`"`) {
		t.Fatalf("server did not serve its own generated token %q; served:\n%s",
			tok, clitest.Truncate(body, 400))
	}

	// The load-bearing assertion: the bytes the page serves must not be the
	// harness signing key. The exact regression the comment at the call site
	// describes is runThemeEdit setting srv.token = hex(deriveListenerSecret()),
	// publishing the HMAC key on an unauthenticated page.
	if tok == signingKey {
		t.Fatalf("the theme editor's served bearer token equals the harness signing key.\n" +
			"runThemeEdit must use freshly random bytes (newThemeEditToken), " +
			"not deriveListenerSecret().")
	}

	// And the token must be per-session random, not a constant or a derived
	// value the way the signing key is.
	tok2, err := themecmd.NewThemeEditToken()
	if err != nil {
		t.Fatalf("second newThemeEditToken: %v", err)
	}
	if tok == tok2 {
		t.Errorf("theme edit token is not per-session random: two calls returned the same value %q", tok)
	}
}
