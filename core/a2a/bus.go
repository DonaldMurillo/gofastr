package a2a

import (
	"log/slog"
	"sync"
)

const subscriberBuffer = 64

type busEvent struct {
	ev      StreamResponse
	version int64
}

type taskBus struct {
	mu   sync.Mutex
	subs map[chan busEvent]struct{}
	log  *slog.Logger
}

func newTaskBus(log *slog.Logger) *taskBus {
	return &taskBus{subs: map[chan busEvent]struct{}{}, log: log}
}

func (b *taskBus) subscribe() chan busEvent {
	ch := make(chan busEvent, subscriberBuffer)
	b.mu.Lock()
	defer b.mu.Unlock()
	b.subs[ch] = struct{}{}
	return ch
}

func (b *taskBus) unsubscribe(ch chan busEvent) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.subs, ch)
}

func (b *taskBus) publish(ev busEvent) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for ch := range b.subs {
		select {
		case ch <- ev:
		default:
			delete(b.subs, ch)
			close(ch)
			b.log.Warn("a2a: event subscriber too slow, dropped", "taskId", taskIDOf(ev.ev))
		}
	}
}

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

func endsStream(ev StreamResponse) bool {
	if ev.StatusUpdate == nil {
		return false
	}
	st := ev.StatusUpdate.Status.State
	return st.Terminal() || st.Interrupted()
}
