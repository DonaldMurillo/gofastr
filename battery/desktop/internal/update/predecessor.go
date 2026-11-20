package update

import (
	"os"
	"strconv"
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
