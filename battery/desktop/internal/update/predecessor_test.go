package update

import (
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
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

func TestRelaunchEnvReplacesEveryPredecessorEntry(t *testing.T) {
	want := PredecessorEnv + "=" + strconv.Itoa(os.Getpid())
	env := []string{"A=1", PredecessorEnv + "=7", strings.ToLower(PredecessorEnv) + "=8", "B=2"}
	folded := relaunchEnv(env, strings.EqualFold)
	if !slices.Equal(folded, []string{"A=1", "B=2", want}) {
		t.Fatalf("case-folded (Windows) env = %q", folded)
	}
	exact := relaunchEnv(env, func(a, b string) bool { return a == b })
	if !slices.Equal(exact, []string{"A=1", strings.ToLower(PredecessorEnv) + "=8", "B=2", want}) {
		t.Fatalf("exact (Linux) env = %q", exact)
	}
	if env[1] != PredecessorEnv+"=7" {
		t.Fatal("relaunchEnv changed its input")
	}
}
