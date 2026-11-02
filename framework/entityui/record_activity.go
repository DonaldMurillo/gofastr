package entityui

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"net/http"
	"strings"
	"time"

	appui "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/hook"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

// probeText is the value a hook probe puts in an empty text field.
const probeText = "entityui-probe"

// probeValue is a non-empty value of f's type, in the Go type the CRUD
// handler's reads return for it.
func probeValue(f schema.Field) any {
	switch f.Type {
	case schema.Int:
		return int64(1)
	case schema.Float:
		return float64(1)
	case schema.Bool:
		return true
	default:
		return probeText
	}
}

// hookMaskedFields asks the entity's AfterGet chain, the one a hooked
// read runs, which visible fields it rewrites when they hold a value.
// It runs the chain once over a copy of base (the record's stored row,
// or nil for no record) whose empty fields hold a probe value, as the
// caller (elevation removed, as crud runs its hooks), and reports every
// field whose value the chain changed. Comparing the record's raw and
// hooked reads alone misses a hook that masks only a non-empty value
// while the record's own value is empty. A chain that fails or panics on
// the probe reports every field it was probed on; an entity with no
// AfterGet hook reports none. The probe never reads or writes a row.
func hookMaskedFields(ctx context.Context, m *meta, id string, base map[string]any) map[string]bool {
	if m.ch == nil || m.ch.Hooks == nil || len(m.ch.Hooks.HooksFor(hook.AfterGet)) == 0 {
		return nil
	}
	probe := maps.Clone(base)
	if probe == nil {
		probe = map[string]any{}
	}
	before := make(map[string]string, len(m.fields))
	var probed []string
	for _, f := range m.fields {
		if m.isKey(f.Name) {
			continue
		}
		if cell(m.rowValue(probe, f.Name)) == "" {
			probe[m.ch.WireKey(f.Name)] = probeValue(f)
			probed = append(probed, f.Name)
		}
		before[f.Name] = cell(m.rowValue(probe, f.Name))
	}
	hctx := crud.WithoutElevation(ctx)
	r := appui.RequestFromContext(ctx)
	if r != nil {
		r = r.WithContext(hctx)
	} else {
		r, _ = http.NewRequestWithContext(hctx, http.MethodGet, "/", nil)
	}
	payload := &hook.GetPayload{Request: r, ID: id, Result: probe}
	masked := map[string]bool{}
	if err := m.ch.Hooks.ExecuteHooks(hctx, hook.AfterGet, payload); err != nil {
		slog.WarnContext(ctx, "entityui: read hook probe failed; its fields stay out of the change list", "entity", m.name, "error", err)
		for _, name := range probed {
			masked[name] = true
		}
		return masked
	}
	for _, f := range m.fields {
		if was, ok := before[f.Name]; ok && cell(m.rowValue(payload.Result, f.Name)) != was {
			masked[f.Name] = true
		}
	}
	return masked
}

// The Activity tab: this record's audit rows, newest first, on a
// timeline. Each entry says who did what and how long ago, and an edit
// lists the fields it changed, labelled and drawn the way the list draws
// them. Hidden fields never reach this package (the row read drops
// them); masked fields and fields the caller cannot read are left out of
// the change list, so the tab never shows what the API would refuse the
// same caller. A masked field is a NoQuery one, or one an AfterGet hook
// rewrites on this record (rewrittenFields): the audit row stored the
// column as written, so its values would print what the hook hides.
// activityCap is how many trail entries the tab draws.
const activityCap = 50

func (b *RecordBuilder) activityTab(ctx context.Context, m *meta, masked map[string]bool) render.HTML {
	reader := b.ui.host.Audit()
	if reader == nil {
		return ""
	}
	entries, err := reader.Trail(ctx, m.name, b.id, activityCap)
	if err != nil {
		return slotFailed(ctx)
	}
	if len(entries) == 0 {
		return ui.EmptyState(ui.EmptyStateConfig{Title: i18nui.EntityNoActivity.Text(ctx)})
	}
	var rows []map[string]any
	for _, e := range entries {
		rows = append(rows, e.Before, e.After)
	}
	s := &listState{m: m, masked: masked}
	labels := b.ui.resolveRowLabels(ctx, s, rows, m.changeColumns(entries, masked))
	now := time.Now()
	events := make([]ui.TimelineEvent, len(entries))
	for i, e := range entries {
		events[i] = b.ui.activityEvent(ctx, s, labels, e, now)
	}
	return ui.Timeline(ui.TimelineConfig{Events: events})
}

