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

const (
	eventKeyEntity   = "entity"
	eventKeyTable    = "table"
	eventKeyRecord   = "record"
	eventKeyTenantID = "tenantId"
	eventKeyOwnerID  = event.DataKeyOwnerID
)

// EventOutbox is the transactional outbox CRUD stages lifecycle events to,
// satisfied by *outbox.Outbox. Its consumers receive the raw staged row,
// with no AfterGet pass.
type EventOutbox interface {
	// AppendFor writes an event row through ex; owner is the record's
	// owner id, "" on an entity with no owner field.
	AppendFor(ctx context.Context, ex DBExecutor, owner, eventType string, data any) (string, error)
	// Nudge wakes the relay ahead of its poll interval.
	Nudge()
}

func (ch *CrudHandler) eventData(ctx context.Context, record any) map[string]any {
	data := map[string]any{
		eventKeyEntity: ch.Entity.GetName(),
		eventKeyTable:  ch.Entity.GetTable(),
		eventKeyRecord: ch.withoutCascadeChildren(record),
	}
	if ch.Entity.Config.Scope.MultiTenant {
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

func (ch *CrudHandler) eventOwner(ctx context.Context, record any) (string, bool) {
	if ch.Entity.Config.Scope.OwnerField != "" {
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

// withoutCascadeChildren returns a copy of record minus its cascade-written
// relations, leaving the caller's map intact.
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

// StageEvent writes an entity lifecycle event to the outbox through ch.DB,
// so it must be called inside the write's transaction. It is a no-op
// without an outbox and returns the outbox's error.
func (ch *CrudHandler) StageEvent(ctx context.Context, eventType string, record any) error {
	if ch.Outbox == nil {
		return nil
	}
	owner, _ := ch.eventOwner(ctx, record)
	_, err := ch.Outbox.AppendFor(ctx, ch.DB, owner, eventType, ch.eventData(ctx, record))
	return err
}

// EmitEvent publishes an entity lifecycle event on the live bus, best
// effort and asynchronously, once the write's transaction has committed,
// and nudges the outbox relay when one is configured.
func (ch *CrudHandler) EmitEvent(ctx context.Context, eventType string, record any) {
	if ch.Events != nil {
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

func (ch *CrudHandler) eventStreamReauthInterval() time.Duration {
	if ch.EventStreamReauth >= time.Second {
		return ch.EventStreamReauth
	}
	return 30 * time.Second
}

// EventStream returns a handler that serves this entity's created, updated
// and deleted events as Server-Sent Events, one `event: <type>` and
// `data: <Event JSON>` pair each, filtered to the caller's tenant, owner
// and read scope. It answers 503 with no event bus, 401 or 403 when the
// caller may not read the entity, and 429 with Retry-After when the caller
// holds all its stream seats. The caller's authority is re-checked on each
// delivery and every EventStreamReauth, and the stream closes once it is
// refused. A client too slow for the 32-event buffer loses the oldest.
func (ch *CrudHandler) EventStream() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if ch.Events == nil {
			writeJSONError(w, r, http.StatusServiceUnavailable, reasonEventBusMissing)
			return
		}
		ownerID, ownerOK := ch.RequireOwner(w, r)
		if !ownerOK {
			return
		}
		if ch.Entity.Config.Scope.OwnerField == "" && !ch.Entity.Config.Exposure.Public {
			if _, ok := handler.GetUser(r.Context()); !ok {
				writeJSONError(w, r, http.StatusUnauthorized, handler.ReasonAuthenticationRequired)
				return
			}
		}
		if !ch.RequireTenant(w, r) {
			return
		}
		if !ch.requirePermission(w, r, opRead, "") {
			return
		}

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
		var bufferMu sync.Mutex

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
			bufferMu.Lock()
			defer bufferMu.Unlock()
			select {
			case buf <- event:
			default:
				select {
				case <-buf:
				default:
				}
				buf <- event
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
		if err := sse.WriteComment("subscribed " + entityName); err != nil {
			return
		}

		for {
			select {
			case <-r.Context().Done():
				return
			case <-seat.done:
				return
			case <-reauth.C:
				if !authzHeld() {
					return
				}
			case ev := <-buf:
				if !authzHeld() {
					return
				}
				if !readScopeUnrestricted(liveCtx(), ch.Entity) {
					data, _ := ev.Data.(map[string]any)
					if !ch.readScopeAllowsRecord(data[eventKeyRecord]) {
						continue
					}
				}
				if ch.eventHookScopeHides(r.WithContext(liveCtx()), ev) {
					continue
				}
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

// eventHookScopeHides reports whether the subscriber's BeforeList or
// BeforeGet scope hides the row ev is about.
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

// redactEventRecord returns a copy of ev whose record has been masked and
// run through AfterGet, as a GET would return it.
func (ch *CrudHandler) redactEventRecord(r *http.Request, ev event.Event) event.Event {
	hooked := ch.Hooks != nil && len(ch.Hooks.HooksFor(hook.AfterGet)) > 0
	if !hooked && !ch.hasMasks() {
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
	if ev.Type == event.EntityDeleted {
		return ev
	}

	clone := deepcopy.Map(row)
	id, _ := entity.RowKey(clone, ch.keyCols(), ch.convertKey)
	hctx := context.Background()
	if r != nil {
		hctx = r.Context()
	}
	ch.maskOwn(hctx, clone)
	payload := &hook.GetPayload{Request: r, ID: id, Result: clone}
	if hooked {
		if err := runHooks(ch.Hooks, hookCtx(hctx), hook.AfterGet, payload); err != nil {
			logf("crud: after-get hook failed on %s event; omitting record: %v", ch.Entity.GetName(), err)
			payload.Result = nil
		}
	}

	out := make(map[string]any, len(data))
	maps.Copy(out, data)
	out[eventKeyRecord] = payload.Result
	ev.Data = out
	return ev
}

const ambientTxProbeInterval = 5 * time.Millisecond

var ambientTxProbeTimeout = 2 * time.Minute

var unconfirmedWarnOnce sync.Once

var probeWarnOnce sync.Once

// emitAfterAmbientTx publishes a live-bus event only once the ambient
// transaction tx has committed: through its commit queue when one is on
// ctx, else by waiting for tx to end and checking the write landed.
func (ch *CrudHandler) emitAfterAmbientTx(ctx context.Context, tx *sql.Tx, eventType string, record any) {
	if q, ok := db.CommitQueueFromContext(ctx); ok {
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
		unconfirmedWarnOnce.Do(func() {
			logf("crud: dropping unconfirmed events across all entity types and tables: writes in caller-owned transactions " +
				"with no commit queue " +
				"and no way to confirm they landed; carry the transaction with db.WithTxQueue and drain the queue " +
				"after Commit (see hooks-and-transactions docs)")
		})
		return
	}

	probeWarnOnce.Do(func() {
		logf("crud: a CRUD write joined a caller-owned transaction (db.WithTx with no commit queue); " +
			"its lifecycle event is confirmed by polling the live *sql.Tx, which can race the owner's statements — " +
			"prefer db.WithTxQueue + RunAfterCommit (see hooks-and-transactions docs)")
	})

	data := ch.eventData(ctx, record)
	table := ch.Entity.GetTable()
	pk := ch.keyCols()
	bus := ch.Events
	match := ch.comparableColumns(record)
	softDelete := ch.Entity.Config.Scope.SoftDelete
	if recordMap, ok := record.(map[string]any); ok {
		if recordID, ok := ch.rowID(recordMap); ok {
			softDelete = softDelete && AuditOperationFor(ctx, ch.Entity.GetName(), recordID) != "purge"
		}
	}

	go func() {
		pctx, cancel := context.WithTimeout(context.Background(), ambientTxProbeTimeout)
		defer cancel()
		if !waitTxDone(pctx, tx) {
			logf("crud: dropping %s for %s/%v: ambient transaction still open after %s",
				eventType, table, fmt.Sprint(id...), ambientTxProbeTimeout)
			return
		}

		committed, err := landed(pctx, base, eventType, table, pk, id, match, softDelete)
		if err != nil {
			logf("crud: dropping %s for %s/%v: cannot confirm the ambient transaction committed: %v",
				eventType, table, fmt.Sprint(id...), err)
			return
		}
		if !committed {
			return
		}
		bus.EmitAsync(context.Background(), event.Event{Type: eventType, Data: data})
	}()
}

// waitTxDone reports true once tx has committed or rolled back, false if
// ctx ends first.
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

// landed reports whether the write an event of eventType describes is
// visible on base.
func landed(ctx context.Context, base *sql.DB, eventType, table string, pk []string, id []any, match map[string]any, softDelete bool) (bool, error) {
	safeTable, err := query.SafeIdent(table)
	if err != nil {
		return false, err
	}
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
	if eventType == event.EntityDeleted && softDelete {
		where = append(where, "deleted_at IS NOT NULL")
	}
	stmt := fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE %s",
		query.QuoteFolded(safeTable), strings.Join(where, " AND "))
	if err := base.QueryRowContext(ctx, stmt, args...).Scan(&n); err != nil {
		return false, err
	}
	if eventType == event.EntityDeleted && !softDelete {
		return n == 0, nil
	}
	return n > 0, nil
}

// comparableColumns maps a record's non-key scalar values, NULL included,
// onto their columns.
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

func (ch *CrudHandler) recordPrimaryKey(record any) ([]any, bool) {
	m, ok := record.(map[string]any)
	if !ok {
		return nil, false
	}
	return ch.rowKeyVals(m)
}
