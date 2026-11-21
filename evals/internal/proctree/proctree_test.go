package proctree

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"runtime"
	"testing"
	"time"
)

func TestExecutableNameIsWindowsSuffixed(t *testing.T) {
	want := "candidate-app"
	if runtime.GOOS == "windows" {
		want += ".exe"
	}
	if got := ExecutableName("candidate-app"); got != want {
		t.Errorf("ExecutableName = %q, want %q on %s", got, want, runtime.GOOS)
	}
}

func TestFreePortYieldsUsableLoopbackPort(t *testing.T) {
	port, err := FreePort()
	if err != nil {
		t.Fatalf("FreePort: %v", err)
	}
	if port <= 0 || port > 65535 {
		t.Fatalf("FreePort = %d, want a valid TCP port", port)
	}
	if _, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port)); err == nil {
		t.Errorf("port %d still answered after FreePort released it", port)
	}
}

// TestOwnedCommandStopWaitsForExit also covers platforms with direct-child teardown.
func TestOwnedCommandStopWaitsForExit(t *testing.T) {
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(binary, "-test.run=^TestOwnedCommandStopHelper$")
	cmd.Env = append(os.Environ(), "GOFASTR_TEST_PROCTREE_STOP=1")
	p, err := Start(t.Context(), cmd)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); <-p.Done() })
	stopped := make(chan struct{}, 2)
	for range 2 {
		go func() { p.Stop(); stopped <- struct{}{} }()
	}
	deadline := time.After(2 * time.Second)
	for range 2 {
		select {
		case <-stopped:
		case <-deadline:
			t.Fatal("concurrent stop did not finish")
		}
	}
	select {
	case <-p.Done():
	default:
		t.Fatal("Stop returned before completion")
	}
	if err := p.Wait(); err == nil {
		t.Fatal("stopped command unexpectedly succeeded")
	}
}

func TestOwnedCommandStopHelper(t *testing.T) {
	if os.Getenv("GOFASTR_TEST_PROCTREE_STOP") == "1" {
		time.Sleep(30 * time.Second)
	}
}
