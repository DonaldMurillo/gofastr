package service

import (
	"bufio"
	"errors"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func startControl(t *testing.T, handle func(string) controlResponse) string {
	t.Helper()
	return startControlServer(t, &controlServer{handle: handle})
}

// startControlServer listens on a fresh socket and serves s.
func startControlServer(t *testing.T, s *controlServer) string {
	t.Helper()
	path := filepath.Join(shortTempDir(t), "c.sock")
	ln, err := listenControl(path)
	if err != nil {
		t.Fatal(err)
	}
	s.ln, s.log = ln, slog.New(slog.DiscardHandler)
	go s.serve()
	t.Cleanup(s.Close)
	return path
}

// rawControl writes payload and returns the reply line.
func rawControl(t *testing.T, path, payload string) string {
	t.Helper()
	c, err := net.Dial("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := io.WriteString(c, payload); err != nil {
		t.Fatal(err)
	}
	line, _ := bufio.NewReader(c).ReadString('\n')
	return line
}

func echoOp(op string) controlResponse {
	switch op {
	case "status":
		return controlResponse{OK: true, PID: 42}
	case "boom":
		panic("handler bug")
	}
	return controlResponse{Error: "unknown op"}
}

func TestControlRoundTrip(t *testing.T) {
	path := startControl(t, echoOp)
	resp, err := callControl(path, "status")
	if err != nil {
		t.Fatal(err)
	}
	if !resp.OK || resp.PID != 42 {
		t.Fatalf("status reply %+v", resp)
	}
	if _, err := callControl(path, "nope"); err == nil || !strings.Contains(err.Error(), "unknown op") {
		t.Fatalf("unknown op error = %v", err)
	}
}

// A panicking op answers "internal error" and the server keeps serving.
func TestControlRecoversHandlerPanic(t *testing.T) {
	path := startControl(t, echoOp)
	if _, err := callControl(path, "boom"); err == nil || !strings.Contains(err.Error(), "internal error") {
		t.Fatalf("panicking op error = %v", err)
	}
	if _, err := callControl(path, "status"); err != nil {
		t.Fatalf("server died after a handler panic: %v", err)
	}
}

func TestControlRefusesMalformedRequests(t *testing.T) {
	path := startControl(t, echoOp)
	cases := map[string]string{
		"unknown field":   `{"op":"status","extra":1}` + "\n",
		"duplicate key":   `{"op":"status","op":"token"}` + "\n",
		"case-folded key": `{"OP":"status"}` + "\n",
		"not json":        "status\n",
	}
	for name, payload := range cases {
		if got := rawControl(t, path, payload); !strings.Contains(got, "malformed request") {
			t.Errorf("%s: reply %q; want malformed request", name, got)
		}
	}
}

func TestControlBoundsRequestSize(t *testing.T) {
	path := startControl(t, echoOp)
	payload := `{"op":"` + strings.Repeat("a", maxControlRequest) + `"}` + "\n"
	if got := rawControl(t, path, payload); !strings.Contains(got, "under 4 KiB") {
		t.Fatalf("oversized request reply %q", got)
	}
}

func TestListenControlRefusesLiveSocket(t *testing.T) {
	path := startControl(t, echoOp)
	if _, err := listenControl(path); !errors.Is(err, errAlreadyRunning) {
		t.Fatalf("second listen error = %v; want errAlreadyRunning", err)
	}
}

func TestListenControlReplacesStaleSocket(t *testing.T) {
	path := filepath.Join(shortTempDir(t), "c.sock")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	ln, err := listenControl(path)
	if err != nil {
		t.Fatalf("stale socket file not replaced: %v", err)
	}
	ln.Close()
}

func TestListenControlRestrictsSocket(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("mode bits do not apply")
	}
	path := startControl(t, echoOp)
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm()&0o077 != 0 {
		t.Fatalf("control socket mode %v; want owner-only", fi.Mode().Perm())
	}
}

func TestListenControlRefusesLongPath(t *testing.T) {
	path := "/" + strings.Repeat("d", maxSocketPath) + "/c.sock"
	if _, err := listenControl(path); err == nil || !strings.Contains(err.Error(), dataDirEnv) {
		t.Fatalf("long path error = %v; want a hint naming %s", err, dataDirEnv)
	}
}

func TestCallControlNotRunning(t *testing.T) {
	path := filepath.Join(shortTempDir(t), "missing.sock")
	if _, err := callControl(path, "status"); !errors.Is(err, errNotRunning) {
		t.Fatalf("error = %v; want errNotRunning", err)
	}
}

// The peer check accepts the daemon's own user. Another user cannot be
// produced in a unit test; peercred_*.go carry that half.
func TestPeerIsSelfAcceptsOwnUser(t *testing.T) {
	path := filepath.Join(shortTempDir(t), "p.sock")
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	errc := make(chan error, 1)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			errc <- err
			return
		}
		defer c.Close()
		errc <- peerIsSelf(c.(*net.UnixConn))
	}()
	c, err := net.Dial("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err := <-errc; err != nil {
		t.Fatalf("peerIsSelf refused this process: %v", err)
	}
}

// A connection from another OS user is dropped before the handler
// runs: no op executes and no reply is written.
func TestControlRefusesForeignPeer(t *testing.T) {
	prev := peerCheck
	peerCheck = func(*net.UnixConn) error { return errors.New("peer uid 502 is not the daemon's uid 501") }
	t.Cleanup(func() { peerCheck = prev })
	var calls atomic.Int32
	path := startControl(t, func(op string) controlResponse {
		calls.Add(1)
		return controlResponse{OK: true, Token: "t1"}
	})
	// The server may drop the connection before the request is even
	// written, so a write error is an expected outcome here.
	c, err := net.Dial("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(5 * time.Second))
	_, _ = io.WriteString(c, `{"op":"token"}`+"\n")
	if got, _ := io.ReadAll(c); len(got) != 0 {
		t.Fatalf("foreign peer got a reply: %q", got)
	}
	if calls.Load() != 0 {
		t.Fatal("handler ran for a foreign peer")
	}
}
