package entityui

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/DonaldMurillo/gofastr/core-ui/interactive"
	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/access"
	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/filter"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
	"github.com/DonaldMurillo/gofastr/framework/tenant"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

// Bulk actions
//
// A list with bulk on (ListBuilder.Bulk; on in the admin) draws a select
// column and a bulk bar. The bar is a form RPC to POST <api>/_bulk, the
// route App.EntityUI mounts beside each entity's CRUD routes (BulkHandler);
// entityui itself still mounts nothing. The browser names an action and a
// scope: the checked rows, the page's rows, or every match of the list's
// query. The server resolves the selection itself:
//
//   - ids are re-read through the scoped CRUD handler under the caller's
//     own context, so an id from another owner or tenant, or one the read
//     scope hides, drops out before anything runs;
//   - every match rebuilds the list's narrowing (view, search, filter,
//     facets) from the query and reads the matching ids, at most
//     EveryMatchCap. The run goes ahead only when those ids are the ones
//     the screen offered (the bar carries a digest of them), so a row
//     that entered the list after it drew is never touched. A page's
//     .Where pins are not in the query: they shape what a page shows, and
//     a caller who drops them still acts only on rows the API lets them
//     write.
//
// Each record is then asked its write gate (CanUpdateRecordScoped or
// CanDeleteRecordScoped, the routes' per-record Decider question) before
// its write, and a refused record counts as skipped. Up to InRequestCap
// records run in the request. A larger selection is written to the
// snapshot store and queued on Extensions.Jobs (RunBulkJob runs it), or
// refused naming the cap when there is none.
//
// Undo. A bar under ListBuilder.Undo posts undo and its list's path. A
// delete run in the request on a soft-deleting entity then answers a
// toast whose Undo button posts the deleted scope: the ids the run
// deleted that the caller could update before it, restored through the
// CRUD handler under the caller's own context and read back from the
// trash under the same scope, at most InRequestCap. Every restore is
// gated again, so a posted id the caller may not restore is skipped.
// The ids ride the toast header, so a run whose button would pass
// undoActionBudget offers no Undo; the Deleted view restores it.

// bulkBodyLimit caps a bulk request body: an action, a scope, a list query
// and at most a page of ids.
const bulkBodyLimit = 256 << 10

// Bulk scopes, the bar's "Apply to" values, and the record scope a
// record header's action button posts.
const (
	bulkScopeSelected = "selected"
	bulkScopePage     = "page"
	bulkScopeEvery    = "every"
	bulkScopeRecord   = "record"
	// bulkScopeDeleted is Undo's scope: soft-deleted ids, for the
	// restore action only.
	bulkScopeDeleted = "deleted"
)

// bulkRestoreKey is the restore action's key, offered on the deleted
// scope only.
const bulkRestoreKey = "restore"

// undoActionBudget caps the encoded Undo button a delete's toast header
// carries: reverse proxies refuse a response whose headers outgrow a
// few KiB (nginx's default proxy buffer is one 4 KiB page), and the
// delete has already been written by then.
const undoActionBudget = 2 << 10

type bulkKind int

const (
	bulkDelete bulkKind = iota + 1
	bulkSet
	bulkMove
	bulkRun
	bulkRestore
)

// bulkAction is one action the bar offers. key is the option value the
// browser posts back; the server finds it again among the actions it
// offers this caller, so a posted key never reaches a write unchecked.
type bulkAction struct {
	key   string
	kind  bulkKind
	label string
	field string
	value any
	move  string
	perm  string // a move's or an app action's own Permission
	app   Action
}

// bulkActions are the actions the bar offers ctx's caller on m, in bar
// order: delete, set a field, the moves, then the app's bulk actions. The
// collection-level write gates decide what is offered; each record is
// asked again when the action runs.
func (u *UI) bulkActions(ctx context.Context, m *meta) []bulkAction {
	var out []bulkAction
	if m.ch.CanDeleteRecordScoped(ctx, "") {
		out = append(out, bulkAction{key: "delete", kind: bulkDelete, label: i18nui.T(ctx, i18nui.KeyEntityBulkDelete)})
	}
	if !m.ch.CanUpdateRecordScoped(ctx, "") {
		return out
	}
	for _, f := range m.fields {
		if !bulkSettable(m, f) {
			continue
		}
		for _, v := range bulkValues(f) {
			label := i18nui.TVars(ctx, i18nui.KeyEntityBulkSet, map[string]string{
				"field": m.label(ctx, f.Name),
				"value": bulkValueLabel(ctx, m, f, v),
			})
			var value any = v
			if f.Type == schema.Bool {
				value = v == "true"
			}
			out = append(out, bulkAction{key: "set:" + f.Name + ":" + v, kind: bulkSet, label: label, field: f.Name, value: value})
		}
	}
	for _, t := range crud.RoutableTransitions(m.states) {
		if !holdsExact(ctx, m, t.Permission) {
			continue
		}
		name := t.Label
		if name == "" {
			name = t.Key
		}
		out = append(out, bulkAction{
			key: "move:" + t.Key, kind: bulkMove, move: t.Key, perm: t.Permission,
			label: i18nui.TVars(ctx, i18nui.KeyEntityBulkMove, map[string]string{"move": name}),
		})
	}
	for _, a := range m.ext.Actions {
		if !a.Bulk || !holdsExact(ctx, m, a.Permission) {
			continue
		}
		out = append(out, bulkAction{key: "run:" + a.Key, kind: bulkRun, label: actionLabel(a), perm: a.Permission, app: a})
	}
	return out
}

