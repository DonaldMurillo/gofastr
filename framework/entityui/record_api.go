package entityui

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

func (b *RecordBuilder) apiTabBody(ctx context.Context, m *meta, row map[string]any) render.HTML {
	pretty, err := json.MarshalIndent(row, "", "  ")
	if err != nil {
		return slotFailed(ctx)
	}
	return ui.Stack(ui.StackConfig{},
		ui.Section(ui.SectionConfig{Ctx: ctx, Heading: i18nui.EntityApiRestTitle.Text(ctx)},
			b.restSection(ctx, m)...),
		b.mcpSection(ctx, m),
		ui.Section(ui.SectionConfig{Ctx: ctx, Heading: i18nui.EntityApiJsonTitle.Text(ctx)},
			ui.CodeBlock(ui.CodeBlockConfig{
				Ctx:      ctx,
				Language: "json",
				Code:     string(pretty),
				ShowCopy: true,
				Scroll:   true,
			})),
	)
}

func (b *RecordBuilder) restSection(ctx context.Context, m *meta) []render.HTML {
	base, ok := b.ui.restBase(m.e)
	if !ok {
		return []render.HTML{ui.Callout(ui.CalloutConfig{
			Title:   i18nui.EntityApiRestTitle.Text(ctx),
			Variant: ui.StatusInfo,
		}, render.Text(i18nui.EntityApiRestOff.Text(ctx)))}
	}
	items := []ui.DetailItem{{
		Label: i18nui.EntityApiResource.Text(ctx),
		Value: render.Text(restResourceMethods + " " + base),
	}}
	items = append(items, ui.DetailItem{
		Label: i18nui.EntityApiRecord.Text(ctx),
		Value: render.Text(restRecordMethods + " " + base + crud.KeyPath(m.keys)),
	})
	return []render.HTML{
		ui.DetailList(ui.DetailListConfig{Items: items}),
		ui.LinkButton(ui.LinkButtonConfig{
			Label:   i18nui.EntityApiIndex.Text(ctx),
			Href:    "/api/llm.md",
			Variant: ui.ButtonSecondary,
		}),
	}
}

func (b *RecordBuilder) mcpSection(ctx context.Context, m *meta) render.HTML {
	if m.e.Config.Exposure == nil || !m.e.Config.Exposure.MCP {
		return ui.Section(ui.SectionConfig{Ctx: ctx, Heading: i18nui.EntityApiMcpTitle.Text(ctx)},
			ui.Callout(ui.CalloutConfig{
				Title:   i18nui.EntityApiMcpTitle.Text(ctx),
				Variant: ui.StatusInfo,
			}, render.Text(i18nui.EntityApiMcpOff.Text(ctx))))
	}
	names := crud.MCPToolNames(m.ch)
	if len(names) == 0 {
		return ""
	}
	return ui.Section(ui.SectionConfig{Ctx: ctx, Heading: i18nui.EntityApiMcpTitle.Text(ctx)},
		ui.CodeBlock(ui.CodeBlockConfig{Ctx: ctx, Code: strings.Join(names, "\n"), Language: "text", Scroll: true}))
}

func (u *UI) restBase(e *entity.Entity) (string, bool) {
	h := u.host
	for {
		switch v := h.(type) {
		case apiPathBulkHost:
			h = v.Host
		case apiPathHost:
			h = v.Host
		default:
			return h.APIPath(e)
		}
	}
}

const (
	restResourceMethods = "GET, POST"
	restRecordMethods   = "GET, PUT, PATCH, DELETE"
)
