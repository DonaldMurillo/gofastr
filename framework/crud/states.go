package crud

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/access"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/event"
	"github.com/DonaldMurillo/gofastr/framework/hook"
)

// Errors RunTransition and the state write checks answer with.
var (
	ErrNoStates               = errors.New("crud: entity declares no states")
	ErrUnknownTransition      = errors.New("crud: no such move")
	ErrStateOverrideUnaudited = errors.New("crud: a state override needs an audit log on the entity")
	ErrStateOverrideNoReason  = errors.New("crud: a state override needs a reason")
	ErrBulkStateWrite         = errors.New("crud: a bulk update cannot change the state field or a stamp; run the move per record")
	ErrReentrantMove          = errors.New("crud: a move cannot run on a record while a write of that record is in flight")
)

type recordWrite struct {
	entity, id string
	parent     *recordWrite
}

type recordWriteKey struct{}

func withRecordWrite(ctx context.Context, entity, id string) context.Context {
	parent, _ := ctx.Value(recordWriteKey{}).(*recordWrite)
	return context.WithValue(ctx, recordWriteKey{}, &recordWrite{entity: entity, id: id, parent: parent})
}

func recordWriteInFlight(ctx context.Context, entity, id string) bool {
	for w, _ := ctx.Value(recordWriteKey{}).(*recordWrite); w != nil; w = w.parent {
		if w.entity == entity && w.id == id {
			return true
		}
	}
	return false
}

// StateError refuses a write that changes the state field or a stamp
// outside a move (422). It names the moves open from the stored value, or
// on a create the values it may start at, unless the caller cannot read
// the record.
type StateError struct {
	Field   string   // the state field or stamp the write changed
	Current string   // the stored state; empty on a create
	Moves   []string // the non-system moves open from Current
	Initial []string // on a create: the values it may set
	create  bool
	hidden  bool // the caller's ReadScope hides the record: no state named
}

func (e *StateError) Error() string {
	p, params := e.Phrased()
	return p.Format(params)
}

// Phrased implements i18n.Phrased.
func (e *StateError) Phrased() (i18n.Phrase, map[string]any) {
	if e.create {
		if e.Initial == nil {
			return reasonStateSetByMove, map[string]any{"field": e.Field}
		}
		return reasonStateInitial, map[string]any{"initial": strings.Join(e.Initial, " or ")}
	}
	if e.hidden {
		return reasonStateMoveOnly, map[string]any{"field": e.Field}
	}
	if len(e.Moves) == 0 {
		return reasonStateNoMoves, map[string]any{"field": e.Field, "current": strconv.Quote(e.Current)}
	}
	return reasonStateMovesOpen, map[string]any{"field": e.Field, "current": strconv.Quote(e.Current), "moves": strings.Join(e.Moves, ", ")}
}

// TransitionConflictError refuses a move whose From does not hold the
// record's current value (409), naming the moves that are open unless the
// caller cannot read the record.
type TransitionConflictError struct {
	Key     string
	Current string
	Moves   []string
	hidden  bool // the caller's ReadScope hides the record: no state named
}

func (e *TransitionConflictError) Error() string {
	p, params := e.Phrased()
	return p.Format(params)
}

// Phrased implements i18n.Phrased.
func (e *TransitionConflictError) Phrased() (i18n.Phrase, map[string]any) {
	if e.hidden {
		return reasonMoveNotOpen, map[string]any{"key": strconv.Quote(e.Key)}
	}
	if len(e.Moves) == 0 {
		return reasonMoveNoneOpen, map[string]any{"key": strconv.Quote(e.Key), "current": strconv.Quote(e.Current)}
	}
	return reasonMoveOthersOpen, map[string]any{"key": strconv.Quote(e.Key), "current": strconv.Quote(e.Current), "moves": strings.Join(e.Moves, ", ")}
}

type stateOverrideKey struct{}

// WithStateOverride lets trusted Go code write an entity's state field
// and stamps outside a move; set it only from Go, never from request data.
// A write that changes a guarded column under it needs a non-empty reason
// and an audit log on the entity, and an update is audited as
// "state_override". WithServerWrites does not release the state field.
// TypedQuery.UpdateAll refuses guarded columns even under the override.
func WithStateOverride(ctx context.Context, reason string) context.Context {
	return context.WithValue(ctx, stateOverrideKey{}, stateOverride{reason: strings.TrimSpace(reason)})
}

type stateOverride struct{ reason string }

