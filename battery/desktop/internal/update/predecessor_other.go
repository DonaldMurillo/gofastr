//go:build !windows

package update

import (
	"time"

	"github.com/DonaldMurillo/gofastr/internal/procalive"
)

func waitExit(pid int, limit time.Duration) {
	deadline := time.Now().Add(limit)
	for procalive.Alive(pid) && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
}
