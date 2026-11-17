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
	"strings"
	"time"

	"github.com/DonaldMurillo/gofastr/core-ui/interactive"
	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/access"
	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
	"github.com/DonaldMurillo/gofastr/framework/tenant"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

const bulkBodyLimit = 256 << 10

const (
	bulkScopeSelected = "selected"
	bulkScopePage     = "page"
	bulkScopeEvery    = "every"
	bulkScopeRecord   = "record"
	bulkScopeDeleted  = "deleted"
)

const bulkRestoreKey = "restore"

const undoActionBudget = 2 << 10

type bulkKind int

const (
	bulkDelete bulkKind = iota + 1
	bulkSet
	bulkMove
	bulkRun
	bulkRestore
)

type bulkAction struct {
	key   string
	kind  bulkKind
	label string
	field string
	value any
	move  string
	perm  string
	app   Action
}

func (u *UI) bulkActions(ctx context.Context, m *meta) []bulkAction {
	var out []bulkAction
	if m.ch.CanDeleteRecordScoped(ctx, "") {
		out = append(out, bulkAction{key: "delete", kind: bulkDelete, label: i18nui.EntityBulkDelete.Text(ctx)})
	}
	if !m.ch.CanUpdateRecordScoped(ctx, "") {
		return out
	}
	for _, f := range m.fields {
		if !bulkSettable(m, f) {
			continue
		}
		for _, v := range bulkValues(f) {
			label := i18nui.EntityBulkSet.Text(ctx, map[string]any{
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
		name := i18nui.TransitionLabel(ctx, m.tr, m.name, t.Key, t.Label)
		out = append(out, bulkAction{
			key: "move:" + t.Key, kind: bulkMove, move: t.Key, perm: t.Permission,
			label: i18nui.EntityBulkMove.Text(ctx, map[string]any{"move": name}),
		})
	}
	for _, a := range m.ext.Actions {
		if !a.Bulk || !holdsExact(ctx, m, a.Permission) {
			continue
		}
		out = append(out, bulkAction{key: "run:" + a.Key, kind: bulkRun, label: actionLabel(ctx, a), perm: a.Permission, app: a})
	}
	return out
}

func recordActions(ctx context.Context, m *meta) []bulkAction {
	var out []bulkAction
	for _, a := range m.ext.Actions {
		out = append(out, bulkAction{key: "run:" + a.Key, kind: bulkRun, label: actionLabel(ctx, a), perm: a.Permission, app: a})
	}
	return out
}

func restoreActions(ctx context.Context, m *meta) []bulkAction {
	if !m.e.Config.Scope.SoftDelete {
		return nil
	}
	return []bulkAction{{key: bulkRestoreKey, kind: bulkRestore, label: i18nui.EntityRestore.Text(ctx)}}
}

func actionLabel(ctx context.Context, a Action) string {
	if a.Label.IsZero() {
		return a.Key
	}
	return a.Label.Text(ctx)
}

func holdsExact(ctx context.Context, m *meta, perm string) bool {
	return perm == "" || access.CanResourceExact(ctx, access.Permission(perm), access.Ref{Type: m.name})
}

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
			return i18nui.EntityYes.Text(ctx)
		}
		return i18nui.EntityNo.Text(ctx)
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

func bulkOn(m *meta) bool { return m.hasAPI && !m.d.NoBulk }

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

type bulkBody struct {
	CSRF   string     `json:"_csrf"`
	Action string     `json:"action"`
	Scope  string     `json:"scope"`
	IDs    stringList `json:"ids"`
	Page   stringList `json:"page"`
	Key    string     `json:"key"`
	Query  string     `json:"query"`
	Match  string     `json:"match"`
	// Undo and Back ask for Undo on a delete's toast and name the list it returns to.
	Undo string `json:"undo"`
	Back string `json:"back"`
}

type bulkRefusal struct {
	status int
	reason i18n.Phrase
	params map[string]any
}

func (e *bulkRefusal) Error() string { return e.reason.Format(e.params) }

func refuse(status int, reason i18n.Phrase, params ...map[string]any) error {
	ref := &bulkRefusal{status: status, reason: reason}
	if len(params) > 0 {
		ref.params = params[0]
	}
	return ref
}

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
			return nil, refuse(http.StatusUnprocessableEntity, reasonBulkBadScope)
		}
		return u.visibleIDs(ctx, m, ids)
	case bulkScopeDeleted:
		ids = dedupeIDs(body.IDs)
		if len(ids) > InRequestCap {
			return nil, refuse(http.StatusUnprocessableEntity, reasonBulkOverCap, map[string]any{"cap": InRequestCap})
		}
		return u.readIDs(ctx, m, ids, true)
	default:
		return nil, refuse(http.StatusUnprocessableEntity, reasonBulkBadScope)
	}
	ids = dedupeIDs(ids)
	if len(ids) == 0 {
		return nil, refuse(http.StatusUnprocessableEntity, reasonBulkNone)
	}
	if len(ids) > EveryMatchCap {
		return nil, refuse(http.StatusUnprocessableEntity, reasonBulkOverCap, map[string]any{"cap": EveryMatchCap})
	}
	return u.visibleIDs(ctx, m, ids)
}

