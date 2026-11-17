package outbox

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/DonaldMurillo/gofastr/framework/event"
)

// Consume declares a durable consumer of eventType before StartRelay. It
// panics on an empty name or type, a nil handler, or a (name, eventType) pair
// already declared.
//
// Event.ID is the row ID and is the same for every consumer of the row, so a
// handler that dedupes keys on (name, Event.ID). Delivery is at least once: a
// handler that errors, panics or outruns [WithHandlerTimeout] is retried with
// backoff and then dead-lettered, apart from its siblings. A consumer receives
// rows still pending when it is declared; rows abandoned while it was removed
// come back only through [Outbox.ReplayConsumer].
func (o *Outbox) Consume(name, eventType string, handler event.EventHandler) {
	if name == "" {
		panic("outbox: Consume requires a non-empty consumer name")
	}
	if eventType == "" {
		panic("outbox: Consume requires a non-empty event type")
	}
	if handler == nil {
		panic("outbox: Consume requires a non-nil handler")
	}
	o.consumerMu.Lock()
	defer o.consumerMu.Unlock()
	byType, ok := o.consumers[eventType]
	if !ok {
		byType = map[string]event.EventHandler{}
		o.consumers[eventType] = byType
	}
	if _, dup := byType[name]; dup {
		panic(fmt.Sprintf("outbox: consumer %q for event %q already registered", name, eventType))
	}
	byType[name] = handler
}

func (o *Outbox) lookupHandler(eventType, name string) (event.EventHandler, bool) {
	o.consumerMu.RLock()
	defer o.consumerMu.RUnlock()
	h, ok := o.consumers[eventType][name]
	return h, ok
}

func (o *Outbox) hasConsumers() bool {
	o.consumerMu.RLock()
	defer o.consumerMu.RUnlock()
	return len(o.consumers) > 0
}

func (o *Outbox) declaredSnapshot() []consumerEntry {
	o.consumerMu.RLock()
	defer o.consumerMu.RUnlock()
	out := make([]consumerEntry, 0, 8)
	for eventType, byName := range o.consumers {
		for name, h := range byName {
			out = append(out, consumerEntry{eventType: eventType, name: name, handler: h})
		}
	}
	return out
}

func (o *Outbox) declaredPairs() []consumerEntry {
	out := o.declaredSnapshot()
	slices.SortFunc(out, func(a, b consumerEntry) int {
		if c := strings.Compare(a.eventType, b.eventType); c != 0 {
			return c
		}
		return strings.Compare(a.name, b.name)
	})
	return out
}

func (o *Outbox) declaredFor(eventType string) []string {
	o.consumerMu.RLock()
	defer o.consumerMu.RUnlock()
	return slices.Sorted(maps.Keys(o.consumers[eventType]))
}

type consumerEntry struct {
	eventType string
	name      string
	handler   event.EventHandler
}
