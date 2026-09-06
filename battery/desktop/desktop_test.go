package desktop

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	appui "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

// Group 8: Emit.

func TestEmitPayloadRoundTripsAsJSONString(t *testing.T) {
	b, _ := newTestBattery(t)
	payload := map[string]any{
		"html":       "</script>",
		"whitespace": "line\u2028sep\u2029here",
		"proto":      map[string]any{"__proto__": 1},
	}
	win := &fakeWindow{}
	b.windowMu.Lock()
	b.window = win
	b.windowMu.Unlock()
	if err := b.Emit("menu_file_new", payload); err != nil {
		t.Fatal(err)
	}
	evals := win.evals()
	if len(evals) != 1 {
		t.Fatalf("evals = %v", evals)
	}
	js := evals[0]
	if !strings.Contains(js, "JSON.parse(") {
		t.Fatalf("eval must hand the payload to JSON.parse: %q", js)
	}
	// No object literal: the payload appears only inside a quoted
	// string. Assert the quoted-JSON form is present.
	data, _ := json.Marshal(payload)
	if !strings.Contains(js, fmt.Sprintf("%q", string(data))) {
		t.Fatalf("eval does not carry the quoted JSON payload:\n%s\nwant %q", js, string(data))
	}
	if !strings.Contains(js, `"menu_file_new"`) {
		t.Fatalf("event name not embedded as a quoted string: %q", js)
	}
}

func TestEmitInvalidNameRejected(t *testing.T) {
	b, _ := newTestBattery(t)
	win := &fakeWindow{}
	b.windowMu.Lock()
	b.window = win
	b.windowMu.Unlock()
	for _, bad := range []string{"", "BadName", "menu.file.new", "9x", "with space"} {
		if err := b.Emit(bad, nil); err == nil {
			t.Fatalf("Emit(%q) accepted", bad)
		}
	}
	if len(win.evals()) != 0 {
		t.Fatal("invalid name still evaluated JS")
	}
}

func TestEmitBeforeWindowErrors(t *testing.T) {
	b, _ := newTestBattery(t)
	if err := b.Emit("evt", nil); err == nil {
		t.Fatal("Emit before window must error")
	}
}

// e2eScreen is a one-screen site for the Run e2e.
type e2eScreen struct{}

func (e2eScreen) Render() render.HTML {
	return html.Heading(html.HeadingConfig{Level: 1}, render.Text("Desktop e2e"))
}

