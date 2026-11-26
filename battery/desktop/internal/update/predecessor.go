package update

import (
	"os"
	"strconv"
	"strings"
	"time"
)

// PredecessorEnv carries the process ID of the instance an update relaunch replaces.
const PredecessorEnv = "GOFASTR_DESKTOP_UPDATE_PREDECESSOR"

const predecessorWait = 30 * time.Second

// AwaitPredecessor removes PredecessorEnv from the environment (so the
// app's own children never inherit it) and, when it named another live
// process, waits up to predecessorWait for that process to exit.
func AwaitPredecessor() bool {
	v, ok := os.LookupEnv(PredecessorEnv)
	if !ok {
		return false
	}
	_ = os.Unsetenv(PredecessorEnv)
	pid, err := strconv.Atoi(v)
	if err != nil || pid <= 0 || pid == os.Getpid() {
		return false
	}
	waitExit(pid, predecessorWait)
	return true
}

// relaunchEnv is env for the process an update relaunches: every
// PredecessorEnv entry, by the OS's key comparison same, replaced with
// one naming this process.
func relaunchEnv(env []string, same func(a, b string) bool) []string {
	out := env[:0:0]
	for _, kv := range env {
		k, _, _ := strings.Cut(kv, "=")
		if !same(k, PredecessorEnv) {
			out = append(out, kv)
		}
	}
	return append(out, PredecessorEnv+"="+strconv.Itoa(os.Getpid()))
}
