package crud

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/access"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/event"
	"github.com/DonaldMurillo/gofastr/framework/hook"
)

// Errors RunTransition and the state write check answer with. In-process
// callers map them the way writeCRUDError does.
var (
	// ErrNoStates: the entity declares no EntityConfig.States.
	ErrNoStates = errors.New("crud: entity declares no states")
	// ErrUnknownTransition: no move has that key (404 on the route, which
	// also answers it for a System move).
	ErrUnknownTransition = errors.New("crud: no such move")
	// ErrStateOverrideUnaudited: WithStateOverride changed a state field
	// on an entity no audit log records. The override's only trail is
	// the audit row, so it is refused without one (App.WithAuditLog).
	ErrStateOverrideUnaudited = errors.New("crud: a state override needs an audit log on the entity")
	// ErrStateOverrideNoReason: WithStateOverride was given an empty
	// reason.
	ErrStateOverrideNoReason = errors.New("crud: a state override needs a reason")
	// ErrBulkStateWrite: TypedQuery.UpdateAll named the state field or a
	// stamp. One value written onto many rows is a move per row, so run
	// RunTransition per record instead.
	ErrBulkStateWrite = errors.New("crud: a bulk update cannot change the state field or a stamp; run the move per record")
)

// StateError refuses a write that changes the state field or a stamp
// outside a move. It answers 422 and names the moves open from the
// stored value (an update) or the values a create may start at.
type StateError struct {
	Field   string   // the state field or stamp the write changed
	Current string   // the stored state; empty on a create
	Moves   []string // the non-system moves open from Current
	Initial []string // on a create: the values it may set
	create  bool
}

func (e *StateError) Error() string { return e.message() }

func (e *StateError) message() string {
	if e.create {
		if e.Initial == nil {
			return fmt.Sprintf("%s is set by a move, not on create", e.Field)
		}
		return fmt.Sprintf("a new record starts at %s", strings.Join(e.Initial, " or "))
	}
	if len(e.Moves) == 0 {
		return fmt.Sprintf("%s changes only through a move, and none is open from %q", e.Field, e.Current)
	}
	return fmt.Sprintf("%s changes only through a move; open from %q: %s", e.Field, e.Current, strings.Join(e.Moves, ", "))
}

// TransitionConflictError refuses a move whose From does not hold the
// record's current value, including the race where another write moved
// it first. It answers 409 and names the moves that are open.
type TransitionConflictError struct {
	Key     string
	Current string
	Moves   []string
}

func (e *TransitionConflictError) Error() string {
	if len(e.Moves) == 0 {
		return fmt.Sprintf("move %q is not open from %q, and no move is", e.Key, e.Current)
	}
	return fmt.Sprintf("move %q is not open from %q; open: %s", e.Key, e.Current, strings.Join(e.Moves, ", "))
}

type stateOverrideKey struct{}

// WithStateOverride lets trusted Go code (seeds, imports, repair jobs)
// write an entity's state field and stamps directly, outside a move. It is
// set only from Go, never from a request value. Every write that changes
// a guarded column under it is audited: the audit row (App.WithAuditLog)
// carries the reason and the operation "state_override" on an update, and
// the write is refused on an entity with no audit log. WithServerWrites
// does not release the state field; this does, and only this.
//
// TypedQuery.UpdateAll refuses guarded columns even under the override:
// a bulk UPDATE runs no hooks, so it would leave no audit row.
func WithStateOverride(ctx context.Context, reason string) context.Context {
	return context.WithValue(ctx, stateOverrideKey{}, stateOverride{reason: strings.TrimSpace(reason)})
}

type stateOverride struct{ reason string }

// StateOverrideReason returns the reason WithStateOverride put on ctx, or
// "" when there is none. The audit log writes it to the row's reason
// column.
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
// running, or "" outside RunTransition. A BeforeUpdate hook that needs to
// tell a move from an edit reads it.
func TransitionFromContext(ctx context.Context) string {
	k, _ := ctx.Value(transitionKey{}).(string)
	return k
}

