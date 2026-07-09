package event

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/DonaldMurillo/gofastr/core/fanout"
)

// fanoutTopic is the single channel every event-bus bridge publishes on and
// subscribes to. The topic is fixed (one logical event network); per-event-
// type filtering happens at the bus handler level, exactly as on a single
// process.
const fanoutTopic = "gofastr.events"

// bridgeQueueDepth bounds the bridge's publish queue. Emit/EmitStrict invoke
// the tap synchronously, so the tap must never block on a stalled backend:
// it enqueues here and a dedicated publisher goroutine drains it. Overflow
// drops the oldest queued envelope (lossy real-time lane).
const bridgeQueueDepth = 256

// bridgePublishTimeout bounds a single Publish from the publisher goroutine.
// A well-behaved backend honors the ctx and fails fast; it is defense-in-depth
// only — the tap is non-blocking regardless of backend behavior.
const bridgePublishTimeout = 5 * time.Second

// AttachFanout bridges bus to f: every locally-emitted event (via Emit,
// EmitStrict, or EmitAsync) is published to the fanout so other replicas see
// it, and events published by OTHER replicas are re-emitted on this bus.
// With a fanout attached, On/Subscribe handlers fire on EVERY replica.
//
// Delivery is lossy best-effort (the real-time lane); durable delivery is the
// outbox's job. Each event is wrapped in a node-id envelope; messages that
// originated on this bus are dropped on receive so broadcasts do not echo
// back. Remote re-emits run through EmitAsync with the tap suppressed
// (withRemoteReemit) so they are not re-published — preventing loops.
//
// The tap NEVER blocks a synchronous emitter: it enqueues to a bounded queue
// serviced by a single publisher goroutine (drop-oldest on overflow), so a
// stalled fanout backend cannot stall Emit/EmitStrict.
//
// DERIVED EVENTS: handlers that react to an event by emitting a NEW event
// must gate the derivation on [IsRemote] so it runs only on the origin
// replica — `if IsRemote(ctx) { return nil }`. Without the gate, every
// replica derives its own copy and remote replicas observe duplicates
// (the reaction handler runs on every replica, each re-deriving).
//
// Event.Data round-trips through JSON: remote handlers see map[string]any
// for object payloads (crud events already are).
//
// At most one fanout may be attached per bus; a second AttachFanout returns
// an error (detach via stop first). The returned stop detaches the tap,
// cancels the subscription, and stops the publisher goroutine; safe to call
// multiple times.
func AttachFanout(bus *EventBus, f fanout.Fanout) (stop func(), err error) {
	if f == nil {
		return nil, fmt.Errorf("event: AttachFanout: nil fanout")
	}
	bus.mu.Lock()
	if bus.fanoutAttached {
		bus.mu.Unlock()
		return nil, fmt.Errorf("event: AttachFanout: fanout already attached")
	}
	bus.fanoutAttached = true
	bus.mu.Unlock()

	nodeID := fanout.NewNodeID()

	// Marshal-failure log-once: a non-JSON Event.Data skips remote publish
	// (logging the onset once) but never breaks local emit. Touched only by
	// the single tap.
	var (
		mu               sync.Mutex
		marshalErrLogged bool
	)
	logMarshalErrOnce := func(eventType string, err error) {
		mu.Lock()
		defer mu.Unlock()
		if marshalErrLogged {
			return
		}
		marshalErrLogged = true
		slog.Default().Error(
			"event: fanout bridge skipped non-marshalable event data; remote publish disabled for this bus until restart (logging once)",
			"topic", fanoutTopic, "type", eventType, "err", err,
		)
	}

	// Publisher goroutine: the only caller of f.Publish. Drains the bounded
	// queue so a blocking backend stalls only this goroutine, never a
	// synchronous emitter (the tap's queue send is non-blocking).
	queue := make(chan []byte, bridgeQueueDepth)
	stopped := make(chan struct{})
	publisherDone := make(chan struct{})
	var overflowOnce sync.Once
	logOverflowOnce := func() {
		overflowOnce.Do(func() {
			slog.Default().Warn("event: fanout bridge publish queue overflowed; dropping oldest envelopes (lossy real-time lane)", "topic", fanoutTopic)
		})
	}
	go func() {
		defer close(publisherDone)
		for {
			select {
			case <-stopped:
				return
			case data := <-queue:
				ctx, cancel := context.WithTimeout(context.Background(), bridgePublishTimeout)
				if perr := f.Publish(ctx, fanoutTopic, data); perr != nil {
					slog.Default().Debug("event: fanout publish failed (best-effort)", "topic", fanoutTopic, "err", perr)
				}
				cancel()
			}
		}
	}()

	tap := func(ctx context.Context, ev Event) {
		if IsRemote(ctx) {
			// A remote re-emit: already came in over the fanout. Publishing
			// it back would loop. (Secondary guard behind the node-id drop.)
			return
		}
		data, err := json.Marshal(ev)
		if err != nil {
			logMarshalErrOnce(ev.Type, err)
			return
		}
		wrapped := fanout.Wrap(nodeID, data)
		select {
		case queue <- wrapped:
		default:
			// Queue full: drop oldest and retry (lossy lane). Receiving from
			// a buffered channel needs no concurrent consumer.
			select {
			case <-queue:
			default:
			}
			select {
			case queue <- wrapped:
			default:
				// Still full (concurrent producer raced the drop): drop newest.
				logOverflowOnce()
			}
		}
	}
	clearTap := bus.setTap(tap)

	cancel, subErr := f.Subscribe(fanoutTopic, func(raw []byte) {
		origin, body, uerr := fanout.Unwrap(raw)
		if uerr != nil {
			return // malformed; ignore (best-effort lane)
		}
		if origin == nodeID {
			return // own-node: drop to avoid echo
		}
		var ev Event
		if umerr := json.Unmarshal(body, &ev); umerr != nil {
			return
		}
		// Re-emit locally with the tap suppressed so this is not rebroadcast.
		bus.EmitAsync(withRemoteReemit(context.Background()), ev)
	})
	if subErr != nil {
		clearTap()
		close(stopped)
		<-publisherDone
		bus.mu.Lock()
		bus.fanoutAttached = false
		bus.mu.Unlock()
		return nil, fmt.Errorf("event: AttachFanout: subscribe %q: %w", fanoutTopic, subErr)
	}

	var once sync.Once
	return func() {
		once.Do(func() {
			cancel()
			clearTap()
			close(stopped)
			<-publisherDone
			bus.mu.Lock()
			bus.fanoutAttached = false
			bus.mu.Unlock()
		})
	}, nil
}
