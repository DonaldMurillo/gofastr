//go:build windows && amd64

package windows

import (
	"sync"
	"testing"

	"github.com/DonaldMurillo/gofastr/battery/desktop"
)

func TestTrayTitleDuringInstall(t *testing.T) {
	s := &winShell{}
	cfg := &desktop.Tray{Title: "default"}
	start := make(chan struct{})
	var calls sync.WaitGroup
	calls.Go(func() {
		<-start
		for range 20 {
			s.installTray(cfg)
		}
	})
	calls.Go(func() {
		<-start
		for range 100 {
			_ = s.SetTrayTitle("updated")
		}
	})
	close(start)
	calls.Wait()
}
