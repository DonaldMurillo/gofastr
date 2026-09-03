//go:build red

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

// ---------------------------------------------------------------------------
// Property: a panic in the SetGate gate function is isolated to the job; the
// worker pool (and the process) survives it.
// Surfaces: MemoryQueue.worker (memory.go:235-244) → processJob gate check
// (memory.go:313-319: `if gate != nil && !gate(job.Type)`), invoked inline in
// the worker loop with NO recover.
// Finding: handlers are panic-isolated (safeHandle, memory.go:361-371) but
// the gate callback is not: it runs bare between waitAndPop and safeHandle,
// so a gate that panics unwinds the worker goroutine — and an unrecovered
// panic in any goroutine kills the whole process. The DBQueue twin IS
// guarded (db.go:931-963: superviseWorker respawns, runWorker recovers,
// "guaranteeing the pool size is preserved across poison-message panics");
// the asymmetry is the evidence that the memory side is an oversight, not a
// design choice. A gate is host-supplied callback code exactly like a
// handler, and SetGate's doc offers no panic contract, so a buggy gate takes
// down every in-flight request of the process, not just its own job.
// Severity: production-facing (worker availability / process liveness).
// Fix direction: mirror the handler treatment — either wrap the gate call so
// a panic becomes a defer (treat the job as gated: re-enqueue after
// gateDeferDelay) or dead-letters it, and/or recover at the worker() loop
// boundary like DBQueue.runWorker so the pool size is preserved. The child
// must survive AND process the follow-up job; today the gate panic kills the
// child process (the parent observes the crash — the red failing assertion).
// Proven by re-exec: pattern core/fanout subscriber_queue_security_test.go,
// same as framework/cron/cron_red_test.go.
// ---------------------------------------------------------------------------

const queueGatePanicChildEnv = "GOFASTR_TEST_QUEUE_GATE_PANIC_CHILD"

func TestMemoryQueueRedGatePanicIsolated(t *testing.T) {
	if os.Getenv(queueGatePanicChildEnv) == "1" {
		queueGatePanicChild()
		return // unreachable; queueGatePanicChild exits
	}

	cmd := exec.Command(os.Args[0],
		"-test.run", "^TestMemoryQueueRedGatePanicIsolated$",
		"-test.count=1")
	cmd.Env = append(os.Environ(), queueGatePanicChildEnv+"=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() >= 10 {
			t.Fatalf("gate-panic child scenario contract broken (exit %d):\n%s", exitErr.ExitCode(), out)
		}
		t.Errorf("SECURITY: [gate-panic-isolation] a panicking SetGate callback killed the process: the worker did not survive it (memory.go:313 gate runs bare in worker(), unlike safeHandle for handlers and DBQueue.runWorker's recover at db.go:955): %v\n--- child output ---\n%s", err, out)
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
// process (the finding).
func queueGatePanicChild() {
	q := NewMemoryQueue(1)

	healthy := make(chan struct{})
	q.RegisterHandler("red.healthy", func(context.Context, Job) error {
		close(healthy)
		return nil
	})
	// The gate must run for the poison type, so a handler has to exist
	// (processJob dead-letters unknown types before reaching the gate).
	q.RegisterHandler("red.gate.panics", func(context.Context, Job) error {
		return nil
	})
	q.SetGate(func(jobType string) bool {
		if jobType == "red.gate.panics" {
			panic("gate boom")
		}
		return true
	})
	q.Start()

	ctx := context.Background()
	// Poison first: equal priority is FIFO by enqueue order, so the single
	// worker hits the panicking gate before the healthy job.
	if err := q.Enqueue(ctx, Job{Type: "red.gate.panics"}); err != nil {
		os.Exit(12)
	}
	if err := q.Enqueue(ctx, Job{Type: "red.healthy"}); err != nil {
		os.Exit(13)
	}

	// Secondary assertion, survives the reshape: if the process lives but
	// the only worker died on the panic, the follow-up job never runs.
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
