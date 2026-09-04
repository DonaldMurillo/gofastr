package crud

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"maps"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/core/stream"
	"github.com/DonaldMurillo/gofastr/framework/access"
	"github.com/DonaldMurillo/gofastr/framework/db"
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

// EventOutbox is the transactional-outbox surface CRUD needs, satisfied by
// *outbox.Outbox (framework/outbox). An interface rather than the concrete
// type so crud carries no outbox import and tests can record staging calls.
//
// CONTRACT: durable consumers registered via the outbox's Consume receive
// the RAW staged row — the post-write RETURNING record, with no AfterGet
// pass at stage or delivery time. Durable consumers are trusted machinery
// and own their masking; the SSE lane is the one that redacts at delivery
// (redactEventRecord). See the "Durable consumers receive the raw row"
// bullet in framework/docs/content/events.md.
type EventOutbox interface {
	// Append writes an event row using the passed executor, inside a CRUD
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
	if ch.Entity.Config.Scope.MultiTenant {
		if tid := tenant.GetTenantID(ctx); tid != "" {
			data[eventKeyTenantID] = tid
		}
	}
	if ch.Entity.Config.Scope.OwnerField != "" {
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
			if id, ok := rec[ch.Entity.Config.Scope.OwnerField]; ok {
				data[eventKeyOwnerID] = fmt.Sprint(id)
			}
		}
	}
	return data
}