// StateOverrideReason returns the reason WithStateOverride put on ctx, or
// "".
func StateOverrideReason(ctx context.Context) string {
	o, _ := ctx.Value(stateOverrideKey{}).(stateOverride)
	return o.reason
}

func hasStateOverride(ctx context.Context) bool {
	_, ok := ctx.Value(stateOverrideKey{}).(stateOverride)
	return ok
}

type transitionKey struct{}

// TransitionFromContext returns the key of the move whose hooks are
// running, or "" outside RunTransition.
func TransitionFromContext(ctx context.Context) string {
	k, _ := ctx.Value(transitionKey{}).(string)
	return k
}

// enforcedStates is the entity's states unless absent or Advisory.
func (ch *CrudHandler) enforcedStates() *entity.StatesConfig {
	st := ch.Entity.Config.States
	if st == nil || st.Advisory {
		return nil
	}
	return st
}

// writesStateDefault reports whether a create writes f's Default though
// f is ReadOnly or Hidden: only the state field's.
func (ch *CrudHandler) writesStateDefault(f schema.Field) bool {
	st := ch.Entity.Config.States
	return st != nil && f.Name == st.Field
}

func (ch *CrudHandler) stateOverrideColumn(ctx context.Context, col string) bool {
	st := ch.enforcedStates()
	return st != nil && hasStateOverride(ctx) && slices.Contains(st.Guarded(), col)
}

func (ch *CrudHandler) allowStateOverride(ctx context.Context) error {
	if StateOverrideReason(ctx) == "" {
		return ErrStateOverrideNoReason
	}
	if !ch.Entity.Audited() {
		return ErrStateOverrideUnaudited
	}
	return nil
}

func openMoves(st *entity.StatesConfig, value string) []string {
	var keys []string
	for _, key := range st.Open(value) {
		if t, _ := st.Transition(key); !t.System {
			keys = append(keys, key)
		}
	}
	return keys
}

// checkStateCreate refuses a create that sets the state field to a value
// outside Initial, or sets a stamp, unless an override allows it.
func (ch *CrudHandler) checkStateCreate(ctx context.Context, body map[string]any) error {
	st := ch.enforcedStates()
	if st == nil {
		return nil
	}
	initial := st.InitialValues(ch.Entity.Config.Fields)
	var changed string
	if v, ok := body[st.Field]; ok {
		if s, isStr := v.(string); !isStr || !slices.Contains(initial, s) {
			changed = st.Field
		}
	}
	for _, col := range st.Guarded()[1:] {
		if v, ok := body[col]; ok {
			if v == nil {
				delete(body, col)
				continue
			}
			if changed == "" {
				changed = col
			}
		}
	}
	if changed == "" {
		return nil
	}
	if hasStateOverride(ctx) {
		return ch.allowStateOverride(ctx)
	}
	e := &StateError{Field: changed, create: true}
	if changed == st.Field {
		e.Initial = initial
	}
	return e
}

// checkStateUpdate refuses an update that changes a guarded column of row
// id unless an override allows it, and drops one sent with its stored
// value from body.
func (ch *CrudHandler) checkStateUpdate(ctx context.Context, r *http.Request, id string, body map[string]any) (context.Context, error) {
	st := ch.enforcedStates()
	if st == nil {
		return ctx, nil
	}
	guarded := st.Guarded()
	touched := false
	for _, col := range guarded {
		if _, ok := body[col]; ok {
			touched = true
			break
		}
	}
	if !touched {
		return ctx, nil
	}
	stored, err := ch.selectMoveTarget(ctx, r, id, guarded)
	if err != nil {
		return ctx, err
	}
	override := hasStateOverride(ctx)
	var changed []string
	for _, col := range guarded {
		given, ok := body[col]
		if !ok {
			continue
		}
		if sameGuardedValue(ch.fieldByName(col), stored[ch.convertKey(col)], given) {
			if !override {
				delete(body, col)
			}
			continue
		}
		changed = append(changed, col)
	}
	if len(changed) == 0 {
		return ctx, nil
	}
	if override {
		if err := ch.allowStateOverride(ctx); err != nil {
			return ctx, err
		}
		return withAuditOperation(ctx, ch.Entity.GetName(), id, "state_override"), nil
	}
	current, _ := stored[ch.convertKey(st.Field)].(string)
	e := &StateError{Field: changed[0]}
	e.Current, e.Moves, e.hidden = ch.refusalState(ctx, r, id, st, current)
	return ctx, e
}

