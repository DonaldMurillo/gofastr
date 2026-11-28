package db

import (
	"context"
	"database/sql"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
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

// Adds racing the drain: whichever side of the transition each Add lands
// on, its callback must run exactly once — queued-and-drained or run
// immediately, never lost, never doubled. Meaningful under -race.
func TestCommitQueueConcurrentAddsRunExactlyOnce(t *testing.T) {
	const adders = 16
	var q CommitQueue
	var runs [adders]atomic.Int32

	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < adders; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			q.Add(func() { runs[i].Add(1) })
		}(i)
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-start
		q.RunAfterCommit()
	}()
	close(start)
	wg.Wait()
	// Callbacks queued before the drain ran inside RunAfterCommit; any
	// that arrived after ran inline in their Add. Either way: exactly one
	// run each, once all goroutines have joined.
	q.RunAfterCommit() // second drain is a no-op; must not re-run anything
	for i := 0; i < adders; i++ {
		if n := runs[i].Load(); n != 1 {
			t.Fatalf("callback %d ran %d times, want exactly 1", i, n)
		}
	}
}

func TestWithoutTxMasksTxAndQueue(t *testing.T) {
	type otherKey struct{}
	base := context.WithValue(context.Background(), otherKey{}, "kept")
	ctx, _ := WithTxQueue(base, nil)

	masked := WithoutTx(ctx)
	if _, ok := TxFromContext(masked); ok {
		t.Fatal("WithoutTx left the transaction visible")
	}
	if _, ok := CommitQueueFromContext(masked); ok {
		t.Fatal("WithoutTx left the commit queue visible")
	}
	if masked.Value(otherKey{}) != "kept" {
		t.Fatal("WithoutTx dropped an unrelated context value")
	}
}

func TestWithTxQueueRoundTrip(t *testing.T) {
	tx := new(sql.Tx)
	ctx, q := WithTxQueue(context.Background(), tx)
	got, ok := CommitQueueFromContext(ctx)
	if !ok || got != q {
		t.Fatalf("CommitQueueFromContext = %v, %v; want the queue WithTxQueue attached", got, ok)
	}
	if _, ok := CommitQueueFromContext(context.Background()); ok {
		t.Fatal("bare context reported a commit queue")
	}
	if got, ok := TxFromContext(ctx); !ok || got != tx {
		t.Fatal("WithTxQueue did not also attach the tx")
	}
}

func TestWithTxDoesNotReuseQueueForAnotherTransaction(t *testing.T) {
	outer := new(sql.Tx)
	ctx, queue := WithTxQueue(context.Background(), outer)
	if inherited, ok := CommitQueueFromContext(WithTx(ctx, outer)); !ok || inherited != queue {
		t.Fatal("the same transaction lost its queue")
	}

	inner := new(sql.Tx)
	ctx = WithTx(ctx, inner)
	if got, ok := TxFromContext(ctx); !ok || got != inner {
		t.Fatal("the replacement transaction was not attached")
	}
	if _, ok := CommitQueueFromContext(ctx); ok {
		t.Fatal("the replacement transaction inherited the outer commit queue")
	}
}

func TestInSavepointAttachesExplicitTransaction(t *testing.T) {
	conn, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	mock.ExpectBegin()
	tx, err := conn.Begin()
	if err != nil {
		t.Fatal(err)
	}
	mock.ExpectExec(`^SAVEPOINT gofastr_sp_[0-9]+$`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(`^RELEASE SAVEPOINT gofastr_sp_[0-9]+$`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectRollback()
	err = InSavepoint(context.Background(), tx, func(ctx context.Context, arg *sql.Tx) error {
		if got, ok := TxFromContext(ctx); !ok || got != arg {
			t.Error("the savepoint callback context does not carry its transaction")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestWithNilTxMasksAmbientTransaction(t *testing.T) {
	ctx, _ := WithTxQueue(t.Context(), new(sql.Tx))
	ctx = WithTx(ctx, nil)
	if tx, ok := TxFromContext(ctx); ok || tx != nil {
		t.Fatalf("nil transaction reported present: %v, %v", tx, ok)
	}
}