// enforcedStates returns the entity's states when the CRUD handler
// enforces them: declared and not Advisory.
func (ch *CrudHandler) enforcedStates() *entity.StatesConfig {
	st := ch.Entity.Config.States
	if st == nil || st.Advisory {
		return nil
	}
	return st
}

// stateOverrideColumn reports whether col is a guarded column ctx's state
// override releases, so the field loops write it even when the schema
// marks it ReadOnly or Hidden.
func (ch *CrudHandler) stateOverrideColumn(ctx context.Context, col string) bool {
	st := ch.enforcedStates()
	return st != nil && hasStateOverride(ctx) && slices.Contains(st.Guarded(), col)
}

// allowStateOverride is the gate every guarded-column change under
// WithStateOverride passes: a reason, and an audit log on the entity.
func (ch *CrudHandler) allowStateOverride(ctx context.Context) error {
	if StateOverrideReason(ctx) == "" {
		return ErrStateOverrideNoReason
	}
	if !ch.Entity.Audited() {
		return ErrStateOverrideUnaudited
	}
	return nil
}

// openMoves returns the non-system moves open from value: the ones a
// caller could run.
func openMoves(st *entity.StatesConfig, value string) []string {
	var keys []string
	for _, key := range st.Open(value) {
		if t, _ := st.Transition(key); !t.System {
			keys = append(keys, key)
		}
	}
	return keys
}

// checkStateCreate is the state check for a create, run after the
// BeforeCreate hooks so a hook cannot set the field either. The state
// field may be absent (the Default, which the boot check holds to
// Initial) or an Initial value; a stamp may be absent or null. Under
// WithStateOverride any value passes once allowStateOverride does.
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

// checkStateUpdate is the state check for an update of row id, run after
// the BeforeUpdate hooks and just before the SQL. A guarded column sent
// with its stored value is dropped from body: writing it back is not a
// change, and dropping it means a concurrent move can never be clobbered
// by a stale round-trip. A different value is refused with a StateError
// naming the open moves. Under WithStateOverride, a change passes once
// allowStateOverride does, and the audit row's operation becomes
// "state_override"; the returned ctx carries that.
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
	// Read under the UPDATE's own WHERE, columns named directly so a
	// Hidden state field is still compared.
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
	return ctx, &StateError{Field: changed[0], Current: current, Moves: openMoves(st, current)}
}

// checkStateUpsert is the state check for UpsertOne, run after its
// preflight: an update of the row the body's key names when that row is
// visible, a create otherwise. UpsertOne also keeps every guarded column
// out of its DO UPDATE SET (unless overridden), so the insert arm's
// Default can never clobber a stored state on conflict.
func (ch *CrudHandler) checkStateUpsert(ctx context.Context, r *http.Request, body map[string]any) (context.Context, error) {
	if ch.enforcedStates() == nil {
		return ctx, nil
	}
	if id, ok := body[ch.PrimaryKey]; ok && id != nil {
		next, err := ch.checkStateUpdate(ctx, r, fmt.Sprint(id), body)
		if !errors.Is(err, errNotFound) {
			return next, err
		}
	}
	return ctx, ch.checkStateCreate(ctx, body)
}

// upsertSetsGuarded reports whether UpsertOne's DO UPDATE SET may name
// col: not a guarded column, unless a state override releases it.
func (ch *CrudHandler) upsertSetsGuarded(ctx context.Context, col string) bool {
	st := ch.enforcedStates()
	return st == nil || hasStateOverride(ctx) || !slices.Contains(st.Guarded(), col)
}

// checkStateBulk refuses a TypedQuery.UpdateAll body that names a guarded
// column. Comparing against a stored value is per row, and a bulk UPDATE
// runs no hooks to audit an override, so the refusal has no exception.
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