// checkStateUpsert checks an upsert as an update of the visible row its
// key names, else as a create.
func (ch *CrudHandler) checkStateUpsert(ctx context.Context, r *http.Request, body map[string]any) (context.Context, error) {
	if ch.enforcedStates() == nil {
		return ctx, nil
	}
	if id, ok := entity.RowKey(body, ch.keyCols(), nil); ok {
		next, err := ch.checkStateUpdate(ctx, r, id, body)
		if !errors.Is(err, errNotFound) {
			return next, err
		}
	}
	return ctx, ch.checkStateCreate(ctx, body)
}

// upsertSetsGuarded reports whether UpsertOne's DO UPDATE SET may name
// col: a guarded column only when sent under an override that passes.
func (ch *CrudHandler) upsertSetsGuarded(ctx context.Context, col string, sent bool) bool {
	st := ch.enforcedStates()
	if st == nil || !slices.Contains(st.Guarded(), col) {
		return true
	}
	return sent && hasStateOverride(ctx) && ch.allowStateOverride(ctx) == nil
}

// checkStateBulk refuses a TypedQuery.UpdateAll body that names a guarded
// column.
func (ch *CrudHandler) checkStateBulk(body map[string]any) error {
	st := ch.enforcedStates()
	if st == nil {
		return nil
	}
	for _, col := range st.Guarded() {
		if _, ok := body[col]; ok {
			return ErrBulkStateWrite
		}
	}
	return nil
}

func (ch *CrudHandler) fieldByName(name string) schema.Field {
	for _, f := range ch.Entity.GetFields() {
		if f.Name == name {
			return f
		}
	}
	return schema.Field{Name: name}
}

// sameGuardedValue reports whether given is stored written back, however
// the driver and the client spell a date or instant.
func sameGuardedValue(f schema.Field, stored, given any) bool {
	if stored == nil || given == nil {
		return stored == nil && given == nil
	}
	switch f.Type {
	case schema.Date:
		a, okA := dateOf(stored)
		b, okB := dateOf(given)
		return okA && okB && a == b
	case schema.Timestamp:
		a, okA := instantOf(stored)
		b, okB := instantOf(given)
		return okA && okB && a.Equal(b)
	default:
	}
	s, okS := stringOf(stored)
	g, okG := given.(string)
	return okS && okG && s == g
}

func stringOf(v any) (string, bool) {
	switch x := v.(type) {
	case string:
		return x, true
	case []byte:
		return string(x), true
	}
	return "", false
}

var timeLayouts = []string{
	time.RFC3339Nano,
	"2006-01-02 15:04:05.999999999-07:00",
	"2006-01-02 15:04:05.999999999Z07:00",
	"2006-01-02T15:04:05.999999999",
	"2006-01-02 15:04:05.999999999",
	"2006-01-02",
}

