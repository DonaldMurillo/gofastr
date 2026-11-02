package crud

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/core/stream"
	"github.com/DonaldMurillo/gofastr/framework/db"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/event"
	"github.com/DonaldMurillo/gofastr/framework/hook"
	"github.com/DonaldMurillo/gofastr/framework/owner"
	"github.com/DonaldMurillo/gofastr/framework/tenant"
	"github.com/DonaldMurillo/gofastr/internal/deepcopy"
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
	eventKeyOwnerID = event.DataKeyOwnerID
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
	// AppendFor writes an event row using the passed executor, inside a
	// CRUD transaction that is the *sql.Tx, so the row commits or rolls
	// back with the business write. owner is the record's owner id ("" on
	// an entity with no owner field), which an erasure of that user
	// matches.
	AppendFor(ctx context.Context, ex DBExecutor, owner, eventType string, data any) (string, error)
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
		eventKeyRecord: ch.withoutCascadeChildren(record),
	}
	if ch.Entity.Config.Scope.MultiTenant {
		// The record's own tenant column names whose row this is; the
		// writer's tenant differs from it under tenant.AllowCrossTenant,
		// and stamping the writer's sent the row to their tenant's
		// subscribers. A record that does not carry the column falls back
		// to the row's stamp (event_stamp.go), then the context.
		if tid, ok := ch.recordColumn(record, ch.Entity.Config.TenantColumn()); ok && tid != nil && fmt.Sprint(tid) != "" {
			data[eventKeyTenantID] = fmt.Sprint(tid)
		} else if s, ok := ch.rowStampFor(ctx, record); ok && s.tenant != nil && fmt.Sprint(s.tenant) != "" {
			data[eventKeyTenantID] = fmt.Sprint(s.tenant)
		} else if tid := tenant.GetTenantID(ctx); tid != "" {
			data[eventKeyTenantID] = tid
		}
	}
	if id, ok := ch.eventOwner(ctx, record); ok {
		data[eventKeyOwnerID] = id
	}
	return data
}

// eventOwner is the owner id an event about record carries, when the
// entity has an owner field.
func (ch *CrudHandler) eventOwner(ctx context.Context, record any) (string, bool) {
	if ch.Entity.Config.Scope.OwnerField != "" {
		// Stamp the owner id as a STRING so it survives the fanout bridge's
		// JSON round-trip unchanged: a numeric owner (e.g. a BIGINT user
		// key) would arrive as float64 on the remote replica, and the
		// per-subscriber owner filter (interface{} != below) would then
		// compare float64 != int64 → always unequal → every cross-replica
		// event dropped. fmt.Sprint on both stamp and filter sides keeps
		// local and remote payloads identical.
		// Prefer the record's own owner column: the event is about that
		// row, and the subscriber filter routes it to the row's owner.
		// The writer is not always the owner: under owner.AllowCrossOwner
		// Alice updates Bob's row, and stamping her id delivered Bob's
		// record to Alice's subscription and not to Bob's. A record that
		// does not carry the column (a Hidden owner field, a delete's key
		// payload) falls back to the row's stamp (event_stamp.go), then
		// the context.
		if id, ok := ch.recordColumn(record, ch.Entity.Config.Scope.OwnerField); ok && id != nil {
			return fmt.Sprint(id), true
		} else if s, ok := ch.rowStampFor(ctx, record); ok && s.owner != nil {
			return fmt.Sprint(s.owner), true
		} else if id, ok := owner.Get(ctx); ok {
			return fmt.Sprint(id), true
		}
	}
	return "", false
}

// recordColumn reads column col from an event record, under its wire key
// (the write's response row is JSON-cased) or, failing that, its column
// name (a raw row).
func (ch *CrudHandler) recordColumn(record any, col string) (any, bool) {
	rec, ok := record.(map[string]any)
	if !ok || col == "" {
		return nil, false
	}
	if v, ok := rec[ch.convertKey(col)]; ok {
		return v, true
	}
	v, ok := rec[col]
	return v, ok
}

