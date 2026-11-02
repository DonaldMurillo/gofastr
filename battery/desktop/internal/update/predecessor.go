package update

import (
	"os"
	"strconv"
	"time"
)

// PredecessorEnv carries the process ID of the instance an update
// relaunch replaces. The relaunched app waits for that process to exit
// before it claims the single-instance lock, opens its database, or
// binds its listener; otherwise it would find the quitting instance
// still holding all three and hand its launch over to it.
const PredecessorEnv = "GOFASTR_DESKTOP_UPDATE_PREDECESSOR"

// predecessorWait bounds the wait. The old instance quits a quarter
// second after the swap; a process still alive after this is wedged,
// and the new one starts anyway.
const predecessorWait = 30 * time.Second

// AwaitPredecessor removes PredecessorEnv from the environment (so the
// app's own children never inherit it) and, when it named another live
// process, waits up to predecessorWait for that process to exit. It
// reports whether it waited.
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