func instantOf(v any) (time.Time, bool) {
	if t, ok := v.(time.Time); ok {
		return t, true
	}
	s, ok := stringOf(v)
	if !ok {
		return time.Time{}, false
	}
	for _, layout := range timeLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

func dateOf(v any) (string, bool) {
	if t, ok := v.(time.Time); ok {
		return t.Format(time.DateOnly), true
	}
	s, ok := stringOf(v)
	if !ok || len(s) < len(time.DateOnly) {
		return "", false
	}
	if _, err := time.Parse(time.DateOnly, s[:len(time.DateOnly)]); err != nil {
		return "", false
	}
	return s[:len(time.DateOnly)], true
}

// RunTransition runs the move key on record id, the one way an enforced
// state field changes, and returns the moved row. A non-System move needs
// the update permission and the move's own Permission for this record
// (WithServerWrites does not skip them); a row outside the caller's
// tenant, owner write or soft-delete scope answers errNotFound; a row
// whose value is not in From, or that another write moved first, answers
// a TransitionConflictError. BeforeUpdate hooks see the move's writes and
// its key (TransitionFromContext) and may veto; the audit operation is
// "transition:<key>". It also runs on an Advisory entity. Called from a
// hook on the record being written, it answers ErrReentrantMove.
func (ch *CrudHandler) RunTransition(ctx context.Context, id, key string) (map[string]any, error) {
	ctx = withEventStamps(ctx)
	st := ch.Entity.Config.States
	if st == nil {
		return nil, ErrNoStates
	}
	t, ok := st.Transition(key)
	if !ok {
		return nil, ErrUnknownTransition
	}
	if recordWriteInFlight(ctx, ch.Entity.GetName(), id) {
		return nil, ErrReentrantMove
	}
	if ch.Entity.Config.Scope.OwnerField != "" && !serverWrites(ctx) {
		if err := ch.requireOwnerContext(ctx); err != nil {
			return nil, err
		}
	}
	if err := ch.requireTenantContext(ctx); err != nil {
		return nil, err
	}
	if !t.System {
		if !ch.itemPermitted(ctx, opUpdate, id) {
			return nil, &transitionDeniedError{perm: ch.permissionForOp(opUpdate)}
		}
		if t.Permission != "" && !access.CanResourceExact(ctx, access.Permission(t.Permission), access.Ref{Type: ch.Entity.GetName(), ID: id}) {
			return nil, &transitionDeniedError{perm: t.Permission}
		}
	}
	req := syntheticRequest(ctx, http.MethodPatch, "/")
	var result map[string]any
	err := ch.inTx(ctx, func(ctx context.Context, ch *CrudHandler) error {
		res, err := ch.doTransition(ctx, req, id, st, t)
		if err != nil {
			return err
		}
		result = res
		return nil
	})
	if err != nil {
		return nil, err
	}
	ch.EmitEvent(ctx, event.EntityUpdated, result)
	return result, nil
}

type transitionDeniedError struct{ perm string }

func (e *transitionDeniedError) Error() string {
	p, params := e.Phrased()
	return p.Format(params)
}

// Phrased implements i18n.Phrased.
func (e *transitionDeniedError) Phrased() (i18n.Phrase, map[string]any) {
	if e.perm == permRefused {
		return reasonOpRefused, nil
	}
	return handler.ReasonMissingPermission, map[string]any{"permission": e.perm}
}

func (ch *CrudHandler) doTransition(ctx context.Context, r *http.Request, id string, st *entity.StatesConfig, t entity.Transition) (map[string]any, error) {
	lock := "UPDATE " + query.QuoteFolded(ch.Entity.GetTable()) + " SET " + query.QuoteFolded(st.Field) + " = " + query.QuoteFolded(st.Field) + " WHERE 1 = 0"
	if _, err := ch.conn(ctx).ExecContext(ctx, lock); err != nil {
		return nil, err
	}
	if err := ch.refuseHiddenTarget(ctx, r, id, targetLive); err != nil {
		return nil, err
	}
	if err := ch.stampRow(ctx, id); err != nil {
		return nil, err
	}
	cols := ch.visibleFields()
	if !slices.Contains(cols, st.Field) {
		cols = append(slices.Clone(cols), st.Field)
	}
	pre, err := ch.selectMoveTarget(ctx, r, id, cols)
	if err != nil {
		return nil, err
	}
	current, _ := pre[ch.convertKey(st.Field)].(string)
	if !slices.Contains(t.From, current) {
		e := &TransitionConflictError{Key: t.Key}
		e.Current, e.Moves, e.hidden = ch.refusalState(ctx, r, id, st, current)
		return nil, e
	}
	ctx = WithAuditPreImage(ctx, pre)
	ctx = withAuditOperation(ctx, ch.Entity.GetName(), id, "transition:"+t.Key)
	ctx = context.WithValue(ctx, transitionKey{}, t.Key)
	ctx = withRecordWrite(ctx, ch.Entity.GetName(), id)

	writes := map[string]any{st.Field: t.To}
	if t.Stamp != "" {
		writes[t.Stamp] = stampValue(ch.fieldByName(t.Stamp))
	}
	if ch.Hooks != nil {
		body := make(map[string]any, len(writes))
		for k, v := range writes {
			body[k] = v
		}
		if err := runHooks(ch.Hooks, ctx, hook.BeforeUpdate, body); err != nil {
			return nil, &beforeHookError{err: err}
		}
	}

	ub := query.Update(ch.Entity.GetTable()).Set(st.Field, t.To)
	if t.Stamp != "" {
		ub.Set(t.Stamp, ch.bindJSONValue(t.Stamp, writes[t.Stamp]))
	}
	if col := autoUpdatedAtColumn(ch.Entity); col != "" {
		ub.Set(col, generateFieldValue(schema.AutoTimestamp))
	}
	kc, ka := ch.keyWhere(id)
	ub.Where(kc, ka...)
	placeholders := make([]string, len(t.From))
	from := make([]any, len(t.From))
	for i, v := range t.From {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		from[i] = v
	}
	ub.Where(query.QuoteKeyword(st.Field)+" IN ("+strings.Join(placeholders, ", ")+")", from...)
	ch.ApplyTenantScopeUpdate(ub, r)
	ch.ApplyOwnerScopeUpdate(ub, r)
	if ch.Entity.Config.Scope.SoftDelete {
		ub.Where("deleted_at IS NULL")
	}
	visFields := ch.visibleFields()
	ub.Returning(ch.keyedCols(visFields)...)
	sqlStr, args := ub.Build()
	result, err := ch.writeOwn(ctx, id, visFields, sqlStr, args)
	if errors.Is(err, sql.ErrNoRows) {
		now, rerr := ch.selectMoveTarget(ctx, r, id, cols)
		if rerr != nil {
			return nil, rerr
		}
		cur, _ := now[ch.convertKey(st.Field)].(string)
		e := &TransitionConflictError{Key: t.Key}
		e.Current, e.Moves, e.hidden = ch.refusalState(ctx, r, id, st, cur)
		return nil, e
	}
	if err != nil {
		return nil, fmt.Errorf("transition %s: %w", t.Key, err)
	}
	if ch.Hooks != nil {
		if err := ch.afterRow(ctx, hook.AfterUpdate, result); err != nil {
			return nil, fmt.Errorf("after-update hook: %w", err)
		}
	}
	if err := ch.StageEvent(ctx, event.EntityUpdated, result); err != nil {
		return nil, fmt.Errorf("stage event: %w", err)
	}
	return result, nil
}

func (ch *CrudHandler) selectMoveTarget(ctx context.Context, r *http.Request, id string, cols []string) (map[string]any, error) {
	kc, ka := ch.keyWhere(id)
	qb := query.Select(ch.keyedCols(cols)...).
		From(ch.Entity.GetTable()).
		Where(kc, ka...)
	ch.ApplyTenantScope(qb, r)
	applyOwnerScope(ch, qb, r, false)
	if ch.Entity.Config.Scope.SoftDelete {
		qb.Where("deleted_at IS NULL")
	}
	sqlStr, args := qb.Build()
	row, err := ch.scanOwn(ctx, id, cols, sqlStr, args)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errNotFound
	}
	return row, err
}

