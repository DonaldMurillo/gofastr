package crud

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"reflect"
	"time"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/stream"
	"github.com/DonaldMurillo/gofastr/framework/event"
	"github.com/DonaldMurillo/gofastr/framework/hook"
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
		// Stamp the owner id as a STRING so it survives the fanout bridge's
		// JSON round-trip unchanged: a numeric owner (e.g. a BIGINT user
		// key) would arrive as float64 on the remote replica, and the
		// per-subscriber owner filter (interface{} != below) would then
		// compare float64 != int64 → always unequal → every cross-replica
		// event dropped. fmt.Sprint on both stamp and filter sides keeps
		// local and remote payloads identical.
		// Prefer the extractor (matches the request-handling user); fall
		// back to the record's own owner column (covers admin / background
		// emitters whose ctx has no user).
		if id, ok := owner.Get(ctx); ok {
			data[eventKeyOwnerID] = fmt.Sprint(id)
		} else if rec, ok := record.(map[string]any); ok {
			if id, ok := rec[ch.Entity.Config.OwnerField]; ok {
				data[eventKeyOwnerID] = fmt.Sprint(id)
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
		// The SSE baseline is deliberately STRICTER than the CRUD-wide
		// requireAuthenticated gate: a declared Access block with a blank
		// Read permission means "public static reads", but that must not
		// extend to a live feed of every write (see the comment above).
		// Only Public — the full, deliberate opt-out — makes the stream
		// anonymous; OwnerField entities are already authenticated via
		// RequireOwner.
		if ch.Entity.Config.OwnerField == "" && !ch.Entity.Config.Public {
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
		if !ch.requirePermission(w, r, opRead, "") {
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
			if ownerScope && ownerID != nil && fmt.Sprint(data[eventKeyOwnerID]) != fmt.Sprint(ownerID) {
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
				// Redact at DELIVERY, not when the event is built. The record
				// is captured from the write's RETURNING, so it holds stored
				// values and a subscriber would otherwise read past every mask
				// the read paths apply. Doing it here rather than in eventData
				// keeps the hook out of the write transaction (where it would
				// run on a connection that cannot see the uncommitted row, and
				// would block against a single-connection pool) and runs it
				// once per delivery instead of twice per write.
				ev = ch.redactEventRecord(r, ev)
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

// redactEventRecord returns ev with its record run through AfterGet, so an
// SSE subscriber sees the same values a GET would return.
//
// Deliberately tolerant of shape: a delete stages a primary-key-only stub,
// which a redaction hook written for full rows may not expect. A hook error
// leaves the record out rather than publishing it raw — the subscriber still
// learns something changed and can re-read through the API.
func (ch *CrudHandler) redactEventRecord(r *http.Request, ev event.Event) event.Event {
	if ch.Hooks == nil || len(ch.Hooks.HooksFor(hook.AfterGet)) == 0 {
		return ev
	}
	data, ok := ev.Data.(map[string]any)
	if !ok {
		return ev
	}
	row, ok := data[eventKeyRecord].(map[string]any)
	if !ok {
		return ev
	}
	// A delete stages a primary-key-only stub, not a row. Test the event type
	// rather than the map size: len(row) <= 1 was a proxy that also skipped
	// masking for an entity whose visible projection is the PK alone.
	if ev.Type == event.EntityDeleted {
		return ev
	}

	// Deep enough to isolate subscribers. EmitAsync hands every handler the
	// SAME event value, so each SSE stream redacts its own copy of the top
	// level — but a shallow copy leaves nested maps and slices shared, and two
	// subscribers masking inside one is a concurrent map write. Driver values
	// are scalars after convertValue, so a nested container only exists when a
	// write hook injected one; copy it anyway rather than depend on that.
	clone := deepCopyRecord(row)
	id := ""
	if v, ok := clone[ch.convertKey(ch.PrimaryKey)]; ok {
		id = fmt.Sprint(v)
	}
	// The only caller passes the SSE handler's request, so r is non-nil in
	// production — but a nil here would panic inside the delivery goroutine
	// and kill the stream, which is a poor trade for one comparison.
	hctx := context.Background()
	if r != nil {
		hctx = r.Context()
	}
	payload := &hook.GetPayload{Request: r, ID: id, Result: clone}
	if err := ch.Hooks.ExecuteHooks(hookCtx(hctx), hook.AfterGet, payload); err != nil {
		log.Printf("crud: after-get hook failed on %s event; omitting record: %v", ch.Entity.GetName(), err)
		payload.Result = nil
	}

	// Copy the envelope so the redacted view never reaches another
	// subscriber's copy or the durable outbox row. The owner and tenant
	// stamps are carried over untouched — they decide delivery, not content.
	out := make(map[string]any, len(data))
	for k, v := range data {
		out[k] = v
	}
	out[eventKeyRecord] = payload.Result
	ev.Data = out
	return ev
}

// deepCopyRecord copies a scanned row far enough that a redaction hook cannot
// reach another holder of the same event. Maps and slices are rebuilt;
// everything else is a scalar by the time convertValue is done with it.
func deepCopyRecord(row map[string]any) map[string]any {
	out := make(map[string]any, len(row))
	for k, v := range row {
		out[k] = deepCopyValue(v)
	}
	return out
}

// deepCopyValue rebuilds every map and slice reachable from v.
//
// The fast paths below are the shapes the framework itself produces. They are
// not sufficient on their own: the containers that matter here are the ones an
// application's write hook INJECTS, and a hand-written type switch only ever
// covers the shapes whoever wrote it thought of. A first version listed three,
// which left []byte, []string, map[string]string, [][]map[string]any and
// anything else sharing storage with the record already handed to the event
// goroutine — so a hook masking inside one wrote through into the bus, and two
// SSE subscribers masking concurrently raced on it. That race is "concurrent
// map writes", a runtime throw no recover() catches. Hence the reflective
// fallback: unknown container, still copied.
//
// Pointers and structs are deliberately NOT traversed. Copying an arbitrary
// struct means copying whatever it embeds — a mutex, a file handle, a driver
// connection — and a value that reaches a record as a pointer is not something
// this package can safely clone. Scalars need no copy.
func deepCopyValue(v any) any {
	switch t := v.(type) {
	case nil:
		return nil
	case string, bool, int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64, float32, float64, time.Time:
		return v
	case map[string]any:
		return deepCopyRecord(t)
	case []map[string]any:
		cp := make([]map[string]any, len(t))
		for i, m := range t {
			cp[i] = deepCopyRecord(m)
		}
		return cp
	case []any:
		cp := make([]any, len(t))
		for i, e := range t {
			cp[i] = deepCopyValue(e)
		}
		return cp
	case []byte:
		return append([]byte(nil), t...)
	}
	return deepCopyReflect(v)
}

// deepCopyReflect handles the container shapes the type switch above does not
// name. It copies maps, slices and arrays element-wise and returns anything
// else unchanged.
func deepCopyReflect(v any) any {
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Map:
		if rv.IsNil() {
			return v
		}
		out := reflect.MakeMapWithSize(rv.Type(), rv.Len())
		iter := rv.MapRange()
		for iter.Next() {
			out.SetMapIndex(iter.Key(), deepCopyReflectValue(iter.Value()))
		}
		return out.Interface()
	case reflect.Slice:
		if rv.IsNil() {
			return v
		}
		out := reflect.MakeSlice(rv.Type(), rv.Len(), rv.Len())
		for i := 0; i < rv.Len(); i++ {
			out.Index(i).Set(deepCopyReflectValue(rv.Index(i)))
		}
		return out.Interface()
	case reflect.Array:
		out := reflect.New(rv.Type()).Elem()
		for i := 0; i < rv.Len(); i++ {
			out.Index(i).Set(deepCopyReflectValue(rv.Index(i)))
		}
		return out.Interface()
	default:
		return v
	}
}

// deepCopyReflectValue recurses into an element, going back through the typed
// fast paths when the element is an interface holding one of them.
func deepCopyReflectValue(elem reflect.Value) reflect.Value {
	switch elem.Kind() {
	case reflect.Interface:
		if elem.IsNil() {
			return elem
		}
		return reflect.ValueOf(deepCopyValue(elem.Interface())).Convert(elem.Type())
	case reflect.Map, reflect.Slice, reflect.Array:
		cp := deepCopyReflect(elem.Interface())
		return reflect.ValueOf(cp)
	default:
		return elem
	}
}
