package queue

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// ============================================================================
// Property: Replay must never lose the terminal (failed) record when the
// re-enqueue fails — the enqueue-first/remove-second ordering that
// RedisQueue.Replay documents as the lossless rule ("a failure between
// the two ops leaves the job on the dead list (recoverable) rather than
// dropping it"). Surface: MemoryQueue.Replay, which inverts it — it
// deletes from q.dead BEFORE calling Enqueue, so a cancelled context
// (battery/admin passes r.Context(), a client disconnect or request
// deadline fires it) or a Replay racing Close loses the record
// permanently: the visible error invites a retry that is then the
// documented idempotent no-op against a vanished job.
//
// (The companion memory-surface finding from the same recon pass — the
// worker pool dropping unregistered-type jobs without a trace — is
// already pinned, RED, by TestUnknownTypeJobDropIsObservable/memory in
// queue_security_test.go; not duplicated here.)
// ============================================================================

// TestReplayKeepsJobWhenEnqueueFails: a failed re-enqueue must leave the
// job retrievable from ListJobs("failed") so the visible error can be
// retried. Today the record is consumed first and the loss is permanent.
func TestReplayKeepsJobWhenEnqueueFails(t *testing.T) {
	assertRetained := func(t *testing.T, q *MemoryQueue, id string, replayErr error) {
		t.Helper()
		dead, err := q.ListJobs(context.Background(), "failed", 0)
		if err != nil {
			t.Fatalf("list failed jobs: %v", err)
		}
		if len(dead) != 1 || dead[0].ID != id {
			t.Fatalf("Replay consumed the terminal record before the re-enqueue succeeded (replay err=%v, failed jobs=%v): the visible error invites a retry that is now the documented idempotent no-op against a vanished job — permanent loss", replayErr, dead)
		}
	}

	t.Run("cancelled context", func(t *testing.T) {
		q := NewMemoryQueue(0)
		q.retainDead(Job{ID: "dead-1", Type: "x", MaxAttempts: 1})

		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		err := q.Replay(ctx, "dead-1")
		if err == nil || !errors.Is(err, context.Canceled) {
			t.Fatalf("Replay with a cancelled context = %v, want context.Canceled surfaced", err)
		}
		assertRetained(t, q, "dead-1", err)
	})

	t.Run("closed queue", func(t *testing.T) {
		q := NewMemoryQueue(0)
		q.retainDead(Job{ID: "dead-2", Type: "x", MaxAttempts: 1})
		if err := q.Close(); err != nil {
			t.Fatalf("close: %v", err)
		}

		err := q.Replay(context.Background(), "dead-2")
		if err == nil || !errors.Is(err, ErrQueueClosed) {
			t.Fatalf("Replay against a closed queue = %v, want ErrQueueClosed surfaced", err)
		}
		assertRetained(t, q, "dead-2", err)
	})
}

// Property: a panic in the SetGate gate function is isolated to the job;
// the worker pool (and the process) survives it and keeps processing.
// Handlers are panic-isolated via safeHandle; the gate gets the same net
// through gateAllows (recover → log + fail closed → defer), pinned here
// process-level by re-exec because an unrecovered panic in the worker
// goroutine kills the whole process.
const queueGatePanicChildEnv = "GOFASTR_TEST_QUEUE_GATE_PANIC_CHILD"

func TestMemoryQueueGatePanicIsolated(t *testing.T) {
	if os.Getenv(queueGatePanicChildEnv) == "1" {
		queueGatePanicChild()
		return // unreachable; queueGatePanicChild exits
	}

	cmd := exec.Command(os.Args[0],
		"-test.run", "^TestMemoryQueueGatePanicIsolated$",
		"-test.count=1")
	cmd.Env = append(os.Environ(), queueGatePanicChildEnv+"=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() >= 10 {
			t.Fatalf("gate-panic child scenario contract broken (exit %d):\n%s", exitErr.ExitCode(), out)
		}
		t.Errorf("SECURITY: [gate-panic-isolation] a panicking SetGate callback killed the process: the worker did not survive it (memory.go processJob must route the gate through gateAllows like DBQueue): %v\n--- child output ---\n%s", err, out)
		return
	}
	if strings.Contains(string(out), "panic:") {
		t.Errorf("SECURITY: [gate-panic-isolation] child survived but reported a panic:\n%s", out)
	}
}