// refusalState is the stored state and open moves a refusal names, or
// hidden when the caller may not read the field or the record.
func (ch *CrudHandler) refusalState(ctx context.Context, r *http.Request, id string, st *entity.StatesConfig, current string) (string, []string, bool) {
	if ch.fieldByName(st.Field).Hidden {
		return "", nil, true
	}
	if hidden, err := ch.readScopeHidesRow(ctx, ch.keyPayload(id)); hidden || err != nil {
		return "", nil, true
	}
	if ch.Hooks != nil && len(ch.Hooks.HooksFor(hook.BeforeGet)) > 0 {
		if hr, apply := scopeRequest(ctx, r); apply {
			vals, ok := ch.idVals(id)
			if !ok || ch.hookScopeHidesKey(ctx, hr, vals, id, false) {
				return "", nil, true
			}
		}
	}
	return current, openMoves(st, current), false
}

func stampValue(f schema.Field) any {
	if f.Type == schema.Date {
		return time.Now().UTC().Format(time.DateOnly)
	}
	return generateFieldValue(schema.AutoTimestamp)
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// Transition is the HTTP handler for POST <path>/{id}/transitions/{key}:
// RunTransition behind the update route's gates and JSON content-type
// check. It takes no body; an unknown or System move answers 404. It
// answers the moved record as Update does, or its id only when the
// caller's read scope hides it.
func (ch *CrudHandler) Transition() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if err := enforceJSONOnly(r); err != nil {
			writeContentTypeError(w, r, err)
			return
		}
		if !ch.requireScope(w, r, opUpdate) {
			return
		}
		id, key := ch.pathID(r), r.PathValue(transitionParam(ch.keyCols()))
		if t, ok := ch.Entity.Config.States.Transition(key); !ok || t.System {
			writeJSONError(w, r, http.StatusNotFound, i18n.StatusPhrase(http.StatusNotFound))
			return
		}
		result, err := ch.RunTransition(withRealRequest(WithAuditRequest(r.Context(), r), r), id, key)
		if err != nil {
			writeCRUDError(w, r, err, ch.convertKey)
			return
		}
		hidden, herr := ch.readScopeHidesRow(r.Context(), result)
		ch.writeUpdated(w, r, result, hidden || herr != nil)
	}
}

// RoutableTransitions returns every non-System move, in declared order.
func RoutableTransitions(st *entity.StatesConfig) []entity.Transition {
	if st == nil {
		return nil
	}
	var out []entity.Transition
	for _, t := range st.Transitions {
		if !t.System {
			out = append(out, t)
		}
	}
	return out
}
