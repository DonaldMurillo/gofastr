package update

import (
	"os"
	"os/exec"
	"strconv"
	"testing"
	"time"
)

func TestAwaitPredecessorConsumesTheVariable(t *testing.T) {
	for _, v := range []string{"junk", "0", "-4", strconv.Itoa(os.Getpid())} {
		t.Setenv(PredecessorEnv, v)
		if AwaitPredecessor() {
			t.Errorf("%s=%q made the app wait", PredecessorEnv, v)
		}
		if _, ok := os.LookupEnv(PredecessorEnv); ok {
			t.Errorf("%s=%q is still in the environment for the app's children", PredecessorEnv, v)
		}
	}
}

func TestAwaitPredecessorWaitsForALiveProcess(t *testing.T) {
	sleep, err := exec.LookPath("sleep")
	if err != nil {
		t.Skip("no sleep binary")
	}
	child := exec.Command(sleep, "0.5")
	started := time.Now()
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	go func() { _ = child.Wait() }()
	t.Setenv(PredecessorEnv, strconv.Itoa(child.Process.Pid))
	if !AwaitPredecessor() {
		t.Fatal("AwaitPredecessor did not wait for a live predecessor")
	}
	if waited := time.Since(started); waited < 400*time.Millisecond {
		t.Fatalf("AwaitPredecessor returned after %v, before the 500ms predecessor exited", waited)
	}
}