// activityCount is the Activity tab's count: the trail entries the tab
// draws, "50+" past its cap, or "" when the trail cannot be read.
func (b *RecordBuilder) activityCount(ctx context.Context, m *meta) string {
	reader := b.ui.host.Audit()
	if reader == nil {
		return ""
	}
	entries, err := reader.Trail(ctx, m.name, b.id, activityCap+1)
	if err != nil {
		return ""
	}
	if len(entries) > activityCap {
		return i18n.Number(ctx, activityCap) + "+"
	}
	return i18n.Number(ctx, len(entries))
}

// activityEvent draws one audit entry: the headline, how long ago, and
// for an edit the fields it changed, then the reason when one was given.
func (u *UI) activityEvent(ctx context.Context, s *listState, labels labelResolver, e AuditEntry, now time.Time) ui.TimelineEvent {
	changes := u.changes(ctx, s, labels, e.Before, e.After)
	ev := ui.TimelineEvent{
		Lead:    u.activityLead(ctx, s.m, e, len(changes) > 0),
		Icon:    activityIcon(e.Operation),
		Variant: activityVariant(e.Operation),
	}
	if !e.At.IsZero() {
		ev.Meta = ui.Ago(ctx, now, e.At)
	}
	var body []render.HTML
	if len(changes) > 0 {
		body = append(body, ui.ChangeList(ui.ChangeListConfig{Changes: changes, Ctx: ctx}))
	}
	if e.Reason != "" {
		body = append(body, html.Paragraph(html.TextConfig{}, render.Text(i18nui.EntityActivityReason.Text(ctx, map[string]any{"reason": e.Reason}))))
	}
	ev.Body = render.Join(body...)
	return ev
}

// activityLead is an entry's headline: the actor in bold and what they
// did to this record. An update that changed no field the caller may see
// (a save that only moved updatedAt, or a masked column) reads as a save:
// "made changes" over an empty list would claim what it cannot show. Each part is escaped before it fills the
// translated line, and the line is filled in one pass, so a name or a
// label holding a placeholder stays text.
func (u *UI) activityLead(ctx context.Context, m *meta, e AuditEntry, changed bool) render.HTML {
	vars := map[string]render.HTML{
		"actor":  html.Strong(html.TextConfig{}, render.Text(u.actorLabel(ctx, e.Actor))),
		"entity": render.Text(m.noun(ctx, false)),
	}
	key := i18nui.EntityActivityOther
	switch op := e.Operation; {
	case op == "create":
		key = i18nui.EntityActivityCreated
	case op == "update" && changed:
		key = i18nui.EntityActivityUpdated
	case op == "update":
		key = i18nui.EntityActivitySaved
	case op == "delete":
		key = i18nui.EntityActivityDeleted
	case op == "restore":
		key = i18nui.EntityActivityRestored
	case op == "state_override":
		key = i18nui.EntityActivityOverride
	case strings.HasPrefix(op, "transition:"):
		key = i18nui.EntityActivityMoved
		vars["action"] = render.Text(m.transitionLabel(ctx, strings.TrimPrefix(op, "transition:")))
	default:
		vars["action"] = render.Text(op)
	}
	return i18nui.HTML(ctx, key, vars)
}

// actorLabel names an entry's actor: "System" for a row with no actor,
// the WithActorName answer when it gives one, else the stored id. A
// panicking name func degrades to the id, never a failed tab.
func (u *UI) actorLabel(ctx context.Context, id string) (label string) {
	if id == "" {
		return i18nui.EntityActivitySystem.Text(ctx)
	}
	if u.actorName == nil {
		return id
	}
	defer func() {
		if r := recover(); r != nil {
			//gofastr:allow(recoverlog) logs only the panic value's type: its text may carry account data
			slog.ErrorContext(ctx, "entityui: actor name panicked", "panic", fmt.Sprintf("%T", r))
			label = id
		}
	}()
	if n := u.actorName(ctx, id); n != "" {
		return n
	}
	return id
}

// transitionLabel is a move's label by its key, the key title-cased
// when the entity no longer declares it.
func (m *meta) transitionLabel(ctx context.Context, key string) string {
	display := ""
	if m.states != nil {
		if t, ok := m.states.Transition(key); ok {
			display = t.Label
		}
	}
	return i18nui.TransitionLabel(ctx, m.tr, m.name, key, display)
}

// activityIcon is the marker an operation draws on the timeline.
func activityIcon(op string) string {
	switch {
	case op == "create":
		return "plus"
	case op == "update":
		return "pencil"
	case op == "delete" || op == "purge":
		return "trash"
	case op == "restore":
		return "rotate-ccw"
	case op == "state_override" || strings.HasPrefix(op, "transition:"):
		return "repeat"
	}
	return "activity"
}

