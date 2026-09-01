package a2a

import (
	"log/slog"
	"sync"
)

// subscriberBuffer is the per-subscriber channel capacity. Events are
// small structs; 64 covers a burst of status and artifact updates
// without the run ever blocking on a reader.
const subscriberBuffer = 64

// taskBus fans one task's events out to this process's subscribers. One
// bus exists per run, not per server: a task running here owns its bus,
// and subscribers reach it through the run registry, so two runs of the
// same task (create, then a later resume) never share a channel set.
type taskBus struct {
	mu   sync.Mutex
	subs map[chan StreamResponse]struct{}
	log  *slog.Logger
}

func newTaskBus(log *slog.Logger) *taskBus {
	return &taskBus{subs: map[chan StreamResponse]struct{}{}, log: log}
}

// subscribe returns a buffered channel receiving every event published
// after this call.
func (b *taskBus) subscribe() chan StreamResponse {
	ch := make(chan StreamResponse, subscriberBuffer)
	b.mu.Lock()
	defer b.mu.Unlock()
	b.subs[ch] = struct{}{}
	return ch
}

// unsubscribe removes ch. Safe to call twice: a dropped (closed)
// channel is already absent from the set.
func (b *taskBus) unsubscribe(ch chan StreamResponse) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.subs, ch)
}

// publish delivers ev to every subscriber without ever blocking the
// run. A subscriber whose buffer is full is closed and dropped instead:
// one slow SSE client must not stall the skill handler and every other
// subscriber. Publishing after a drop is a no-op for that subscriber
// (it left the set before it was closed), so a channel is never sent on
// or closed twice. This is the whole reason the bus is per task — a
// slow reader harms only its own task's stream.
func (b *taskBus) publish(ev StreamResponse) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for ch := range b.subs {
		select {
		case ch <- ev:
		default:
			// Dropped under the lock: only publish closes channels, and
			// the delete above guarantees this one is never selected
			// again.
			delete(b.subs, ch)
			close(ch)
			b.log.Warn("a2a: event subscriber too slow, dropped", "taskId", taskIDOf(ev))
		}
	}
}

// taskIDOf digs the task id out of whichever arm a StreamResponse
// carries, for log lines.
func taskIDOf(ev StreamResponse) string {
	switch {
	case ev.Task != nil:
		return ev.Task.ID
	case ev.StatusUpdate != nil:
		return ev.StatusUpdate.TaskID
	case ev.ArtifactUpdate != nil:
		return ev.ArtifactUpdate.TaskID
	case ev.Message != nil:
		return ev.Message.TaskID
	}
	return ""
}

// endsStream reports whether an event settles the task for streaming
// purposes: a terminal state closes the stream for good, an interrupted
// state closes it until the client resumes with a new message.
func endsStream(ev StreamResponse) bool {
	if ev.StatusUpdate == nil {
		return false
	}
	st := ev.StatusUpdate.Status.State
	return st.Terminal() || st.Interrupted()
}
