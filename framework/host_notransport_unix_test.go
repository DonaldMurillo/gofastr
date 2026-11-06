//go:build unix

package framework

import (
	"bytes"
	"context"
	"database/sql"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/pprof"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

const servicesHostChildEnv = "GOFASTR_TEST_SERVICES_HOST_CHILD"

// ServicesHost constructs no transport. Not "no request arrived": the
// proof runs in a fresh process, so nothing else in the test binary can
// open a socket, and checks that the process holds no more sockets once
// the App serves than before it was built, that no net/http server,
// connection or client goroutine exists, and that the App never built or
// wrapped an http.Server.
func TestServicesHostOpensNoTransport(t *testing.T) {
	if os.Getenv(servicesHostChildEnv) == "1" {
		servicesHostChild(t)
		return
	}
	// os.Executable, not os.Args[0]: Start retitles the process by
	// rewriting os.Args[0], and an earlier test in this binary may have.
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(exe, "-test.run=^TestServicesHostOpensNoTransport$", "-test.count=1", "-test.v")
	cmd.Env = append(os.Environ(), servicesHostChildEnv+"=1")
	out, err := cmd.CombinedOutput()
	if err != nil || !strings.Contains(string(out), "services host child: no transport") {
		t.Fatalf("child process: %v\n%s", err, out)
	}
}

func servicesHostChild(t *testing.T) {
	before := socketFDs(t)

	db, err := sql.Open("sqlite3", "file:"+filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	app := NewApp(WithDB(db), WithoutDefaultMiddleware(), WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))))
	app.startupOutput = &bytes.Buffer{}
	app.OnStop(db.Close)
	app.Entity("items", entity.EntityConfig{
		Fields: []schema.Field{{Name: "name", Type: schema.String, Required: true}},
	})
	started := make(chan struct{})
	app.OnStart(func(context.Context) error { close(started); return nil })

	done := make(chan error, 1)
	go func() { done <- app.Run(context.Background(), ServicesHost()) }()
	select {
	case <-app.ServicesReady():
	case err := <-done:
		t.Fatalf("Run: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("services never became ready")
	}
	<-started
	if _, err := app.MustCrudHandler("items").CreateOne(context.Background(), map[string]any{"name": "in-process"}); err != nil {
		t.Fatalf("in-process create: %v", err)
	}
	time.Sleep(50 * time.Millisecond) // let a stray server goroutine reach Accept

	if after := socketFDs(t); len(after) > len(before) {
		t.Fatalf("the process holds %d sockets serving, %d before the App existed: %v -> %v", len(after), len(before), before, after)
	}
	var stacks bytes.Buffer
	_ = pprof.Lookup("goroutine").WriteTo(&stacks, 2)
	for _, frame := range []string{"net/http.(*Server)", "net/http.(*conn)", "net/http.(*persistConn)", "net/http.(*Transport)", "net.(*TCPListener)", "net.(*UnixListener)"} {
		if strings.Contains(stacks.String(), frame) {
			t.Fatalf("a %s goroutine runs under ServicesHost:\n%s", frame, stacks.String())
		}
	}
	app.serverMu.Lock()
	srv, wrapped, bound := app.server, app.serverWrapped, app.boundHost
	app.serverMu.Unlock()
	if srv != nil || wrapped || bound != "" {
		t.Fatalf("ServicesHost built HTTP state: server %v, wrapped %v, bound %q", srv != nil, wrapped, bound)
	}

	sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := app.Shutdown(sctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("Run: %v", err)
	}
	t.Log("services host child: no transport")
}

// socketFDs lists the process's open file descriptors that are sockets.
func socketFDs(t *testing.T) []int {
	t.Helper()
	dir := "/proc/self/fd"
	if _, err := os.Stat(dir); err != nil {
		dir = "/dev/fd"
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("list descriptors: %v", err)
	}
	var socks []int
	for _, e := range entries {
		fd, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		var st syscall.Stat_t
		if syscall.Fstat(fd, &st) != nil {
			continue // the directory's own descriptor, closed by now
		}
		if uint32(st.Mode)&syscall.S_IFMT == syscall.S_IFSOCK {
			socks = append(socks, fd)
		}
	}
	return socks
}