func (u *UI) visibleIDs(ctx context.Context, m *meta, ids []string) ([]string, error) {
	return u.readIDs(ctx, m, ids, false)
}

func (u *UI) readIDs(ctx context.Context, m *meta, ids []string, deleted bool) ([]string, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	rows, err := m.listByIDs(ctx, ids, crud.ListOptions{
		Fields:  m.keys,
		Deleted: deleted,
	})
	if err != nil {
		return nil, err
	}
	found := make(map[string]bool, len(rows))
	for _, row := range rows {
		found[m.rowID(row)] = true
	}
	out := make([]string, 0, len(rows))
	for _, id := range ids {
		if found[id] {
			out = append(out, id)
		}
	}
	return out, nil
}

func (u *UI) everyMatch(ctx context.Context, m *meta, body bulkBody) ([]string, error) {
	q, err := url.ParseQuery(body.Query)
	if err != nil || body.Match == "" {
		return nil, refuse(http.StatusUnprocessableEntity, reasonBulkBadScope)
	}
	ids, err := u.matchIDs(ctx, m, body.Key, q)
	if err != nil {
		return nil, err
	}
	if matchDigest(ids) != body.Match {
		return nil, refuse(http.StatusConflict, reasonBulkStale)
	}
	return ids, nil
}

func (u *UI) matchIDs(ctx context.Context, m *meta, key string, q url.Values) ([]string, error) {
	rows, err := u.matchRows(ctx, m, key, q, m.keys)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, m.rowID(row))
	}
	return ids, nil
}

func matchDigest(ids []string) string {
	h := sha256.New()
	for _, id := range slices.Sorted(slices.Values(ids)) {
		fmt.Fprintf(h, "%d:%s,", len(id), id)
	}
	return hex.EncodeToString(h.Sum(nil))
}

func (u *UI) matchRows(ctx context.Context, m *meta, key string, q url.Values, fields []string) ([]map[string]any, error) {
	if key != "" && !entity.ValidKey(key) {
		return nil, refuse(http.StatusUnprocessableEntity, reasonBulkBadScope)
	}
	s := &listState{m: m, key: key, p: listParamsFor(key), q: q}
	b := &ListBuilder{ui: u, entity: m.name, key: key}
	if err := b.narrow(ctx, s); err != nil {
		return nil, err
	}
	if s.filterBad {
		return nil, refuse(http.StatusUnprocessableEntity, reasonBulkBadFilter)
	}
	where, err := s.predicate(b)
	if err != nil {
		return nil, err
	}
	rows, err := m.listRows(ctx, crud.ListOptions{
		Where:   where,
		Filters: s.facetFilters(),
		Search:  s.search,
		Fields:  fields,
		Sorts:   m.keySorts(),
		Limit:   EveryMatchCap + 1,
	})
	if err != nil {
		return nil, err
	}
	if len(rows) > EveryMatchCap {
		return nil, refuse(http.StatusUnprocessableEntity, reasonBulkEveryOverCap, map[string]any{"cap": EveryMatchCap})
	}
	return rows, nil
}

