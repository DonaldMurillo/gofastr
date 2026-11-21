package access

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/fanout"
)

type attachmentTestFanout struct {
	receive func([]byte)
	stops   atomic.Int32
}

func (*attachmentTestFanout) Publish(context.Context, string, []byte) error { return nil }

func (f *attachmentTestFanout) Subscribe(_ string, receive func([]byte)) (func(), error) {
	f.receive = receive
	return func() { f.stops.Add(1) }, nil
}

func TestGrantStoreReplacesRunningFanout(t *testing.T) {
	s := NewGrantStore(nil, nil, WithReconcileInterval(time.Microsecond))
	first, second := &attachmentTestFanout{}, &attachmentTestFanout{}
	stopFirst, err := s.SetFanout(first)
	if err != nil {
		t.Fatal(err)
	}
	payload := fanout.Wrap("remote-node", []byte(`{"role":"reader"}`))
	delivering := make(chan struct{})
	finished := make(chan struct{})
	var deliveries sync.WaitGroup
	deliveries.Go(func() {
		first.receive(payload)
		close(delivering)
		for {
			select {
			case <-finished:
				return
			default:
				first.receive(payload)
			}
		}
	})
	<-delivering
	stopSecond, err := s.SetFanout(second)
	close(finished)
	deliveries.Wait()
	if err != nil {
		stopFirst()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		var stops sync.WaitGroup
		stops.Go(stopFirst)
		stops.Go(stopSecond)
		stops.Wait()
	})
	if got := first.stops.Load(); got != 1 {
		t.Fatalf("first attachment canceled %d times after replacement, want 1", got)
	}
	stopFirst()
	if got := second.stops.Load(); got != 0 {
		t.Fatalf("stale stop canceled the second attachment %d times", got)
	}
	if got := first.stops.Load(); got != 1 {
		t.Fatalf("stale stop canceled the first attachment %d times, want 1 total", got)
	}
}
