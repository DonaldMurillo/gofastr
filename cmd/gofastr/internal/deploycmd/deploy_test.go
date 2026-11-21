package deploycmd

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/sdlisten"
)

// The test binary doubles as the app being deployed: started with this
// variable set, it serves the health path on that port until killed, or
// exits at once when told to be a release that does not start.
func TestMain(m *testing.M) {
	if port := os.Getenv("GOFASTR_DEPLOY_TEST_SERVE"); port != "" {
		if os.Getenv("GOFASTR_DEPLOY_TEST_BROKEN") != "" {
			os.Exit(1)
		}
		mux := http.NewServeMux()
		mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, "ok") })
		mux.HandleFunc("/ready", func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, "ok") })
		mux.HandleFunc("/env", func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, os.Getenv("SECRET")) })
		mux.HandleFunc("/sick", func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "no", http.StatusServiceUnavailable) })
		_ = http.ListenAndServe("127.0.0.1:"+port, mux)
		os.Exit(1)
	}
	os.Exit(m.Run())
}

// server is a stand-in for the machine being deployed to: a directory
// for its root, stubs for the commands only root or systemd can run,
// and a runner that executes what the command would send over SSH with
// bash, here. The scripts themselves, the release directories, the
// link swap and the health check are the real ones.
type server struct {
	t     *testing.T
	dir   string
	stubs string
	port  int
	arch  string
	calls []string // what the command asked ssh to run
	// unreachable makes ssh itself fail.
	unreachable bool
	// built is every package the command asked to build.
	built []string
	// broken makes the next build produce a release that exits at once.
	broken bool
	// socketApp makes the next build produce a binary that can take a
	// socket from systemd, as an app on this framework version is.
	socketApp bool
}

