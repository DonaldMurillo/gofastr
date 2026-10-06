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

// bulkBodyLimit caps a bulk request body: an action, a scope, a list query
// and at most a page of ids.
const bulkBodyLimit = 256 << 10

// Bulk scopes, the bar's "Apply to" values.
const (
	bulkScopeSelected = "selected"
	bulkScopePage     = "page"
	bulkScopeEvery    = "every"
)

type bulkKind int

const (
	bulkDelete bulkKind = iota + 1
	bulkSet
	bulkMove
	bulkRun
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
		label := a.Label
		if label == "" {
			label = a.Key
		}
		out = append(out, bulkAction{key: "run:" + a.Key, kind: bulkRun, label: label, perm: a.Permission, app: a})
	}
	return out
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
	rows, err := m.ch.ListAll(crud.WithReadHooks(ctx), crud.ListOptions{
		Where:  &filter.Predicate{Field: m.pk, Op: filter.OpIn, Values: ids},
		Fields: []string{m.pk},
		Limit:  len(ids),
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
func (u *UI) runBulk(ctx context.Context, m *meta, act bulkAction, ids []string) map[string]string {
	out := make(map[string]string, len(ids))
	if act.kind == bulkRun {
		u.runAppAction(ctx, m, act, ids, out)
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
	}
	if err != nil {
		slog.WarnContext(ctx, "entityui: bulk record failed", "entity", m.name, "action", act.key, "error", err)
		return BulkRowFailed
	}
	return BulkRowDone
}

// runAppAction hands an app action the ids its caller may run it on, in
// one call.
func (u *UI) runAppAction(ctx context.Context, m *meta, act bulkAction, ids []string, out map[string]string) {
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
	if !runAction(ctx, m.name, act.app, ActionContext{Entity: m.name, IDs: allowed, Crud: m.ch}) {
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
	if err := a.Run(ctx, actx); err != nil {
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

// auditBulk writes the run's summary row: the action, the count and the
// run's id (the snapshot id of a queued run). A host with no audit log
// writes nothing.
func (u *UI) auditBulk(ctx context.Context, m *meta, runID, action string, count int, t bulkTally, status string) {
	bh := u.bulkHost()
	if bh == nil {
		return
	}
	detail := map[string]any{
		"action": action, "count": count, "status": status,
		"done": t.done, "skipped": t.skipped, "failed": t.failed,
	}
	if err := bh.AuditEvent(ctx, m.name, "bulk", runID, detail); err != nil {
		slog.ErrorContext(ctx, "entityui: bulk audit row", "entity", m.name, "run", runID, "error", err)
	}
}

func newRunID() string { return strings.ToLower(rand.Text()) }

// BulkHandler serves POST <api>/_bulk for the entity: the bulk bar's form
// RPC. App.EntityUI mounts it for every entity with write routes. It
// answers 404 for an entity with no write routes or with Display.NoBulk,
// 403 when the caller may not read the entity, 422 naming what to fix,
// and on success a toast header with the counts (200) or the queued count
// (202).
func (u *UI) BulkHandler(entityName string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		ctx := r.Context()
		m, err := u.meta(entityName)
		if err != nil || !bulkOn(m) {
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
		if !canRead(ctx, m.ch) {
			writeBulkError(w, http.StatusForbidden, "access denied")
			return
		}
		act, ok := findBulkAction(u.bulkActions(ctx, m), body.Action)
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
		outcomes := u.runBulk(ctx, m, act, ids)
		var t bulkTally
		t.add(outcomes)
		u.auditBulk(ctx, m, runID, act.key, len(ids), t, BulkDone)
		variant := ui.StatusSuccess
		if t.failed > 0 {
			variant = ui.StatusWarning
		}
		ui.AddToast(w, ui.ToastTrigger{Variant: variant, TTL: 6000, Title: i18nui.TVars(ctx, i18nui.KeyEntityBulkDone, map[string]string{
			"done": strconv.Itoa(t.done), "skipped": strconv.Itoa(t.skipped), "failed": strconv.Itoa(t.failed),
		})})
		writeBulkJSON(w, http.StatusOK, map[string]any{"run": runID, "done": t.done, "skipped": t.skipped, "failed": t.failed})
	})
}

// queueBulk writes a selection over InRequestCap to the snapshot store and
// hands it to the JobRunner, or refuses naming the cap.
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
	sum := sha256.Sum256([]byte(body.Scope + "\x00" + body.Key + "\x00" + body.Query))
	job := BulkJob{
		ID: newRunID(), Entity: m.name, Action: act.key, Count: len(ids),
		Creator: creator, Tenant: tenant.GetTenantID(ctx),
		FilterHash: hex.EncodeToString(sum[:]), Status: BulkQueued,
	}
	if err := bh.BulkStore().Create(ctx, job, ids); err != nil {
		slog.ErrorContext(ctx, "entityui: bulk snapshot", "entity", m.name, "error", err)
		writeBulkError(w, http.StatusInternalServerError, i18nui.T(ctx, i18nui.KeyEntityBulkFailed))
		return
	}
	if err := u.ext.Jobs.Enqueue(ctx, job); err != nil {
		slog.ErrorContext(ctx, "entityui: bulk enqueue", "entity", m.name, "job", job.ID, "error", err)
		if ferr := bh.BulkStore().Finish(ctx, job.ID, BulkStopped); ferr != nil {
			slog.ErrorContext(ctx, "entityui: bulk finish", "entity", m.name, "job", job.ID, "error", ferr)
		}
		writeBulkError(w, http.StatusInternalServerError, i18nui.T(ctx, i18nui.KeyEntityBulkFailed))
		return
	}
	u.auditBulk(ctx, m, job.ID, act.key, len(ids), bulkTally{}, BulkQueued)
	ui.AddToast(w, ui.ToastTrigger{Variant: ui.StatusInfo, TTL: 6000, Title: i18nui.TVars(ctx, i18nui.KeyEntityBulkQueued, map[string]string{
		"count": strconv.Itoa(len(ids)), "entity": m.plural(ctx),
	})})
	writeBulkJSON(w, http.StatusAccepted, map[string]any{"job": job.ID, "count": len(ids)})
}

// RunBulkJob runs the queued bulk job id, the JobRunner worker's call. It
// walks the snapshot's unsettled ids in chunks of InRequestCap. Before
// each chunk it rebuilds the creator's context (JobRunner.Principal) and
// finds the action again under it: a creator who is gone, or who lost the
// role the action needs, stops the run. Each chunk is re-read under that
// context, so a row the creator can no longer see is skipped, and each
// record is asked its write gate before its write. A retried call skips
// the rows already settled. A finished or stopped job answers nil.
func (u *UI) RunBulkJob(ctx context.Context, id string) error {
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
	// finish settles the job's status and writes its summary row, counted
	// from the store so the chunks an earlier call ran are in it.
	finish := func(cctx context.Context, status string) error {
		counts, err := store.Tally(ctx, id)
		if err != nil {
			return err
		}
		if err := store.Finish(ctx, id, status); err != nil {
			return err
		}
		t := bulkTally{done: counts[BulkRowDone], skipped: counts[BulkRowSkipped], failed: counts[BulkRowFailed]}
		u.auditBulk(cctx, m, id, job.Action, job.Count, t, status)
		return nil
	}
	stop := func(cctx context.Context) error { return finish(cctx, BulkStopped) }
	for {
		ids, err := store.Pending(ctx, id, InRequestCap)
		if err != nil {
			return err
		}
		if len(ids) == 0 {
			return finish(ctx, BulkDone)
		}
		cctx, err := u.ext.Jobs.Principal(ctx, job)
		if err != nil {
			slog.WarnContext(ctx, "entityui: bulk job stopped: creator context", "entity", m.name, "job", id, "error", err)
			return stop(ctx)
		}
		if !bulkOn(m) || !canRead(cctx, m.ch) {
			return stop(cctx)
		}
		act, ok := findBulkAction(u.bulkActions(cctx, m), job.Action)
		if !ok {
			slog.WarnContext(ctx, "entityui: bulk job stopped: action no longer offered", "entity", m.name, "job", id)
			return stop(cctx)
		}
		visible, err := u.visibleIDs(cctx, m, ids)
		if err != nil {
			return err
		}
		outcomes := u.runBulk(cctx, m, act, visible)
		for _, rid := range ids {
			if _, ran := outcomes[rid]; !ran {
				outcomes[rid] = BulkRowSkipped
			}
		}
		if err := store.Settle(ctx, id, outcomes); err != nil {
			return err
		}
	}
}

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