// sameGuardedValue reports whether given is stored written back: the same
// enum string, or the same date or instant however the driver and the
// client spell it. Anything it cannot read as the field's type is a
// change, so the check fails closed.
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
		// The state field is an Enum and every stamp a Date or
		// Timestamp (registration refuses anything else), so this arm
		// compares an Enum's strings.
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

// timeLayouts are the spellings a Date or Timestamp arrives in: JSON
// clients send RFC 3339, SQLite stores the driver's text forms.
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

// RunTransition runs the move key on record id: the one way an enforced
// state field changes. In order:
//
//   - The caller may move: a System move skips this (no route reaches
//     it; only Go code calls it); any other asks the entity's update
//     permission and, when the move sets one, its Permission, both about
//     this record so a Decider can answer per row. WithServerWrites does
//     not skip it.
//   - A read of the row under tenant, owner-write and soft-delete scope:
//     not visible answers errNotFound (404), so another owner's id
//     reveals nothing. A visible row whose value is not in From answers a
//     TransitionConflictError (409) naming the open moves.
//   - BeforeUpdate hooks, with the move's writes as the body and its key
//     on ctx (TransitionFromContext). A hook can veto; changes it makes to
//     the body are not written.
//   - One conditional UPDATE under the same scope, pinned to From:
//     SET field = To, stamp = <server's UTC date or time>. Zero rows means
//     another write moved the record first: a TransitionConflictError.
//   - AfterUpdate hooks, the audit row (operation "transition:<key>") and
//     the entity.updated event, in the same transaction.
//
// It works on an Advisory entity too, where the moves stay as calls.
func (ch *CrudHandler) RunTransition(ctx context.Context, id, key string) (map[string]any, error) {
	st := ch.Entity.Config.States
	if st == nil {
		return nil, ErrNoStates
	}
	t, ok := st.Transition(key)
	if !ok {
		return nil, ErrUnknownTransition
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
		if t.Permission != "" && !access.CanResource(ctx, access.Permission(t.Permission), access.Ref{Type: ch.Entity.GetName(), ID: id}) {
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

// transitionDeniedError is RunTransition's permission refusal (403 on
// the route).
type transitionDeniedError struct{ perm string }

func (e *transitionDeniedError) Error() string {
	return "access denied: missing permission " + e.perm
}

// doTransition runs the move inside the caller's transaction; see
// RunTransition for the steps.
func (ch *CrudHandler) doTransition(ctx context.Context, r *http.Request, id string, st *entity.StatesConfig, t entity.Transition) (map[string]any, error) {
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
		return nil, &TransitionConflictError{Key: t.Key, Current: current, Moves: openMoves(st, current)}
	}
	ctx = WithAuditPreImage(ctx, pre)
	ctx = withAuditOperation(ctx, ch.Entity.GetName(), id, "transition:"+t.Key)
	ctx = context.WithValue(ctx, transitionKey{}, t.Key)

	writes := map[string]any{st.Field: t.To}
	if t.Stamp != "" {
		writes[t.Stamp] = stampValue(ch.fieldByName(t.Stamp))
	}
	if ch.Hooks != nil {
		body := make(map[string]any, len(writes))
		for k, v := range writes {
			body[k] = v
		}
		if err := ch.Hooks.ExecuteHooks(ctx, hook.BeforeUpdate, body); err != nil {
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
	ub.Where(ch.PrimaryKey+" = $1", id)
	placeholders := make([]string, len(t.From))
	from := make([]any, len(t.From))
	for i, v := range t.From {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		from[i] = v
	}
	ub.Where(st.Field+" IN ("+strings.Join(placeholders, ", ")+")", from...)
	ch.ApplyTenantScopeUpdate(ub, r)
	ch.ApplyOwnerScopeUpdate(ub, r)
	if ch.Entity.Config.Scope.SoftDelete {
		ub.Where("deleted_at IS NULL")
	}
	visFields := ch.visibleFields()
	ub.Returning(visFields...)
	sqlStr, args := ub.Build()
	result, err := ch.scanOne(ch.DB.QueryRowContext(ctx, sqlStr, args...), visFields)
	if errors.Is(err, sql.ErrNoRows) {
		// Another write moved the record (or removed it) between the read
		// and this statement. Re-read to name where it stands now.
		now, rerr := ch.selectMoveTarget(ctx, r, id, cols)
		if rerr != nil {
			return nil, rerr
		}
		cur, _ := now[ch.convertKey(st.Field)].(string)
		return nil, &TransitionConflictError{Key: t.Key, Current: cur, Moves: openMoves(st, cur)}
	}
	if err != nil {
		return nil, fmt.Errorf("transition %s: %w", t.Key, err)
	}
	if ch.Hooks != nil {
		if err := ch.Hooks.ExecuteHooks(ctx, hook.AfterUpdate, result); err != nil {
			return nil, fmt.Errorf("after-update hook: %w", err)
		}
	}
	if err := ch.StageEvent(ctx, event.EntityUpdated, result); err != nil {
		return nil, fmt.Errorf("stage event: %w", err)
	}
	return result, nil
}

// selectMoveTarget reads cols of row id under the move's WHERE (tenant,
// owner-write scope, deleted_at IS NULL); absent answers errNotFound.
func (ch *CrudHandler) selectMoveTarget(ctx context.Context, r *http.Request, id string, cols []string) (map[string]any, error) {
	qb := query.Select(cols...).
		From(ch.Entity.GetTable()).
		Where(ch.PrimaryKey+" = $1", id)
	ch.ApplyTenantScope(qb, r)
	applyOwnerScope(ch, qb, r, false)
	if ch.Entity.Config.Scope.SoftDelete {
		qb.Where("deleted_at IS NULL")
	}
	sqlStr, args := qb.Build()
	row, err := ch.scanOne(ch.DB.QueryRowContext(ctx, sqlStr, args...), cols)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errNotFound
	}
	return row, err
}

// stampValue is the server's current UTC date for a Date stamp, or the
// current time for a Timestamp one, bound the way updated_at is.
func stampValue(f schema.Field) any {
	if f.Type == schema.Date {
		return time.Now().UTC().Format(time.DateOnly)
	}
	return generateFieldValue(schema.AutoTimestamp)
}

// nonNil keeps an empty move list a JSON [] rather than null.
func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// Transition is the HTTP handler for POST <path>/{id}/transitions/{key}:
// RunTransition behind the update route's gates (owner, tenant, session,
// the update permission for this id) and its JSON content-type check,
// which a cross-site form cannot satisfy. The route takes no body. A
// System move answers 404, as an unknown key does. The response is the
// moved record in Update's envelope.
func (ch *CrudHandler) Transition() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if err := enforceJSONContentType(r); err != nil {
			writeJSONError(w, http.StatusUnsupportedMediaType, "unsupported media type")
			return
		}
		if !ch.requireScope(w, r, opUpdate) {
			return
		}
		id, key := r.PathValue("id"), r.PathValue("key")
		if t, ok := ch.Entity.Config.States.Transition(key); !ok || t.System {
			writeJSONError(w, http.StatusNotFound, "not found")
			return
		}
		result, err := ch.RunTransition(WithAuditRequest(r.Context(), r), id, key)
		if err != nil {
			writeCRUDError(w, err)
			return
		}
		// The move is committed; a read-scope probe that fails hides the
		// row rather than risk returning one the caller may not read.
		hidden, herr := ch.readScopeHidesRow(r.Context(), result)
		ch.writeUpdated(w, r, result, hidden || herr != nil)
	}
}

// RoutableTransitions returns the moves the transition route and the MCP
// tools serve: every non-system move, in declared order.
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