// recordActions are m's app actions, Bulk or not, in declaration order:
// what the record scope may run. The built-in bar actions never run on it
// (the record has its own delete and moves). Who may run each is asked
// about the record itself, by mayRun, so a grant on that one record
// counts where the collection-level holdsExact would refuse it.
func recordActions(m *meta) []bulkAction {
	var out []bulkAction
	for _, a := range m.ext.Actions {
		out = append(out, bulkAction{key: "run:" + a.Key, kind: bulkRun, label: actionLabel(a), perm: a.Permission, app: a})
	}
	return out
}

// restoreActions is the deleted scope's one action, on a soft-deleting
// entity.
func restoreActions(ctx context.Context, m *meta) []bulkAction {
	if !m.e.Config.Scope.SoftDelete {
		return nil
	}
	return []bulkAction{{key: bulkRestoreKey, kind: bulkRestore, label: i18nui.T(ctx, i18nui.KeyEntityRestore)}}
}

func actionLabel(a Action) string {
	if a.Label == "" {
		return a.Key
	}
	return a.Label
}

// holdsExact reports whether ctx holds perm by name on the entity ("" is
// held by everyone): the collection-level form of the per-record check
// mayRun repeats, so the bar offers no action every record would skip.
func holdsExact(ctx context.Context, m *meta, perm string) bool {
	return perm == "" || access.CanResourceExact(ctx, access.Permission(perm), access.Ref{Type: m.name})
}

// bulkSettable reports a field "set a field" may write: an enum or bool
// the forms may edit. Locked fields, the state field and every stamp
// (guarded), system and omitted fields never are.
func bulkSettable(m *meta, f schema.Field) bool {
	if m.locked(f) || m.omitted(f.Name) {
		return false
	}
	switch f.Type {
	case schema.Enum:
		return len(f.Values) > 0
	case schema.Bool:
		return true
	default:
		// Free-form values have no closed set a bulk bar could offer.
		return false
	}
}

func bulkValues(f schema.Field) []string {
	if f.Type == schema.Bool {
		return []string{"true", "false"}
	}
	return f.Values
}

func bulkValueLabel(ctx context.Context, m *meta, f schema.Field, v string) string {
	if f.Type == schema.Bool {
		if v == "true" {
			return i18nui.T(ctx, i18nui.KeyEntityYes)
		}
		return i18nui.T(ctx, i18nui.KeyEntityNo)
	}
	return m.valueLabel(ctx, f.Name, v)
}

func findBulkAction(actions []bulkAction, key string) (bulkAction, bool) {
	for _, a := range actions {
		if a.key == key {
			return a, true
		}
	}
	return bulkAction{}, false
}

// bulkOn reports whether m offers bulk actions at all: write routes, and
// not turned off by Display.NoBulk.
func bulkOn(m *meta) bool { return m.hasAPI && !m.d.NoBulk }

// stringList decodes a form field the runtime sends as one string when a
// single control carries it and as an array when several do.
type stringList []string

func (l *stringList) UnmarshalJSON(b []byte) error {
	var one string
	if err := json.Unmarshal(b, &one); err == nil {
		*l = stringList{one}
		return nil
	}
	var many []string
	if err := json.Unmarshal(b, &many); err != nil {
		return fmt.Errorf("want a string or a list of strings")
	}
	*l = many
	return nil
}

// bulkBody is the bar's form as the runtime posts it.
type bulkBody struct {
	CSRF   string     `json:"_csrf"`
	Action string     `json:"action"`
	Scope  string     `json:"scope"`
	IDs    stringList `json:"ids"`
	Page   stringList `json:"page"`
	Key    string     `json:"key"`
	Query  string     `json:"query"`
	Match  string     `json:"match"`
	// Undo and Back are the bar's ask for Undo on a delete's toast and
	// the list it returns to.
	Undo string `json:"undo"`
	Back string `json:"back"`
}

// bulkRefusal is a refusal the caller can act on, drawn from the catalog.
// Its message never quotes input.
type bulkRefusal struct {
	status int
	msg    string
}

func (e *bulkRefusal) Error() string { return e.msg }

func refuse(status int, msg string) error { return &bulkRefusal{status: status, msg: msg} }