// waitFor polls until cond is true or the deadline passes.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestRunEndToEndWithFakeShell(t *testing.T) {
	t.Setenv("GOFASTR_ISOLATION", "off")

	site := appui.NewApp("DesktopE2E")
	layout := appui.NewLayout("public").WithContainer()
	site.Register("/", e2eScreen{}, layout)
	host := uihost.New(site)

	shell := newFakeShell()
	b := New(Config{ID: "e2e.example.app", Title: "E2E", Shell: shell})
	app := framework.NewApp(
		framework.WithConfig(framework.AppConfig{Name: "desktope2e"}),
	)
	app.Mount(host)
	app.RegisterBattery(b)

	runErr := make(chan error, 1)
	go func() { runErr <- b.Run(app) }()

	// The window opens and navigates to the enter URL.
	waitFor(t, "shell.Run to start", func() bool {
		_, ok := shell.config()
		return ok
	})
	win := func() *fakeWindow {
		w, _ := b.Window()
		fw, _ := w.(*fakeWindow)
		return fw
	}
	waitFor(t, "ready callback to store the window", func() bool { return win() != nil })
	waitFor(t, "initial navigation", func() bool { return len(win().navURLs()) > 0 })
	enterURL := win().navURLs()[0]
	if !strings.Contains(enterURL, "/__gofastr/desktop/enter?t=") {
		t.Fatalf("enter URL = %q", enterURL)
	}

	// A real HTTP client walks the handshake and gets the page.
	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Get(enterURL)
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("enter: %d", resp.StatusCode)
	}
	cookies := resp.Cookies()
	if len(cookies) != 1 || cookies[0].Name != sessionCookieName {
		t.Fatalf("enter cookies = %+v", cookies)
	}

	pageURL := strings.Replace(enterURL, "/__gofastr/desktop/enter?t="+b.bootToken, "/", 1)
	pageReq, _ := http.NewRequest(http.MethodGet, pageURL, nil)
	pageReq.AddCookie(cookies[0])
	pageResp, err := http.DefaultClient.Do(pageReq)
	if err != nil {
		t.Fatal(err)
	}
	pageBody, _ := io.ReadAll(pageResp.Body)
	pageResp.Body.Close()
	if pageResp.StatusCode != http.StatusOK {
		t.Fatalf("page: %d %s", pageResp.StatusCode, pageBody)
	}
	if !strings.Contains(string(pageBody), "Desktop e2e") {
		t.Fatalf("page body missing the screen heading: %.200s", pageBody)
	}

	// A bridge call round-trips with the cookie.
	callURL := strings.Replace(enterURL, "/__gofastr/desktop/enter?t="+b.bootToken, "/__gofastr/desktop/call/window/title", 1)
	req, _ := http.NewRequest(http.MethodPost, callURL, strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookies[0])
	callResp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	callBody, _ := io.ReadAll(callResp.Body)
	callResp.Body.Close()
	if callResp.StatusCode != http.StatusOK {
		t.Fatalf("bridge call: %d %s", callResp.StatusCode, callBody)
	}
	if !strings.Contains(string(callBody), `"ok":true`) || !strings.Contains(string(callBody), "E2E") {
		t.Fatalf("bridge call body: %s", callBody)
	}

	// Without the cookie the same call is refused.
	bareURL := strings.Replace(callURL, "http://", "http://", 1)
	bare, _ := http.NewRequest(http.MethodPost, bareURL, strings.NewReader("{}"))
	bare.Header.Set("Content-Type", "application/json")
	bareResp, err := http.DefaultClient.Do(bare)
	if err != nil {
		t.Fatal(err)
	}
	bareResp.Body.Close()
	if bareResp.StatusCode != http.StatusForbidden {
		t.Fatalf("no-cookie call: %d, want 403", bareResp.StatusCode)
	}

	// Quit: Run returns nil and the app is shut down; a second request
	// is refused with a connection error.
	shell.Quit()
	select {
	case err := <-runErr:
		if err != nil {
			t.Fatalf("Run returned %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("Run did not return after Quit")
	}

	hostPort := strings.TrimPrefix(strings.TrimPrefix(callURL, "http://"), "")
	_, port, _ := net.SplitHostPort(hostPort)
	conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", port), 2*time.Second)
	if err == nil {
		conn.Close()
		t.Fatal("listener still open after Run returned")
	}
	// The dial error IS the proof; err != nil is asserted above by
	// failing when the dial succeeded.
}

// Group 11: AppOptions.

func TestAppOptionsCreatesDirDBAndSecretStable(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(base, "xdg"))
	t.Setenv("HOME", filepath.Join(base, "home"))

	opts, err := AppOptions("notes.example.app")
	if err != nil {
		t.Fatal(err)
	}
	if len(opts) != 2 {
		t.Fatalf("opts = %d, want 2", len(opts))
	}

	cfgBase, err := os.UserConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(cfgBase, "notes.example.app")
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		t.Fatalf("data dir: %v", err)
	}
	if perm := info.Mode().Perm(); perm&0o077 != 0 {
		t.Fatalf("data dir mode = %o", perm)
	}
	if _, err := os.Stat(filepath.Join(dir, "app.db")); err != nil {
		t.Fatalf("app.db: %v", err)
	}
	secretPath := filepath.Join(dir, "secret")
	sinfo, err := os.Stat(secretPath)
	if err != nil {
		t.Fatalf("secret: %v", err)
	}
	if perm := sinfo.Mode().Perm(); perm != 0o600 {
		t.Fatalf("secret mode = %o, want 0600", perm)
	}
	secret1, err := loadOrMintSecret(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(secret1) < 40 {
		t.Fatalf("secret suspiciously short: %d chars", len(secret1))
	}

	// A second call reuses both (same secret value).
	opts2, err := AppOptions("notes.example.app")
	if err != nil {
		t.Fatal(err)
	}
	_ = opts2
	secret2, err := loadOrMintSecret(dir)
	if err != nil {
		t.Fatal(err)
	}
	if secret1 != secret2 {
		t.Fatal("secret not stable across calls")
	}
}

func TestAppOptionsRejectsBadID(t *testing.T) {
	if _, err := AppOptions("no-dot"); err == nil {
		t.Fatal("bad id accepted")
	}
}

// New/Config validation.

func TestNewValidatesConfig(t *testing.T) {
	cases := []string{"", "nodot", "has space.example", "slash/example"}
	for _, id := range cases {
		func() {
			defer func() {
				if r := recover(); r == nil {
					t.Fatalf("New(%q) accepted", id)
				}
			}()
			New(Config{ID: id})
		}()
	}
}

func TestNewDefaultsAndAccessors(t *testing.T) {
	shell := newFakeShell()
	b := New(Config{ID: "ok.example.app", Shell: shell})
	if b.Name() != "desktop" {
		t.Fatalf("Name = %q", b.Name())
	}
	if b.Shell() != shell {
		t.Fatal("Shell() mismatch")
	}
	if _, ok := b.Window(); ok {
		t.Fatal("Window before ready must be false")
	}
	cfg, _ := shell.config()
	// Not run yet.
	_ = cfg
}

// component.Component compile-time check for the e2e screen.
var _ component.Component = e2eScreen{}