func dedupeIDs(ids []string) []string {
	seen := make(map[string]bool, len(ids))
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

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

func (u *UI) runBulk(ctx context.Context, m *meta, act bulkAction, ids []string, run string) map[string]string {
	ctx = crud.WithReadHooks(ctx)
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

func mayRun(ctx context.Context, m *meta, act bulkAction, id string) bool {
	if !m.ch.CanUpdateRecordScoped(ctx, id) {
		return false
	}
	return act.perm == "" || access.CanResourceExact(ctx, access.Permission(act.perm), access.Ref{Type: m.name, ID: id})
}

func (u *UI) runOne(ctx context.Context, m *meta, act bulkAction, id string) string {
	ctx = crud.WithReadHooks(ctx)
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

func (u *UI) bulkHost() BulkHost {
	bh, _ := u.host.(BulkHost)
	return bh
}

func (u *UI) auditBulk(ctx context.Context, m *meta, runID, action, creator string, count int, t bulkTally, status string) error {
	bh := u.bulkHost()
	if bh == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), bulkSettleTimeout)
	defer cancel()
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

// BulkHandler serves POST <api>/_bulk: the bulk bar's form RPC and the
// record header's actions (scope "record"). It answers 404 for an entity
// with no write routes or a list scope under Display.NoBulk; 415, 413 or
// 400 for a body that is not JSON, too large or malformed; the list
// route's refusal for a caller who may not read; 403 for an action not
// offered; 422 naming what to fix; else 200 with the counts or 202 queued.
func (u *UI) BulkHandler(entityName string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		ctx := r.Context()
		m, err := u.meta(entityName)
		if err != nil || !m.hasAPI {
			writeBulkError(w, r, http.StatusNotFound, i18n.StatusPhrase(http.StatusNotFound))
			return
		}
		if m.tr != nil {
			ctx = i18n.WithTranslator(ctx, m.tr)
		}
		if !handler.IsJSONContentType(r.Header.Get("Content-Type")) {
			writeBulkError(w, r, http.StatusUnsupportedMediaType, i18n.StatusPhrase(http.StatusUnsupportedMediaType))
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, bulkBodyLimit)
		var body bulkBody
		if err := handler.DecodeStrict(r.Body, &body); err != nil {
			if _, tooBig := errors.AsType[*http.MaxBytesError](err); tooBig {
				writeBulkError(w, r, http.StatusRequestEntityTooLarge, i18n.StatusPhrase(http.StatusRequestEntityTooLarge))
				return
			}
			writeBulkError(w, r, http.StatusBadRequest, i18n.ReasonInvalidJSONBody)
			return
		}
		record := body.Scope == bulkScopeRecord
		if !record && !bulkOn(m) {
			writeBulkError(w, r, http.StatusNotFound, i18n.StatusPhrase(http.StatusNotFound))
			return
		}
		if !requireRead(w, r, ctx, m) {
			return
		}
		offered := u.bulkActions
		switch body.Scope {
		case bulkScopeRecord:
			offered = recordActions
		case bulkScopeDeleted:
			offered = restoreActions
		}
		act, ok := findBulkAction(offered(ctx, m), body.Action)
		if !ok {
			writeBulkError(w, r, http.StatusForbidden, reasonBulkUnknown)
			return
		}
		ids, err := u.resolveSelection(ctx, m, body)
		if err == nil && len(ids) == 0 {
			err = refuse(http.StatusUnprocessableEntity, reasonBulkNone)
		}
		if err != nil {
			if ref, ok := errors.AsType[*bulkRefusal](err); ok {
				writeBulkError(w, r, ref.status, ref.reason, ref.params)
				return
			}
			slog.ErrorContext(ctx, "entityui: bulk selection", "entity", m.name, "error", err)
			writeBulkError(w, r, http.StatusInternalServerError, reasonBulkFailed)
			return
		}
		if len(ids) > InRequestCap {
			u.queueBulk(ctx, w, r, m, act, body, ids)
			return
		}
		runID := newRunID()
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
		if err := u.auditBulk(ctx, m, runID, act.key, "", len(ids), t, BulkDone); err != nil {
			slog.ErrorContext(ctx, "entityui: bulk audit row", "entity", m.name, "run", runID, "error", err)
		}
		if record {
			answerRecordAction(w, r, runID, t)
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

func bulkToast(ctx context.Context, m *meta, act bulkAction, t bulkTally) string {
	key := i18nui.EntityBulkUpdated
	switch act.kind {
	case bulkDelete:
		key = i18nui.EntityBulkDeleted
	case bulkRestore:
		key = i18nui.EntityBulkRestored
	case bulkRun:
		key = i18n.Phrase{}
	}
	if key.IsZero() || t.skipped > 0 || t.failed > 0 {
		return i18nui.EntityBulkDone.Text(ctx, map[string]any{
			"done": t.done, "skipped": t.skipped, "failed": t.failed,
		})
	}
	return key.Text(ctx, map[string]any{"count": t.done, "entity": m.noun(ctx, t.done != 1)})
}

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
	a := interactive.NewToastAction(i18nui.EntityUndo.Text(ctx),
		interactive.Post(m.api+"/_bulk").WithBody(string(body)).
			OnSuccess(interactive.Navigate(back)).
			OnErrorToast(i18nui.EntityRestoreFailed.Text(ctx)))
	if enc, err := json.Marshal(a); err != nil || len(enc) > undoActionBudget {
		return nil
	}
	return a
}

func answerRecordAction(w http.ResponseWriter, r *http.Request, runID string, t bulkTally) {
	switch {
	case t.done == 1:
		writeBulkJSON(w, http.StatusOK, map[string]any{"run": runID, "done": 1, "skipped": 0, "failed": 0})
	case t.skipped == 1:
		writeBulkError(w, r, http.StatusForbidden, reasonBulkUnknown)
	default:
		writeBulkError(w, r, http.StatusInternalServerError, reasonBulkFailed)
	}
}

func (u *UI) queueBulk(ctx context.Context, w http.ResponseWriter, r *http.Request, m *meta, act bulkAction, body bulkBody, ids []string) {
	bh := u.bulkHost()
	if u.ext.Jobs == nil || bh == nil || bh.BulkStore() == nil {
		writeBulkError(w, r, http.StatusUnprocessableEntity, reasonBulkOverCap, map[string]any{"cap": InRequestCap})
		return
	}
	creator := userID(ctx)
	if creator == "" {
		writeBulkError(w, r, http.StatusUnprocessableEntity, reasonBulkNeedsUser, map[string]any{"cap": InRequestCap})
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
		writeBulkError(w, r, http.StatusInternalServerError, reasonBulkFailed)
		return
	}
	if held.ID == job.ID {
		if err := u.enqueueJob(ctx, job); err != nil {
			slog.ErrorContext(ctx, "entityui: bulk enqueue", "entity", m.name, "job", job.ID, "error", err)
			u.abandonJob(ctx, store, m, job)
			writeBulkError(w, r, http.StatusInternalServerError, reasonBulkFailed)
			return
		}
		if err := store.Enqueued(ctx, job.ID); err != nil {
			slog.WarnContext(ctx, "entityui: bulk enqueued mark", "entity", m.name, "job", job.ID, "error", err)
		}
	}
	ui.AddToast(w, ui.ToastTrigger{Variant: ui.StatusInfo, TTL: 6000, Title: i18nui.EntityBulkQueued.Text(ctx, map[string]any{
		"count": held.Count, "entity": m.plural(ctx),
	})})
	writeBulkJSON(w, http.StatusAccepted, map[string]any{"job": held.ID, "count": held.Count})
}

func (u *UI) abandonJob(ctx context.Context, store BulkStore, m *meta, job BulkJob) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), bulkSettleTimeout)
	defer cancel()
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

func runKey(job BulkJob, ids []string) string {
	h := sha256.New()
	for _, part := range []string{job.Creator, job.Tenant, job.Entity, job.Action, matchDigest(ids)} {
		fmt.Fprintf(h, "%d:%s,", len(part), part)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// BulkJobLease is how long one RunBulkJob call holds a job between chunks;
// each chunk renews it.
const BulkJobLease = 5 * time.Minute

const bulkLease = BulkJobLease

// RunBulkJob runs the queued bulk job id under its lease, in chunks of
// InRequestCap, each re-read and re-gated under the creator's rebuilt
// context. A finished or stopped job answers nil; a held lease
// ErrBulkJobBusy, a lost one ErrBulkLeaseLost.
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
	defer func() {
		if err != nil && held && !errors.Is(err, ErrBulkLeaseLost) {
			now := u.now()
			rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), bulkSettleTimeout)
			defer cancel()
			if _, rerr := store.Claim(rctx, id, runner, now, now); rerr != nil {
				slog.WarnContext(ctx, "entityui: bulk lease release", "entity", m.name, "job", id, "error", rerr)
			}
		}
	}()
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
		settleCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), bulkSettleTimeout)
		err = store.Settle(settleCtx, id, runner, outcomes)
		cancel()
		if err != nil {
			return err
		}
	}
}

const bulkSettleTimeout = 10 * time.Second

// ResumeBulkJobs hands the JobRunner every queued job older than grace
// that was never marked enqueued or whose lease expired, and reports how many.
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
// reports how many.
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

// BulkRetention is how long a finished bulk job's row is kept.
const BulkRetention = 30 * 24 * time.Hour

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

func writeBulkError(w http.ResponseWriter, r *http.Request, status int, reason i18n.Phrase, params ...map[string]any) {
	writeBulkJSON(w, status, map[string]string{"error": reason.Text(r.Context(), params...), "reason": reason.Code()})
}

func (u *UI) enqueueJob(ctx context.Context, job BulkJob) (err error) {
	defer func() {
		if recover() != nil {
			err = errors.New("entityui: the job runner's Enqueue panicked")
		}
	}()
	return u.ext.Jobs.Enqueue(ctx, job)
}

func (u *UI) jobPrincipal(ctx context.Context, job BulkJob) (c context.Context, err error) {
	defer func() {
		if recover() != nil {
			c, err = nil, errors.New("entityui: the job runner's Principal panicked")
		}
	}()
	return u.ext.Jobs.Principal(ctx, job)
}
