package entityui

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"net/http"
	"strings"
	"sync"
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
	"github.com/DonaldMurillo/gofastr/internal/fieldtext"
)

const probeText = "entityui-probe"

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
		if fieldtext.Cell(m.rowValue(probe, f.Name)) == "" {
			probe[m.ch.WireKey(f.Name)] = probeValue(f)
			probed = append(probed, f.Name)
		}
		before[f.Name] = fieldtext.Cell(m.rowValue(probe, f.Name))
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
		if was, ok := before[f.Name]; ok && fieldtext.Cell(m.rowValue(payload.Result, f.Name)) != was {
			masked[f.Name] = true
		}
	}
	return masked
}

type changeCacheKey struct{}

type changeRecord struct{ entity, id string }

type changeCache struct {
	mu     sync.Mutex
	masked map[changeRecord]map[string]bool
}

// WithChangeCache returns a context under which Changes asks an entity's
// read hooks about each record once. Install it once per page render and
// do not keep it past the render.
func WithChangeCache(ctx context.Context) context.Context {
	return context.WithValue(ctx, changeCacheKey{}, &changeCache{masked: map[changeRecord]map[string]bool{}})
}

func changeMask(ctx context.Context, m *meta, id string) map[string]bool {
	if m.ch == nil || m.ch.Hooks == nil || len(m.ch.Hooks.HooksFor(hook.AfterGet)) == 0 {
		return nil
	}
	c, _ := ctx.Value(changeCacheKey{}).(*changeCache)
	if c == nil {
		return recordHookMask(ctx, m, id)
	}
	k := changeRecord{entity: m.name, id: id}
	c.mu.Lock()
	masked, ok := c.masked[k]
	c.mu.Unlock()
	if ok {
		return masked
	}
	masked = recordHookMask(ctx, m, id)
	c.mu.Lock()
	c.masked[k] = masked
	c.mu.Unlock()
	return masked
}

func recordHookMask(ctx context.Context, m *meta, id string) map[string]bool {
	if id == "" {
		return hookMaskedFields(ctx, m, "", nil)
	}
	raw, err := m.getRowUnhooked(ctx, id)
	if err != nil || raw == nil {
		return hookMaskedFields(ctx, m, "", nil)
	}
	row, err := m.getRow(ctx, id)
	if err != nil || row == nil {
		all := make(map[string]bool, len(m.fields))
		for _, f := range m.fields {
			all[f.Name] = true
		}
		return all
	}
	masked := changedFields(m, raw, row, nil)
	maps.Copy(masked, hookMaskedFields(ctx, m, id, raw))
	return masked
}

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

func (m *meta) transitionLabel(ctx context.Context, key string) string {
	display := ""
	if m.states != nil {
		if t, ok := m.states.Transition(key); ok {
			display = t.Label
		}
	}
	return i18nui.TransitionLabel(ctx, m.tr, m.name, key, display)
}

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

// Changes draws the fields one audit entry changed on a record of
// entityName, in field order, drawn the way the list draws them. Fields
// the caller's read hooks rewrite are left out. It is "" for an unknown
// entity, an entry missing either side, and an edit that changed nothing
// the caller may see.
func (u *UI) Changes(ctx context.Context, entityName string, before, after map[string]any) render.HTML {
	m, err := u.meta(entityName)
	if err != nil || before == nil || after == nil {
		return ""
	}
	e := AuditEntry{Before: before, After: after}
	id := m.rowID(after)
	if id == "" {
		id = m.rowID(before)
	}
	masked := changeMask(ctx, m, id)
	s := &listState{m: m, masked: masked}
	labels := u.resolveRowLabels(ctx, s, []map[string]any{before, after}, m.changeColumns([]AuditEntry{e}, masked))
	changes := u.changes(ctx, s, labels, before, after)
	if len(changes) == 0 {
		return ""
	}
	return ui.ChangeList(ui.ChangeListConfig{Changes: changes, Ctx: ctx})
}

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

func (u *UI) changeValue(ctx context.Context, s *listState, labels labelResolver, f schema.Field, row map[string]any) render.HTML {
	if fieldtext.Cell(s.m.rowValue(row, f.Name)) == "" {
		return ""
	}
	return u.cellHTML(ctx, s, labels, f, row, f.Name)
}

func (m *meta) changeFields(before, after map[string]any, masked map[string]bool) []schema.Field {
	var out []schema.Field
	for _, f := range m.fields {
		if m.isKey(f.Name) || f.AutoGenerate != schema.AutoNone || m.hiddenFromCaller(f) || masked[f.Name] {
			continue
		}
		if fieldtext.Cell(m.rowValue(before, f.Name)) == fieldtext.Cell(m.rowValue(after, f.Name)) {
			continue
		}
		out = append(out, f)
	}
	return out
}

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

func (m *meta) hiddenFromCaller(f schema.Field) bool {
	if f.NoQuery {
		return true
	}
	return false
}
