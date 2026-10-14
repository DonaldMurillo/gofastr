package service

import (
	"bufio"
	"io"
	"net"
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
		done <- d.b.mcpBridge(true, inR, outW)
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
	err := b.mcpBridge(true, strings.NewReader(""), io.Discard)
	if err == nil || !strings.Contains(err.Error(), "service run") {
		t.Fatalf("err = %v; want a not-running hint", err)
	}
}

// A daemon with no MCP server refuses the op; past the cap a new
// session is refused while the open ones carry on.
func TestControlMCPRefusals(t *testing.T) {
	path := startControl(t, echoOp)
	if _, _, _, err := openControl(path, "mcp"); err == nil || !strings.Contains(err.Error(), "serves no MCP") {
		t.Fatalf("err = %v", err)
	}

	release := make(chan struct{})
	path = startControlServer(t, &controlServer{handle: echoOp, mcp: func(io.Reader, io.Writer) { <-release }})
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
	if r.Service != d.b.cfg.ID || !r.Dev || len(r.MCPCommand) != 4 ||
		!slices.Equal(r.MCPCommand[1:], []string{"service", "mcp", "--dev"}) || r.URL != "http://"+d.addr {
		t.Fatalf("record = %+v", r)
	}
}
