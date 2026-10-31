package a2a

import (
	"context"
	"testing"
	"time"
)

// TestShutdownCancelsAndWaitsForRuns: a run started with
// returnImmediately is detached from its request, so only Shutdown can
// reach it. Shutdown cancels the run's context, returns once the run has
// settled as CANCELED, and refuses runs claimed after it.
func TestShutdownCancelsAndWaitsForRuns(t *testing.T) {
	h := newHarness(t, nil)
	started := make(chan struct{})
	returned := make(chan struct{})
	h.setHandler(func(ctx context.Context, _ TaskContext) error {
		close(started)
		<-ctx.Done()
		time.Sleep(50 * time.Millisecond) // teardown Shutdown must wait out
		close(returned)
		return ctx.Err()
	})
	task := h.send("alice", map[string]any{"returnImmediately": true})
	<-started

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := h.srv.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	select {
	case <-returned:
	default:
		t.Fatal("Shutdown returned while the run's handler was still running")
	}
	h.waitTask("alice", task.ID, TaskStateCanceled, time.Second)

	_, e, raw := h.call("alice", MethodSendMessage, map[string]any{
		"message": map[string]any{
			"role":     "ROLE_USER",
			"parts":    []any{map[string]any{"text": "late"}},
			"metadata": map[string]any{"skill": "echo"},
		},
	})
	if e.Error == nil || e.Error.Code != CodeUnsupportedOperation {
		t.Fatalf("a send after Shutdown = %s, want an unsupported-operation refusal", raw)
	}
}

// TestShutdownHonoursItsDeadline: a handler that ignores cancellation
// cannot hold Shutdown past its context.
func TestShutdownHonoursItsDeadline(t *testing.T) {
	h := newHarness(t, nil)
	started, release := make(chan struct{}), make(chan struct{})
	defer close(release)
	h.setHandler(func(context.Context, TaskContext) error {
		close(started)
		<-release
		return nil
	})
	h.send("alice", map[string]any{"returnImmediately": true})
	<-started

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := h.srv.Shutdown(ctx); err != context.DeadlineExceeded {
		t.Fatalf("Shutdown = %v, want context.DeadlineExceeded", err)
	}
}
