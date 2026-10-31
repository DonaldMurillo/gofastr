package framework

//gofastr:test-reads examples/processmodule-demo/

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"testing"
	"time"
)

// A child whose own peer faulted kept running: its read loop drained
// stdin and answered nothing, the host peer saw no fault, and every call
// to the module waited out its deadline (the CI hang on PR #624, after a
// false stall fault in the child). The demo child, the shape the docs
// teach, now exits non-zero on a fault, so the supervisor restarts it.
func TestDemoChildExitsOnPeerFault(t *testing.T) {
	if os.Getenv(childEnvName) != "" {
		return
	}
	bin, _ := buildDemoBinary(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin)
	cmd.WaitDelay = time.Second
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer stdin.Close()
	// A response to a call the child never made is a terminal protocol
	// fault on the child's peer; stdin stays open, so only the fault can
	// end the process.
	if _, err := stdin.Write([]byte(`{"jsonrpc":"2.0","id":99,"result":"x"}` + "\n")); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if code := cmd.ProcessState.ExitCode(); code != 1 {
			t.Fatalf("faulted child exited %d (%v), want 1; stderr: %s", code, err, stderr.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a child whose peer faulted kept running with stdin open")
	}
}
