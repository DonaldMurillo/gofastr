package outbox

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/DonaldMurillo/gofastr/framework/event"
)

// Consume registers a durable consumer. Must be called before StartRelay.
// name is a stable identity used to track per-consumer delivery across
// restarts/replicas; (eventType, name) must be unique, registering the
// same pair twice panics. handler is invoked with Event.ID set to the
// outbox row id. Note Event.ID is shared across ALL consumers of that row,
// so a handler that deduplicates must key on (name, Event.ID), never
// Event.ID alone, or one consumer's success would suppress a sibling's
// delivery. A consumer declared
// after events have already been staged sees only events staged from its
// registration forward (the relay expands deliveries for pending rows each
// pump, so it catches up on still-pending rows immediately and misses only
// rows already dispatched before it was declared).
//
// The handler is invoked by the relay with a wall-clock budget
// ([WithHandlerTimeout], 30s by default): it should be side-effecting and
// idempotent (at-least-once delivery means a duplicate is always possible
// after a crash, lease expiry, or a handler that outran its budget). The
// handler's context is cancelled at the deadline; a handler that respects
// its context simply returns and the delivery is retried as usual, while
// one that blocks past the deadline has its delivery settled as failed so
// retries and sibling consumers proceed. A handler that returns an error
// or panics is retried with exponential backoff and eventually
// dead-lettered, independently of its sibling consumers for the same
// event (sibling isolation).
//
// Re-adding a previously-removed consumer resumes delivery for events
// staged from the re-add forward automatically. Events whose deliveries
// were abandoned during the removal gap are NOT re-delivered automatically
// (a terminal abandoned row blocks re-expansion); recover those explicitly
// with [Outbox.ReplayConsumer].
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

// lookupHandler returns the handler registered for (eventType, name), or
// ok=false when no consumer with that identity is declared on this replica
// (e.g. declared on another replica mid rolling-deploy). The relay leaves
// such a delivery pending with a short backoff rather than dead-lettering.
func (o *Outbox) lookupHandler(eventType, name string) (event.EventHandler, bool) {
	o.consumerMu.RLock()
	defer o.consumerMu.RUnlock()
	h, ok := o.consumers[eventType][name]
	return h, ok
}

// hasConsumers reports whether any consumer at all is declared. Used at
// relay start to warn loudly when the durable lane is a no-op (staged
// events would otherwise be silently dropped by the orphan sweep).
func (o *Outbox) hasConsumers() bool {
	o.consumerMu.RLock()
	defer o.consumerMu.RUnlock()
	return len(o.consumers) > 0
}

// declaredSnapshot returns a stable, lock-held copy of the declared
// consumers as (eventType, name, handler) triples. The relay reads this
// once per pump to drive delivery expansion and the retire sweep; handlers
// are immutable after registration (Consume panics on duplicates and must
// precede StartRelay), so the snapshot is safe to use outside the lock.
func (o *Outbox) declaredSnapshot() []consumerEntry {
	o.consumerMu.RLock()
	defer o.consumerMu.RUnlock()
	out := make([]consumerEntry, 0, 8)
	// Iterate event types deterministically is unnecessary for correctness
	// (each consumer is independent), but stable order aids log readability.
	for eventType, byName := range o.consumers {
		for name, h := range byName {
			out = append(out, consumerEntry{eventType: eventType, name: name, handler: h})
		}
	}
	return out
}

// declaredPairs is declaredSnapshot ordered by event type, then consumer
// name, so the SQL built from it is the same on every call.
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

// declaredFor returns the names of the consumers declared here for
// eventType, sorted.
//
// consumerCoverage: a parent completes only once each of them holds a
// delivery. Expansion creates each consumer's deliveries in its own
// batch-limited INSERT, so two relays, or a consumer catching up on a
// backlog, leave a parent holding one consumer's delivery before
// another's exists; completing it then (no pending delivery, older than
// the grace) let expansion, which visits pending parents only, skip it
// for the lagging consumer for good. The check reads this relay's
// declared set, so it only ever delays completion: a replica that lacks a
// consumer mid rolling deploy completes as before, under the grace.
func (o *Outbox) declaredFor(eventType string) []string {
	o.consumerMu.RLock()
	defer o.consumerMu.RUnlock()
	return slices.Sorted(maps.Keys(o.consumers[eventType]))
}

// consumerEntry is one declared durable consumer.
type consumerEntry struct {
	eventType string
	name      string
	handler   event.EventHandler
}