func newServer(t *testing.T) *server {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("the server-side script is for a Linux server (bash, /dev/tcp, mv -T)")
	}
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("no bash")
	}
	s := &server{t: t, dir: t.TempDir(), stubs: t.TempDir(), port: freePort(t), arch: "x86_64"}
	stub := func(name, body string) {
		if err := os.WriteFile(filepath.Join(s.stubs, name), []byte("#!/bin/bash\n"+body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	log := `echo "$(basename "$0") $*" >> "` + s.dir + `/calls.log"` + "\n"
	stub("id", `[ -f "`+s.dir+`/user-$2" ]`+"\n")
	stub("useradd", log+`touch "`+s.dir+`/user-${@: -1}"`+"\n")
	stub("chown", log)
	stub("journalctl", log+`echo "the app said: cannot start"`+"\n")
	stub("systemctl", log+`
pidfile="`+s.dir+`/pid"
case "$1" in
  restart)
    if [ -f "$pidfile" ]; then kill "$(cat "$pidfile")" 2>/dev/null || true; rm -f "$pidfile"; sleep 0.2; fi
    # A server where nothing starts (a full disk, a port held).
    if [ -f "`+s.dir+`/nothing-starts" ]; then exit 1; fi
    if [ -f "`+s.dir+`/restart-error-once" ]; then
      unlink "`+s.dir+`/restart-error-once"
      echo "service activation refused" >&2
      exit 1
    fi
    envfile="$(sed -n 's/^EnvironmentFile=-//p' "`+s.dir+`/etc/systemd/system/$2.service")"
    if [ -f "$envfile" ]; then set -a; . "$envfile"; set +a; fi
    "`+s.dir+`/opt/$2/current/$2" >/dev/null 2>&1 &
    echo $! > "$pidfile"
    ;;
  enable)
    if [ "$2" = "--now" ]; then
      if [ -f "`+s.dir+`/socket-error" ]; then
        echo "socket activation refused" >&2
        exit 1
      fi
      touch "`+s.dir+`/socket-on"
    fi
    ;;
  disable)
    rm -f "`+s.dir+`/socket-on"
    ;;
  stop)
    if [ "$2" = "shop.socket" ]; then
      unlink "`+s.dir+`/socket-on"
    elif [ -f "$pidfile" ]; then
      kill "$(cat "$pidfile")" 2>/dev/null || true
      unlink "$pidfile"
    fi
    ;;
  is-active)
    name="${@: -1}"
    case "$name" in *.socket) [ -f "`+s.dir+`/socket-on" ]; exit $? ;; esac
    if [ -f "`+s.dir+`/always-active" ] || { [ -f "$pidfile" ] && kill -0 "$(cat "$pidfile")" 2>/dev/null; }; then
      [ "$2" = "--quiet" ] || echo active
      exit 0
    fi
    [ "$2" = "--quiet" ] || echo inactive; exit 3
    ;;
esac
exit 0
`)
	t.Cleanup(func() {
		if b, err := os.ReadFile(filepath.Join(s.dir, "pid")); err == nil {
			if pid, err := strconv.Atoi(strings.TrimSpace(string(b))); err == nil {
				if p, err := os.FindProcess(pid); err == nil {
					_ = p.Kill()
				}
			}
		}
	})
	return s
}

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

// ssh runs command with bash as the server would, with the stubs first
// on PATH.
func (s *server) ssh(_ context.Context, o *options, command string, stdin io.Reader) (string, int, error) {
	s.calls = append(s.calls, command)
	if s.unreachable {
		return "", 255, errors.New("ssh could not reach " + o.target + ": connection refused")
	}
	if strings.HasPrefix(command, "uname -m") {
		dir, err := os.MkdirTemp(s.dir, "upload-")
		if err != nil {
			s.t.Fatal(err)
		}
		return s.arch + "\n" + dir + "\n", 0, nil
	}
	cmd := exec.Command("bash", "-c", strings.ReplaceAll(command, "sudo -n bash -s", "bash -s"))
	cmd.Env = append(os.Environ(), "PATH="+s.stubs+":"+os.Getenv("PATH"))
	cmd.Stdin = stdin
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return out.String(), exit.ExitCode(), nil
	}
	if err != nil {
		s.t.Fatal(err)
	}
	return out.String(), 0, nil
}

func (s *server) root() string { return s.dir }

// build writes the "binary": a script that runs this test binary as the
// app, on the server's port.
func (s *server) build(_ context.Context, pkg, arch, out string) error {
	s.built = append(s.built, pkg+" "+arch)
	self, err := os.Executable()
	if err != nil {
		return err
	}
	broken := ""
	if s.broken {
		broken = "GOFASTR_DEPLOY_TEST_BROKEN=1 "
	}
	mark := ""
	if s.socketApp {
		mark = "# " + sdlisten.Marker + "\n"
	}
	return os.WriteFile(out, []byte("#!/bin/bash\n"+mark+broken+"GOFASTR_DEPLOY_TEST_SERVE="+strconv.Itoa(s.port)+" exec "+self+"\n"), 0o755)
}

func (s *server) run(at time.Time, args ...string) (string, error) {
	var out bytes.Buffer
	err := Run(&out, append(args, "--port", strconv.Itoa(s.port), "--wait", "5"), at, s)
	return out.String(), err
}

func (s *server) current() string {
	to, err := os.Readlink(filepath.Join(s.dir, "opt/shop/current"))
	if err != nil {
		return ""
	}
	return filepath.Base(to)
}

func releaseTime(id string) string {
	stamp, _, _ := strings.Cut(id, "-")
	return stamp
}

func (s *server) releases() []string {
	entries, _ := os.ReadDir(filepath.Join(s.dir, "opt/shop/releases"))
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

// serving reports whether the app answers its health path, giving a
// release that was just started a moment to listen.
func (s *server) serving() bool {
	for range 30 {
		resp, err := http.Get("http://127.0.0.1:" + strconv.Itoa(s.port) + "/healthz")
		if err == nil {
			resp.Body.Close()
			return resp.StatusCode == 200
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false
}

// down reports that nothing answers, now.
func (s *server) down() bool {
	_, err := http.Get("http://127.0.0.1:" + strconv.Itoa(s.port) + "/healthz")
	return err != nil
}

var t0 = time.Date(2026, 11, 14, 4, 0, 0, 0, time.UTC)

const (
	rel0 = "20261114T040000Z"
	rel1 = "20261114T040100Z"
	rel2 = "20261114T040200Z"
)

// A first deploy creates the user, the directories, the unit and the
// link, starts the app and checks it; a second one replaces it; a
// release that does not come up is replaced by the one before it and
// removed; and rollback goes back one release by hand.
func TestDeployLifecycle(t *testing.T) {
	s := newServer(t)
	env := filepath.Join(t.TempDir(), "prod.env")
	if err := os.WriteFile(env, []byte("SECRET=one\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	out, err := s.run(t0, "deploy@example.com", "--name", "shop", "--env-file", env)
	if err != nil {
		t.Fatalf("first deploy: %v\n%s", err, out)
	}
	first := s.current()
	if releaseTime(first) != rel0 || !s.serving() || !strings.Contains(out, "is running release "+first) {
		t.Fatalf("after the first deploy: current %q, serving %v, said:\n%s", s.current(), s.serving(), out)
	}
	if len(s.built) != 1 || s.built[0] != ". amd64" {
		t.Errorf("built %v, want the package in this directory for the server's CPU", s.built)
	}
	calls, _ := os.ReadFile(filepath.Join(s.dir, "calls.log"))
	for _, want := range []string{"useradd --system --home-dir " + s.dir + "/var/lib/shop --shell /usr/sbin/nologin shop", "systemctl daemon-reload", "systemctl enable shop", "systemctl restart shop",
		"chown root:shop " + s.dir + "/opt/shop/releases/" + first + "/env", "chown shop:shop " + s.dir + "/var/lib/shop"} {
		if !strings.Contains(string(calls), want) {
			t.Errorf("the server was not asked to: %s\n%s", want, calls)
		}
	}
	envPath := filepath.Join(s.dir, "opt/shop/current/env")
	if got, _ := os.ReadFile(envPath); string(got) != "SECRET=one\n" {
		t.Errorf("the env file on the server holds %q", got)
	}
	if info, err := os.Stat(envPath); err != nil {
		t.Fatal(err)
	} else if info.Mode().Perm() != 0o640 {
		t.Errorf("the env file is %v, want 0640 (root and the app's group)", info.Mode().Perm())
	}
	unit, _ := os.ReadFile(filepath.Join(s.dir, "etc/systemd/system/shop.service"))
	for _, want := range []string{"User=shop", "ExecStart=" + s.dir + "/opt/shop/current/shop", "Restart=always", "EnvironmentFile=-" + envPath, "ProtectSystem=strict", "NoNewPrivileges=true", "StateDirectory=shop",
		// Never left stopped after a run of crashes, and its data is its own.
		"StartLimitIntervalSec=0", "StateDirectoryMode=0750",
		// Dev-only features are off on a server, whatever the env file
		// was copied from; the file is read after and can say otherwise.
		"Environment=GOFASTR_ENV=production\nEnvironmentFile=-"} {
		if !strings.Contains(string(unit), want) {
			t.Errorf("the unit lacks %q:\n%s", want, unit)
		}
	}
	if left, _ := filepath.Glob(filepath.Join(s.dir, "upload-*")); len(left) != 0 {
		t.Errorf("the upload directory was left behind: %v", left)
	}

	// A second release: the user exists and the environment is inherited.
	out, err = s.run(t0.Add(time.Minute), "deploy@example.com", "--name", "shop")
	second := s.current()
	if err != nil || releaseTime(second) != rel1 || !s.serving() {
		t.Fatalf("second deploy: %v, current %q, serving %v\n%s", err, s.current(), s.serving(), out)
	}
	calls, _ = os.ReadFile(filepath.Join(s.dir, "calls.log"))
	if n := strings.Count(string(calls), "useradd"); n != 1 {
		t.Errorf("the user was created %d times", n)
	}
	if got, _ := os.ReadFile(envPath); string(got) != "SECRET=one\n" {
		t.Errorf("a deploy with no --env-file changed the env file to %q", got)
	}

	// A release that exits at once: the one before it runs again, the
	// failed one is gone, and the command fails and says so.
	s.broken = true
	out, err = s.run(t0.Add(2*time.Minute), "deploy@example.com", "--name", "shop")
	s.broken = false
	if err == nil || !strings.Contains(err.Error(), "back on release "+second) || !strings.Contains(err.Error(), "did not pass its health check") {
		t.Fatalf("a broken release: err %v\n%s", err, out)
	}
	if s.current() != second || !s.serving() {
		t.Fatalf("after a broken release: current %q, serving %v; want %s serving", s.current(), s.serving(), second)
	}
	if !strings.Contains(out, "the app said: cannot start") {
		t.Errorf("the failed release's log lines were not shown:\n%s", out)
	}
	for _, r := range s.releases() {
		if releaseTime(r) == rel2 {
			t.Error("the failed release was kept")
		}
	}

	// Back one release by hand.
	out, err = s.run(t0, "rollback", "deploy@example.com", "--name", "shop")
	if err != nil || s.current() != first || !s.serving() || !strings.Contains(out, "is running release "+first) {
		t.Fatalf("rollback: %v, current %q, serving %v\n%s", err, s.current(), s.serving(), out)
	}
	// Nothing older is kept.
	if _, err = s.run(t0, "rollback", "deploy@example.com", "--name", "shop"); err == nil || !strings.Contains(err.Error(), "no earlier release") || s.current() != first {
		t.Errorf("a rollback past the oldest release: %v, current %q", err, s.current())
	}

	out, err = s.run(t0, "status", "deploy@example.com", "--name", "shop")
	if err != nil || !strings.Contains(out, "release: "+first) || !strings.Contains(out, "service: active") || !strings.Contains(out, "health: ok") || !strings.Contains(out, "  "+second) {
		t.Errorf("status: %v\n%s", err, out)
	}
}

// A first release that does not come up has nothing to go back to: the
// command says the app is not serving, with a different exit path.
func TestFirstReleaseThatFails(t *testing.T) {
	s := newServer(t)
	s.broken = true
	out, err := s.run(t0, "deploy@example.com", "--name", "shop")
	if err == nil || !strings.Contains(err.Error(), "no earlier release came up") || !strings.Contains(err.Error(), "is not serving") {
		t.Fatalf("err %v\n%s", err, out)
	}
	if !s.down() {
		t.Error("something is serving after a first release that failed")
	}
	if kept := s.releases(); len(kept) != 1 || kept[0] != s.current() {
		t.Errorf("first failed release: current %q, kept %v", s.current(), kept)
	}
	// The check failing is the expected case here: it says nothing of
	// its own, the command does.
	if strings.Contains(out, "/dev/tcp") || strings.Contains(out, "Connection refused") {
		t.Errorf("the health check's own errors reached the output:\n%s", out)
	}

	s.broken = false
	if out, err := s.run(t0.Add(time.Minute), "prod", "--name", "shop"); err != nil {
		t.Fatalf("deploy after the failure: %v\n%s", err, out)
	}
	if kept := s.releases(); !slices.Equal(kept, []string{s.current()}) {
		t.Errorf("kept %v, want only the successful release %s", kept, s.current())
	}
}

// The health check is the app answering 2xx on the path given, not the
// process being alive: a release that runs and answers 503 fails.
func TestHealthCheckReadsTheAnswer(t *testing.T) {
	s := newServer(t)
	if _, err := s.run(t0, "deploy@example.com", "--name", "shop"); err != nil {
		t.Fatal(err)
	}
	first := s.current()
	out, err := s.run(t0.Add(time.Minute), "deploy@example.com", "--name", "shop", "--health", "/sick")
	if err == nil || !strings.Contains(err.Error(), "did not pass its health check") || s.current() != first {
		t.Fatalf("a release answering 503 on its health path: %v, current %q\n%s", err, s.current(), out)
	}
	if _, err := s.run(t0.Add(2*time.Minute), "deploy@example.com", "--name", "shop", "--health", "/missing"); err == nil {
		t.Error("a release answering 404 on its health path passed")
	}
}

// Old releases go: the running one and --keep before it stay.
func TestOldReleasesArePruned(t *testing.T) {
	s := newServer(t)
	for i := range 5 {
		if out, err := s.run(t0.Add(time.Duration(i)*time.Minute), "deploy@example.com", "--name", "shop", "--keep", "2"); err != nil {
			t.Fatalf("deploy %d: %v\n%s", i, err, out)
		}
	}
	kept := s.releases()
	for i, release := range kept {
		kept[i] = releaseTime(release)
	}
	got := strings.Join(kept, ",")
	if want := "20261114T040200Z,20261114T040300Z,20261114T040400Z"; got != want {
		t.Errorf("releases kept: %s\nwant the running one and two before it: %s", got, want)
	}
	if releaseTime(s.current()) != "20261114T040400Z" {
		t.Errorf("current is %q", s.current())
	}
}

// --bin deploys a file as it is, with no build.
func TestBinSkipsTheBuild(t *testing.T) {
	s := newServer(t)
	bin := filepath.Join(t.TempDir(), "prebuilt")
	if err := s.build(context.Background(), "x", "amd64", bin); err != nil {
		t.Fatal(err)
	}
	s.built = nil
	if out, err := s.run(t0, "deploy@example.com", "--name", "shop", "--bin", bin); err != nil || !s.serving() {
		t.Fatalf("%v\n%s", err, out)
	}
	if len(s.built) != 0 {
		t.Errorf("--bin still built %v", s.built)
	}
	if _, err := s.run(t0, "deploy@example.com", "--name", "shop", "--bin", filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Error("a --bin that does not exist was deployed")
	}
}

// What the command refuses before it touches a server: every value
// that ends up in a root shell's script or on ssh's command line.
func TestRefusedBeforeAnythingRuns(t *testing.T) {
	for _, args := range [][]string{
		{},
		{"-oProxyCommand=evil", "--name", "shop"},
		{"deploy@example.com", "extra", "--name", "shop"},
		{"deploy@example.com", "--name", "Shop"},
		{"deploy@example.com", "--name", "shop;rm -rf /"},
		{"deploy@example.com", "--name", "9lives"},
		{"deploy@example.com", "--name", strings.Repeat("a", 40)},
		{"deploy@example.com", "--name", "shop", "--health", "/x'; reboot #"},
		{"deploy@example.com", "--name", "shop", "--health", "healthz"},
		{"deploy@example.com", "--name", "shop", "--health", "/a b"},
		{"deploy@example.com", "--name", "shop", "--port", "0"},
		{"deploy@example.com", "--name", "shop", "--port", "70000"},
		{"deploy@example.com", "--name", "shop", "--wait", "0"},
		{"deploy@example.com", "--name", "shop", "--keep", "-1"},
		{"deploy@exa mple.com", "--name", "shop"},
		{"deploy@example.com;id", "--name", "shop"},
		{"deploy@example.com", "--name", "shop", "--nonsense"},
	} {
		s := &server{t: t}
		var out bytes.Buffer
		err := Run(&out, args, t0, s)
		if !errors.Is(err, errUsage) {
			t.Errorf("%q: err %v, want a usage error", args, err)
		}
		if len(s.calls) != 0 || len(s.built) != 0 {
			t.Errorf("%q: reached the server or the compiler before being refused: %v %v", args, s.calls, s.built)
		}
	}
	if err := Run(io.Discard, []string{"--help"}, t0, &server{t: t}); !errors.Is(err, flag.ErrHelp) {
		t.Errorf("--help: %v", err)
	}
	// Accepted: an alias, an IPv6 literal, a name with digits and hyphens.
	for _, target := range []string{"prod", "root@10.0.0.5", "deploy@[2001:db8::1]", "deploy@web-1.example.com"} {
		o := options{target: target, name: "shop-2", port: 8080, health: "/healthz", wait: 30, keep: 5}
		if err := o.validate(); err != nil {
			t.Errorf("%s refused: %v", target, err)
		}
	}
}

// A server that is not Linux on a CPU this builds for, one that cannot
// be reached, and one that names an odd upload directory each stop the
// deploy before anything is built or sent.
func TestServersThatCannotBeDeployedTo(t *testing.T) {
	s := newServer(t)
	s.arch = "riscv64"
	if _, err := s.run(t0, "deploy@example.com", "--name", "shop"); err == nil || !strings.Contains(err.Error(), "riscv64") || len(s.built) != 0 {
		t.Errorf("a riscv64 server: %v, built %v", err, s.built)
	}
	s.arch = "aarch64"
	if _, err := s.run(t0, "deploy@example.com", "--name", "shop"); err != nil || s.built[0] != ". arm64" {
		t.Errorf("an aarch64 server: %v, built %v", err, s.built)
	}
	s.unreachable = true
	if _, err := s.run(t0.Add(time.Minute), "deploy@example.com", "--name", "shop"); err == nil || !strings.Contains(err.Error(), "could not reach") {
		t.Errorf("an unreachable server: %v", err)
	}
}

type oddUpload struct{ *server }

func (o oddUpload) ssh(ctx context.Context, opt *options, command string, stdin io.Reader) (string, int, error) {
	if strings.HasPrefix(command, "uname -m") {
		return "x86_64\n/tmp/x';reboot;'\n", 0, nil
	}
	return o.server.ssh(ctx, opt, command, stdin)
}

func TestOddUploadDirectoryIsRefused(t *testing.T) {
	s := newServer(t)
	var out bytes.Buffer
	err := Run(&out, []string{"deploy@example.com", "--name", "shop"}, t0, oddUpload{s})
	if err == nil || !strings.Contains(err.Error(), "upload directory") || len(s.built) != 0 {
		t.Errorf("err %v, built %v", err, s.built)
	}
}

// The scripts a real server gets use the real paths, quote every value,
// and run through sudo for anyone but root.
func TestScriptsForARealServer(t *testing.T) {
	p := plan{name: "shop", release: rel0, upload: "/tmp/tmp.Ab12", hasEnv: true, port: 8080, health: "/healthz", wait: 30, keep: 5}
	script := p.install()
	for _, want := range []string{"set -eu\n", "NAME=shop\n", "ROOT=''\n", "WorkingDirectory=/var/lib/shop\n", "EnvironmentFile=-/opt/shop/current/env\n", "UPLOAD='/tmp/tmp.Ab12'\n", "DEFAULT_HEALTH='/healthz'\n", "ExecStart=/opt/shop/current/shop\n", `env_source="$UPLOAD/env"`} {
		if !strings.Contains(script, want) {
			t.Errorf("the install script lacks %q", want)
		}
	}
	p.hasEnv = false
	if strings.Contains(p.install(), `"$UPLOAD/env"`) {
		t.Error("with no env file the script still installs one")
	}
	if !strings.Contains(asRoot, "sudo -n bash -s") || !strings.Contains(asRoot, `"$(id -u)" = 0`) {
		t.Errorf("asRoot = %q", asRoot)
	}
	if got := shellQuote(`a'b`); got != `'a'\''b'` {
		t.Errorf("shellQuote = %s", got)
	}
}

func TestRollbackClearsFailedMarkBeforeRestart(t *testing.T) {
	s := newServer(t)
	if _, err := s.run(t0, "deploy@example.com", "--name", "shop"); err != nil {
		t.Fatal(err)
	}
	first := s.current()
	if err := os.WriteFile(filepath.Join(s.dir, "restart-error-once"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := s.run(t0.Add(time.Minute), "deploy@example.com", "--name", "shop")
	if err == nil || !strings.Contains(err.Error(), "back on release "+first) {
		t.Fatalf("a failing release on a server whose restart reports an error: %v\n%s", err, out)
	}
	if s.current() != first || !s.serving() {
		t.Fatalf("current %q, serving %v; want %s serving", s.current(), s.serving(), first)
	}
	calls, _ := os.ReadFile(filepath.Join(s.dir, "calls.log"))
	lines := strings.Split(strings.TrimSpace(string(calls)), "\n")
	for i, l := range lines {
		if l == "systemctl restart shop" && (i == 0 || lines[i-1] != "systemctl reset-failed shop") {
			t.Fatalf("a restart without the failed mark cleared first:\n%s", calls)
		}
	}
	// The app's own lines are asked for, not the unit's with systemd's
	// account of every restart.
	if !strings.Contains(string(calls), "journalctl _SYSTEMD_UNIT=shop.service -n 20 --no-pager -o cat") {
		t.Errorf("the log lines were asked for another way:\n%s", calls)
	}
}

// "Back on the release before" is said only when that release answers
// again. When it does not either, the command says the app is down.
func TestRollbackThatDoesNotComeUpIsNotCalledARollback(t *testing.T) {
	s := newServer(t)
	if _, err := s.run(t0, "deploy@example.com", "--name", "shop"); err != nil {
		t.Fatal(err)
	}
	first := s.current()
	if err := os.WriteFile(filepath.Join(s.dir, "nothing-starts"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := s.run(t0.Add(time.Minute), "deploy@example.com", "--name", "shop")
	if err == nil || strings.Contains(err.Error(), "is back on") || !strings.Contains(err.Error(), "is not serving") || !strings.Contains(err.Error(), first) {
		t.Fatalf("err %v\n%s", err, out)
	}
	if s.current() != first {
		t.Errorf("current is %q, want the release before left in place", s.current())
	}
}

// A binary that can take a socket from systemd gets its port held for
// it: a socket unit on the app's port, the service bound to it, and the
// app running before stopped once so systemd can open the port. A
// binary that cannot keeps opening its own, and a server that was
// handed the port takes it back for such a binary.
func TestPortIsHeldBySystemdWhenTheAppCanTakeIt(t *testing.T) {
	s := newServer(t)
	// An older build first: no socket unit.
	if out, err := s.run(t0, "deploy@example.com", "--name", "shop"); err != nil || strings.Contains(out, "systemd holds port") {
		t.Fatalf("%v\n%s", err, out)
	}
	sock := filepath.Join(s.dir, "etc/systemd/system/shop.socket")
	if _, err := os.Stat(sock); err == nil {
		t.Fatal("a socket unit was written for a binary that opens its own port")
	}
	unit, _ := os.ReadFile(filepath.Join(s.dir, "etc/systemd/system/shop.service"))
	if strings.Contains(string(unit), "shop.socket") {
		t.Errorf("the service is bound to a socket it does not have:\n%s", unit)
	}

	s.socketApp = true
	out, err := s.run(t0.Add(time.Minute), "deploy@example.com", "--name", "shop")
	if err != nil || !strings.Contains(out, "systemd holds port "+strconv.Itoa(s.port)) || !s.serving() {
		t.Fatalf("%v\n%s", err, out)
	}
	got, _ := os.ReadFile(sock)
	if !strings.Contains(string(got), "ListenStream="+strconv.Itoa(s.port)+"\n") || !strings.Contains(string(got), "WantedBy=sockets.target") {
		t.Errorf("the socket unit:\n%s", got)
	}
	unit, _ = os.ReadFile(filepath.Join(s.dir, "etc/systemd/system/shop.service"))
	if !strings.Contains(string(unit), "Requires=shop.socket\nAfter=shop.socket\n") {
		t.Errorf("the service is not bound to its socket:\n%s", unit)
	}
	calls, _ := os.ReadFile(filepath.Join(s.dir, "calls.log"))
	stop, enable := strings.Index(string(calls), "systemctl stop shop\n"), strings.Index(string(calls), "systemctl enable --now shop.socket")
	if stop < 0 || enable < 0 || stop > enable {
		t.Errorf("the app holding the port was not stopped before systemd opened it:\n%s", calls)
	}

	// The next release finds the port held, and stops nothing.
	if out, err := s.run(t0.Add(2*time.Minute), "deploy@example.com", "--name", "shop"); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	calls, _ = os.ReadFile(filepath.Join(s.dir, "calls.log"))
	if n := strings.Count(string(calls), "systemctl stop shop\n"); n != 1 {
		t.Errorf("the app was stopped %d times across three deploys, want once: for the hand-over", n)
	}
	if n := strings.Count(string(calls), "enable --now shop.socket"); n != 1 {
		t.Errorf("the socket was enabled %d times", n)
	}

	// Back to a binary that opens its own port.
	s.socketApp = false
	if out, err := s.run(t0.Add(3*time.Minute), "deploy@example.com", "--name", "shop"); err != nil || !s.serving() {
		t.Fatalf("%v\n%s", err, out)
	}
	if _, err := os.Stat(sock); err == nil {
		t.Error("the socket unit was left for a binary that opens its own port: the port would be taken")
	}
	calls, _ = os.ReadFile(filepath.Join(s.dir, "calls.log"))
	if !strings.Contains(string(calls), "systemctl disable --now shop.socket") {
		t.Errorf("the socket was not handed back:\n%s", calls)
	}
}

// The marker is found wherever it sits in the file, a read boundary
// included, and not in a file without it.
func TestTakesSocketFindsTheMarker(t *testing.T) {
	dir := t.TempDir()
	for name, c := range map[string]struct {
		body []byte
		want bool
	}{
		"none":          {bytes.Repeat([]byte("x"), 3<<20), false},
		"empty":         {nil, false},
		"at the start":  {append([]byte(sdlisten.Marker), bytes.Repeat([]byte("x"), 100)...), true},
		"at the end":    {append(bytes.Repeat([]byte("x"), 2<<20+17), sdlisten.Marker...), true},
		"over a read":   {append(append(bytes.Repeat([]byte("x"), 1<<20-9), sdlisten.Marker...), bytes.Repeat([]byte("y"), 1<<20)...), true},
		"over a second": {append(append(bytes.Repeat([]byte("x"), 2<<20-len(sdlisten.Marker)-3), sdlisten.Marker...), 'z'), true},
		"cut short":     {append(bytes.Repeat([]byte("x"), 1<<20-5), sdlisten.Marker[:len(sdlisten.Marker)-1]...), false},
	} {
		path := filepath.Join(dir, strings.ReplaceAll(name, " ", "-"))
		if err := os.WriteFile(path, c.body, 0o600); err != nil {
			t.Fatal(err)
		}
		if got, err := takesSocket(path); err != nil || got != c.want {
			t.Errorf("%s: %v, %v; want %v", name, got, err, c.want)
		}
	}
	if _, err := takesSocket(filepath.Join(dir, "missing")); err == nil {
		t.Error("a file that is not there read as a binary")
	}
}

// A release keeps the units it was deployed with, and going back to it
// puts them back. Found on a real server: a release that opens its own
// port failed after one that takes it from systemd, and the rollback
// left the old release running without the socket it had.
func TestRollbackRestoresTheReleasesOwnUnits(t *testing.T) {
	s := newServer(t)
	sock := filepath.Join(s.dir, "etc/systemd/system/shop.socket")
	unit := func() string {
		b, _ := os.ReadFile(filepath.Join(s.dir, "etc/systemd/system/shop.service"))
		return string(b)
	}
	held := func() bool {
		_, on := os.Stat(filepath.Join(s.dir, "socket-on"))
		_, file := os.Stat(sock)
		return on == nil && file == nil && strings.Contains(unit(), "Requires=shop.socket")
	}
	s.socketApp = true
	if out, err := s.run(t0, "deploy@example.com", "--name", "shop"); err != nil || !held() {
		t.Fatalf("setup: %v, port held %v\n%s", err, held(), out)
	}
	first := s.current()
	// A release that opens its own port, and does not come up.
	s.socketApp, s.broken = false, true
	out, err := s.run(t0.Add(time.Minute), "deploy@example.com", "--name", "shop")
	s.broken = false
	if err == nil || !strings.Contains(err.Error(), "back on release "+first) {
		t.Fatalf("%v\n%s", err, out)
	}
	if !held() || !s.serving() {
		t.Fatalf("after the rollback: port held by systemd %v, serving %v; want the first release as it was deployed", held(), s.serving())
	}

	// The same by hand, both ways: forward to a release that opens its
	// own port, back to the one that does not.
	if out, err := s.run(t0.Add(2*time.Minute), "deploy@example.com", "--name", "shop"); err != nil || held() {
		t.Fatalf("a release that opens its own port: %v, port still held %v\n%s", err, held(), out)
	}
	second := s.current()
	if strings.Contains(unit(), "shop.socket") {
		t.Errorf("the service is still bound to the socket:\n%s", unit())
	}
	if out, err := s.run(t0, "rollback", "deploy@example.com", "--name", "shop"); err != nil || !held() || s.current() != first {
		t.Fatalf("rollback: %v, current %q, port held %v\n%s", err, s.current(), held(), out)
	}
	// Each release holds its own copy.
	for rel, want := range map[string]bool{first: true, second: false} {
		_, err := os.Stat(filepath.Join(s.dir, "opt/shop/releases", rel, "units/shop.socket"))
		if (err == nil) != want {
			t.Errorf("release %s keeps a socket unit: %v, want %v", rel, err == nil, want)
		}
		if _, err := os.Stat(filepath.Join(s.dir, "opt/shop/releases", rel, "units/shop.service")); err != nil {
			t.Errorf("release %s keeps no service unit", rel)
		}
	}
}

// With systemd holding the port, a connection is accepted while the app
// is down, and nothing answers it. The health check gives such a
// connection two seconds, not for ever.
func TestProbeDoesNotWaitForEver(t *testing.T) {
	s := newServer(t)
	if err := os.WriteFile(filepath.Join(s.dir, "always-active"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(s.port))
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			defer c.Close() // held open, never answered
		}
	}()
	type result struct {
		out string
		err error
	}
	done := make(chan result, 1)
	go func() {
		out, err := s.run(t0, "status", "deploy@example.com", "--name", "shop")
		done <- result{out, err}
	}()
	select {
	case r := <-done:
		if r.err != nil || !strings.Contains(r.out, "health: no answer") {
			t.Errorf("status against a port that accepts and says nothing: %v\n%s", r.err, r.out)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("the health check is still waiting on a connection nothing answers")
	}
}

func TestHealthSettingsFollowEachRelease(t *testing.T) {
	s := newServer(t)
	s.socketApp = true
	if out, err := s.run(t0, "prod", "--name", "shop", "--health", "/ready"); err != nil {
		t.Fatalf("first deploy: %v\n%s", err, out)
	}
	first := s.current()
	s.port = freePort(t)
	if out, err := s.run(t0, "status", "prod", "--name", "shop", "--health", "/missing"); err != nil || !strings.Contains(out, "health: ok") {
		t.Errorf("status should use the release's port and path: %v\n%s", err, out)
	}
	wantCheck := fmt.Sprintf("health check on port %d/sick", s.port)
	if out, err := s.run(t0.Add(time.Minute), "prod", "--name", "shop", "--health", "/sick"); err == nil ||
		!strings.Contains(err.Error(), "back on release "+first) || !strings.Contains(err.Error(), wantCheck) {
		t.Errorf("automatic rollback should use the previous release's port and path: %v\n%s", err, out)
	}
	if out, err := s.run(t0.Add(2*time.Minute), "prod", "--name", "shop"); err != nil {
		t.Fatalf("second healthy deploy: %v\n%s", err, out)
	}
	if out, err := s.run(t0, "rollback", "prod", "--name", "shop", "--health", "/missing"); err != nil || s.current() != first {
		t.Errorf("manual rollback should use the target release's port and path: %v, current %q\n%s", err, s.current(), out)
	}
}

func TestLegacyReleaseUsesInvocationHealth(t *testing.T) {
	s := newServer(t)
	p := plan{root: s.dir, name: "shop"}
	app := filepath.Join(s.dir, "opt/shop")
	release := filepath.Join(app, "releases", rel0)
	legacyEnv := filepath.Join(s.dir, "etc/shop/env")
	legacyUnit := strings.Replace(p.unit(), app+"/current/env", legacyEnv, 1)
	keptUnits := filepath.Join(release, "units")
	activeUnits := filepath.Join(s.dir, "etc/systemd/system")
	for _, dir := range []string{keptUnits, activeUnits, filepath.Dir(legacyEnv)} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, dir := range []string{keptUnits, activeUnits} {
		if err := os.WriteFile(filepath.Join(dir, "shop.service"), []byte(legacyUnit), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(legacyEnv, []byte("SECRET=legacy\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := s.build(context.Background(), ".", "amd64", filepath.Join(release, "shop")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("releases/"+rel0, filepath.Join(app, "current")); err != nil {
		t.Fatal(err)
	}
	if out, code, err := s.ssh(context.Background(), nil, asRoot, strings.NewReader("systemctl restart shop\n")); err != nil || code != 0 || !s.serving() {
		t.Fatalf("start legacy release: %v (exit %d)\n%s", err, code, out)
	}
	if out, err := s.run(t0, "status", "prod", "--name", "shop", "--health", "/missing"); err != nil ||
		!strings.Contains(out, fmt.Sprintf("health: no answer on port %d/missing", s.port)) {
		t.Errorf("status should use the invocation's health settings for the legacy release: %v\n%s", err, out)
	}
	env := filepath.Join(t.TempDir(), "prod.env")
	if err := os.WriteFile(env, []byte("SECRET=new\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := s.run(t0.Add(time.Minute), "prod", "--name", "shop", "--env-file", env); err != nil {
		t.Fatalf("deploy after legacy release: %v\n%s", err, out)
	}
	if out, err := s.run(t0, "rollback", "prod", "--name", "shop", "--health", "/ready"); err != nil || s.current() != rel0 {
		t.Fatalf("rollback to legacy release: %v, current %q\n%s", err, s.current(), out)
	}
	resp, err := http.Get("http://127.0.0.1:" + strconv.Itoa(s.port) + "/env")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if raw, err := io.ReadAll(resp.Body); err != nil || string(raw) != "legacy" {
		t.Errorf("legacy environment: got %q (%v), want legacy", raw, err)
	}
}

func TestEnvironmentFollowsEachRelease(t *testing.T) {
	s := newServer(t)
	env := filepath.Join(t.TempDir(), "prod.env")
	deploy := func(at time.Time, secret, health string) (string, error) {
		t.Helper()
		if err := os.WriteFile(env, []byte("SECRET="+secret+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		return s.run(at, "prod", "--name", "shop", "--env-file", env, "--health", health)
	}
	assertEnv := func(want string) {
		t.Helper()
		resp, err := http.Get("http://127.0.0.1:" + strconv.Itoa(s.port) + "/env")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		got, err := io.ReadAll(resp.Body)
		if err != nil || string(got) != want {
			t.Errorf("active release's environment: got %q (%v), want %q", got, err, want)
		}
	}
	if out, err := deploy(t0, "one", "/healthz"); err != nil {
		t.Fatalf("first deploy: %v\n%s", err, out)
	}
	assertEnv("one")
	first := s.current()
	if out, err := deploy(t0.Add(time.Minute), "failed", "/sick"); err == nil || s.current() != first {
		t.Fatalf("failed deploy should restore the first release: %v, current %q\n%s", err, s.current(), out)
	}
	assertEnv("one")
	if out, err := deploy(t0.Add(2*time.Minute), "two", "/healthz"); err != nil {
		t.Fatalf("second healthy deploy: %v\n%s", err, out)
	}
	assertEnv("two")
	if out, err := s.run(t0, "rollback", "prod", "--name", "shop"); err != nil || s.current() != first {
		t.Fatalf("manual rollback: %v, current %q\n%s", err, s.current(), out)
	}
	assertEnv("one")
}

func TestActivationFailureRecoversTheRunningRelease(t *testing.T) {
	for _, unit := range []string{"socket", "service"} {
		for _, action := range []string{"deploy", "rollback"} {
			t.Run(action+"/"+unit, func(t *testing.T) {
				s := newServer(t)
				s.socketApp = action == "rollback" && unit == "socket"
				if out, err := s.run(t0, "prod", "--name", "shop"); err != nil {
					t.Fatalf("first deploy: %v\n%s", err, out)
				}
				s.socketApp = false
				if action == "rollback" {
					if out, err := s.run(t0.Add(time.Minute), "prod", "--name", "shop"); err != nil {
						t.Fatalf("second deploy: %v\n%s", err, out)
					}
				}
				current := s.current()
				marker := "restart-error-once"
				if unit == "socket" {
					marker = "socket-error"
				}
				if err := os.WriteFile(filepath.Join(s.dir, marker), nil, 0o600); err != nil {
					t.Fatal(err)
				}
				args := []string{"prod", "--name", "shop"}
				if action == "rollback" {
					args = append([]string{"rollback"}, args...)
				} else {
					s.socketApp = unit == "socket"
				}
				out, err := s.run(t0.Add(time.Minute), args...)
				if err == nil || !strings.Contains(err.Error(), "back on release "+current) ||
					!strings.Contains(out, unit+" activation refused") || s.current() != current || !s.serving() {
					t.Fatalf("%s after %s error: %v, current %q, want %q serving\n%s", action, unit, err, s.current(), current, out)
				}
				wantKept := 1
				if action == "rollback" {
					wantKept = 2
				}
				if got := len(s.releases()); got != wantKept {
					t.Errorf("releases kept = %d, want %d", got, wantKept)
				}
			})
		}
	}
}

func TestWaitIsElapsedSeconds(t *testing.T) {
	s := newServer(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close() // connections enter the listen backlog without an HTTP reply
	if err := os.WriteFile(filepath.Join(s.dir, "always-active"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	p := plan{root: s.dir, name: "shop", port: ln.Addr().(*net.TCPAddr).Port, health: "/healthz", wait: 1}
	start := time.Now()
	out, code, err := s.ssh(context.Background(), nil, asRoot, strings.NewReader(p.preamble()+"read_health ''; healthy\n"))
	elapsed := time.Since(start)
	if err != nil || code == 0 || elapsed < 800*time.Millisecond || elapsed > 2*time.Second {
		t.Fatalf("one-second health deadline: exit %d after %s, err %v\n%s", code, elapsed, err, out)
	}
}

func TestSameInstantDeploysGetDistinctReleases(t *testing.T) {
	s := newServer(t)
	if out, err := s.run(t0, "prod", "--name", "shop"); err != nil {
		t.Fatalf("first deploy: %v\n%s", err, out)
	}
	first := s.current()
	if out, err := s.run(t0, "prod", "--name", "shop"); err != nil {
		t.Fatalf("second deploy: %v\n%s", err, out)
	}
	if got := s.releases(); len(got) != 2 || s.current() == first {
		t.Fatalf("same instant: first %q, current %q, releases %v", first, s.current(), got)
	}
}

func TestRollbackFollowsServerOrderForEqualClientTimestamps(t *testing.T) {
	s := newServer(t)
	first := "20261114T040000Z-000000000-ZAAAAAAAAAAAAAAAAAAAAAAAAA"
	second := "20261114T040000Z-000000000-AAAAAAAAAAAAAAAAAAAAAAAAAA"
	for _, release := range []string{first, second} {
		p := s.uploadPlan(release)
		if out, code, err := s.ssh(context.Background(), nil, asRoot, strings.NewReader(p.install())); code != 0 || err != nil {
			t.Fatalf("deploy %s: exit %d, error %v\n%s", release, code, err, out)
		}
	}
	if out, err := s.run(t0, "rollback", "prod", "--name", "shop"); err != nil || s.current() != first {
		t.Fatalf("rollback: %v, current %q, want %q\n%s", err, s.current(), first, out)
	}
}

func TestRetentionFollowsServerOrderForEqualClientTimestamps(t *testing.T) {
	s := newServer(t)
	releases := []string{
		"20261114T040000Z-000000000-ZAAAAAAAAAAAAAAAAAAAAAAAAA",
		"20261114T040000Z-000000000-AAAAAAAAAAAAAAAAAAAAAAAAAA",
		"20261114T040000Z-000000000-MAAAAAAAAAAAAAAAAAAAAAAAAA",
	}
	for _, release := range releases {
		p := s.uploadPlan(release)
		p.keep = 1
		if out, code, err := s.ssh(context.Background(), nil, asRoot, strings.NewReader(p.install())); code != 0 || err != nil {
			t.Fatalf("deploy %s: exit %d, error %v\n%s", release, code, err, out)
		}
	}
	if got := s.releases(); !slices.Equal(got, releases[1:]) {
		t.Fatalf("kept %q, want %q", got, releases[1:])
	}
}

func TestDeployReconcilesUnrecordedReleases(t *testing.T) {
	const (
		recorded = "20261114T040003Z"
		previous = "20261114T040002Z"
		running  = "20261114T040001Z"
		next     = "20261114T040004Z"
		missing  = "20261114T040005Z"
	)
	for _, tc := range []struct {
		name      string
		keep      int
		wantOrder []string
		wantLink  string
	}{
		{"rollback", 0, []string{running, previous, recorded}, previous},
		{"retain-running", 1, []string{next, running}, next},
		{"retain-older", 2, []string{next, running, previous}, next},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newServer(t)
			for _, release := range []string{recorded, previous, running} {
				p := s.uploadPlan(release)
				if out, code, err := s.ssh(context.Background(), nil, asRoot, strings.NewReader(p.install())); code != 0 || err != nil {
					t.Fatalf("deploy %s: exit %d, error %v\n%s", release, code, err, out)
				}
			}
			order := filepath.Join(s.dir, "opt/shop/.release-order")
			// Re-create the stale order left by two deploys from an older client.
			if err := os.WriteFile(order, []byte(missing+"\n"+recorded+"\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			if s.current() != running {
				t.Fatalf("fixture current = %q, want %q", s.current(), running)
			}
			p := s.uploadPlan(next)
			p.keep = tc.keep
			script := p.install
			if tc.keep == 0 {
				script = p.rollback
			}
			if out, code, err := s.ssh(context.Background(), nil, asRoot, strings.NewReader(script())); code != 0 || err != nil || s.current() != tc.wantLink {
				t.Fatalf("%s: exit %d, error %v, current %q, want %q\n%s", tc.name, code, err, s.current(), tc.wantLink, out)
			}
			gotOrder, err := os.ReadFile(order)
			if want := strings.Join(tc.wantOrder, "\n") + "\n"; err != nil || string(gotOrder) != want {
				t.Errorf("release order %q, error %v, want %q", gotOrder, err, want)
			}
			wantDirs := slices.Clone(tc.wantOrder)
			slices.Sort(wantDirs)
			if got := s.releases(); !slices.Equal(got, wantDirs) {
				t.Errorf("kept %q, want %q", got, wantDirs)
			}
		})
	}
}

func TestDeployRetainsLastActivatedReleaseAfterInterruption(t *testing.T) {
	const (
		first       = rel0
		interrupted = rel1
		aborted     = 97
	)
	for _, tc := range []struct {
		name         string
		boundary     string
		injectBefore bool
		wantCurrent  string
		wantPrevious string
	}{
		{"after binary copy", `install -m 0755 "$UPLOAD/bin" "$APP/releases/$REL/$NAME"` + "\n", false, first, first},
		{"before health check", "  if ! healthy; then\n", true, interrupted, first},
		{"before order publish", `mv -T "$ORDER.new" "$ORDER"` + "\n" + `retained="$(cat "$ORDER")"` + "\n", true, interrupted, interrupted},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newServer(t)
			p := s.uploadPlan(first)
			if out, code, err := s.ssh(context.Background(), nil, asRoot, strings.NewReader(p.install())); code != 0 || err != nil {
				t.Fatalf("first deploy: exit %d, error %v\n%s", code, err, out)
			}
			p = s.uploadPlan(interrupted)
			script := p.install()
			if strings.Count(script, tc.boundary) != 1 {
				t.Fatalf("expected one interruption boundary %q", tc.boundary)
			}
			stop := fmt.Sprintf("exit %d\n", aborted)
			replacement := tc.boundary + stop
			if tc.injectBefore {
				replacement = stop + tc.boundary
			}
			script = strings.Replace(script, tc.boundary, replacement, 1)
			if out, code, err := s.ssh(context.Background(), nil, asRoot, strings.NewReader(script)); code != aborted || err != nil || s.current() != tc.wantCurrent {
				t.Fatalf("interrupted deploy: exit %d, error %v, current %q, want %q\n%s", code, err, s.current(), tc.wantCurrent, out)
			}
			order := filepath.Join(s.dir, "opt/shop/.release-order")
			if got, err := os.ReadFile(order); err != nil || string(got) != first+"\n" {
				t.Fatalf("order after interruption = %q, error %v, want only %q", got, err, first)
			}
			if out, err := s.run(t0.Add(3*time.Minute), "prod", "--name", "shop", "--keep=1"); err != nil {
				t.Fatalf("next deploy: %v\n%s", err, out)
			}
			next := s.current()
			wantOrder := next + "\n" + tc.wantPrevious + "\n"
			if got, err := os.ReadFile(order); err != nil || string(got) != wantOrder {
				t.Errorf("release order = %q, error %v, want %q", got, err, wantOrder)
			}
			if got := s.releases(); !slices.Equal(got, []string{tc.wantPrevious, next}) {
				t.Errorf("kept %q, want %q and %q", got, tc.wantPrevious, next)
			}
			if out, err := s.run(t0, "rollback", "prod", "--name", "shop"); err != nil || s.current() != tc.wantPrevious || !s.serving() {
				t.Fatalf("rollback: %v, current %q, want %q serving\n%s", err, s.current(), tc.wantPrevious, out)
			}
		})
	}
}

func TestDeployAndRollbackWaitForTheAppLock(t *testing.T) {
	for _, action := range []string{"install", "rollback"} {
		t.Run(action, func(t *testing.T) {
			s := newServer(t)
			env := filepath.Join(t.TempDir(), "env")
			var first string
			for i, at := range []time.Time{t0, t0.Add(time.Minute)} {
				if err := os.WriteFile(env, []byte(fmt.Sprintf("SECRET=%d\n", i)), 0o600); err != nil {
					t.Fatal(err)
				}
				if out, err := s.run(at, "prod", "--name", "shop", "--env-file", env); err != nil {
					t.Fatalf("setup deploy: %v\n%s", err, out)
				}
				if i == 0 {
					first = s.current()
				}
			}
			current := s.current()
			p := s.uploadPlan(rel2)
			script, want := p.install(), rel2
			if action == "rollback" {
				script, want = p.rollback(), first
			}
			lock := exec.Command("flock", "-x", filepath.Join(s.dir, "opt/shop/.deploy.lock"), "bash", "-c", "echo locked; read -r unlock")
			unlock, err := lock.StdinPipe()
			if err != nil {
				t.Fatal(err)
			}
			ready, err := lock.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}
			if err := lock.Start(); err != nil {
				t.Fatal(err)
			}
			defer func() {
				_ = unlock.Close()
				_ = lock.Wait()
			}()
			if line, err := bufio.NewReader(ready).ReadString('\n'); line != "locked\n" || err != nil {
				t.Fatalf("could not hold deploy lock: %q, %v", line, err)
			}

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, "bash", "-s")
			cmd.Env = append(os.Environ(), "PATH="+s.stubs+":"+os.Getenv("PATH"))
			cmd.Stdin = strings.NewReader(script)
			var out bytes.Buffer
			cmd.Stdout, cmd.Stderr = &out, &out
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() { done <- cmd.Wait() }()
			select {
			case err := <-done:
				t.Fatalf("%s finished while lock held: %v\n%s", action, err, out.String())
			case <-time.After(2 * time.Second):
				if got := s.current(); got != current {
					t.Errorf("%s changed current under a held lock: %q, want %q", action, got, current)
				}
			}
			if action == "install" {
				app := filepath.Join(s.dir, "opt/shop")
				next := filepath.Join(app, "fixture-current")
				if err := os.Symlink("releases/"+first, next); err != nil {
					t.Fatal(err)
				}
				if err := os.Rename(next, filepath.Join(app, "current")); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := io.WriteString(unlock, "unlock\n"); err != nil {
				t.Error(err)
			}
			if err := <-done; err != nil || s.current() != want {
				t.Fatalf("%s: %v, current %q, want %q\n%s", action, err, s.current(), want, out.String())
			}
			if action == "install" {
				got, err := os.ReadFile(filepath.Join(s.dir, "opt/shop/current/env"))
				if err != nil || string(got) != "SECRET=0\n" {
					t.Errorf("installed environment %q, error %v; want the release selected under the lock", got, err)
				}
			}
		})
	}
}

func (s *server) uploadPlan(release string) plan {
	s.t.Helper()
	upload := s.t.TempDir()
	if err := s.build(context.Background(), ".", s.arch, filepath.Join(upload, "bin")); err != nil {
		s.t.Fatal(err)
	}
	return plan{root: s.dir, name: "shop", release: release, upload: upload, port: s.port, health: "/healthz", wait: 5, keep: 2}
}

func TestInstallRefusesAnExistingReleaseDirectory(t *testing.T) {
	s := newServer(t)
	if out, err := s.run(t0, "prod", "--name", "shop"); err != nil {
		t.Fatalf("first deploy: %v\n%s", err, out)
	}
	release := s.current()
	binary := filepath.Join(s.dir, "opt/shop/releases", release, "shop")
	original, err := os.ReadFile(binary)
	if err != nil {
		t.Fatal(err)
	}
	upload := t.TempDir()
	if err := os.WriteFile(filepath.Join(upload, "bin"), append(bytes.Clone(original), []byte("# different binary\n")...), 0o755); err != nil {
		t.Fatal(err)
	}
	p := plan{root: s.dir, name: "shop", release: release, upload: upload, port: s.port, health: "/healthz", wait: 5}
	out, code, err := s.ssh(context.Background(), nil, asRoot, strings.NewReader(p.install()))
	got, readErr := os.ReadFile(binary)
	if err != nil || code == 0 || readErr != nil || !bytes.Equal(got, original) || s.current() != release {
		t.Fatalf("colliding install: err %v, code %d, binary unchanged %v, current %q\n%s", err, code, bytes.Equal(got, original), s.current(), out)
	}
}

func TestInactiveStatusIgnoresOtherHTTPServers(t *testing.T) {
	s := newServer(t)
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer other.Close()
	s.port = other.Listener.Addr().(*net.TCPAddr).Port
	out, err := s.run(t0, "status", "prod", "--name", "shop")
	if err != nil || !strings.Contains(out, "service: inactive") || !strings.Contains(out, "health: service is not active") || strings.Contains(out, "health: ok") {
		t.Fatalf("status with a non-service responder: %v\n%s", err, out)
	}
}