// StageEvent durably stages an entity lifecycle event when an outbox is
// configured. It MUST be called from inside the operation's transaction
// (ch.DB is the tx-scoped executor there, doCreate/doUpdate/doDelete and
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
//     design, a crash here drops the in-memory signal, but the durable lane
//     still guarantees delivery.
//   - Durable lane: when an outbox is configured, the row staged in-tx by
//     StageEvent is delivered to declared consumers by the relay. The relay
//     no longer touches the bus, so there is no double delivery.
func (ch *CrudHandler) EmitEvent(ctx context.Context, eventType string, record any) {
	if ch.Events != nil {
		// An ambient transaction (db.WithTx / App.InTx) is still open at
		// this point: the operation joined it and the outer owner decides
		// whether it commits. Publishing now would announce a write that
		// may be rolled back a moment later.
		if tx, ok := db.TxFromContext(ctx); ok {
			ch.emitAfterAmbientTx(ctx, tx, eventType, record)
		} else {
			ch.Events.EmitAsync(ctx, event.Event{Type: eventType, Data: ch.eventData(ctx, record)})
		}
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
// Authorization is re-validated for the life of the stream, not only at
// connect: the entity's read permission is re-checked on every delivery and
// on a ticker (EventStreamReauth, 30s by default), and the read-scope lift
// is re-evaluated per event. A caller whose CURRENT authorization forbids
// reads (session revoked, role dropped, a resource decider flipped to deny)
// has the stream closed at the next check, so an established feed never
// outlives the authority that opened it.
//
// Each accepted event is written as:
//
//	event: entity.created (or entity.updated / entity.deleted)
//	data:  {<full Event JSON>}
//
// Disconnects from the client unsubscribe automatically. A backpressure
// buffer of 32 is enforced, if the client cannot keep up, events are
// dropped rather than blocking emitters.

// eventStreamReauthInterval resolves EventStreamReauth: the configured
// interval when it is at least a second, otherwise the 30s default. The
// floor keeps a misconfigured handler from turning the idle re-check into
// a busy loop; there is no "off" value.
func (ch *CrudHandler) eventStreamReauthInterval() time.Duration {
	if ch.EventStreamReauth >= time.Second {
		return ch.EventStreamReauth
	}
	return 30 * time.Second
}

func (ch *CrudHandler) EventStream() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if ch.Events == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "event bus not configured")
			return
		}
		// Real-time event streams are authenticated regardless of
		// whether the entity declares OwnerField. An anonymous SSE
		// subscriber would otherwise scrape every row update on the
		// server in real time, turning a "public list endpoint" into
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
		// Only Public, the full, deliberate opt-out, makes the stream
		// anonymous; OwnerField entities are already authenticated via
		// RequireOwner.
		if ch.Entity.Config.Scope.OwnerField == "" && !ch.Entity.Config.Exposure.Public {
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
		// The live feed is a READ surface, it streams every create/update/
		// delete. Gate it with the same Access.Read permission as List/Get,
		// or an authenticated user lacking docs:read could subscribe here for
		// a real-time read of all writes despite 403 on the static endpoints.
		if !ch.requirePermission(w, r, opRead, "") {
			return
		}

		sse := stream.NewSSEWriter(w)
		sse.WriteComment("subscribed " + ch.Entity.GetName())

		entityName := ch.Entity.GetName()
		tenantScope := ch.Entity.Config.Scope.MultiTenant
		tenantID := tenant.GetTenantID(r.Context())
		ownerScope := ch.Entity.Config.Scope.OwnerField != ""
		// The read scope is a property of THIS subscriber, decided from
		// the subscribing request rather than per event: the emitter's
		// context belongs to whoever wrote the row, not to whoever is
		// listening.
		//
		// Without this the feed was a second door to rows every other
		// surface hides. A caller who gets 404 from GET /<entity>/<id>
		// and an empty list from GET /<entity> received the full draft
		// record here the moment an editor saved it, which is the
		// existence disclosure the 404 exists to prevent. The comment
		// above about a real-time read of all writes is the same argument
		// one level down: entity-level posture was enforced, row-level
		// was not.
		//
		// The unrestricted LIFT is re-evaluated per event (below, in the
		// filter) rather than captured here: it consults the access
		// decider, which answers from a live store, so a caller whose
		// lift is revoked mid-stream stops seeing hidden rows without
		// reconnecting.

		// authzHeld re-runs the connect-time permission gate. It is the
		// live seam: access.CanResource consults the decider, which reads
		// whatever store backs it NOW, so the frozen request context still
		// answers "may this caller read, as of this call". The owner and
		// tenant resolutions above read immutable context values and
		// cannot flip on a held context; the decider-backed gates can, and
		// this is the re-run of them.
		readPerm := ch.permissionForOp(opRead)
		authzHeld := func() bool {
			if readPerm == "" {
				return true
			}
			return access.CanResource(r.Context(), access.Permission(readPerm),
				access.Ref{Type: ch.Entity.GetName(), ID: ""})
		}

		reauth := time.NewTicker(ch.eventStreamReauthInterval())
		defer reauth.Stop()

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
			if !readScopeUnrestricted(r.Context(), ch.Entity) && !ch.readScopeAllowsRecord(data[eventKeyRecord]) {
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
			case <-reauth.C:
				// Idle streams re-validate on a ticker so a revocation
				// closes the feed even when no event arrives to carry the
				// per-delivery check. The client's EventSource reconnects
				// and meets the full connect-time gate, which now refuses.
				if !authzHeld() {
					return
				}
			case ev := <-buf:
				// Authorization is a property of the caller NOW, not of the
				// moment they connected: re-run the permission gate per
				// delivery, cheaply, against the live decider. Refusal
				// closes the stream; the buffered event is dropped with it.
				if !authzHeld() {
					return
				}
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
// leaves the record out rather than publishing it raw, the subscriber still
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
	// level, but a shallow copy leaves nested maps and slices shared, and two
	// subscribers masking inside one is a concurrent map write. Driver values
	// are scalars after convertValue, so a nested container only exists when a
	// write hook injected one; copy it anyway rather than depend on that.
	clone := deepCopyRecord(row)
	id := ""
	if v, ok := clone[ch.convertKey(ch.PrimaryKey)]; ok {
		id = fmt.Sprint(v)
	}
	// The only caller passes the SSE handler's request, so r is non-nil in
	// production, but a nil here would panic inside the delivery goroutine
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
	// stamps are carried over untouched, they decide delivery, not content.
	out := make(map[string]any, len(data))
	maps.Copy(out, data)
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
// goroutine, so a hook masking inside one wrote through into the bus, and two
// SSE subscribers masking concurrently raced on it. That race is "concurrent
// map writes", a runtime throw no recover() catches. Hence the reflective
// fallback: unknown container, still copied.
//
// Structs are deliberately NOT traversed, and neither is a pointer TO one.
// Copying an arbitrary struct means copying whatever it embeds, a mutex, a
// file handle, a driver connection, and this package cannot know which of
// those a host's hook put in the record. Scalars need no copy.
//
// That rationale covers opaque shapes; it does not cover a pointer to a plain
// container. *map[string]any, *[]any and *[]map[string]any are still the
// record's own data, just one indirection out, and leaving them aliased
// reproduced the exact bug the reflective fallback was added for: a redaction
// hook mutating the response copy wrote through into the record already handed
// to the event goroutine, so two subscribers masking concurrently raced on one
// map, "concurrent map writes", a runtime throw no recover() catches. So
// deepCopyReflect DOES traverse a pointer whose element is a map, slice or
// array: it copies the pointee and allocates a fresh pointer to it. Every
// other pointer element kind (struct above all, but also chan/func/unsafe)
// is returned as-is.
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
// name. It copies maps, slices and arrays element-wise, plus pointers TO
// those three, which alias the record just as directly, and returns anything
// else unchanged.
func deepCopyReflect(v any) any {
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Pointer:
		// A pointer to a container is still the record's data. A pointer to a
		// struct (or anything else) is not something this package can clone,
		// see deepCopyValue's comment.
		if rv.IsNil() {
			return v
		}
		switch rv.Type().Elem().Kind() {
		case reflect.Map, reflect.Slice, reflect.Array:
		default:
			return v
		}
		cp := reflect.ValueOf(deepCopyValue(rv.Elem().Interface()))
		if !cp.IsValid() || !cp.Type().AssignableTo(rv.Type().Elem()) {
			return v
		}
		out := reflect.New(rv.Type().Elem())
		out.Elem().Set(cp)
		return out.Interface()
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
	case reflect.Map, reflect.Slice, reflect.Array, reflect.Pointer:
		// Pointer is here for the same reason it is in deepCopyReflect: a
		// []*map[string]any element aliases the record too. deepCopyReflect
		// returns non-container pointers unchanged, so structs stay opaque.
		cp := deepCopyReflect(elem.Interface())
		return reflect.ValueOf(cp)
	default:
		return elem
	}
}

// ============================================================================
// Ambient-transaction event gating
// ============================================================================

// ambientTxProbeInterval is how often a held-back emission checks whether the
// caller's transaction has finished. Short enough that a committed write
// reaches SSE subscribers promptly, long enough not to spin.
const ambientTxProbeInterval = 5 * time.Millisecond

// ambientTxProbeTimeout bounds the wait. A caller that never commits or rolls
// back would otherwise pin the goroutine for the process lifetime; past this
// the emission is dropped and logged, because an event announcing a write
// nobody committed is worse than a missing one.
const ambientTxProbeTimeout = 2 * time.Minute

// probeWarnOnce gates the once-per-process warning that the caller-owned
// fallback below is probing a live transaction.
var probeWarnOnce sync.Once

// emitAfterAmbientTx holds a live-bus emission until the caller's ambient
// transaction resolves, then publishes it only if the write actually landed.
//
// A CRUD operation joined to an ambient tx (db.WithTx / App.InTx) returns
// BEFORE that transaction commits — inTx's ambient branch deliberately does
// not commit, the outer owner does. Emitting on the live bus at that point
// announced a row that might never exist: a subsequent Rollback left SSE
// subscribers and On/Subscribe handlers holding the full record payload of a
// phantom write. The durable outbox lane never had this problem because
// StageEvent writes inside the tx and rolls back with it.
//
// Two mechanisms answer "did it commit?", split by who owns the tx:
//
//   - Framework-owned (App.InTx, crud's inTx): the owner attached a
//     db.CommitQueue to the context and drains it only after Commit
//     succeeds, so the emission is enqueued and the question never
//     arises. No statement touches the live tx and no row re-check runs.
//   - Caller-owned (db.WithTx around the caller's own Begin): database/sql
//     exposes no commit callback, and a committed and a rolled-back Tx are
//     indistinguishable afterwards (both report sql.ErrTxDone). So the
//     outcome is read from the database: once the transaction is done, the
//     write is re-checked against the base connection. That is the same
//     question a subscriber would ask, which makes it the right one to
//     gate on. The done-poll this requires races the owner's statements on
//     the tx's connection (see CommitQueueFromContext), which is why every
//     framework path uses the queue.
//
// "Re-checked" is per event kind, because row presence alone does not
// answer it. A rolled-back CREATE leaves no row and a rolled-back DELETE
// leaves the row, so presence decides those. A rolled-back UPDATE leaves the
// row exactly as present as a committed one, so an update is matched on the
// emitted VALUES as well: the row counts as landed only if the columns the
// event announced are the columns the database now holds.
func (ch *CrudHandler) emitAfterAmbientTx(ctx context.Context, tx *sql.Tx, eventType string, record any) {
	if q, ok := db.CommitQueueFromContext(ctx); ok {
		// The tx owner is framework code (App.InTx / crud's inTx): it
		// drains this queue only after Commit succeeds and drops it on
		// rollback, so the emission needs no probe and no row re-check.
		// Probing is not an option on this path — a statement on the live
		// *sql.Tx from a spawned goroutine races the owner's own
		// statements on the transaction's single connection, and the
		// interleaved wire protocol crossed their results (#353: an
		// INSERT…RETURNING scanning sql.ErrNoRows, a well-formed query
		// failing with a pq syntax error).
		data := ch.eventData(ctx, record)
		bus := ch.Events
		q.Add(func() {
			bus.EmitAsync(context.Background(), event.Event{Type: eventType, Data: data})
		})
		return
	}
	base, ok := ch.DB.(*sql.DB)
	if !ok {
		// The handler itself is bound to a transaction, so there is no
		// independent connection to verify against. Nothing sensible to
		// gate on; publish as before rather than silently dropping. The
		// tx is masked first (#367): EmitAsync hands the context to a
		// goroutine per subscriber, and a live *sql.Tx in a goroutine
		// running beside the transaction's owner is the same
		// one-connection statements race the commit queue exists to
		// avoid. eventData reads identity values, which survive the mask.
		ch.Events.EmitAsync(db.WithoutTx(ctx), event.Event{Type: eventType, Data: ch.eventData(ctx, record)})
		return
	}
	id, ok := ch.recordPrimaryKey(record)
	if !ok {
		ch.Events.EmitAsync(db.WithoutTx(ctx), event.Event{Type: eventType, Data: ch.eventData(ctx, record)})
		return
	}

	// Reaching here means a caller-owned transaction with no commit queue,
	// so the outcome must be learned by probing the live tx — a statement
	// that can interleave with the caller's own on the transaction's one
	// connection. Scream once per process rather than racing silently;
	// the caller can remove the probe entirely by switching to
	// db.WithTxQueue and draining after Commit.
	probeWarnOnce.Do(func() {
		log.Printf("crud: a CRUD write joined a caller-owned transaction (db.WithTx with no commit queue); " +
			"its lifecycle event is confirmed by polling the live *sql.Tx, which can race the owner's statements — " +
			"prefer db.WithTxQueue + RunAfterCommit (see hooks-and-transactions docs)")
	})

	// Snapshot the payload now: it is built from the request context's
	// tenant/owner identity, which will not be valid to read later.
	data := ch.eventData(ctx, record)
	table := ch.Entity.GetTable()
	pk := ch.PrimaryKey
	bus := ch.Events
	match := ch.comparableColumns(record)

	go func() {
		deadline := time.Now().Add(ambientTxProbeTimeout)
		for {
			// Any statement on a finished Tx reports sql.ErrTxDone,
			// whether it committed or rolled back.
			if _, err := tx.ExecContext(context.Background(), "SELECT 1"); errors.Is(err, sql.ErrTxDone) {
				break
			}
			if time.Now().After(deadline) {
				log.Printf("crud: dropping %s for %s/%v: ambient transaction still open after %s",
					eventType, table, id, ambientTxProbeTimeout)
				return
			}
			time.Sleep(ambientTxProbeInterval)
		}

		committed, err := landed(base, eventType, table, pk, id, match)
		if err != nil {
			log.Printf("crud: dropping %s for %s/%v: cannot confirm the ambient transaction committed: %v",
				eventType, table, id, err)
			return
		}
		if !committed {
			return // rolled back; the row never existed
		}
		bus.EmitAsync(context.Background(), event.Event{Type: eventType, Data: data})
	}()
}

// landed reports whether the write the event describes is visible on the
// base connection. A delete landed when the row is gone; a create when it is
// there; an update when it is there AND carries the announced values (match),
// since the row survives a rollback either way.
func landed(base *sql.DB, eventType, table, pk string, id any, match map[string]any) (bool, error) {
	safeTable, err := query.SafeIdent(table)
	if err != nil {
		return false, err
	}
	safePK, err := query.SafeIdent(pk)
	if err != nil {
		return false, err
	}
	// $N placeholders, matching what core/query's builders emit for every
	// dialect. Postgres rejects `?` outright — with it, this confirm query
	// failed as `pq: syntax error at end of input` on every ambient-tx
	// event, and the emission was dropped as unconfirmable.
	where := []string{query.QuoteIdent(safePK) + " = $1"}
	args := []any{id}
	if eventType == event.EntityUpdated {
		for _, col := range slices.Sorted(maps.Keys(match)) {
			safeCol, err := query.SafeIdent(col)
			if err != nil {
				continue
			}
			where = append(where, fmt.Sprintf("%s = $%d",
				query.QuoteIdent(safeCol), len(args)+1))
			args = append(args, match[col])
		}
	}
	var n int
	stmt := fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE %s",
		query.QuoteIdent(safeTable), strings.Join(where, " AND "))
	if err := base.QueryRow(stmt, args...).Scan(&n); err != nil {
		return false, err
	}
	if eventType == event.EntityDeleted {
		return n == 0, nil
	}
	return n > 0, nil
}

