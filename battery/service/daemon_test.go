package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/internal/fileperm/filepermtest"
	"time"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/owner"
	"github.com/DonaldMurillo/gofastr/internal/localuser"
)

// devRun is one foreground dev run of a small App, served in-process.
type devRun struct {
	b    *Battery
	ft   *fakeTools
	l    layout
	rt   Runtime
	addr string
	done chan error
	// holding fires when the MCP tool "hold" starts; held then reports
	// whether the App stopped while the cancelled call wound down.
	// appStopped closes when the App's OnStop runs.
	holding    chan struct{}
	held       chan bool
	appStopped chan struct{}
	// stuck fires when the MCP tool "stuck" starts; the call ignores
	// its context and returns only when release closes.
	stuck   chan struct{}
	release chan struct{}
}

func startDevRun(t *testing.T) *devRun {
	t.Helper()
	return startRun(t, "service", "run")
}

// startRun runs the battery with args and waits for its control
// channel.
func startRun(t *testing.T, args ...string) *devRun {
	t.Helper()
	t.Setenv("GOFASTR_ISOLATION", "off")
	t.Setenv("GOFASTR_ROLE", "")
	t.Setenv("GOFASTR_SETUP", "")
	b, ft, _ := testBattery(t, runtime.GOOS, Config{})
	d := &devRun{b: b, ft: ft, done: make(chan error, 1), holding: make(chan struct{}, 1), held: make(chan bool, 1), appStopped: make(chan struct{}),
		stuck: make(chan struct{}, 1), release: make(chan struct{})}
	t.Cleanup(func() { close(d.release) })
	built := make(chan Runtime, 1)
	build := func(rt Runtime) (*framework.App, error) {
		built <- rt
		opts, err := rt.AppOptions()
		if err != nil {
			return nil, err
		}
		app := framework.NewApp(append(opts, framework.WithConfig(framework.AppConfig{Name: "svctest"}))...)
		app.Router().Get("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.WriteString(w, "hello from svctest")
		}))
		// whoami reports the owner id owner-scoped CRUD would use.
		app.Router().Get("/whoami", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id, ok := owner.Get(r.Context())
			if !ok {
				_, _ = io.WriteString(w, "anonymous")
				return
			}
			_, _ = fmt.Fprint(w, id)
		}))
		// whoami over MCP: the identity a `service mcp` session runs as.
		if err := app.MCP.RegisterTool("whoami", "the caller's user id", nil, func(ctx context.Context, _ map[string]any) (any, error) {
			if u, ok := handler.GetUser(ctx); ok {
				if id, ok := u.(interface{ GetID() string }); ok {
					return id.GetID(), nil
				}
			}
			return "anonymous", nil
		}); err != nil {
			return nil, err
		}
		app.OnStop(func() error { close(d.appStopped); return nil })
		// hold blocks until its call is cancelled, then winds down for up
		// to 500ms, the way a real call does. A daemon that waits for the
		// session (up to mcpStopWait, longer than that) cannot run OnStop
		// inside the window; one that drains at once runs it there.
		if err := app.MCP.RegisterTool("hold", "block until cancelled", nil, func(ctx context.Context, _ map[string]any) (any, error) {
			d.holding <- struct{}{}
			<-ctx.Done()
			select {
			case <-d.appStopped:
				d.held <- true
			case <-time.After(500 * time.Millisecond):
				d.held <- false
			}
			return nil, ctx.Err()
		}); err != nil {
			return nil, err
		}
		if err := app.MCP.RegisterTool("stuck", "ignore cancellation", nil, func(context.Context, map[string]any) (any, error) {
			d.stuck <- struct{}{}
			<-d.release
			return nil, nil
		}); err != nil {
			return nil, err
		}
		return app, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() { d.done <- b.Run(ctx, args, build) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-d.done:
		case <-time.After(15 * time.Second):
			t.Error("dev run did not stop")
		}
	})
	l, err := b.layoutFor(true)
	if err != nil {
		t.Fatal(err)
	}
	d.l = l
	deadline := time.Now().Add(20 * time.Second)
	for {
		resp, err := callControl(l.socketPath(), "status")
		if err == nil {
			d.addr = resp.Addr
			break
		}
		select {
		case err := <-d.done:
			t.Fatalf("dev run exited before ready: %v", err)
		default:
		}
		if time.Now().After(deadline) {
			t.Fatalf("dev run never answered status: %v", err)
		}
		time.Sleep(50 * time.Millisecond)
	}
	d.rt = <-built
	return d
}

