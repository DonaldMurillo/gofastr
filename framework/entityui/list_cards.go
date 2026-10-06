package entityui

import (
	"context"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

// cards draws the rows as a grid of Cards, from Display.Card or, when
// the entity declares none, the title field and the first columns.
func (b *ListBuilder) cards(ctx context.Context, s *listState, rows []map[string]any, total int, known bool, page int) render.HTML {
	if len(rows) == 0 {
		return ui.EmptyState(b.emptyState(ctx, s))
	}
	card := cardFieldsOf(s)
	allCols := append([]string{card.title, card.subtitle, card.badge}, card.meta...)
	labels := b.ui.resolveRowLabels(ctx, s, rows, allCols)

	cards := make([]render.HTML, 0, len(rows))
	for i, row := range rows {
		id := cell(rowValue(row, s.m.pk))
		title := ""
		if f, ok := s.m.field(card.title); ok {
			title = b.ui.plainText(ctx, s, labels, f, row, card.title)
		}
		if title == "" {
			title = id
		}
		header := []render.HTML{ui.Link(ui.LinkConfig{Href: s.recordHref(id), Text: title})}
		if b.noLinks {
			header[0] = render.Text(title)
		}
		if card.badge != "" {
			if f, ok := s.m.field(card.badge); ok && f.Type == schema.Enum {
				if v := cell(rowValue(row, card.badge)); v != "" {
					header = append(header, ui.StatusBadge(ui.StatusBadgeConfig{
						Label:   s.m.valueLabel(ctx, card.badge, v),
						Variant: enumVariant(v),
					}))
				}
			}
		}
		var meta []string
		for _, name := range card.meta {
			f, ok := s.m.field(name)
			if !ok {
				continue
			}
			if v := b.ui.plainText(ctx, s, labels, f, row, name); v != "" {
				meta = append(meta, s.m.label(ctx, name)+": "+v)
			}
		}
		body := render.Text(strings.Join(meta, " · "))
		desc := ""
		if card.subtitle != "" {
			if f, ok := s.m.field(card.subtitle); ok {
				desc = b.ui.plainText(ctx, s, labels, f, row, card.subtitle)
			}
		}
		var footer render.HTML
		if !b.noLinks {
			footer = b.rowActions(ctx, s, row, i)
		}
		cards = append(cards, ui.Card(ui.CardConfig{
			Header:      ui.Cluster(ui.ClusterConfig{Gap: ui.GapSM, Align: ui.AlignCenter}, header...),
			Description: desc,
			Footer:      footer,
		}, html.Span(html.TextConfig{Class: "fui-card__text", ExtraAttrs: html.Attrs{"data-cui-internal": ""}}, body)))
	}
	out := ui.Grid(ui.GridConfig{Min: "20rem", Gap: ui.GapMD}, cards...)
	if known && pagesFor(total, s.limit) > 1 {
		out = render.Join(out, ui.Pagination(ui.PaginationConfig{
			Pages:     pagesFor(total, s.limit),
			Page:      page,
			Path:      s.path,
			Query:     s.pagerQuery(),
			PageParam: s.p.page,
			Ctx:       ctx,
		}))
	}
	return out
}

// cardFieldSet is the resolved card shape: Display.Card when the entity
// declares one, else the title field with the first columns as meta.
func cardFieldsOf(s *listState) (out cardFieldSet) {
	if s.m.d.Card != nil {
		out.title = s.m.d.Card.Title
		out.subtitle = s.m.d.Card.Subtitle
		out.badge = s.m.d.Card.Badge
		out.meta = s.m.d.Card.Meta
	}
	if out.title == "" {
		out.title = s.m.titleField()
	}
	if len(out.meta) == 0 {
		for _, c := range s.columns {
			if c == out.title || c == out.subtitle || c == out.badge {
				continue
			}
			out.meta = append(out.meta, c)
			if len(out.meta) == 3 {
				break
			}
		}
	}
	return out
}

type cardFieldSet struct {
	title, subtitle, badge string
	meta                   []string
}
