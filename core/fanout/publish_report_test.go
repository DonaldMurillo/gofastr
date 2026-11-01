package fanout

import (
	"errors"
	"testing"
	"time"
)

// PublishQueue logged a failed publish at Debug and dropped an overflowing
// payload, and told its caller about neither, so a cross-replica rtc
// signal could vanish while both peers waited on it (2026-10-27 bug bash
// R19). A payload sent with a failed callback reports either outcome.
func TestPublishQueueReportsFailure(t *testing.T) {
	f := &errFanout{}
	f.calls.Add(1)
	send, stop := PublishQueueReporting(f, "t", 8)
	defer stop()
	got := make(chan error, 1)
	send([]byte("x"), func(err error) { got <- err })
	select {
	case err := <-got:
		if err == nil || err.Error() != "boom" {
			t.Fatalf("failed callback got %v, want the backend's error", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("a failed publish was not reported")
	}
}

func TestPublishQueueReportsOverflow(t *testing.T) {
	f := newStalledFanout() // Publish blocks: the queue fills
	send, stop := PublishQueueReporting(f, "t", 2)
	defer func() {
		close(f.release)
		stop()
	}()
	got := make(chan error, 8)
	report := func(err error) { got <- err }
	for range 5 {
		send([]byte("x"), report)
	}
	select {
	case err := <-got:
		if !errors.Is(err, ErrPublishDropped) {
			t.Fatalf("overflow reported %v, want ErrPublishDropped", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("an overflow drop was not reported")
	}
}

// A published payload does not call its callback, and a panicking
// callback does not take the drainer down.
func TestPublishQueueQuietOnSuccess(t *testing.T) {
	f := newStalledFanout()
	close(f.release)
	send, stop := PublishQueueReporting(f, "t", 8)
	defer stop()
	called := make(chan struct{}, 1)
	send([]byte("a"), func(error) { called <- struct{}{} })
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		f.mu.Lock()
		n := len(f.got)
		f.mu.Unlock()
		if n == 1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	select {
	case <-called:
		t.Fatal("callback ran for a published payload")
	case <-time.After(50 * time.Millisecond):
	}
}

func TestPublishQueueCallbackPanicContained(t *testing.T) {
	f := &errFanout{}
	f.calls.Add(2)
	send, stop := PublishQueueReporting(f, "t", 8)
	defer stop()
	send([]byte("1"), func(error) { panic("host bug") })
	got := make(chan error, 1)
	send([]byte("2"), func(err error) { got <- err })
	select {
	case <-got:
	case <-time.After(2 * time.Second):
		t.Fatal("the drainer stopped after a callback panicked")
	}
}
