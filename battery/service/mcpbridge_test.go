package service

import (
	"bufio"
	"context"
	"io"
	"log/slog"
	"net"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/localrun"
	"github.com/DonaldMurillo/gofastr/internal/localuser"
)

// bridge runs `service mcp --dev` against d and returns a writer for
// its stdin, a reader of its stdout lines, and its exit.
func bridge(t *testing.T, d *devRun) (io.WriteCloser, *bufio.Scanner, <-chan error) {
	t.Helper()
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	done := make(chan error, 1)
	go func() {
		done <- d.b.mcpBridge(true, "", inR, outW)
		outW.Close()
	}()
	t.Cleanup(func() { inW.Close() })
	return inW, bufio.NewScanner(outR), done
}

func readLine(t *testing.T, sc *bufio.Scanner) string {
	t.Helper()
	got := make(chan bool, 1)
	go func() { got <- sc.Scan() }()
	select {
	case ok := <-got:
		if !ok {
			t.Fatalf("bridge closed stdout: %v", sc.Err())
		}
		return sc.Text()
	case <-time.After(10 * time.Second):
		t.Fatal("no reply from the bridge")
	}
	return ""
}

// An agent's stdio session reaches the app's MCP server through the
// control socket and runs as the service's local user: the socket's
// peer check is the credential.
func TestMCPBridgeRunsAsLocalUser(t *testing.T) {
	keepOwnerExtractor(t)
	d := startDevRun(t)
	stored, err := localuser.LoadOrMint(d.l.dir)
	if err != nil {
		t.Fatal(err)
	}
	in, out, done := bridge(t, d)

	io.WriteString(in, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`+"\n")
	if line := readLine(t, out); !strings.Contains(line, `"id":1`) || !strings.Contains(line, `"serverInfo"`) {
		t.Fatalf("initialize reply = %s", line)
	}
	io.WriteString(in, `{"jsonrpc":"2.0","method":"notifications/initialized"}`+"\n")
	io.WriteString(in, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"whoami"}}`+"\n")
	if line := readLine(t, out); !strings.Contains(line, `"id":2`) || !strings.Contains(line, stored.GetID()) {
		t.Fatalf("whoami reply = %s; want the local identity %q", line, stored.GetID())
	}

	in.Close()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("bridge exit: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("bridge did not exit when stdin closed")
	}
}

// Stopping the daemon ends an open session instead of waiting on it.
func TestMCPSessionEndsOnStop(t *testing.T) {
	d := startDevRun(t)
	in, out, done := bridge(t, d)
	io.WriteString(in, `{"jsonrpc":"2.0","id":1,"method":"ping"}`+"\n")
	readLine(t, out)

	if _, err := callControl(d.l.socketPath(), "stop"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-d.done:
	case <-time.After(15 * time.Second):
		t.Fatal("daemon did not stop with an MCP session open")
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("bridge outlived the daemon")
	}
	d.done <- nil // for startRun's cleanup
}

func TestMCPBridgeNotRunning(t *testing.T) {
	b, _, _ := testBattery(t, "darwin", Config{})
	err := b.mcpBridge(true, "", strings.NewReader(""), io.Discard)
	if err == nil || !strings.Contains(err.Error(), "service run") {
		t.Fatalf("err = %v; want a not-running hint", err)
	}
	if err := b.mcpBridge(false, "relative/dir", strings.NewReader(""), io.Discard); err == nil || !strings.Contains(err.Error(), "absolute") {
		t.Fatalf("relative --state-dir: err = %v", err)
	}
}

// A tool call in flight when the daemon stops is cancelled, and the
// session ends before the App drains: the call never runs on into an
// App that is shutting down.
func TestMCPSessionEndsBeforeDrain(t *testing.T) {
	d := startDevRun(t)
	in, out, done := bridge(t, d)
	io.WriteString(in, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"hold"}}`+"\n")
	select {
	case <-d.holding:
	case <-time.After(10 * time.Second):
		t.Fatal("hold never ran")
	}
	if _, err := callControl(d.l.socketPath(), "stop"); err != nil {
		t.Fatal(err)
	}
	select {
	case stopped := <-d.held:
		if stopped {
			t.Fatal("the App drained before the session's tool call was cancelled")
		}
	case <-time.After(15 * time.Second):
		t.Fatal("the in-flight tool call was never cancelled")
	}
	select {
	case <-d.done:
	case <-time.After(15 * time.Second):
		t.Fatal("daemon did not stop")
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("bridge outlived the daemon")
	}
	_ = out
	d.done <- nil // for startRun's cleanup
}

// A tool call that ignores its cancelled context cannot stretch a stop
// past stopTimeout: the MCP wait, the drain and the control channel's
// close share one deadline.
func TestStopBudgetWithStuckTool(t *testing.T) {
	prevStop, prevMCP := stopTimeout, mcpStopWait
	stopTimeout, mcpStopWait = 800*time.Millisecond, 100*time.Millisecond
	t.Cleanup(func() { stopTimeout, mcpStopWait = prevStop, prevMCP })
	d := startDevRun(t)
	in, _, _ := bridge(t, d)
	io.WriteString(in, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"stuck"}}`+"\n")
	select {
	case <-d.stuck:
	case <-time.After(10 * time.Second):
		t.Fatal("stuck never ran")
	}
	start := time.Now()
	if _, err := callControl(d.l.socketPath(), "stop"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-d.done:
	case <-time.After(15 * time.Second):
		t.Fatal("daemon did not stop")
	}
	if took := time.Since(start); took > stopTimeout+400*time.Millisecond {
		t.Fatalf("stop took %s with a %s budget", took, stopTimeout)
	}
	d.done <- nil // for startRun's cleanup
}

