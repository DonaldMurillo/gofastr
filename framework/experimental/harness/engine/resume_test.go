package engine

import (
	"context"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/control"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/ids"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/provider"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/tool"
)

// sliceLog is an EventLog over a fixed list.
type sliceLog []control.EventEnvelope

func (l sliceLog) EventsSince(_ context.Context, _ ids.SessionID, since uint64, limit int) ([]control.EventEnvelope, error) {
	var out []control.EventEnvelope
	for _, e := range l {
		if e.ID > since && (limit <= 0 || len(out) < limit) {
			out = append(out, e)
		}
	}
	return out, nil
}

func publishN(t *testing.T, b *Bus, n int) []control.EventEnvelope {
	t.Helper()
	var out []control.EventEnvelope
	for range n {
		env, err := b.Publish(control.TextDelta{Text: "x"}, "")
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, env)
	}
	return out
}

// next reads one envelope or fails after a second.
func next(t *testing.T, ch <-chan control.EventEnvelope) control.EventEnvelope {
	t.Helper()
	select {
	case env, ok := <-ch:
		if !ok {
			t.Fatal("stream closed")
		}
		return env
	case <-time.After(time.Second):
		t.Fatal("no event")
	}
	return control.EventEnvelope{}
}

// A client that reconnects with the last id it saw gets what it missed,
// then the live stream.
func TestSubscribeSinceReplaysThenLive(t *testing.T) {
	b := NewBus(ids.NewSessionID())
	defer b.Close()
	publishN(t, b, 3)
	ch := b.SubscribeSince(t.Context(), 1)
	for _, want := range []uint64{2, 3} {
		if got := next(t, ch).ID; got != want {
			t.Fatalf("replayed id %d, want %d", got, want)
		}
	}
	publishN(t, b, 1)
	if got := next(t, ch).ID; got != 4 {
		t.Fatalf("live id %d, want 4", got)
	}
}

// Events older than the in-memory window come from the session log.
func TestSubscribeSinceReadsLog(t *testing.T) {
	b := NewBus(ids.NewSessionID())
	defer b.Close()
	b.histMax = 2
	all := publishN(t, b, 5)
	b.SetLog(sliceLog(all))
	ch := b.SubscribeSince(t.Context(), 0)
	for want := uint64(1); want <= 5; want++ {
		if got := next(t, ch).ID; got != want {
			t.Fatalf("replayed id %d, want %d", got, want)
		}
	}
}

// With no log to fill the hole, the client is told what it lost.
func TestSubscribeSinceMarksGap(t *testing.T) {
	b := NewBus(ids.NewSessionID())
	defer b.Close()
	b.histMax = 2
	publishN(t, b, 5)
	ch := b.SubscribeSince(t.Context(), 0)
	first := next(t, ch)
	ev, err := control.DecodeEvent(first)
	if err != nil {
		t.Fatal(err)
	}
	gap, ok := ev.(control.StreamGap)
	if !ok || gap.From != 1 || gap.To != 3 {
		t.Fatalf("first event = %s %+v, want StreamGap 1..3", first.Kind, ev)
	}
	for _, want := range []uint64{4, 5} {
		if got := next(t, ch).ID; got != want {
			t.Fatalf("replayed id %d, want %d", got, want)
		}
	}
}

// A sub-agent's bus takes ids from its parent, so both land in one
// session log without colliding.
func TestSubBusSharesParentIDs(t *testing.T) {
	parent := NewBus(ids.NewSessionID())
	defer parent.Close()
	sub := NewSubBus(parent, "agent_1")
	defer sub.Close()
	a, _ := parent.Publish(control.TextDelta{Text: "a"}, "")
	b, _ := sub.Publish(control.TextDelta{Text: "b"}, "")
	c, _ := parent.Publish(control.TextDelta{Text: "c"}, "")
	if !(a.ID < b.ID && b.ID < c.ID) {
		t.Fatalf("ids %d %d %d, want one increasing sequence", a.ID, b.ID, c.ID)
	}
	if b.SubAgent != "agent_1" || a.SubAgent != "" {
		t.Fatalf("tags %q %q", a.SubAgent, b.SubAgent)
	}
}