func (d *devRun) get(t *testing.T, path, host string, cookie *http.Cookie) (*http.Response, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, "http://"+d.addr+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if host != "" {
		req.Host = host
	}
	if cookie != nil {
		req.AddCookie(cookie)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp, string(body)
}

func (d *devRun) login(t *testing.T, tok string) *http.Response {
	t.Helper()
	resp, err := http.PostForm("http://"+d.addr+loginPath, url.Values{"t": {tok}})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	return resp
}

func TestDevRunEndToEnd(t *testing.T) {
	d := startDevRun(t)

	if !d.rt.Dev || d.rt.DataDir != d.l.dir || len(d.rt.Secret) < 32 {
		t.Fatalf("builder got Runtime %+v; want Dev, DataDir %s, a secret", d.rt, d.l.dir)
	}
	filepermtest.AssertOwnerOnly(t, d.l.dir, 0o700)
	if !strings.HasPrefix(d.addr, "127.0.0.1:") {
		t.Fatalf("bound %q; want 127.0.0.1", d.addr)
	}

	// Every path is gated, the framework's own included.
	for _, p := range []string{"/", "/healthz", "/__gofastr/sse"} {
		resp, body := d.get(t, p, "", nil)
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("GET %s without a session answered %d; want 401", p, resp.StatusCode)
		}
		if !strings.Contains(body, "svctest service open --dev") {
			t.Errorf("GET %s 401 hint %q does not name the dev run's sign-in", p, body)
		}
	}
	_, port, _ := strings.Cut(d.addr, ":")
	if resp, _ := d.get(t, "/", "rebind.example:"+port, nil); resp.StatusCode != http.StatusForbidden {
		t.Errorf("foreign Host answered %d; want 403", resp.StatusCode)
	}

	tokResp, err := callControl(d.l.socketPath(), "token")
	if err != nil {
		t.Fatal(err)
	}
	resp := d.login(t, tokResp.Token)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login answered %d", resp.StatusCode)
	}
	cookies := resp.Cookies()
	if len(cookies) != 1 {
		t.Fatalf("login set %d cookies", len(cookies))
	}
	if r, body := d.get(t, "/", "", cookies[0]); r.StatusCode != http.StatusOK || body != "hello from svctest" {
		t.Fatalf("signed-in GET / answered %d %q", r.StatusCode, body)
	}
	if r := d.login(t, tokResp.Token); r.StatusCode != http.StatusForbidden {
		t.Fatalf("token redeemed twice: %d", r.StatusCode)
	}

	// A second dev run of the same app refuses before building.
	var out strings.Builder
	b2 := New(Config{ID: d.b.cfg.ID, Port: d.b.cfg.Port, Stdout: &out, Stderr: &out})
	err = b2.Run(context.Background(), []string{"service", "run"}, func(Runtime) (*framework.App, error) {
		t.Error("second instance called the builder")
		return nil, nil
	})
	if err == nil || !strings.Contains(err.Error(), "is already running") || !strings.Contains(err.Error(), "service status") {
		t.Fatalf("second instance error = %v", err)
	}

	// Stop over the control channel: Run returns nil (exit 0).
	if _, err := callControl(d.l.socketPath(), "stop"); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-d.done:
		if err != nil {
			t.Fatalf("graceful stop returned %v", err)
		}
		d.done <- nil
	case <-time.After(15 * time.Second):
		t.Fatal("control stop did not end the run")
	}
	if _, err := callControl(d.l.socketPath(), "status"); err == nil {
		t.Fatal("control channel still answers after stop")
	}
}