// A tool call that ignores its cancelled context cannot hold the
// daemon open: every wait shares one deadline, so the stuck session
// costs it once. Waiting per step, the daemon's endMCP, Close's
// endMCP and Close's own wait each spent the full bound, which ran a
// stop past systemd's TimeoutStopSec.
func TestCloseBoundsAStuckSession(t *testing.T) {
	const bound = 300 * time.Millisecond
	prev := closeWait
	closeWait = bound
	t.Cleanup(func() { closeWait = prev })
	stuck := make(chan struct{})
	t.Cleanup(func() { close(stuck) })
	stuckServer := func() *controlServer {
		ln, err := listenControl(filepath.Join(shortTempDir(t), "c.sock"))
		if err != nil {
			t.Fatal(err)
		}
		cs := &controlServer{ln: ln, handle: echoOp, log: slog.New(slog.DiscardHandler),
			mcp: func(context.Context, io.Reader, io.Writer) { <-stuck }}
		go cs.serve()
		c, _, _, err := openControl(ln.Addr().String(), "mcp")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { c.Close() })
		return cs
	}
	within := func(name string, f func()) {
		start := time.Now()
		f()
		if d := time.Since(start); d > bound+bound/2 {
			t.Fatalf("%s took %s with a %s bound", name, d, bound)
		}
	}

	cs := stuckServer()
	within("Close", cs.Close)

	// The daemon's order: end MCP first, drain, then close, all inside
	// one deadline.
	cs = stuckServer()
	within("endMCP then closeUntil", func() {
		deadline := time.Now().Add(bound)
		cs.endMCP(deadline)
		cs.closeUntil(deadline)
	})
}

// A daemon with no MCP server refuses the op; past the cap a new
// session is refused while the open ones carry on.
func TestControlMCPRefusals(t *testing.T) {
	path := startControl(t, echoOp)
	if _, _, _, err := openControl(path, "mcp"); err == nil || !strings.Contains(err.Error(), "serves no MCP") {
		t.Fatalf("err = %v", err)
	}

	release := make(chan struct{})
	path = startControlServer(t, &controlServer{handle: echoOp, mcp: func(context.Context, io.Reader, io.Writer) { <-release }})
	defer close(release)
	var open []net.Conn
	defer func() {
		for _, c := range open {
			c.Close()
		}
	}()
	for range maxMCPSessions {
		c, _, _, err := openControl(path, "mcp")
		if err != nil {
			t.Fatal(err)
		}
		open = append(open, c)
	}
	if _, _, _, err := openControl(path, "mcp"); err == nil || !strings.Contains(err.Error(), "too many MCP sessions") {
		t.Fatalf("session %d: err = %v", maxMCPSessions+1, err)
	}
}

// The daemon's run record points agents at `service mcp`.
func TestDaemonRecordsMCPCommand(t *testing.T) {
	t.Setenv(localrun.EnvDir, t.TempDir())
	d := startDevRun(t)
	recs, err := localrun.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 1 {
		t.Fatalf("records = %+v", recs)
	}
	r := recs[0]
	if r.Service != d.b.cfg.ID || !r.Dev || len(r.MCPCommand) != 6 ||
		!slices.Equal(r.MCPCommand[1:], []string{"service", "mcp", "--dev", "--state-dir", d.l.dir}) || r.URL != "http://"+d.addr {
		t.Fatalf("record = %+v", r)
	}
}

// The recorded command reaches its own daemon from anywhere: an agent
// starts it in its own directory and environment, where neither the
// dev run's worktree nor the install's data-dir variable applies.
func TestRecordedMCPCommandFindsItsDaemon(t *testing.T) {
	d := startDevRun(t)
	argv := Runtime{DataDir: d.l.dir, Dev: true}.MCPCommand()
	// The agent's environment resolves no layout at all: --state-dir
	// must not consult it.
	t.Setenv(dataDirEnv, "relative")
	if _, err := d.b.layoutFor(true); err == nil {
		t.Fatal("test setup: the layout still resolves")
	}
	var out strings.Builder
	d.b.cfg.Stdin = strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}` + "\n")
	d.b.cfg.Stdout = &out
	// argv is [exe service mcp ...]; dispatch takes what follows "service".
	if err := d.b.dispatch(context.Background(), argv[2:], nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"id":1`) {
		t.Fatalf("reply = %q", out.String())
	}
}