// resolveSelection turns the posted scope into the ids the caller may
// read, in a stable order. See the package notes on Bulk actions.
func (u *UI) resolveSelection(ctx context.Context, m *meta, body bulkBody) ([]string, error) {
	var ids []string
	switch body.Scope {
	case bulkScopeSelected:
		ids = body.IDs
	case bulkScopePage:
		ids = body.Page
	case bulkScopeEvery:
		return u.everyMatch(ctx, m, body)
	case bulkScopeRecord:
		if ids = dedupeIDs(body.IDs); len(ids) != 1 {
			return nil, refuse(http.StatusUnprocessableEntity, i18nui.T(ctx, i18nui.KeyEntityBulkBadScope))
		}
		return u.visibleIDs(ctx, m, ids)
	case bulkScopeDeleted:
		ids = dedupeIDs(body.IDs)
		if len(ids) > InRequestCap {
			return nil, refuse(http.StatusUnprocessableEntity, i18nui.TVars(ctx, i18nui.KeyEntityBulkOverCap, map[string]string{"cap": strconv.Itoa(InRequestCap)}))
		}
		return u.readIDs(ctx, m, ids, true)
	default:
		return nil, refuse(http.StatusUnprocessableEntity, i18nui.T(ctx, i18nui.KeyEntityBulkBadScope))
	}
	ids = dedupeIDs(ids)
	if len(ids) == 0 {
		return nil, refuse(http.StatusUnprocessableEntity, i18nui.T(ctx, i18nui.KeyEntityBulkNone))
	}
	if len(ids) > EveryMatchCap {
		return nil, refuse(http.StatusUnprocessableEntity, i18nui.TVars(ctx, i18nui.KeyEntityBulkOverCap, map[string]string{"cap": strconv.Itoa(EveryMatchCap)}))
	}
	return u.visibleIDs(ctx, m, ids)
}

// visibleIDs re-reads ids through the scoped CRUD handler under ctx and
// returns the ones that come back, in the order given.
func (u *UI) visibleIDs(ctx context.Context, m *meta, ids []string) ([]string, error) {
	return u.readIDs(ctx, m, ids, false)
}

// readIDs is visibleIDs over the live rows, or over the trash when
// deleted is set.
func (u *UI) readIDs(ctx context.Context, m *meta, ids []string, deleted bool) ([]string, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	rows, err := m.ch.ListAll(crud.WithReadHooks(ctx), crud.ListOptions{
		Where:   &filter.Predicate{Field: m.pk, Op: filter.OpIn, Values: ids},
		Fields:  []string{m.pk},
		Limit:   len(ids),
		Deleted: deleted,
	})
	if err != nil {
		return nil, err
	}
	found := make(map[string]bool, len(rows))
	for _, row := range rows {
		found[cell(rowValue(row, m.pk))] = true
	}
	out := make([]string, 0, len(rows))
	for _, id := range ids {
		if found[id] {
			out = append(out, id)
		}
	}
	return out, nil
}

// everyMatch reads the ids of every row the list's query matches, the
// narrowing the screen applied, at most EveryMatchCap. The body carries
// the digest of the ids the screen offered (matchDigest); any other set
// is refused (409), so a list that changed underneath, even to the same
// size, or a query that is not the screen's, never runs over rows the
// caller did not confirm.
func (u *UI) everyMatch(ctx context.Context, m *meta, body bulkBody) ([]string, error) {
	q, err := url.ParseQuery(body.Query)
	if err != nil || body.Match == "" {
		return nil, refuse(http.StatusUnprocessableEntity, i18nui.T(ctx, i18nui.KeyEntityBulkBadScope))
	}
	ids, err := u.matchIDs(ctx, m, body.Key, q)
	if err != nil {
		return nil, err
	}
	if matchDigest(ids) != body.Match {
		return nil, refuse(http.StatusConflict, i18nui.T(ctx, i18nui.KeyEntityBulkStale))
	}
	return ids, nil
}

// matchIDs is matchRows reduced to the primary keys.
func (u *UI) matchIDs(ctx context.Context, m *meta, key string, q url.Values) ([]string, error) {
	rows, err := u.matchRows(ctx, m, key, q, []string{m.pk})
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, cell(rowValue(row, m.pk)))
	}
	return ids, nil
}

