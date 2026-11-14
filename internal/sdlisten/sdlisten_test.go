package sdlisten

import (
	"bytes"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

// A socket is taken only when the variables name this process and say
// there is exactly one; anything else is "no socket", or an error where
// taking one would be wrong.
func TestInheritedReadsTheProtocol(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	file := func() *os.File {
		f, err := ln.(*net.TCPListener).File()
		if err != nil {
			t.Fatal(err)
		}
		return f
	}
	opened := 0
	open := func(int) *os.File { opened++; return file() }

	for name, vars := range map[string]map[string]string{
		"nothing set":       {},
		"another process":   {"LISTEN_PID": "1", "LISTEN_FDS": "1"},
		"no count":          {"LISTEN_PID": "42"},
		"zero":              {"LISTEN_PID": "42", "LISTEN_FDS": "0"},
		"not a number":      {"LISTEN_PID": "42", "LISTEN_FDS": "one"},
		"pid with a suffix": {"LISTEN_PID": "42x", "LISTEN_FDS": "1"},
	} {
		got, err := inherited(env(vars), 42, open)
		if got != nil || err != nil || opened != 0 {
			t.Errorf("%s: listener %v, err %v, descriptor opened %d times; want nothing taken", name, got, err, opened)
		}
	}

	got, err := inherited(env(map[string]string{"LISTEN_PID": "42", "LISTEN_FDS": "1"}), 42, open)
	if err != nil || got == nil {
		t.Fatalf("one socket for this process: %v, %v", got, err)
	}
	defer got.Close()
	if got.Addr().String() != ln.Addr().String() {
		t.Errorf("took %s, want the socket passed, %s", got.Addr(), ln.Addr())
	}

	if got, err := inherited(env(map[string]string{"LISTEN_PID": "42", "LISTEN_FDS": "2"}), 42, open); got != nil || err == nil {
		t.Errorf("two sockets: %v, %v; want an error, not a guess at which", got, err)
	}
	if got, err := inherited(env(map[string]string{"LISTEN_PID": "42", "LISTEN_FDS": "1"}), 42, func(int) *os.File { return nil }); got != nil || err == nil {
		t.Errorf("descriptor 3 closed: %v, %v", got, err)
	}
	notSocket, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := inherited(env(map[string]string{"LISTEN_PID": "42", "LISTEN_FDS": "1"}), 42, func(int) *os.File { return notSocket }); got != nil || err == nil {
		t.Errorf("descriptor 3 a plain file: %v, %v", got, err)
	}
}

// The real thing, across a process boundary: a child started the way
// systemd starts a service, with the socket at descriptor 3 and the two
// variables, serves on it; and the variables are gone from its
// environment afterwards, so its own children do not take them.
func TestChildServesOnThePassedSocket(t *testing.T) {
	if os.Getenv("SDLISTEN_CHILD") == "1" {
		ln, err := Inherited()
		if err != nil || ln == nil {
			os.Stderr.WriteString("child: no socket: " + strings.TrimSpace(strings.Join([]string{os.Getenv("LISTEN_PID"), os.Getenv("LISTEN_FDS")}, " ")) + "\n")
			os.Exit(3)
		}
		left := os.Getenv("LISTEN_PID") + os.Getenv("LISTEN_FDS")
		_ = http.Serve(ln, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("served by the child; left=" + left)) }))
		return
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	f, err := ln.(*net.TCPListener).File()
	if err != nil {
		t.Fatal(err)
	}
	// LISTEN_PID must be the child's own pid, which exists only once it
	// has started: a shell sets it and then becomes the child, as
	// systemd sets it between fork and exec.
	cmd := exec.Command("sh", "-c", `LISTEN_PID=$$ LISTEN_FDS=1 exec "$0" -test.run=TestChildServesOnThePassedSocket`, os.Args[0])
	cmd.Env = append(os.Environ(), "SDLISTEN_CHILD=1")
	cmd.ExtraFiles = []*os.File{f} // descriptor 3
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Skipf("no sh to start the child with: %v", err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	// The parent's copies are closed: only the child holds the socket.
	addr := ln.Addr().String()
	f.Close()
	ln.Close()
	resp, err := http.Get("http://" + addr + "/")
	if err != nil {
		t.Fatalf("the child did not serve on the passed socket: %v\n%s", err, stderr.String())
	}
	defer resp.Body.Close()
	var body bytes.Buffer
	body.ReadFrom(resp.Body)
	if body.String() != "served by the child; left=" {
		t.Errorf("answer %q; want the child's, with the variables cleared", body.String())
	}
}

// The marker is in the binary: a deploy reads the file for it.
func TestMarkerIsLinkedIn(t *testing.T) {
	self, err := os.ReadFile(os.Args[0])
	if err != nil {
		t.Skip(err)
	}
	if !bytes.Contains(self, []byte(Marker)) {
		t.Error("the marker is not in a binary that links the package")
	}
}
