//go:build !windows

package update

import (
	"time"

	"github.com/DonaldMurillo/gofastr/internal/procalive"
)

// waitExit polls until pid is gone or limit passes. No relaunch sets
// PredecessorEnv off Windows today; this keeps the contract the same
// on every host.
func waitExit(pid int, limit time.Duration) {
	deadline := time.Now().Add(limit)
	for procalive.Alive(pid) && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
}
