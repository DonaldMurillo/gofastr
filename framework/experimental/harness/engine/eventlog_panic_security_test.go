package engine

import (
	"context"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/control"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/ids"
)

// panicLog is an EventLog whose read panics.
type panicLog struct{}

func (panicLog) EventsSince(context.Context, ids.SessionID, uint64, int) ([]control.EventEnvelope, error) {
	panic("session store exploded")
}

// TestSubscribeSinceLogPanicContained: the EventLog SetLog installs is a
// session store, and SubscribeSince reads it on its own goroutine, which
// no per-request recover net reaches. A panicking store used to kill the
// process. The unreadable range is now reported as a StreamGap and the
// replay goes on from the window.
func TestSubscribeSinceLogPanicContained(t *testing.T) {
	b := NewBus(ids.NewSessionID())
	defer b.Close()
	b.histMax = 2
	publishN(t, b, 5)
	b.SetLog(panicLog{})
	ch := b.SubscribeSince(t.Context(), 0)
	ev, err := control.DecodeEvent(next(t, ch))
	if err != nil {
		t.Fatal(err)
	}
	if gap, ok := ev.(control.StreamGap); !ok || gap.From != 1 || gap.To != 3 {
		t.Fatalf("first event = %+v, want StreamGap 1..3", ev)
	}
	for _, want := range []uint64{4, 5} {
		if got := next(t, ch).ID; got != want {
			t.Fatalf("replayed id %d, want %d", got, want)
		}
	}
}