// comparableColumns maps the scalar values an emitted record carries onto
// their database columns, skipping the primary key, NULLs, and anything not
// directly comparable in SQL (nested objects, arrays, byte slices). It is
// what lets an UPDATE be distinguished from its own rollback.
func (ch *CrudHandler) comparableColumns(record any) map[string]any {
	m, ok := record.(map[string]any)
	if !ok {
		return nil
	}
	known := map[string]bool{}
	for _, f := range ch.Entity.GetFields() {
		known[f.Name] = true
	}
	out := map[string]any{}
	for key, v := range m {
		col := ch.unconvertKeyRaw(key)
		if col == ch.PrimaryKey || !known[col] || v == nil {
			continue
		}
		switch v.(type) {
		case string, bool,
			int, int8, int16, int32, int64,
			uint, uint8, uint16, uint32, uint64,
			float32, float64, time.Time:
			out[col] = v
		}
	}
	return out
}

// recordPrimaryKey pulls the primary-key value out of an emitted record,
// tolerating either wire casing.
func (ch *CrudHandler) recordPrimaryKey(record any) (any, bool) {
	m, ok := record.(map[string]any)
	if !ok {
		return nil, false
	}
	for _, key := range []string{ch.convertKey(ch.PrimaryKey), ch.PrimaryKey} {
		if v, ok := m[key]; ok && v != nil {
			return v, true
		}
	}
	return nil, false
}