// withoutCascadeChildren returns record minus the cascade-written relations a
// create or update attached for its response. Each child already publishes
// its own event under its own entity. On SSE that event passes the child's
// read gate and AfterGet redaction; a copy inside the parent's payload
// reached any parent subscriber with neither. Durable outbox consumers get
// the child's raw row either way (see the EventOutbox contract). The caller's
// map is left intact: the response still carries the children.
func (ch *CrudHandler) withoutCascadeChildren(record any) any {
	row, ok := record.(map[string]any)
	if !ok || ch.Entity == nil {
		return record
	}
	var out map[string]any
	for _, rel := range ch.Entity.Config.Relations {
		if !rel.CascadeWrite {
			continue
		}
		key := ch.convertKey(rel.Name)
		if _, ok := row[key]; !ok {
			continue
		}
		if out == nil {
			out = maps.Clone(row)
		}
		delete(out, key)
	}
	if out == nil {
		return record
	}
	return out
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
	owner, _ := ch.eventOwner(ctx, record)
	_, err := ch.Outbox.AppendFor(ctx, ch.DB, owner, eventType, ch.eventData(ctx, record))
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
		// A handler bound to a caller's *sql.Tx is in the same position:
		// its write commits when, and if, the caller commits.
		if tx, ok := db.TxFromContext(ctx); ok {
			ch.emitAfterAmbientTx(ctx, tx, eventType, record)
		} else if tx, ok := ch.DB.(*sql.Tx); ok {
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
// connect. On every delivery and on a ticker (EventStreamReauth, 30s by
// default) the stream re-establishes its caller through
// handler.RecheckPrincipal, which the auth middleware installs: the session,
// API token or JWT is looked up again, its owner re-resolved, and the roles
// re-derived by access.Middleware. The entity's read permission is then
// re-checked on that refreshed principal, and the read-scope lift is
// re-evaluated per event. A caller who would now be refused (session
// revoked, token revoked or expired, user deleted, role dropped, a resource
// decider flipped to deny) has the stream closed at the next check, whether
// or not the entity declares a read permission, so an established feed
// never outlives the authority that opened it. Middleware that sets a user
// without installing a check (handler.WithPrincipalCheck) leaves the
// identity as connected; only the permission re-check applies then.
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
			writeJSONError(w, r, http.StatusServiceUnavailable, reasonEventBusMissing)
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
				writeJSONError(w, r, http.StatusUnauthorized, handler.ReasonAuthenticationRequired)
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

		// Seat admission: the per-principal cap on concurrent streams
		// (EventStreamSeats / EventStreamSeatOverflow). Each accepted
		// stream pins a goroutine, a 32-entry buffer, and three bus
		// subscriptions until it disconnects, so one authenticated
		// caller must not be able to hold an unbounded number. Refusal
		// happens before any byte of the stream is written — the caller
		// is answered 429 with Retry-After (EventSource backs off), not
		// left holding an empty stream. The seat is released on every
		// exit below: disconnect, write error, re-auth refusal, or an
		// EvictOldest admission closing this stream's done channel.
		//
		// An eviction also cuts a write this stream is parked in: a client
		// that stopped reading holds the handler inside Write, where done
		// never reaches it. A writer that cannot take a deadline
		// (ErrNotSupported) leaves the parked write to the server's own
		// WriteTimeout.
		rc := http.NewResponseController(w)
		seat, admitted := ch.seatRegistry().admit(
			ch.streamSeatPrincipal(r, ownerID),
			ch.EventStreamSeats, ch.EventStreamSeatOverflow,
			func() { _ = rc.SetWriteDeadline(time.Now()) })
		if !admitted {
			w.Header().Set("Retry-After", "1")
			writeJSONError(w, r, http.StatusTooManyRequests, i18n.ReasonTooManyStreams)
			return
		}
		defer ch.seatRegistry().release(seat)

		sse := stream.NewSSEWriter(w)

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

		// authzHeld re-establishes the caller and re-runs the connect-time
		// permission gate. The request context was built once, at connect,
		// so on its own it keeps answering for a session that has since
		// been deleted, a token since revoked, a role since dropped.
		// handler.RecheckPrincipal re-runs what the auth middleware did
		// (session lookup, token lookup, JWT validation and owner lookup)
		// and what access.Middleware did (role resolution), and fails when
		// a fresh request with the same credentials would no longer carry
		// this principal. It runs even when the entity declares no read
		// permission: the baseline gate is "authenticated", and a revoked
		// session is no longer that. The permission check then runs on the
		// refreshed context, and the decider reads its live store as before.
		// live holds that refreshed context for the per-event read-scope
		// lift and the delivery-time redaction.
		readPerm := ch.permissionForOp(opRead)
		var liveMu sync.Mutex
		live := r.Context()
		liveCtx := func() context.Context {
			liveMu.Lock()
			defer liveMu.Unlock()
			return live
		}
		authzHeld := func() bool {
			fresh, ok := handler.RecheckPrincipal(r.Context())
			if !ok {
				return false
			}
			// The per-event filter keys on the connect-time tenant. A
			// principal the refreshed context places in another tenant
			// would keep reading the old one's writes, so close instead.
			if tenantScope && tenant.GetTenantID(fresh) != tenantID {
				return false
			}
			liveMu.Lock()
			live = fresh
			liveMu.Unlock()
			if readPerm == "" {
				return true
			}
			return ch.accessAllows(fresh, readPerm, "")
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
			if !readScopeUnrestricted(liveCtx(), ch.Entity) && !ch.readScopeAllowsRecord(data[eventKeyRecord]) {
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
		// A client can publish as soon as this acknowledgement arrives.
		// Install the subscriptions first so that write cannot be lost.
		if err := sse.WriteComment("subscribed " + entityName); err != nil {
			return
		}

		for {
			select {
			case <-r.Context().Done():
				return
			case <-seat.done:
				// An EvictOldest admission (this principal's next stream
				// took the seat) closed this stream: depart exactly as a
				// disconnect would, releasing the bus subscriptions through
				// the deferred cancels and the seat through the deferred
				// release (a no-op splice: the eviction already removed it).
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
				// The filter decided the read-scope lift on the context
				// the previous check left, so a lift removed since then
				// still passed this event. Decide it again on the context
				// authzHeld just refreshed.
				if !readScopeUnrestricted(liveCtx(), ch.Entity) {
					data, _ := ev.Data.(map[string]any)
					if !ch.readScopeAllowsRecord(data[eventKeyRecord]) {
						continue
					}
				}
				// The subscriber's own BeforeList/BeforeGet scope, run as
				// List and Get run it, on the context authzHeld just
				// refreshed. Here, not in the filter: the filter runs on
				// the emitter's goroutine, and the check reads the table.
				if ch.eventHookScopeHides(r.WithContext(liveCtx()), ev) {
					continue
				}
				// Redact at DELIVERY, not when the event is built. The record
				// is captured from the write's RETURNING, so it holds stored
				// values and a subscriber would otherwise read past every mask
				// the read paths apply. Doing it here rather than in eventData
				// keeps the hook out of the write transaction (where it would
				// run on a connection that cannot see the uncommitted row, and
				// would block against a single-connection pool) and runs it
				// once per delivery instead of twice per write.
				ev = ch.redactEventRecord(r.WithContext(liveCtx()), ev)
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

// eventHookScopeHides reports whether the subscriber's own read-scoping
// hooks hide the row ev is about, so the feed withholds it. The feed is a
// list of rows and a read of each, so both chains run: BeforeList, as
// GET /<entity> runs it, and BeforeGet with the row's id, as GET
// /<entity>/{id} does. A row either route would hide is withheld.
//
// A created or updated row is checked where it is stored, after the write
// committed. A delete has no row left to check on a hard-deleting entity,
// so it reaches only a subscriber the hooks do not restrict (they append
// no clause for it); a restricted one is not told, as a delete does not
// reach a caller a declared ReadScope restricts. A soft delete leaves the
// row in place, so its delete is checked like any other event. A hook
// error, or a record with no key, withholds the event.
func (ch *CrudHandler) eventHookScopeHides(r *http.Request, ev event.Event) bool {
	if ch.Hooks == nil || (len(ch.Hooks.HooksFor(hook.BeforeList)) == 0 && len(ch.Hooks.HooksFor(hook.BeforeGet)) == 0) {
		return false
	}
	data, _ := ev.Data.(map[string]any)
	row, ok := data[eventKeyRecord].(map[string]any)
	if !ok {
		return true
	}
	vals, ok := ch.rowKeyVals(row)
	if !ok {
		return true
	}
	id, _ := ch.rowID(row)
	ctx := r.Context()
	clauses, err := ch.ownBeforeScopes(ctx, r, id, true)
	if err != nil {
		return true
	}
	if len(clauses) == 0 {
		return false
	}
	if ev.Type == event.EntityDeleted && !ch.Entity.Config.Scope.SoftDelete {
		return true
	}
	return beforeScopeHidesKey(ctx, ch.conn(ctx), ch.Entity.GetTable(), ch.keyCols(), vals, clauses)
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
	clone := deepcopy.Map(row)
	id, _ := entity.RowKey(clone, ch.keyCols(), ch.convertKey)
	// The only caller passes the SSE handler's request, so r is non-nil in
	// production, but a nil here would panic inside the delivery goroutine
	// and kill the stream, which is a poor trade for one comparison.
	hctx := context.Background()
	if r != nil {
		hctx = r.Context()
	}
	payload := &hook.GetPayload{Request: r, ID: id, Result: clone}
	if err := runHooks(ch.Hooks, hookCtx(hctx), hook.AfterGet, payload); err != nil {
		logf("crud: after-get hook failed on %s event; omitting record: %v", ch.Entity.GetName(), err)
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

// ============================================================================
// Ambient-transaction event gating
// ============================================================================

// ambientTxProbeInterval is how often a held-back emission checks whether the
// caller's transaction has finished. Short enough that a committed write
// reaches SSE subscribers promptly, long enough not to spin.
const ambientTxProbeInterval = 5 * time.Millisecond

// ambientTxProbeTimeout bounds the wait, from the emission to the decision.
// A caller that never commits or rolls back, or a statement of its own that
// holds the transaction's connection, would otherwise pin the goroutine for
// the process lifetime; past this the emission is dropped and logged,
// because an event announcing a write nobody committed is worse than a
// missing one. A var so tests can shorten it.
var ambientTxProbeTimeout = 2 * time.Minute

// unconfirmedWarnOnce gates the once-per-process warning that an emission
// was dropped because its transaction's outcome cannot be learned.
var unconfirmedWarnOnce sync.Once

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
	id, hasKey := ch.recordPrimaryKey(record)
	if !ok || !hasKey {
		// The handler itself is bound to a transaction, so there is no
		// independent connection to check the write against, or the
		// record carries no key to check it by. Either way the outcome
		// cannot be learned, and publishing now announced writes the
		// caller went on to roll back (2026-10-27 bug bash D35). The
		// emission is dropped: the live bus is lossy by design, and a
		// caller that wants these events carries the transaction's commit
		// queue on the context (db.WithTxQueue) and drains it after
		// Commit, which the branch above serves.
		unconfirmedWarnOnce.Do(func() {
			logf("crud: dropping %s for %s: the write runs in a caller-owned transaction with no commit queue "+
				"and no way to confirm it landed; carry the transaction with db.WithTxQueue and drain the queue "+
				"after Commit (see hooks-and-transactions docs)", eventType, ch.Entity.GetTable())
		})
		return
	}

	// Reaching here means a caller-owned transaction with no commit queue,
	// so the outcome must be learned by probing the live tx — a statement
	// that can interleave with the caller's own on the transaction's one
	// connection. Scream once per process rather than racing silently;
	// the caller can remove the probe entirely by switching to
	// db.WithTxQueue and draining after Commit.
	probeWarnOnce.Do(func() {
		logf("crud: a CRUD write joined a caller-owned transaction (db.WithTx with no commit queue); " +
			"its lifecycle event is confirmed by polling the live *sql.Tx, which can race the owner's statements — " +
			"prefer db.WithTxQueue + RunAfterCommit (see hooks-and-transactions docs)")
	})

	// Snapshot the payload now: it is built from the request context's
	// tenant/owner identity, which will not be valid to read later.
	data := ch.eventData(ctx, record)
	table := ch.Entity.GetTable()
	pk := ch.keyCols()
	bus := ch.Events
	match := ch.comparableColumns(record)

	go func() {
		// One detached context carries the limit for both statements:
		// the request's own context ends with the request, long before a
		// caller's transaction may.
		pctx, cancel := context.WithTimeout(context.Background(), ambientTxProbeTimeout)
		defer cancel()
		if !waitTxDone(pctx, tx) {
			logf("crud: dropping %s for %s/%v: ambient transaction still open after %s",
				eventType, table, fmt.Sprint(id...), ambientTxProbeTimeout)
			return
		}

		committed, err := landed(pctx, base, eventType, table, pk, id, match)
		if err != nil {
			logf("crud: dropping %s for %s/%v: cannot confirm the ambient transaction committed: %v",
				eventType, table, fmt.Sprint(id...), err)
			return
		}
		if !committed {
			return // rolled back; the row never existed
		}
		bus.EmitAsync(context.Background(), event.Event{Type: eventType, Data: data})
	}()
}

// waitTxDone polls tx until it has finished (committed or rolled back) and
// reports true, or reports false once ctx ends. Any statement on a finished
// Tx reports sql.ErrTxDone, whichever way it ended.
//
// The poll statement itself runs on context.Background(), in a goroutine
// the wait does not join. database/sql serializes a transaction's
// statements on one connection lock that no context reaches, so a statement
// the owner has running holds the poll there; and a poll statement whose
// own context ends while it runs makes lib/pq mark the connection bad and
// send a cancel request that lands on whatever that backend runs next,
// which is the owner's transaction. The deadline is therefore enforced on
// the wait: the decision to drop is made on time, and a parked poll
// finishes on its own when the owner's statement releases the connection.
func waitTxDone(ctx context.Context, tx *sql.Tx) bool {
	for {
		res := make(chan error, 1)
		go func() {
			_, err := tx.ExecContext(context.Background(), "SELECT 1")
			res <- err
		}()
		select {
		case err := <-res:
			if errors.Is(err, sql.ErrTxDone) {
				return true
			}
		case <-ctx.Done():
			return false
		}
		select {
		case <-time.After(ambientTxProbeInterval):
		case <-ctx.Done():
			return false
		}
	}
}

// landed reports whether the write the event describes is visible on the
// base connection. A delete landed when the row is gone; a create when it is
// there; an update when it is there AND carries the announced values (match),
// since the row survives a rollback either way.
func landed(ctx context.Context, base *sql.DB, eventType, table string, pk []string, id []any, match map[string]any) (bool, error) {
	safeTable, err := query.SafeIdent(table)
	if err != nil {
		return false, err
	}
	// $N placeholders, matching what core/query's builders emit for every
	// dialect. Postgres rejects `?` outright — with it, this confirm query
	// failed as `pq: syntax error at end of input` on every ambient-tx
	// event, and the emission was dropped as unconfirmable.
	var where []string
	var args []any
	for i, col := range pk {
		safePK, err := query.SafeIdent(col)
		if err != nil {
			return false, err
		}
		where = append(where, fmt.Sprintf("%s = $%d", query.QuoteFolded(safePK), i+1))
		args = append(args, id[i])
	}
	if eventType == event.EntityUpdated {
		for _, col := range slices.Sorted(maps.Keys(match)) {
			safeCol, err := query.SafeIdent(col)
			if err != nil {
				continue
			}
			// A NULL never equals anything in SQL, so `col = NULL` would
			// match no row; IS NULL is the comparison that finds it.
			if match[col] == nil {
				where = append(where, query.QuoteFolded(safeCol)+" IS NULL")
				continue
			}
			where = append(where, fmt.Sprintf("%s = $%d",
				query.QuoteFolded(safeCol), len(args)+1))
			args = append(args, match[col])
		}
	}
	var n int
	stmt := fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE %s",
		query.QuoteFolded(safeTable), strings.Join(where, " AND "))
	if err := base.QueryRowContext(ctx, stmt, args...).Scan(&n); err != nil {
		return false, err
	}
	if eventType == event.EntityDeleted {
		return n == 0, nil
	}
	return n > 0, nil
}

// comparableColumns maps the scalar values an emitted record carries onto
// their database columns, skipping the primary key and anything not
// directly comparable in SQL (nested objects, arrays, byte slices). It is
// what lets an UPDATE be distinguished from its own rollback. A NULL is
// kept as nil and matched with IS NULL: skipped, an edit that only set a
// column to NULL was confirmed on its key alone, rollback or not.
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
		// The wire-key map, not the bare case conversion: snake-casing a
		// camel key misses a mixed-case column or a WireName, and a column
		// left out is a value the confirm query no longer checks.
		col := ch.wireKeyColumn(key)
		if ch.isKeyCol(col) || !known[col] {
			continue
		}
		switch v.(type) {
		case nil:
			out[col] = nil
		case string, bool,
			int, int8, int16, int32, int64,
			uint, uint8, uint16, uint32, uint64,
			float32, float64, time.Time:
			out[col] = v
		}
	}
	return out
}

// recordPrimaryKey pulls the key values out of an emitted record,
// tolerating either wire casing.
func (ch *CrudHandler) recordPrimaryKey(record any) ([]any, bool) {
	m, ok := record.(map[string]any)
	if !ok {
		return nil, false
	}
	return ch.rowKeyVals(m)
}
