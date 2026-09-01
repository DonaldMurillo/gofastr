package db

import (
	"context"
	"testing"
)

func TestCommitQueueDrainsInAddOrder(t *testing.T) {
	var q CommitQueue
	var got []int
	for i := 0; i < 3; i++ {
		i := i
		q.Add(func() { got = append(got, i) })
	}
	if len(got) != 0 {
		t.Fatalf("queued work ran before drain: %v", got)
	}
	q.RunAfterCommit()
	if len(got) != 3 || got[0] != 0 || got[1] != 1 || got[2] != 2 {
		t.Fatalf("drain ran %v, want [0 1 2] in Add order", got)
	}
}

func TestCommitQueueSecondDrainIsANoOp(t *testing.T) {
	var q CommitQueue
	runs := 0
	q.Add(func() { runs++ })
	q.RunAfterCommit()
	q.RunAfterCommit()
	if runs != 1 {
		t.Fatalf("work ran %d times across two drains, want exactly 1", runs)
	}
}

// A late Add — the owner committed and drained while another goroutine was
// still finishing a CRUD call — must not park work on a list nothing reads
// again. The commit already succeeded, so the work runs immediately.
func TestCommitQueueLateAddRunsImmediately(t *testing.T) {
	var q CommitQueue
	q.RunAfterCommit()
	ran := false
	q.Add(func() { ran = true })
	if !ran {
		t.Fatal("Add after drain neither queued-and-ran nor ran immediately — the emission is silently lost")
	}
}

// A dropped queue (rollback path: the owner never drains) must never run
// its work — that is the phantom-event guarantee.
func TestCommitQueueUndrainedNeverRuns(t *testing.T) {
	var q CommitQueue
	ran := false
	q.Add(func() { ran = true })
	if ran {
		t.Fatal("queued work ran without a drain")
	}
}

func TestWithTxQueueRoundTrip(t *testing.T) {
	ctx, q := WithTxQueue(context.Background(), nil)
	got, ok := CommitQueueFromContext(ctx)
	if !ok || got != q {
		t.Fatalf("CommitQueueFromContext = %v, %v; want the queue WithTxQueue attached", got, ok)
	}
	if _, ok := CommitQueueFromContext(context.Background()); ok {
		t.Fatal("bare context reported a commit queue")
	}
	if _, ok := TxFromContext(ctx); !ok {
		t.Fatal("WithTxQueue did not also attach the tx")
	}
}
