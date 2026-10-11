package entityui

import (
	"context"
	"maps"
	"slices"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

// The Activity tab: this record's audit rows, newest first, each drawn
// as a row (time, actor, operation, reason) with a diff of Before and
// After. Hidden fields never reach this package (the row read drops
// them); masked fields and fields the caller cannot read are REMOVED
// from Before and After before a diff is built, so the tab never shows
// what the API would refuse the same caller.
func (b *RecordBuilder) activityTab(ctx context.Context, m *meta) render.HTML {
	reader := b.ui.host.Audit()
	if reader == nil {
		return ""
	}
	entries, err := reader.Trail(ctx, m.name, b.id, 50)
	if err != nil {
		return slotFailed(ctx)
	}
	if len(entries) == 0 {
		return ui.EmptyState(ui.EmptyStateConfig{Title: i18nui.T(ctx, i18nui.KeyEntityNoActivity)})
	}
	rows := make([]render.HTML, 0, len(entries))
	for _, e := range entries {
		rows = append(rows, activityRow(ctx, m, e))
	}
	return render.Join(rows...)
}

// activityRow draws one audit entry: a section named by its operation,
// described by actor, time and reason, holding the diff.
func activityRow(ctx context.Context, m *meta, e AuditEntry) render.HTML {
	var desc []string
	if e.Actor != "" {
		desc = append(desc, e.Actor)
	}
	if !e.At.IsZero() {
		desc = append(desc, e.At.UTC().Format("2006-01-02 15:04 UTC"))
	}
	if e.Reason != "" {
		desc = append(desc, e.Reason)
	}
	cfg := ui.SectionConfig{Heading: e.Operation, Compact: true}
	if len(desc) > 0 {
		cfg.Description = strings.Join(desc, " · ")
	}
	var body []render.HTML
	if patch := diffPatch(m, e.Before, e.After); patch != "" {
		body = append(body, ui.DiffViewer(ui.DiffViewerConfig{
			Patch:      patch,
			LeftLabel:  i18nui.T(ctx, i18nui.KeyEntityBefore),
			RightLabel: i18nui.T(ctx, i18nui.KeyEntityAfter),
		}))
	}
	if len(body) == 0 {
		cfg.Description = strings.TrimSpace(cfg.Description)
		return ui.Section(cfg)
	}
	return ui.Section(cfg, body...)
}

// diffPatch renders one entry's Before/After as the unified-diff body
// a DiffViewer reads: one line per changed key, the union of the keys
// walked in sorted order (never ranged: this writes markup). Hidden
// and masked fields are dropped from both sides first; a field the
// stored row carries that the caller may not read has no readable
// value to diff.
func diffPatch(m *meta, before, after map[string]any) string {
	keys := map[string]bool{}
	for k := range before {
		keys[k] = true
	}
	for k := range after {
		keys[k] = true
	}
	var b strings.Builder
	for _, k := range slices.Sorted(maps.Keys(keys)) {
		f, ok := m.field(k)
		if !ok {
			// A key the entity's visible field set does not name is a
			// Hidden column or a column a schema change removed: either
			// way this screen has no business showing it.
			continue
		}
		if m.hiddenFromCaller(f) {
			continue
		}
		if cell(before[k]) == cell(after[k]) {
			continue
		}
		writeDiffLine(&b, "-", k, before[k])
		writeDiffLine(&b, "+", k, after[k])
	}
	return b.String()
}

func writeDiffLine(b *strings.Builder, prefix, key string, v any) {
	if prefix == "-" && v == nil {
		return // a key absent from Before is not a removed empty value
	}
	if prefix == "+" && v == nil {
		b.WriteString("+" + key + ":\n")
		return
	}
	b.WriteString(prefix + key + ": " + cell(v) + "\n")
}

// hiddenFromCaller reports whether a field must never appear in a
// diff: masked (the audit log already stores the redacted value, but a
// stored shape can predate the mask) or on the query surface the
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