// matchDigest binds an every-match selection to its rows: the hex SHA-256
// of the sorted ids, each length-prefixed so no two id sets share one.
func matchDigest(ids []string) string {
	h := sha256.New()
	for _, id := range slices.Sorted(slices.Values(ids)) {
		fmt.Fprintf(h, "%d:%s,", len(id), id)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// matchRows reads every row the list keyed key matches under the query
// q (the view, search, filter and facets it would apply), with fields
// (every field when nil), at most EveryMatchCap. A filter the list would
// refuse is refused here rather than widening the selection.
func (u *UI) matchRows(ctx context.Context, m *meta, key string, q url.Values, fields []string) ([]map[string]any, error) {
	if key != "" && !entity.ValidKey(key) {
		return nil, refuse(http.StatusUnprocessableEntity, i18nui.T(ctx, i18nui.KeyEntityBulkBadScope))
	}
	s := &listState{m: m, key: key, p: listParamsFor(key), q: q}
	b := &ListBuilder{ui: u, entity: m.name}
	if err := b.narrow(ctx, s); err != nil {
		return nil, err
	}
	if s.filterBad {
		return nil, refuse(http.StatusUnprocessableEntity, i18nui.T(ctx, i18nui.KeyEntityBulkBadFilter))
	}
	where, err := s.predicate(b)
	if err != nil {
		return nil, err
	}
	rows, err := m.ch.ListAll(crud.WithReadHooks(ctx), crud.ListOptions{
		Where:   where,
		Filters: s.facetFilters(),
		Search:  s.search,
		Fields:  fields,
		Sorts:   []filter.ParsedSort{{Field: m.pk}},
		Limit:   EveryMatchCap + 1,
	})
	if err != nil {
		return nil, err
	}
	if len(rows) > EveryMatchCap {
		return nil, refuse(http.StatusUnprocessableEntity, i18nui.TVars(ctx, i18nui.KeyEntityBulkEveryOverCap, map[string]string{"cap": strconv.Itoa(EveryMatchCap)}))
	}
	return rows, nil
}

func dedupeIDs(ids []string) []string {
	seen := make(map[string]bool, len(ids))
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

// bulkTally counts a run's outcomes.
type bulkTally struct{ done, skipped, failed int }

func (t *bulkTally) add(outcomes map[string]string) {
	for _, o := range outcomes {
		switch o {
		case BulkRowDone:
			t.done++
		case BulkRowSkipped:
			t.skipped++
		default:
			t.failed++
		}
	}
}

// runBulk runs act over ids (already re-read under ctx) and returns each
// id's outcome. Every record is asked its own write gate first; a refusal
// is skipped, not failed.
func (u *UI) runBulk(ctx context.Context, m *meta, act bulkAction, ids []string, run string) map[string]string {
	out := make(map[string]string, len(ids))
	if act.kind == bulkRun {
		u.runAppAction(ctx, m, act, ids, run, out)
		return out
	}
	for _, id := range ids {
		out[id] = u.runOne(ctx, m, act, id)
	}
	return out
}

// mayRun reports whether ctx may run act on the record id: the record's
// update gate, plus the action's own Permission held by name.
func mayRun(ctx context.Context, m *meta, act bulkAction, id string) bool {
	if !m.ch.CanUpdateRecordScoped(ctx, id) {
		return false
	}
	return act.perm == "" || access.CanResourceExact(ctx, access.Permission(act.perm), access.Ref{Type: m.name, ID: id})
}

func (u *UI) runOne(ctx context.Context, m *meta, act bulkAction, id string) string {
	var err error
	switch act.kind {
	case bulkDelete:
		if !m.ch.CanDeleteRecordScoped(ctx, id) {
			return BulkRowSkipped
		}
		err = m.ch.DeleteOne(ctx, id)
	case bulkSet:
		if !mayRun(ctx, m, act, id) {
			return BulkRowSkipped
		}
		_, err = m.ch.UpdateOne(ctx, id, map[string]any{act.field: act.value})
	case bulkMove:
		if !mayRun(ctx, m, act, id) {
			return BulkRowSkipped
		}
		_, err = m.ch.RunTransition(ctx, id, act.move)
		if _, conflict := errors.AsType[*crud.TransitionConflictError](err); conflict {
			return BulkRowSkipped
		}
	case bulkRestore:
		// RestoreOne asks the caller's own update gate and scope.
		err = m.ch.RestoreOne(ctx, id)
		if errors.Is(err, crud.ErrForbidden) || crud.IsNotFound(err) || errors.Is(err, crud.ErrNotSoftDeleted) {
			return BulkRowSkipped
		}
	}
	if err != nil {
		slog.WarnContext(ctx, "entityui: bulk record failed", "entity", m.name, "action", act.key, "error", err)
		return BulkRowFailed
	}
	return BulkRowDone
}

// runAppAction hands an app action the ids its caller may run it on, in
// one call.
func (u *UI) runAppAction(ctx context.Context, m *meta, act bulkAction, ids []string, run string, out map[string]string) {
	allowed := make([]string, 0, len(ids))
	for _, id := range ids {
		if !mayRun(ctx, m, act, id) {
			out[id] = BulkRowSkipped
			continue
		}
		allowed = append(allowed, id)
	}
	if len(allowed) == 0 {
		return
	}
	outcome := BulkRowDone
	if !runAction(ctx, m.name, act.app, ActionContext{Entity: m.name, IDs: allowed, Crud: m.ch, Run: run}) {
		outcome = BulkRowFailed
	}
	for _, id := range allowed {
		out[id] = outcome
	}
}

// runAction runs an app action and reports whether it succeeded. A panic
// is recovered and logged by type only, as contain does for slots: its
// text may carry record contents.
func runAction(ctx context.Context, entityName string, a Action, actx ActionContext) (ok bool) {
	defer func() {
		if r := recover(); r != nil {
			//gofastr:allow(recoverlog) logs only the panic value's type: its text may carry record contents
			slog.ErrorContext(ctx, "entityui: action panicked", "entity", entityName, "action", a.Key, "panic", fmt.Sprintf("%T", r))
			ok = false
		}
	}()
	if err := a.Run(asCaller(ctx), actx); err != nil {
		slog.ErrorContext(ctx, "entityui: action failed", "entity", entityName, "action", a.Key, "error", err)
		return false
	}
	return true
}

// bulkHost returns the host's bulk backing, nil when it has none.
func (u *UI) bulkHost() BulkHost {
	bh, _ := u.host.(BulkHost)
	return bh
}

// auditBulk writes the run's one summary row: the action, the count, the
// tallies and the run's id (the snapshot id of a queued run); creator
// names a queued run's confirming user, whose context may be gone by the
// time it finishes. A host with no audit log writes nothing.
func (u *UI) auditBulk(ctx context.Context, m *meta, runID, action, creator string, count int, t bulkTally, status string) error {
	bh := u.bulkHost()
	if bh == nil {
		return nil
	}
	detail := map[string]any{
		"action": action, "count": count, "status": status,
		"done": t.done, "skipped": t.skipped, "failed": t.failed,
	}
	if creator != "" {
		detail["creator"] = creator
	}
	return bh.AuditEvent(ctx, m.name, "bulk", runID, detail)
}

func newRunID() string { return strings.ToLower(rand.Text()) }

// BulkHandler serves POST <api>/_bulk for the entity: the bulk bar's form
// RPC, and the record header's action buttons (scope "record"). App.EntityUI
// mounts it for every entity with write routes. It answers 404 for an
// entity with no write routes, or a list scope on one with
// Display.NoBulk; 403 when the caller may not read the entity or run the
// action; 422 naming what to fix; and on success a toast header with the
// counts (200) or the queued count (202). A record action answers 200
// when it ran, 403 when the record's gates skipped it and 500 when Run
// failed.
func (u *UI) BulkHandler(entityName string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		ctx := r.Context()
		m, err := u.meta(entityName)
		if err != nil || !m.hasAPI {
			writeBulkError(w, http.StatusNotFound, "not found")
			return
		}
		if m.tr != nil {
			ctx = i18nui.WithTranslator(ctx, m.tr)
		}
		// JSON only: a cross-site form can send text/plain with no
		// preflight, and the strict decoder would read it all the same.
		if !handler.IsJSONContentType(r.Header.Get("Content-Type")) {
			writeBulkError(w, http.StatusUnsupportedMediaType, "unsupported media type")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, bulkBodyLimit)
		var body bulkBody
		if err := handler.DecodeStrict(r.Body, &body); err != nil {
			if _, tooBig := errors.AsType[*http.MaxBytesError](err); tooBig {
				writeBulkError(w, http.StatusRequestEntityTooLarge, "request body too large")
				return
			}
			writeBulkError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		record := body.Scope == bulkScopeRecord
		if !record && !bulkOn(m) {
			writeBulkError(w, http.StatusNotFound, "not found")
			return
		}
		if !canRead(ctx, m.ch) {
			writeBulkError(w, http.StatusForbidden, "access denied")
			return
		}
		offered := u.bulkActions
		switch body.Scope {
		case bulkScopeRecord:
			offered = func(_ context.Context, m *meta) []bulkAction { return recordActions(m) }
		case bulkScopeDeleted:
			offered = restoreActions
		}
		act, ok := findBulkAction(offered(ctx, m), body.Action)
		if !ok {
			writeBulkError(w, http.StatusForbidden, i18nui.T(ctx, i18nui.KeyEntityBulkUnknown))
			return
		}
		ids, err := u.resolveSelection(ctx, m, body)
		if err == nil && len(ids) == 0 {
			err = refuse(http.StatusUnprocessableEntity, i18nui.T(ctx, i18nui.KeyEntityBulkNone))
		}
		if err != nil {
			if ref, ok := errors.AsType[*bulkRefusal](err); ok {
				writeBulkError(w, ref.status, ref.msg)
				return
			}
			slog.ErrorContext(ctx, "entityui: bulk selection", "entity", m.name, "error", err)
			writeBulkError(w, http.StatusInternalServerError, i18nui.T(ctx, i18nui.KeyEntityBulkFailed))
			return
		}
		if len(ids) > InRequestCap {
			u.queueBulk(ctx, w, m, act, body, ids)
			return
		}
		runID := newRunID()
		// Undo offers back the ids the caller could update before the
		// delete, asked while the rows are still live.
		var restorable map[string]bool
		if act.kind == bulkDelete && body.Undo == "1" && m.e.Config.Scope.SoftDelete {
			restorable = make(map[string]bool, len(ids))
			for _, id := range ids {
				if canUpdate(ctx, m, id) {
					restorable[id] = true
				}
			}
		}
		outcomes := u.runBulk(ctx, m, act, ids, runID)
		var t bulkTally
		t.add(outcomes)
		// The writes are committed, and each wrote its own audit row
		// through the CRUD hooks; a summary row that fails is logged,
		// not answered as a failed run.
		if err := u.auditBulk(ctx, m, runID, act.key, "", len(ids), t, BulkDone); err != nil {
			slog.ErrorContext(ctx, "entityui: bulk audit row", "entity", m.name, "run", runID, "error", err)
		}
		if record {
			answerRecordAction(ctx, w, runID, t)
			return
		}
		variant := ui.StatusSuccess
		if t.failed > 0 {
			variant = ui.StatusWarning
		}
		ui.AddToast(w, ui.ToastTrigger{Variant: variant, TTL: 6000, Title: bulkToast(ctx, m, act, t),
			Action: bulkUndo(ctx, m, ids, outcomes, restorable, body.Back)})
		writeBulkJSON(w, http.StatusOK, map[string]any{"run": runID, "done": t.done, "skipped": t.skipped, "failed": t.failed})
	})
}

// bulkToast is a run's toast title. A run where every record went
// through says what happened to how many ("2 payments deleted"); one
// that skipped or failed any, or ran an app action, counts each outcome.
func bulkToast(ctx context.Context, m *meta, act bulkAction, t bulkTally) string {
	key := i18nui.KeyEntityBulkUpdated
	switch act.kind {
	case bulkDelete:
		key = i18nui.KeyEntityBulkDeleted
	case bulkRestore:
		key = i18nui.KeyEntityBulkRestored
	case bulkRun:
		key = ""
	}
	if key == "" || t.skipped > 0 || t.failed > 0 {
		return i18nui.TVars(ctx, i18nui.KeyEntityBulkDone, map[string]string{
			"done": strconv.Itoa(t.done), "skipped": strconv.Itoa(t.skipped), "failed": strconv.Itoa(t.failed),
		})
	}
	return i18nui.TVars(ctx, key, map[string]string{"count": strconv.Itoa(t.done), "entity": m.noun(ctx, t.done != 1)})
}

// bulkUndo is a delete's Undo button: a restore of the ids the run
// deleted among restorable, returning to back. It is nil when nothing
// is restorable, back is not a path on this origin, or the button would
// pass undoActionBudget.
func bulkUndo(ctx context.Context, m *meta, ids []string, outcomes map[string]string, restorable map[string]bool, back string) *interactive.ToastAction {
	back, ok := scrubBackPath(back)
	if !ok || len(restorable) == 0 {
		return nil
	}
	var undo []string
	for _, id := range ids {
		if outcomes[id] == BulkRowDone && restorable[id] {
			undo = append(undo, id)
		}
	}
	if len(undo) == 0 {
		return nil
	}
	body, err := json.Marshal(map[string]any{"action": bulkRestoreKey, "scope": bulkScopeDeleted, "ids": undo})
	if err != nil {
		return nil
	}
	a := interactive.NewToastAction(i18nui.T(ctx, i18nui.KeyEntityUndo),
		interactive.Post(m.api+"/_bulk").WithBody(string(body)).
			OnSuccess(interactive.Navigate(back)).
			OnErrorToast(i18nui.T(ctx, i18nui.KeyEntityRestoreFailed)))
	if enc, err := json.Marshal(a); err != nil || len(enc) > undoActionBudget {
		return nil
	}
	return a
}

// answerRecordAction answers a record action's one outcome: ran (200),
// skipped by the record's gates (403) or failed (500). The button's own
// toasts name the action, so the answer carries no toast header.
func answerRecordAction(ctx context.Context, w http.ResponseWriter, runID string, t bulkTally) {
	switch {
	case t.done == 1:
		writeBulkJSON(w, http.StatusOK, map[string]any{"run": runID, "done": 1, "skipped": 0, "failed": 0})
	case t.skipped == 1:
		writeBulkError(w, http.StatusForbidden, i18nui.T(ctx, i18nui.KeyEntityBulkUnknown))
	default:
		writeBulkError(w, http.StatusInternalServerError, i18nui.T(ctx, i18nui.KeyEntityBulkFailed))
	}
}

// queueBulk writes a selection over InRequestCap to the snapshot store and
// hands it to the JobRunner, or refuses naming the cap. A second confirm
// of a run that is still queued answers the queued job rather than
// queuing it twice.
func (u *UI) queueBulk(ctx context.Context, w http.ResponseWriter, m *meta, act bulkAction, body bulkBody, ids []string) {
	capMsg := i18nui.TVars(ctx, i18nui.KeyEntityBulkOverCap, map[string]string{"cap": strconv.Itoa(InRequestCap)})
	bh := u.bulkHost()
	if u.ext.Jobs == nil || bh == nil || bh.BulkStore() == nil {
		writeBulkError(w, http.StatusUnprocessableEntity, capMsg)
		return
	}
	creator := userID(ctx)
	if creator == "" {
		writeBulkError(w, http.StatusUnprocessableEntity, i18nui.TVars(ctx, i18nui.KeyEntityBulkNeedsUser, map[string]string{"cap": strconv.Itoa(InRequestCap)}))
		return
	}
	store := bh.BulkStore()
	sum := sha256.Sum256([]byte(body.Scope + "\x00" + body.Key + "\x00" + body.Query))
	job := BulkJob{
		ID: newRunID(), Entity: m.name, Action: act.key, Count: len(ids),
		Creator: creator, Tenant: tenant.GetTenantID(ctx),
		FilterHash: hex.EncodeToString(sum[:]), Status: BulkQueued,
	}
	job.Key = runKey(job, ids)
	held, err := store.Create(ctx, job, ids)
	if err != nil {
		slog.ErrorContext(ctx, "entityui: bulk snapshot", "entity", m.name, "error", err)
		writeBulkError(w, http.StatusInternalServerError, i18nui.T(ctx, i18nui.KeyEntityBulkFailed))
		return
	}
	if held.ID == job.ID {
		if err := u.enqueueJob(ctx, job); err != nil {
			slog.ErrorContext(ctx, "entityui: bulk enqueue", "entity", m.name, "job", job.ID, "error", err)
			u.abandonJob(ctx, store, m, job)
			writeBulkError(w, http.StatusInternalServerError, i18nui.T(ctx, i18nui.KeyEntityBulkFailed))
			return
		}
		// A lost mark leaves the job for ResumeBulkJobs to hand over
		// again, which is safe: the run is leased.
		if err := store.Enqueued(ctx, job.ID); err != nil {
			slog.WarnContext(ctx, "entityui: bulk enqueued mark", "entity", m.name, "job", job.ID, "error", err)
		}
	}
	ui.AddToast(w, ui.ToastTrigger{Variant: ui.StatusInfo, TTL: 6000, Title: i18nui.TVars(ctx, i18nui.KeyEntityBulkQueued, map[string]string{
		"count": strconv.Itoa(held.Count), "entity": m.plural(ctx),
	})})
	writeBulkJSON(w, http.StatusAccepted, map[string]any{"job": held.ID, "count": held.Count})
}

// abandonJob stops a job the JobRunner refused, so nothing resumes it.
func (u *UI) abandonJob(ctx context.Context, store BulkStore, m *meta, job BulkJob) {
	runner := newRunID()
	now := u.now()
	ok, err := store.Claim(ctx, job.ID, runner, now, now.Add(bulkLease))
	if err == nil && ok {
		err = store.Finish(ctx, job.ID, runner, BulkStopped, now)
	}
	if err != nil {
		slog.ErrorContext(ctx, "entityui: bulk stop", "entity", m.name, "job", job.ID, "error", err)
	}
}

// runKey is BulkJob.Key: the creator, tenant, entity, action and ids,
// each length-prefixed.
func runKey(job BulkJob, ids []string) string {
	h := sha256.New()
	for _, part := range []string{job.Creator, job.Tenant, job.Entity, job.Action, matchDigest(ids)} {
		fmt.Fprintf(h, "%d:%s,", len(part), part)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// bulkLease is how long one RunBulkJob call holds a job between chunks.
// Each chunk renews it, so it only runs out when the runner has died or
// one chunk outlasts it; then another runner takes the job over.
const bulkLease = 5 * time.Minute

// RunBulkJob runs the queued bulk job id, the JobRunner worker's call. It
// takes the job's lease, then walks the snapshot's unsettled ids in
// chunks of InRequestCap, renewing the lease before each. Before each
// chunk it rebuilds the creator's context (JobRunner.Principal) and finds
// the action again under it: a creator who is gone, or who lost the role
// the action needs, stops the run. Each chunk is re-read under that
// context, so a row the creator can no longer see is skipped, and each
// record is asked its write gate before its write.
//
// A retried call skips the rows already settled, and a finished or
// stopped job answers nil. While another runner holds a live lease it
// answers ErrBulkJobBusy, and after losing the lease mid-run
// ErrBulkLeaseLost; the JobRunner retries both. A runner that dies after
// a chunk ran but before its outcomes were saved leaves those records to
// run again (ActionContext.Run names the run for an app action that must
// not repeat an effect). The run's one audit row is written before the
// job is finished, so a failed write leaves the job queued for the retry
// to write again rather than finished without one.
func (u *UI) RunBulkJob(ctx context.Context, id string) (err error) {
	bh := u.bulkHost()
	if u.ext.Jobs == nil || bh == nil || bh.BulkStore() == nil {
		return fmt.Errorf("entityui: RunBulkJob needs Extensions.Jobs and a host that keeps snapshots")
	}
	store := bh.BulkStore()
	job, err := store.Job(ctx, id)
	if err != nil {
		return err
	}
	if job.Status != BulkQueued {
		return nil
	}
	m, err := u.meta(job.Entity)
	if err != nil {
		return err
	}
	runner := newRunID()
	held := false
	// A call that fails while holding the lease hands it back (renewed to
	// end now), so the JobRunner's retry need not wait it out. A runner
	// that dies keeps it until it runs out.
	defer func() {
		if err != nil && held && !errors.Is(err, ErrBulkLeaseLost) {
			now := u.now()
			if _, rerr := store.Claim(context.WithoutCancel(ctx), id, runner, now, now); rerr != nil {
				slog.WarnContext(ctx, "entityui: bulk lease release", "entity", m.name, "job", id, "error", rerr)
			}
		}
	}()
	// finish writes the summary row under actor (the creator's rebuilt
	// context when there is one), then closes the job.
	finish := func(actor context.Context, status string) error {
		counts, err := store.Tally(ctx, id)
		if err != nil {
			return err
		}
		t := bulkTally{done: counts[BulkRowDone], skipped: counts[BulkRowSkipped], failed: counts[BulkRowFailed]}
		if err := u.auditBulk(actor, m, id, job.Action, job.Creator, job.Count, t, status); err != nil {
			return fmt.Errorf("entityui: bulk audit row: %w", err)
		}
		return store.Finish(ctx, id, runner, status, u.now())
	}
	for {
		now := u.now()
		held, err = store.Claim(ctx, id, runner, now, now.Add(bulkLease))
		if err != nil {
			return err
		}
		if !held {
			cur, err := store.Job(ctx, id)
			if err != nil {
				return err
			}
			if cur.Status != BulkQueued {
				return nil
			}
			return ErrBulkJobBusy
		}
		ids, err := store.Pending(ctx, id, InRequestCap)
		if err != nil {
			return err
		}
		cctx, perr := u.jobPrincipal(ctx, job)
		if len(ids) == 0 {
			if perr != nil {
				cctx = ctx
			}
			return finish(cctx, BulkDone)
		}
		if perr != nil {
			slog.WarnContext(ctx, "entityui: bulk job stopped: creator context", "entity", m.name, "job", id, "error", perr)
			return finish(ctx, BulkStopped)
		}
		if !bulkOn(m) || !canRead(cctx, m.ch) {
			return finish(cctx, BulkStopped)
		}
		act, ok := findBulkAction(u.bulkActions(cctx, m), job.Action)
		if !ok {
			slog.WarnContext(ctx, "entityui: bulk job stopped: action no longer offered", "entity", m.name, "job", id)
			return finish(cctx, BulkStopped)
		}
		visible, err := u.visibleIDs(cctx, m, ids)
		if err != nil {
			return err
		}
		outcomes := u.runBulk(cctx, m, act, visible, id)
		for _, rid := range ids {
			if _, ran := outcomes[rid]; !ran {
				outcomes[rid] = BulkRowSkipped
			}
		}
		if err := store.Settle(ctx, id, runner, outcomes); err != nil {
			return err
		}
	}
}

// ResumeBulkJobs hands the JobRunner every queued job whose Enqueue is not
// known to have happened (the process died between writing the snapshot
// and enqueuing it), created before grace ago, and reports how many.
// App.EntityUI runs it at start; a host that runs for long calls it on a
// schedule. Enqueuing a job twice is safe.
func (u *UI) ResumeBulkJobs(ctx context.Context, grace time.Duration) (int, error) {
	bh := u.bulkHost()
	if u.ext.Jobs == nil || bh == nil || bh.BulkStore() == nil {
		return 0, fmt.Errorf("entityui: ResumeBulkJobs needs Extensions.Jobs and a host that keeps snapshots")
	}
	if grace < 0 {
		return 0, fmt.Errorf("entityui: ResumeBulkJobs: grace %v is negative", grace)
	}
	store := bh.BulkStore()
	jobs, err := store.Unenqueued(ctx, u.now().Add(-grace))
	if err != nil {
		return 0, err
	}
	n := 0
	for _, job := range jobs {
		if err := u.enqueueJob(ctx, job); err != nil {
			return n, fmt.Errorf("entityui: resume bulk job %s: %w", job.ID, err)
		}
		if err := store.Enqueued(ctx, job.ID); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// PruneBulkJobs deletes the jobs that finished more than keep ago, and
// reports how many. A finished job's ids are already gone (Finish deletes
// them); this drops the job row with its tally. App.EntityUI runs it at
// start with BulkRetention; a host that runs for long calls it on a
// schedule.
func (u *UI) PruneBulkJobs(ctx context.Context, keep time.Duration) (int, error) {
	bh := u.bulkHost()
	if bh == nil || bh.BulkStore() == nil {
		return 0, fmt.Errorf("entityui: PruneBulkJobs needs a host that keeps snapshots")
	}
	if keep < 0 {
		return 0, fmt.Errorf("entityui: PruneBulkJobs: keep %v is negative", keep)
	}
	return bh.BulkStore().Prune(ctx, u.now().Add(-keep))
}

// BulkRetention is how long a finished bulk job's row (its creator,
// action, filter hash and tally) is kept before PruneBulkJobs deletes it.
// The audit log keeps the run's summary row on its own terms.
const BulkRetention = 30 * 24 * time.Hour

// userID is the caller's user id, "" when the request carries none that
// can be named again later.
func userID(ctx context.Context) string {
	u, ok := handler.GetUser(ctx)
	if !ok || u == nil {
		return ""
	}
	if id, ok := u.(interface{ GetID() string }); ok {
		return id.GetID()
	}
	return ""
}

func writeBulkJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Debug("entityui: bulk response write", "error", err)
	}
}

func writeBulkError(w http.ResponseWriter, status int, msg string) {
	writeBulkJSON(w, status, map[string]string{"error": msg})
}

// enqueueJob is Jobs.Enqueue with a panic turned into an error, so a
// runner that panics takes the refused path: the job is abandoned, and
// no resume runs it behind a confirm that answered failure.
func (u *UI) enqueueJob(ctx context.Context, job BulkJob) (err error) {
	defer func() {
		if recover() != nil {
			err = errors.New("entityui: the job runner's Enqueue panicked")
		}
	}()
	return u.ext.Jobs.Enqueue(ctx, job)
}

// jobPrincipal is Jobs.Principal with a panic turned into an error, so
// the run stops and hands its lease back instead of holding it until it
// runs out.
func (u *UI) jobPrincipal(ctx context.Context, job BulkJob) (c context.Context, err error) {
	defer func() {
		if recover() != nil {
			c, err = nil, errors.New("entityui: the job runner's Principal panicked")
		}
	}()
	return u.ext.Jobs.Principal(ctx, job)
}
