package crud

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/stream"
	"github.com/DonaldMurillo/gofastr/framework/event"
	"github.com/DonaldMurillo/gofastr/framework/owner"
	"github.com/DonaldMurillo/gofastr/framework/tenant"
)

// eventPayloadEntity is the map key under which CRUD events stamp the entity
// name; SSE subscribers filter on this. Stable string for client-side parsers.
const (
	eventKeyEntity   = "entity"
	eventKeyTable    = "table"
	eventKeyRecord   = "record"
	eventKeyTenantID = "tenantId"
	// eventKeyOwnerID is stamped when the entity declares OwnerField; SSE
	// filters drop events whose owner doesn't match the subscriber's
	// owner. Falls back to extracting the owner from the record itself
	// when the framework's owner extractor returned nothing (e.g. for
	// admin-side events). Without this key, an anonymous SSE subscription
	// would receive every user's row updates.
	eventKeyOwnerID = "ownerId"
)

// EventOutbox is the transactional-outbox surface CRUD needs — satisfied by
// *outbox.Outbox (framework/outbox). An interface rather than the concrete
// type so crud carries no outbox import and tests can record staging calls.
type EventOutbox interface {
	// Append writes an event row using the passed executor — inside a CRUD
	// transaction that is the *sql.Tx, so the row commits or rolls back
	// with the business write.
	Append(ctx context.Context, ex DBExecutor, eventType string, data any) (string, error)
	// Nudge wakes the relay so post-commit delivery isn't bound to its
	// poll interval.
	Nudge()
}

// eventData shapes the lifecycle-event payload so SSE subscribers can filter
// by entity name and tenant without unmarshalling the record. Shared by the
// live-bus path (EmitEvent) and the outbox path (StageEvent) so both deliver
// the identical payload.
func (ch *CrudHandler) eventData(ctx context.Context, record any) map[string]any {
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
	if ch.Entity.Config.OwnerField != "" {
		// Stamp the owner id so SSE filters can scope per-subscriber.
		// Prefer the extractor (matches the request-handling user);
		// fall back to the record's own owner column (covers admin /
		// background emitters whose ctx has no user).
		if id, ok := owner.Get(ctx); ok {
			data[eventKeyOwnerID] = id
		} else if rec, ok := record.(map[string]any); ok {
			if id, ok := rec[ch.Entity.Config.OwnerField]; ok {
				data[eventKeyOwnerID] = id
			}
		}
	}
	return data
}

// StageEvent durably stages an entity lifecycle event when an outbox is
// configured. It MUST be called from inside the operation's transaction
// (ch.DB is the tx-scoped executor there — doCreate/doUpdate/doDelete and
// the upsert closure call it), so the event row commits or rolls back with
// the write. No-op without an outbox: the live-bus EmitEvent covers that
// mode post-commit.
func (ch *CrudHandler) StageEvent(ctx context.Context, eventType string, record any) error {
	if ch.Outbox == nil {
		return nil
	}
	_, err := ch.Outbox.Append(ctx, ch.DB, eventType, ch.eventData(ctx, record))
	return err
}

// EmitEvent fires an entity lifecycle event after the operation's transaction
// has committed. Delivery is split across two disjoint lanes:
//
//   - Real-time lane: the live bus is notified always (best-effort, async),
//     feeding SSE EventStream and ephemeral On/Subscribe handlers. Lossy by
//     design — a crash here drops the in-memory signal, but the durable lane
//     still guarantees delivery.
//   - Durable lane: when an outbox is configured, the row staged in-tx by
//     StageEvent is delivered to declared consumers by the relay. The relay
//     no longer touches the bus, so there is no double delivery.
func (ch *CrudHandler) EmitEvent(ctx context.Context, eventType string, record any) {
	if ch.Events != nil {
		ch.Events.EmitAsync(ctx, event.Event{Type: eventType, Data: ch.eventData(ctx, record)})
	}
	if ch.Outbox != nil {
		ch.Outbox.Nudge()
	}
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
		// Real-time event streams are authenticated regardless of
		// whether the entity declares OwnerField. An anonymous SSE
		// subscriber would otherwise scrape every row update on the
		// server in real time — turning a "public list endpoint" into
		// a live feed of all writes. RequireOwner only fires for
		// OwnerField entities; we also enforce a baseline auth check
		// here for entities without one.
		ownerID, ownerOK := ch.RequireOwner(w, r)
		if !ownerOK {
			return
		}
		if ch.Entity.Config.OwnerField == "" {
			if _, ok := handler.GetUser(r.Context()); !ok {
				writeJSONError(w, http.StatusUnauthorized, "authentication required")
				return
			}
		}
		// MultiTenant entities require a tenant in context: otherwise the
		// per-event tenant filter below (tenantID != "") no-ops and the
		// subscriber receives every tenant's writes in real time.
		if !ch.RequireTenant(w, r) {
			return
		}
		// The live feed is a READ surface — it streams every create/update/
		// delete. Gate it with the same Access.Read permission as List/Get,
		// or an authenticated user lacking docs:read could subscribe here for
		// a real-time read of all writes despite 403 on the static endpoints.
		if !ch.requirePermission(w, r, opRead) {
			return
		}

		sse := stream.NewSSEWriter(w)
		sse.WriteComment("subscribed " + ch.Entity.GetName())

		entityName := ch.Entity.GetName()
		tenantScope := ch.Entity.Config.MultiTenant
		tenantID := tenant.GetTenantID(r.Context())
		ownerScope := ch.Entity.Config.OwnerField != ""

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
			if ownerScope && ownerID != nil && data[eventKeyOwnerID] != ownerID {
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
