package entityui

import (
	"context"
	"log/slog"
	"path"

	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/filter"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

// The Related tab. Every entity the builder names (the admin passes
// every entity it exposes; an app page names its own) draws one list
// of that entity's rows whose relation field points at this record,
// built by calling the list builder's public API: List(other).
// Key(other).Where(fk, id).Base(otherBase). Each list passes that
// entity's own read gate, so a related entity the caller may not read
// draws its refusal, never its rows.
//
// The list's own New pre-fills the foreign key: a list carries each
// Where pin into the create screen's ?prefill_<field>= convention, so
// the link is <otherBase>/create?prefill_<fk>=<id>, and the screen reads
// every ?prefill_<field> whose field a create may set. A query-param
// prefill (not a POSTed value, not the bare field name) is the
// convention because the create URL is public surface: a bare
// ?<fk>=<id> would let a crafted link collide with any future param,
// and the prefill_ prefix names its intent.
func (b *RecordBuilder) relatedTab(ctx context.Context, m *meta, base string) render.HTML {
	var lists []render.HTML
	for _, rl := range b.related {
		other, err := b.ui.entityFor(rl.name)
		if err != nil {
			lists = append(lists, slotFailed(ctx))
			continue
		}
		_, fk := relationTo(other, m.name)
		if fk == "" {
			lists = append(lists, slotFailed(ctx))
			continue
		}
		otherBase := rl.base
		if !rl.fixed {
			otherBase = relatedBase(ctx, base, other.GetName())
		}
		om, err := b.ui.meta(other.GetName())
		if err != nil {
			lists = append(lists, slotFailed(ctx))
			continue
		}
		// The list's own header names the section, one level below the
		// record's title.
		list := b.ui.List(other.GetName()).
			Key(other.GetName()).
			Where(fk, b.id).
			Heading(om.plural(ctx), 2).
			Embedded()
		if otherBase == "" {
			// No screen of its own: rows without links and no New.
			list = list.NoLinks()
		} else {
			list = list.Base(otherBase)
		}
		lists = append(lists, ui.Section(ui.SectionConfig{
			ID:      "eui-related-" + other.GetName(),
			Compact: true,
		}, list.RenderCtx(ctx)))
	}
	// The stack owns the rhythm between the lists.
	return ui.Stack(ui.StackConfig{Gap: ui.GapXL}, lists...)
}

// relatedCount is the Related tab's count: the rows its lists draw,
// summed. Each entity counts through its own read gate and scope, the
// list's, so a row the caller could not see is never counted; an
// entity that refuses or fails adds nothing, and none readable is "".
func (b *RecordBuilder) relatedCount(ctx context.Context, m *meta) string {
	total, any := 0, false
	for _, rl := range b.related {
		other, err := b.ui.entityFor(rl.name)
		if err != nil {
			continue
		}
		_, fk := relationTo(other, m.name)
		if fk == "" {
			continue
		}
		om, err := b.ui.meta(other.GetName())
		if err != nil || !canRead(ctx, om.ch) {
			continue
		}
		n, err := om.ch.CountAll(crud.WithReadHooks(ctx), crud.ListOptions{
			Where: &filter.Predicate{Field: fk, Op: filter.OpEq, Value: b.id},
		})
		if err != nil {
			slog.WarnContext(ctx, "entityui: related count", "entity", om.name, "error", err)
			continue
		}
		total, any = total+n, true
	}
	if !any {
		return ""
	}
	return formatNumber(float64(total), 0)
}

// relatedBase derives the related entity's list path from this
// record's base: the last segment (this entity's name) is replaced by
// the related entity's name, the admin's /admin/entities/<entity>
// shape. A page whose bases do not follow that shape passes its own
// Related lists rather than relying on the derivation.
func relatedBase(ctx context.Context, base, otherName string) string {
	if base == "" || base == "/" {
		return "/" + otherName
	}
	return path.Join(path.Dir(base), otherName)
}
