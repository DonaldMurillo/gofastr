package entityui

import (
	"context"
	"log/slog"
	"path"

	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

func (b *RecordBuilder) relatedTab(ctx context.Context, m *meta, base string) render.HTML {
	var lists []render.HTML
	for _, rl := range b.related {
		list, om, status := b.relatedListFor(ctx, m, rl, base)
		if status == relatedListRefused {
			continue
		}
		if status == relatedListFailed {
			lists = append(lists, slotFailed(ctx))
			continue
		}
		lists = append(lists, ui.Section(ui.SectionConfig{
			ID:      "eui-related-" + om.name,
			Compact: true,
			Ctx:     ctx,
		}, list.RenderCtx(ctx)))
	}
	return ui.Stack(ui.StackConfig{Gap: ui.GapXL}, lists...)
}

type relatedListStatus uint8

const (
	relatedListFailed relatedListStatus = iota
	relatedListRefused
	relatedListReady
)

func (b *RecordBuilder) hasRelatedTab(ctx context.Context, m *meta, base string) bool {
	for _, related := range b.related {
		if _, _, status := b.relatedListFor(ctx, m, related, base); status != relatedListRefused {
			return true
		}
	}
	return false
}

func (b *RecordBuilder) relatedListFor(ctx context.Context, m *meta, rl relatedList, base string) (*ListBuilder, *meta, relatedListStatus) {
	other, err := b.ui.entityFor(rl.name)
	if err != nil {
		return nil, nil, relatedListFailed
	}
	_, fks := relationTo(other, m.name)
	pins, ok := relatedPins(fks, b.id)
	if !ok || len(fks) == 0 {
		return nil, nil, relatedListFailed
	}
	otherBase := rl.base
	if !rl.fixed {
		otherBase = relatedBase(ctx, base, other.GetName())
	}
	om, err := b.ui.meta(other.GetName())
	if err != nil {
		return nil, nil, relatedListFailed
	}
	if !canReadRelated(ctx, om.ch) {
		return nil, nil, relatedListRefused
	}
	list := b.ui.List(other.GetName()).
		Key(other.GetName()).
		Heading(om.plural(ctx), 2).
		Embedded()
	for _, pin := range pins {
		list = list.Where(pin.Field, pin.Value)
	}
	if otherBase == "" {
		list = list.NoLinks().NoCreate()
	} else {
		list = list.Base(otherBase)
	}
	return list, om, relatedListReady
}

func (b *RecordBuilder) relatedCount(ctx context.Context, m *meta, base string) string {
	total, any := 0, false
	for _, rl := range b.related {
		list, om, status := b.relatedListFor(ctx, m, rl, base)
		if status != relatedListReady {
			continue
		}
		_, read, err := list.state(ctx, om)
		if err != nil {
			slog.WarnContext(ctx, "entityui: related count", "entity", om.name, "error", err)
			continue
		}
		n, err := om.countRows(ctx, read)
		if err != nil {
			slog.WarnContext(ctx, "entityui: related count", "entity", om.name, "error", err)
			continue
		}
		total, any = total+n, true
	}
	if !any {
		return ""
	}
	return i18n.Number(ctx, total)
}

func relatedBase(ctx context.Context, base, otherName string) string {
	if base == "" || base == "/" {
		return "/" + otherName
	}
	return path.Join(path.Dir(base), otherName)
}