var reHiddenToken = regexp.MustCompile(`name="t" value="([^"]+)"`)

// `open` hands the browser a 0600 local page whose form POSTs a live
// token; the token never reaches argv.
func TestOpenWritesSignInPage(t *testing.T) {
	d := startDevRun(t)
	if err := d.b.openWith(d.l, false); err != nil {
		t.Fatal(err)
	}
	calls := d.ft.got()
	if len(calls) != 1 {
		t.Fatalf("launch calls %q", calls)
	}
	fields := strings.Fields(calls[0])
	page := fields[len(fields)-1]
	if !strings.HasPrefix(page, d.l.loginDir()) || !strings.HasSuffix(page, ".html") {
		t.Fatalf("launched %q; want a page in %s", calls[0], d.l.loginDir())
	}
	filepermtest.AssertOwnerOnly(t, page, 0o600)
	raw, err := os.ReadFile(page)
	if err != nil {
		t.Fatal(err)
	}
	m := reHiddenToken.FindStringSubmatch(string(raw))
	if m == nil {
		t.Fatalf("no token field in page:\n%s", raw)
	}
	if strings.Contains(calls[0], m[1]) {
		t.Fatal("the token reached the launcher's argv")
	}
	if !strings.Contains(string(raw), `action="http://`+d.addr+loginPath+`"`) {
		t.Fatalf("form action does not target the daemon:\n%s", raw)
	}
	if r := d.login(t, m[1]); r.StatusCode != http.StatusOK {
		t.Fatalf("page token answered %d; want 200", r.StatusCode)
	}
}

func TestOpenPrintWithHatch(t *testing.T) {
	d := startDevRun(t)
	d.b.cfg.InsecureTokenInURL = true
	var out strings.Builder
	d.b.cfg.Stdout = &out
	if err := d.b.openWith(d.l, true); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out.String(), "http://"+d.addr+loginPath+"?t=") {
		t.Fatalf("printed %q", out.String())
	}
	if len(d.ft.got()) != 0 {
		t.Fatalf("--print launched a browser: %q", d.ft.got())
	}
}

// signIn redeems a fresh control-channel token and returns the cookie.
func (d *devRun) signIn(t *testing.T) *http.Cookie {
	t.Helper()
	tok, err := callControl(d.l.socketPath(), "token")
	if err != nil {
		t.Fatal(err)
	}
	resp := d.login(t, tok.Token)
	if resp.StatusCode != http.StatusOK || len(resp.Cookies()) != 1 {
		t.Fatalf("login answered %d with %d cookies", resp.StatusCode, len(resp.Cookies()))
	}
	return resp.Cookies()[0]
}

// keepOwnerExtractor restores the process-wide owner extractor.
func keepOwnerExtractor(t *testing.T) {
	t.Helper()
	prev := owner.GetExtractor()
	t.Cleanup(func() { owner.SetExtractor(prev) })
}

// A signed-in browser is the OS user the service runs as: owner-scoped
// CRUD sees the stable local identity stored in the state dir.
func TestSignedInRequestIsLocalUser(t *testing.T) {
	keepOwnerExtractor(t)
	owner.SetExtractor(nil)
	d := startDevRun(t)
	stored, err := localuser.LoadOrMint(d.l.dir)
	if err != nil {
		t.Fatal(err)
	}
	if r, body := d.get(t, "/whoami", "", d.signIn(t)); r.StatusCode != http.StatusOK || body != stored.GetID() {
		t.Fatalf("signed-in /whoami answered %d %q; want the local identity %q", r.StatusCode, body, stored.GetID())
	}
	if r, body := d.get(t, "/whoami", "", nil); r.StatusCode != http.StatusUnauthorized || strings.Contains(body, stored.GetID()) {
		t.Fatalf("anonymous /whoami answered %d %q; want 401 without the identity", r.StatusCode, body)
	}
}

