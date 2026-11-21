package webhook

import (
	"context"
	"encoding/json"

	"github.com/DonaldMurillo/gofastr/framework/event"
)

// BridgeOption configures Bridge.
type BridgeOption func(*bridgeOpts)

type bridgeOpts struct {
	onMarshalError func(eventType string, err error)
	onPublishError func(eventType string, err error)
	redact         func(eventType string, ev event.Event) event.Event
}

// WithBridgeRedactor installs a transform applied to each event before it is
// sent to subscribers.
func WithBridgeRedactor(fn func(eventType string, ev event.Event) event.Event) BridgeOption {
	return func(o *bridgeOpts) { o.redact = fn }
}

// WithBridgeMarshalError installs a callback for an event payload json.Marshal
// refuses; the default drops it silently.
func WithBridgeMarshalError(fn func(eventType string, err error)) BridgeOption {
	return func(o *bridgeOpts) { o.onMarshalError = fn }
}

// WithBridgePublishError installs a callback for a Manager.Publish error; the
// default drops it silently.
func WithBridgePublishError(fn func(eventType string, err error)) BridgeOption {
	return func(o *bridgeOpts) { o.onPublishError = fn }
}

// Bridge publishes every named bus event (default: the entity lifecycle
// events) to the Manager and returns a cancel that detaches them all.
func Bridge(bus *event.EventBus, mgr *Manager, events ...string) (cancel func()) {
	return BridgeWithOptions(bus, mgr, events)
}

// BridgeWithOptions is Bridge with options; a nil events list takes the
// lifecycle default.
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
			if event.IsRemote(ctx) {
				return nil
			}
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

func eventOwner(e event.Event) string {
	data, ok := e.Data.(map[string]any)
	if !ok {
		return ""
	}
	owner, _ := data[event.DataKeyOwnerID].(string)
	return owner
}
