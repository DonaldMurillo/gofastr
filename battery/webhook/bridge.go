package webhook

import (
	"context"
	"encoding/json"

	"github.com/DonaldMurillo/gofastr/framework/event"
)

// BridgeOption configures the auto-bridge from a framework event bus
// to a webhook Manager.
type BridgeOption func(*bridgeOpts)

type bridgeOpts struct {
	onMarshalError func(eventType string, err error)
	onPublishError func(eventType string, err error)
	redact         func(eventType string, ev event.Event) event.Event
}

// WithBridgeRedactor installs a transform applied to each event before it is
// marshalled and POSTed to subscriber URLs.
//
// This bridge is an OUTBOUND delivery to third-party endpoints, and the event
// record is the row the write produced, stored values. The framework's own
// read redaction (an AfterGet hook) applies to responses and to the SSE
// stream, but this bridge has no handler to run it through, so by default a
// masked field leaves the server here in full.
//
// A field that must never leave the server at all belongs in a Hidden
// column: those are stripped in the SQL projection, so they never reach an
// event in the first place. Use this option for fields that are masked
// rather than hidden:
//
//	webhook.BridgeWithOptions(bus, mgr, nil,
//	    webhook.WithBridgeRedactor(func(_ string, ev event.Event) event.Event {
//	        return maskCardNumber(ev)
//	    }))
//
// The transform must not mutate ev in place, other subscribers hold the
// same event. Return a copy.
func WithBridgeRedactor(fn func(eventType string, ev event.Event) event.Event) BridgeOption {
	return func(o *bridgeOpts) { o.redact = fn }
}

// WithBridgeMarshalError installs a callback invoked when json.Marshal
// fails on an event payload. The default is a silent drop (so a buggy
// emitter cannot take the bus down); supply a callback to surface the
// failure to logs, metrics, or an alerting hook.
func WithBridgeMarshalError(fn func(eventType string, err error)) BridgeOption {
	return func(o *bridgeOpts) { o.onMarshalError = fn }
}

// WithBridgePublishError installs a callback invoked when Manager.Publish
// itself returns an error (typically a store write failure). Same
// default-silent rationale as marshal error.
func WithBridgePublishError(fn func(eventType string, err error)) BridgeOption {
	return func(o *bridgeOpts) { o.onPublishError = fn }
}

// Bridge subscribes the Manager to every named event on the bus so
// that an Emit/EmitAsync automatically fans out to matching webhook
// subscribers.
//
// The returned cancel function detaches every subscription at once,
// call it from your shutdown path before stopping the Manager so no
// in-flight Emit lands after the workers have exited.
//
// If events is empty (the variadic), the bridge defaults to the three
// entity lifecycle events (entity.created / entity.updated /
// entity.deleted). Pass the explicit list when your app emits a custom
// taxonomy.
//
// The event's Data field is marshalled to JSON for the webhook
// payload. Marshal and publish failures are silently dropped by
// default; install callbacks via [WithBridgeMarshalError] /
// [WithBridgePublishError] to observe them.
//
// An event a fanout re-emits from another replica ([event.IsRemote]) is
// skipped: the replica that emitted it queues the deliveries, so every
// replica that emits must run the bridge over one shared Store.
func Bridge(bus *event.EventBus, mgr *Manager, events ...string) (cancel func()) {
	return BridgeWithOptions(bus, mgr, events)
}

// BridgeWithOptions is the option-aware variant of Bridge. The events
// list slot is explicit (use nil/empty for the lifecycle default) so
// the trailing variadic stays available for BridgeOption.
func BridgeWithOptions(bus *event.EventBus, mgr *Manager, events []string, opts ...BridgeOption) (cancel func()) {
	if len(events) == 0 {
		events = []string{
			event.EntityCreated,
			event.EntityUpdated,
			event.EntityDeleted,
		}
	}
	cfg := bridgeOpts{}
	for _, o := range opts {
		o(&cfg)
	}
	cancels := make([]func(), 0, len(events))
	for _, et := range events {
		eventType := et
		c := bus.Subscribe(eventType, func(ctx context.Context, e event.Event) error {
			// The replica that emitted the event queues its deliveries
			// into the shared store; a fanned-out copy queuing again
			// would send the endpoint one delivery per replica.
			if event.IsRemote(ctx) {
				return nil
			}
			// Read before the redactor runs: the owner tags the stored
			// delivery for erasure whatever the payload keeps.
			owner := eventOwner(e)
			if cfg.redact != nil {
				e = cfg.redact(eventType, e)
			}
			payload, err := json.Marshal(e)
			if err != nil {
				if cfg.onMarshalError != nil {
					cfg.onMarshalError(eventType, err)
				}
				return nil
			}
			if _, err := mgr.PublishFor(ctx, owner, eventType, payload); err != nil {
				if cfg.onPublishError != nil {
					cfg.onPublishError(eventType, err)
				}
			}
			return nil
		})
		cancels = append(cancels, c)
	}
	return func() {
		for _, c := range cancels {
			c()
		}
	}
}

// eventOwner is the record owner a CRUD event names under
// event.DataKeyOwnerID, or "" for an event that names none.
func eventOwner(e event.Event) string {
	data, ok := e.Data.(map[string]any)
	if !ok {
		return ""
	}
	owner, _ := data[event.DataKeyOwnerID].(string)
	return owner
}