// The audit subscription (the one the session log uses) sees a spawned
// agent's events, tagged with its id; ordinary subscribers still do not.
func TestSpawnEventsReachAuditSubscribers(t *testing.T) {
	prov := &recordingProvider{events: []provider.StreamEvent{
		{Kind: provider.KindTextDelta, Text: "sub-agent talking"},
		{Kind: provider.KindStop, FinishReason: "stop"},
	}}
	session := ids.NewSessionID()
	bus := NewBus(session)
	defer bus.Close()
	e := NewEngine(session, bus, prov, "m", NewDispatcher(bus, tool.NewRegistry()))
	audit := bus.SubscribeAll(t.Context())
	if _, err := e.Spawn(context.Background(), "", "do x"); err != nil {
		t.Fatal(err)
	}
	deadline := time.After(time.Second)
	for {
		select {
		case env := <-audit:
			if env.Kind == "TextDelta" {
				if env.SubAgent == "" {
					t.Fatal("sub-agent TextDelta carries no sub-agent tag")
				}
				return
			}
		case <-deadline:
			t.Fatal("sub-agent events never reached the audit subscriber")
		}
	}
}

// pageCountLog counts the pages read from it.
type pageCountLog struct {
	sliceLog
	pages int
}

func (l *pageCountLog) EventsSince(ctx context.Context, s ids.SessionID, since uint64, limit int) ([]control.EventEnvelope, error) {
	l.pages++
	return l.sliceLog.EventsSince(ctx, s, since, limit)
}

// A Backlog read with a limit stops paging the log once it has enough:
// a since=0 read of a long session must not load the whole log.
func TestBacklogStopsPagingAtLimit(t *testing.T) {
	b := NewBus(ids.NewSessionID())
	defer b.Close()
	b.histMax = 2
	log := &pageCountLog{sliceLog: sliceLog(publishN(t, b, 3*logPage+5))}
	b.SetLog(log)
	got := b.Backlog(t.Context(), 0, 10)
	if len(got) != 10 || got[0].ID != 1 || got[9].ID != 10 {
		t.Fatalf("backlog = %d events from %d, want ids 1..10", len(got), got[0].ID)
	}
	if log.pages != 1 {
		t.Fatalf("read %d log pages for 10 events, want 1", log.pages)
	}
}

// A cancelled reader stops paging.
func TestBacklogStopsOnCancel(t *testing.T) {
	b := NewBus(ids.NewSessionID())
	defer b.Close()
	b.histMax = 2
	log := &pageCountLog{sliceLog: sliceLog(publishN(t, b, 3*logPage+5))}
	b.SetLog(log)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	b.Backlog(ctx, 0, 0)
	if log.pages > 1 {
		t.Fatalf("read %d log pages after cancel, want at most 1", log.pages)
	}
}

// The session log subscribes with SubscribeAll. A burst past its
// buffer must wait for the reader, not leave holes in the log.
func TestSubscribeAllNeverDrops(t *testing.T) {
	bus := NewBus(ids.NewSessionID())
	defer bus.Close()
	audit := bus.SubscribeAll(t.Context())
	const n = 600 // past the 256-event buffer
	go func() {
		for range n {
			_, _ = bus.Publish(control.TextDelta{Text: "x"}, "")
		}
	}()
	// Read only after the publisher has run far past the buffer: a
	// dropping bus is done publishing by now, a lossless one waits.
	time.Sleep(50 * time.Millisecond)
	for want := uint64(1); want <= n; want++ {
		select {
		case env := <-audit:
			if env.ID != want {
				t.Fatalf("got id %d, want %d", env.ID, want)
			}
		case <-time.After(time.Second):
			t.Fatalf("stalled at id %d: the bus dropped the rest", want)
		}
	}
}

// A reader that stops must not wedge Close: the publish waiting on it
// gives up once the bus starts closing.
func TestCloseFreesBlockedPublish(t *testing.T) {
	bus := NewBus(ids.NewSessionID())
	_ = bus.SubscribeAll(t.Context()) // never read
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range 300 {
			_, _ = bus.Publish(control.TextDelta{Text: "x"}, "")
		}
	}()
	time.Sleep(50 * time.Millisecond) // let the publisher fill the buffer and wait
	closed := make(chan struct{})
	go func() { bus.Close(); close(closed) }()
	for _, ch := range []chan struct{}{closed, done} {
		select {
		case <-ch:
		case <-time.After(3 * time.Second):
			t.Fatal("Close or the blocked publisher never returned")
		}
	}
}