// An app that links battery/auth keeps auth's identity: the gate gates,
// and installs no local admin over auth's "anonymous".
func TestAuthOwnedIdentityGetsNoLocalUser(t *testing.T) {
	keepOwnerExtractor(t)
	owner.SetExtractor(func(ctx context.Context) (any, bool) {
		u, ok := handler.GetUser(ctx)
		if !ok {
			return nil, false
		}
		return fmt.Sprintf("auth:%T", u), true
	})
	d := startDevRun(t)
	if r, body := d.get(t, "/whoami", "", d.signIn(t)); r.StatusCode != http.StatusOK || body != "anonymous" {
		t.Fatalf("signed-in /whoami under a foreign extractor answered %d %q; want anonymous", r.StatusCode, body)
	}
}

// --addr is what `gofastr dev` passes its child: the dev run on that
// loopback port, gate included.
func TestDevLoopAddrIsGatedDevRun(t *testing.T) {
	d := startRun(t, "--addr", "localhost:0")
	if !d.rt.Dev || d.rt.DataDir != d.l.dir {
		t.Fatalf("dev loop Runtime %+v; want the dev state dir %s", d.rt, d.l.dir)
	}
	_, port, _ := strings.Cut(d.addr, ":")
	if r, _ := d.get(t, "/", "localhost:"+port, nil); r.StatusCode != http.StatusUnauthorized {
		t.Fatalf("dev loop GET / without a session answered %d; want 401", r.StatusCode)
	}
	if r, body := d.get(t, "/", "localhost:"+port, d.signIn(t)); r.StatusCode != http.StatusOK || body != "hello from svctest" {
		t.Fatalf("dev loop signed-in GET / answered %d %q", r.StatusCode, body)
	}
}

func TestDevLoopAddrRefusesNonLoopback(t *testing.T) {
	b, _, _ := testBattery(t, runtime.GOOS, Config{})
	for _, addr := range []string{"0.0.0.0:8080", "192.168.1.5:8080", "[::]:8080", "example.com:80", "8080", "localhost:http", "localhost:70000"} {
		err := b.Run(context.Background(), []string{"--addr", addr}, func(Runtime) (*framework.App, error) {
			t.Errorf("builder called for --addr %s", addr)
			return nil, nil
		})
		if err == nil {
			t.Errorf("--addr %s accepted", addr)
		}
	}
}

// A requested stop exits 0 even when the drain fails: under launchd's
// KeepAlive SuccessfulExit=false an exit 1 restarts the daemon that
// `service stop` just stopped. The failure is logged instead.
func TestStopExitsCleanWhenDrainFails(t *testing.T) {
	t.Setenv("GOFASTR_ISOLATION", "off")
	t.Setenv("GOFASTR_ROLE", "")
	t.Setenv("GOFASTR_SETUP", "")
	b, _, _ := testBattery(t, runtime.GOOS, Config{})
	build := func(rt Runtime) (*framework.App, error) {
		opts, err := rt.AppOptions()
		if err != nil {
			return nil, err
		}
		app := framework.NewApp(append(opts, framework.WithConfig(framework.AppConfig{Name: "svctest"}))...)
		app.OnStop(func() error { return errors.New("flush failed") })
		return app, nil
	}
	done := make(chan error, 1)
	go func() { done <- b.Run(context.Background(), []string{"service", "run"}, build) }()
	l, err := b.layoutFor(true)
	if err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(20 * time.Second); ; {
		if _, err := callControl(l.socketPath(), "status"); err == nil {
			break
		}
		select {
		case err := <-done:
			t.Fatalf("run exited before ready: %v", err)
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("run never answered status")
		}
		time.Sleep(50 * time.Millisecond)
	}
	if _, err := callControl(l.socketPath(), "stop"); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("requested stop with a failed drain returned %v; want nil (exit 0)", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("control stop did not end the run")
	}
}
