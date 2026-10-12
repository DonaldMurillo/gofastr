package entityui

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

// The Activity tab: this record's audit rows, newest first, on a
// timeline. Each entry says who did what and how long ago, and an edit
// lists the fields it changed, labelled and drawn the way the list draws
// them. Hidden fields never reach this package (the row read drops
// them); masked fields and fields the caller cannot read are left out of
// the change list, so the tab never shows what the API would refuse the
// same caller.
// activityCap is how many trail entries the tab draws.
const activityCap = 50

func (b *RecordBuilder) activityTab(ctx context.Context, m *meta) render.HTML {
	reader := b.ui.host.Audit()
	if reader == nil {
		return ""
	}
	entries, err := reader.Trail(ctx, m.name, b.id, activityCap)
	if err != nil {
		return slotFailed(ctx)
	}
	if len(entries) == 0 {
		return ui.EmptyState(ui.EmptyStateConfig{Title: i18nui.T(ctx, i18nui.KeyEntityNoActivity)})
	}
	var rows []map[string]any
	for _, e := range entries {
		rows = append(rows, e.Before, e.After)
	}
	s := &listState{m: m}
	labels := b.ui.resolveRowLabels(ctx, s, rows, m.changeColumns(entries))
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
		return formatNumber(activityCap, 0) + "+"
	}
	return formatNumber(float64(len(entries)), 0)
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
		body = append(body, html.Paragraph(html.TextConfig{}, render.Text(i18nui.TVars(ctx, i18nui.KeyEntityActivityReason, map[string]string{"reason": e.Reason}))))
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
	key := i18nui.KeyEntityActivityOther
	switch op := e.Operation; {
	case op == "create":
		key = i18nui.KeyEntityActivityCreated
	case op == "update" && changed:
		key = i18nui.KeyEntityActivityUpdated
	case op == "update":
		key = i18nui.KeyEntityActivitySaved
	case op == "delete":
		key = i18nui.KeyEntityActivityDeleted
	case op == "restore":
		key = i18nui.KeyEntityActivityRestored
	case op == "state_override":
		key = i18nui.KeyEntityActivityOverride
	case strings.HasPrefix(op, "transition:"):
		key = i18nui.KeyEntityActivityMoved
		vars["action"] = render.Text(m.transitionLabel(ctx, strings.TrimPrefix(op, "transition:")))
	default:
		vars["action"] = render.Text(op)
	}
	return i18nui.TVarsHTML(ctx, key, vars)
}

// actorLabel names an entry's actor: "System" for a row with no actor,
// the WithActorName answer when it gives one, else the stored id. A
// panicking name func degrades to the id, never a failed tab.
func (u *UI) actorLabel(ctx context.Context, id string) (label string) {
	if id == "" {
		return i18nui.T(ctx, i18nui.KeyEntityActivitySystem)
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
// that changed nothing the caller may see. Relation titles are read
// through the related entity's own gate as the caller.
func (u *UI) Changes(ctx context.Context, entityName string, before, after map[string]any) render.HTML {
	m, err := u.meta(entityName)
	if err != nil {
		return ""
	}
	e := AuditEntry{Before: before, After: after}
	s := &listState{m: m}
	labels := u.resolveRowLabels(ctx, s, []map[string]any{before, after}, m.changeColumns([]AuditEntry{e}))
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
	for _, f := range s.m.changeFields(before, after) {
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
	if cell(rowValue(row, f.Name)) == "" {
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
func (m *meta) changeFields(before, after map[string]any) []schema.Field {
	var out []schema.Field
	for _, f := range m.fields {
		if f.Name == m.pk || f.AutoGenerate != schema.AutoNone || m.hiddenFromCaller(f) {
			continue
		}
		if cell(rowValue(before, f.Name)) == cell(rowValue(after, f.Name)) {
			continue
		}
		out = append(out, f)
	}
	return out
}

// changeColumns are the relation fields any of entries changed: the
// columns whose related titles one label read resolves.
func (m *meta) changeColumns(entries []AuditEntry) []string {
	seen := map[string]bool{}
	var out []string
	for _, e := range entries {
		if e.Before == nil || e.After == nil {
			continue
		}
		for _, f := range m.changeFields(e.Before, e.After) {
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