// activityVariant tints an operation's marker.
func activityVariant(op string) ui.TimelineEventVariant {
	switch {
	case op == "create" || op == "restore":
		return ui.TimelineSuccess
	case op == "delete" || op == "purge":
		return ui.TimelineDanger
	case op == "state_override":
		return ui.TimelineWarn
	case op == "update" || strings.HasPrefix(op, "transition:"):
		return ui.TimelineInfo
	}
	return ui.TimelineNeutral
}

// Changes draws what one audit entry changed on a record of entityName:
// the fields whose value differs between before and after, in the
// entity's field order, labelled and drawn the way the list draws them,
// related records by title. It is "" for an entity this UI does not
// draw, an entry missing either side (a create or a delete), and an edit
// that changed nothing the caller may see. A field the entity's AfterGet
// hooks rewrite when it holds a value is left out (hookMaskedFields with
// no record: the stored diff would print what every read masks).
// Relation titles are read through the related entity's own gate as the
// caller.
func (u *UI) Changes(ctx context.Context, entityName string, before, after map[string]any) render.HTML {
	m, err := u.meta(entityName)
	if err != nil {
		return ""
	}
	e := AuditEntry{Before: before, After: after}
	masked := hookMaskedFields(ctx, m, "", nil)
	s := &listState{m: m, masked: masked}
	labels := u.resolveRowLabels(ctx, s, []map[string]any{before, after}, m.changeColumns([]AuditEntry{e}, masked))
	changes := u.changes(ctx, s, labels, before, after)
	if len(changes) == 0 {
		return ""
	}
	return ui.ChangeList(ui.ChangeListConfig{Changes: changes, Ctx: ctx})
}

// changes lists one edit's changed fields. An entry missing either side
// lists nothing: a create's values are the record itself, and a delete
// leaves no record to compare.
func (u *UI) changes(ctx context.Context, s *listState, labels labelResolver, before, after map[string]any) []ui.Change {
	if before == nil || after == nil {
		return nil
	}
	var out []ui.Change
	for _, f := range s.m.changeFields(before, after, s.masked) {
		out = append(out, ui.Change{
			Label: s.m.label(ctx, f.Name),
			From:  u.changeValue(ctx, s, labels, f, before),
			To:    u.changeValue(ctx, s, labels, f, after),
		})
	}
	return out
}

// changeValue is one side of a change: the cell the list draws, or ""
// for an empty value, which the change list draws as the empty mark.
func (u *UI) changeValue(ctx context.Context, s *listState, labels labelResolver, f schema.Field, row map[string]any) render.HTML {
	if cell(s.m.rowValue(row, f.Name)) == "" {
		return ""
	}
	return u.cellHTML(ctx, s, labels, f, row, f.Name)
}

// changeFields are the fields one edit changed that the caller may see,
// in the entity's field order (never a ranged map: this writes markup).
// The audit row keys values by their wire spelling, camelCase under the
// JSON API's casing, so each side is read the way a list row is. The
// primary key and generated stamps (created and updated at) are left
// out: every edit moves updatedAt, and saying so on each entry is noise.
// masked names the fields a read hook rewrites on the record, left out
// too.
func (m *meta) changeFields(before, after map[string]any, masked map[string]bool) []schema.Field {
	var out []schema.Field
	for _, f := range m.fields {
		if m.isKey(f.Name) || f.AutoGenerate != schema.AutoNone || m.hiddenFromCaller(f) || masked[f.Name] {
			continue
		}
		if cell(m.rowValue(before, f.Name)) == cell(m.rowValue(after, f.Name)) {
			continue
		}
		out = append(out, f)
	}
	return out
}

// changeColumns are the relation fields any of entries changed: the
// columns whose related titles one label read resolves.
func (m *meta) changeColumns(entries []AuditEntry, masked map[string]bool) []string {
	seen := map[string]bool{}
	var out []string
	for _, e := range entries {
		if e.Before == nil || e.After == nil {
			continue
		}
		for _, f := range m.changeFields(e.Before, e.After, masked) {
			if f.Type == schema.Relation && !seen[f.Name] {
				seen[f.Name] = true
				out = append(out, f.Name)
			}
		}
	}
	return out
}

// hiddenFromCaller reports whether a field must never appear in a
// change list: masked (the audit log already stores the redacted value,
// but a stored shape can predate the mask) or on the query surface the
// caller may not read.
func (m *meta) hiddenFromCaller(f schema.Field) bool {
	if f.NoQuery {
		// A masked column's stored value is off the query surface; the
		// audit diff is a display surface, and it shows the same
		// nothing the API's masked read shows.
		return true
	}
	return false
}
