package framework

import (
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"testing"
	"time"
)

// An app started with a socket from systemd serves on that socket, not
// on the address it was given: the port is the one systemd already
// holds, and the address asked for would find it taken. Started the way
// systemd starts a service: the socket at descriptor 3, LISTEN_FDS=1,
// LISTEN_PID the process's own.
func TestStartServesOnASocketFromSystemd(t *testing.T) {
	if os.Getenv("GOFASTR_SOCKET_CHILD") == "1" {
		app := NewApp(WithoutDefaultMiddleware(), WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))))
		app.startupOutput = io.Discard
		app.Router().Get("/hello", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("from the app")) }))
		// Read when asked, after Start's guards have run.
		app.Router().Get("/dev-tools", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, app.mcpControl) }))
		// An address nothing can bind: were the socket ignored, Start
		// would fail here.
		addr := "256.0.0.1:1"
		if given := os.Getenv("GOFASTR_SOCKET_CHILD_ADDR"); given != "" {
			addr = given
		}
		err := app.Start(addr)
		fmt.Fprintf(os.Stderr, "child: Start returned %v\n", err)
		os.Exit(4)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	f, err := ln.(*net.TCPListener).File()
	if err != nil {
		t.Fatal(err)
	}
	// Not os.Args[0]: another test in the package may have rewritten it.
	self, err := os.Executable()
	if err != nil {
		t.Skip(err)
	}
	cmd := exec.Command("sh", "-c", `LISTEN_PID=$$ LISTEN_FDS=1 exec "$0" -test.run=TestStartServesOnASocketFromSystemd`, self)
	cmd.Env = append(os.Environ(), "GOFASTR_SOCKET_CHILD=1")
	cmd.ExtraFiles = []*os.File{f}
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Start(); err != nil {
		t.Skipf("no sh to start the child with: %v", err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	addr := ln.Addr().String()
	f.Close()
	ln.Close()
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get("http://" + addr + "/hello")
	if err != nil {
		t.Fatalf("the app did not answer on the passed socket: %v\n%s", err, out.String())
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<10))
	if string(body) != "from the app" {
		t.Errorf("answer %q (status %d)", body, resp.StatusCode)
	}
}

// A deploy reads an app's binary for a marker before it hands the port
// to systemd. The marker must be in every binary that links the
// framework: this one does, and names it nowhere (the text below is put
// together at run time, so it is not the copy found).
func TestSocketMarkerIsInAnAppBinary(t *testing.T) {
	path, err := os.Executable()
	if err != nil {
		t.Skip(err)
	}
	self, err := os.ReadFile(path)
	if err != nil {
		t.Skip(err)
	}
	// Sprintf, not "+": the compiler folds joined constants into one.
	marker := []byte(fmt.Sprintf("gofastr:%s-socket-v%d", "systemd", len(path[:1])))
	if !bytes.Contains(self, marker) {
		t.Error("the marker is not in a binary that links the framework: a deploy would take the app for one that cannot be handed a socket")
	}
}

// What a bind may expose is decided from the socket the app serves on,
// not from the address it was given. An app given "localhost:8080" and
// handed a socket on every interface (which is what gofastr deploy
// sets up) must lose its dev-only MCP tools, as one that opened
// 0.0.0.0 itself does. Found in review: the guard read the unused
// address, so a server deployed with GOFASTR_DEV set served the
// ungated dev tools to anyone who could reach the port.
func TestSocketFromSystemdDecidesTheDevToolsGuard(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Skip(err)
	}
	for bind, want := range map[string]string{"0.0.0.0:0": "false", "127.0.0.1:0": "true"} {
		ln, err := net.Listen("tcp4", bind)
		if err != nil {
			t.Fatal(err)
		}
		f, err := ln.(*net.TCPListener).File()
		if err != nil {
			t.Fatal(err)
		}
		// The child is given a loopback address either way: only the
		// socket differs.
		cmd := exec.Command("sh", "-c", `LISTEN_PID=$$ LISTEN_FDS=1 exec "$0" -test.run=TestStartServesOnASocketFromSystemd`, self)
		cmd.Env = append(os.Environ(), "GOFASTR_SOCKET_CHILD=1", "GOFASTR_SOCKET_CHILD_ADDR=localhost:8080", "GOFASTR_DEV=1", "GOFASTR_ENV=", "GOFASTR_DEV_MCP=", "GOFASTR_DEV_MCP_EXPOSE=")
		cmd.ExtraFiles = []*os.File{f}
		var out bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &out
		if err := cmd.Start(); err != nil {
			t.Skipf("no sh to start the child with: %v", err)
		}
		_, port, _ := net.SplitHostPort(ln.Addr().String())
		f.Close()
		ln.Close()
		resp, err := (&http.Client{Timeout: 10 * time.Second}).Get("http://127.0.0.1:" + port + "/dev-tools")
		if err != nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			t.Fatalf("socket on %s: %v\n%s", bind, err, out.String())
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 64))
		resp.Body.Close()
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		if string(body) != want {
			t.Errorf("SECURITY: socket on %s, app given localhost:8080: dev-only tools kept = %s, want %s", bind, body, want)
		}
	}
}