// queueGatePanicChild is the child-side scenario. It never returns.
// Exit codes: 0 = gate panic contained and the follow-up job processed by
// the surviving worker; >=10 = scenario contract broken; runtime crash
// (exit 2) = the gate panic escaped the worker goroutine and killed the
// process.
func queueGatePanicChild() {
	q := NewMemoryQueue(1)

	healthy := make(chan struct{})
	q.RegisterHandler("gatepanics.healthy", func(context.Context, Job) error {
		close(healthy)
		return nil
	})
	// The gate must run for the poison type, so a handler has to exist
	// (processJob dead-letters unknown types before reaching the gate).
	q.RegisterHandler("gatepanics.poison", func(context.Context, Job) error {
		return nil
	})
	q.SetGate(func(jobType string) bool {
		if jobType == "gatepanics.poison" {
			panic("gate boom")
		}
		return true
	})
	q.Start()

	ctx := context.Background()
	// Poison first: equal priority is FIFO by enqueue order, so the single
	// worker hits the panicking gate before the healthy job.
	if err := q.Enqueue(ctx, Job{Type: "gatepanics.poison"}); err != nil {
		os.Exit(12)
	}
	if err := q.Enqueue(ctx, Job{Type: "gatepanics.healthy"}); err != nil {
		os.Exit(13)
	}

	// Secondary assertion: if the process lives but the only worker died
	// on the panic, the follow-up job never runs.
	select {
	case <-healthy:
		// worker survived the panicking gate and processed the next job
	case <-time.After(3 * time.Second):
		os.Exit(14) // silent worker death: follow-up job never ran
	}

	// Alive AND working past the poison job: the gate panic was contained.
	_ = q.Close()
	os.Exit(0)
}

// ============================================================================
// Pins Nack consuming the in-flight job when its re-enqueue fails, found by
// the 2026-09-04 red-probe round; fixed in MemoryQueue.Nack by retaining the
// job in the dead-letter set (and logging) before returning the error.
// Property: a failed re-enqueue must leave the job recoverable — never
// consumed into nowhere. The same write-to-next-home-before-drop rule
// TestReplayKeepsJobWhenEnqueueFails pins above, applied to the
// manual-consumption completion arm.
// Surfaces: memory.go MemoryQueue.Nack; siblings already correct:
// MemoryQueue.Replay (pinned above), RedisQueue.Nack
// (TestRedisNackKeepsJobWhenPushFails), MemoryQueue.processJob's own retry
// path ("queue: job dead-lettered" on enqErr).
// ============================================================================

// TestMemoryNackKeepsJobWhenEnqueueFails: a Nack whose re-enqueue fails
// (cancelled caller context) must leave the job inspectable in the
// dead-letter set, the same lossless rule Replay and the Redis backend
// already follow.
func TestMemoryNackKeepsJobWhenEnqueueFails(t *testing.T) {
	q := NewMemoryQueue(0)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := q.Enqueue(ctx, Job{ID: "n1", Type: "work", MaxAttempts: 3}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	job, err := q.Dequeue(ctx)
	if err != nil {
		t.Fatalf("dequeue: %v", err)
	}
	if job.ID != "n1" {
		t.Fatalf("setup: dequeued %q", job.ID)
	}

	// The caller's context dies between Dequeue and Nack (request
	// disconnect, deadline). Nack must not destroy the job with it.
	cancel()
	nackErr := q.Nack(ctx, job)

	dead, err := q.ListJobs(context.Background(), "failed", 10)
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	found := false
	for _, d := range dead {
		if d.ID == "n1" {
			found = true
		}
	}
	if !found {
		t.Fatalf("SECURITY: [queue] Nack consumed the in-flight job and lost it when its re-enqueue failed "+
			"(nack err=%v): the job is in no store — not pending, not dead, not in-flight — and a retried Nack is "+
			"the documented no-op; the same lossless ordering Replay, RedisQueue.Nack, and processJob's own retry "+
			"path already implement was skipped here", nackErr)
	}
}
