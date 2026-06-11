package framework

import (
	"bytes"
	"context"
	"net"
	"sync"
	"testing"
	"time"
)

// TestStart_BindFailureRunsShutdown pins the cleanup contract: when
// ListenAndServe fails (the canonical case being the port already in use),
// Start must still drain — running OnStop hooks and cancelling the lifecycle
// context — instead of returning early and leaking every worker, cron, and
// queue an earlier start phase spawned.
func TestStart_BindFailureRunsShutdown(t *testing.T) {
	// Occupy a port so the app's bind fails deterministically.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	addr := ln.Addr().String()

	app := NewApp()
	var output bytes.Buffer
	app.startupOutput = &output
	var mu sync.Mutex
	stopRan := false
	app.OnStop(func() error {
		mu.Lock()
		stopRan = true
		mu.Unlock()
		return nil
	})

	done := make(chan error, 1)
	go func() { done <- app.Start(addr) }()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("Start on an occupied port should return an error")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Start did not return after bind failure")
	}

	mu.Lock()
	ran := stopRan
	mu.Unlock()
	if !ran {
		t.Error("bind failure did not run OnStop hooks — Shutdown was skipped (leak)")
	}
	if output.Len() != 0 {
		t.Errorf("bind failure printed a readiness banner: %q", output.String())
	}

	// Lifecycle context must be cancelled too.
	app.serverMu.Lock()
	ctx := app.appCtx
	app.serverMu.Unlock()
	if ctx != nil {
		select {
		case <-ctx.Done():
		default:
			t.Error("lifecycle context not cancelled after bind failure")
		}
	}
	_ = context.Background
}
