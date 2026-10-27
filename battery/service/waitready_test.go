package service

import (
	"log/slog"
	"testing"
	"time"
)

// A daemon binds its control socket before it builds the app, and
// answers only once the app is ready. A probe in that window dials,
// then times out waiting for the reply. install and restart must keep
// waiting, not report that the daemon never came up.
func TestWaitReadyRidesStartupSilence(t *testing.T) {
	inst := layout{dir: shortTempDir(t)}
	ln, err := listenControl(inst.socketPath())
	if err != nil {
		t.Fatal(err)
	}
	s := &controlServer{ln: ln, log: slog.New(slog.DiscardHandler),
		handle: func(string) controlResponse { return controlResponse{OK: true, PID: 42} }}
	t.Cleanup(s.Close)
	go func() {
		time.Sleep(controlDeadline + time.Second) // the first probe times out
		s.serve()
	}()
	resp, err := New(Config{ID: "dev.gofastr.wait", Port: 1}).waitReady(inst)
	if err != nil || resp.PID != 42 {
		t.Fatalf("waitReady = %+v, %v; want the status reply once the daemon answers", resp, err)
	}
}
