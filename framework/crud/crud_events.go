package crud

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/gofastr/gofastr/core/stream"
	"github.com/gofastr/gofastr/framework/event"
	"github.com/gofastr/gofastr/framework/tenant"
)

// eventPayloadEntity is the map key under which CRUD events stamp the entity
// name; SSE subscribers filter on this. Stable string for client-side parsers.
const (
	eventKeyEntity   = "entity"
	eventKeyTable    = "table"
	eventKeyRecord   = "record"
	eventKeyTenantID = "tenantId"
)

// EmitEvent fires an entity lifecycle event (asynchronously). No-op when the
// handler has no event bus attached. The payload is shaped so SSE subscribers
// can filter by entity name and tenant without unmarshalling the record.
func (ch *CrudHandler) EmitEvent(ctx context.Context, eventType string, record any) {
	if ch.Events == nil {
		return
	}
	data := map[string]any{
		eventKeyEntity: ch.Entity.GetName(),
		eventKeyTable:  ch.Entity.GetTable(),
		eventKeyRecord: record,
	}
	if ch.Entity.Config.MultiTenant {
		if tid := tenant.GetTenantID(ctx); tid != "" {
			data[eventKeyTenantID] = tid
		}
	}
	ch.Events.EmitAsync(ctx, event.Event{Type: eventType, Data: data})
}

// EventStream returns an http.HandlerFunc that serves a Server-Sent Events
// stream of EntityCreated / EntityUpdated / EntityDeleted events scoped to
// this entity. When the entity is multi-tenant, events are further filtered
// to the tenant ID extracted from the request context.
//
// Each accepted event is written as:
//
//	event: entity.created (or entity.updated / entity.deleted)
//	data:  {<full Event JSON>}
//
// Disconnects from the client unsubscribe automatically. A backpressure
// buffer of 32 is enforced — if the client cannot keep up, events are
// dropped rather than blocking emitters.
func (ch *CrudHandler) EventStream() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if ch.Events == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "event bus not configured")
			return
		}

		sse := stream.NewSSEWriter(w)
		sse.WriteComment("subscribed " + ch.Entity.GetName())

		entityName := ch.Entity.GetName()
		tenantScope := ch.Entity.Config.MultiTenant
		tenantID := tenant.GetTenantID(r.Context())

		buf := make(chan event.Event, 32)

		filter := func(_ context.Context, event event.Event) error {
			data, ok := event.Data.(map[string]any)
			if !ok {
				return nil
			}
			if data[eventKeyEntity] != entityName {
				return nil
			}
			if tenantScope && tenantID != "" && data[eventKeyTenantID] != tenantID {
				return nil
			}
			select {
			case buf <- event:
			default:
				// Slow client; drop rather than block emitters.
			}
			return nil
		}

		cancels := []func(){
			ch.Events.Subscribe(event.EntityCreated, filter),
			ch.Events.Subscribe(event.EntityUpdated, filter),
			ch.Events.Subscribe(event.EntityDeleted, filter),
		}
		defer func() {
			for _, c := range cancels {
				c()
			}
		}()

		for {
			select {
			case <-r.Context().Done():
				return
			case ev := <-buf:
				payload, err := json.Marshal(ev)
				if err != nil {
					continue
				}
				if err := sse.WriteEvent(ev.Type, string(payload)); err != nil {
					return
				}
			}
		}
	}
}
