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

// The API tab: what a developer or an agent needs to reach this record
// from code. Three sections, all composed from framework/ui:
//
//   - the record as the JSON API's GET returns it under the caller's own
//     context (the hooked row the screen already read: Hidden columns
//     never appear, an AfterGet mask shows the mask), pretty-printed
//     with deterministic key order;
//   - the entity's own REST path and the methods its exposure allows,
//     never the write base a back office swapped in with WithAPIPath;
//   - the MCP tool names crud registers while Exposure.MCP is on.
//
// The tab reads nothing the record screen could not: it re-uses the
// reads the screen already made and the entity's own declaration.

// apiTabBody draws the API tab's body for the row the record screen read.
func (b *RecordBuilder) apiTabBody(ctx context.Context, m *meta, row map[string]any) render.HTML {
	pretty, err := json.MarshalIndent(row, "", "  ")
	if err != nil {
		return slotFailed(ctx)
	}
	return ui.Stack(ui.StackConfig{},
		ui.Section(ui.SectionConfig{Heading: i18nui.T(ctx, i18nui.KeyEntityApiRestTitle)},
			b.restSection(ctx, m)...),
		b.mcpSection(ctx, m),
		ui.Section(ui.SectionConfig{Heading: i18nui.T(ctx, i18nui.KeyEntityApiJsonTitle)},
			ui.CodeBlock(ui.CodeBlockConfig{
				Language: "json",
				Code:     string(pretty),
				ShowCopy: true,
				Scroll:   true,
			})),
	)
}

// restSection draws the REST half: the entity's own base and the methods
// its exposure allows, plus the link to the API's entity index, or the
// notice that the entity mounts no REST routes.
func (b *RecordBuilder) restSection(ctx context.Context, m *meta) []render.HTML {
	base, ok := b.ui.restBase(m.e)
	if !ok {
		return []render.HTML{ui.Callout(ui.CalloutConfig{
			Title:   i18nui.T(ctx, i18nui.KeyEntityApiRestTitle),
			Variant: ui.StatusInfo,
		}, render.Text(i18nui.T(ctx, i18nui.KeyEntityApiRestOff)))}
	}
	items := []ui.DetailItem{{
		Label: i18nui.T(ctx, i18nui.KeyEntityApiResource),
		Value: render.Text(restResourceMethods(crudExposureOn(m.e)) + " " + base),
	}}
	items = append(items, ui.DetailItem{
		Label: i18nui.T(ctx, i18nui.KeyEntityApiRecord),
		Value: render.Text(restRecordMethods(crudExposureOn(m.e)) + " " + base + "/{id}"),
	})
	return []render.HTML{
		ui.DetailList(ui.DetailListConfig{Items: items}),
		ui.LinkButton(ui.LinkButtonConfig{
			Label:   i18nui.T(ctx, i18nui.KeyEntityApiIndex),
			Href:    "/api/llm.md",
			Variant: ui.ButtonSecondary,
		}),
	}
}

// mcpSection draws the MCP half: the tool names crud registers for the
// entity, or the notice that MCP does not expose it.
func (b *RecordBuilder) mcpSection(ctx context.Context, m *meta) render.HTML {
	if m.e.Config.Exposure == nil || !m.e.Config.Exposure.MCP {
		return ui.Section(ui.SectionConfig{Heading: i18nui.T(ctx, i18nui.KeyEntityApiMcpTitle)},
			ui.Callout(ui.CalloutConfig{
				Title:   i18nui.T(ctx, i18nui.KeyEntityApiMcpTitle),
				Variant: ui.StatusInfo,
			}, render.Text(i18nui.T(ctx, i18nui.KeyEntityApiMcpOff))))
	}
	names := crud.MCPToolNames(m.ch)
	if len(names) == 0 {
		return ""
	}
	return ui.Section(ui.SectionConfig{Heading: i18nui.T(ctx, i18nui.KeyEntityApiMcpTitle)},
		ui.CodeBlock(ui.CodeBlockConfig{Code: strings.Join(names, "\n"), Language: "text", Scroll: true}))
}

// restBase is the entity's own REST base: the innermost host's answer,
// the one UI.WithAPIPath wrapped away when a back office moved the
// writes to its own routes. The API tab documents the public API, never
// the write path the forms post to.
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

// crudExposureOn reports whether the entity's declaration generates
// write routes: Exposure nil or CRUD nil means on. A read-only mount
// (App.View) is a mount-time fact the declaration does not carry.
func crudExposureOn(e *entity.Entity) bool {
	return e.Config.Exposure == nil || e.Config.Exposure.CRUD == nil || *e.Config.Exposure.CRUD
}

// restResourceMethods names the methods the resource row serves; writes
// stay off when the entity's exposure turns CRUD off.
func restResourceMethods(writes bool) string {
	if writes {
		return "GET, POST"
	}
	return "GET"
}

// restRecordMethods names the methods one record serves.
func restRecordMethods(writes bool) string {
	if writes {
		return "GET, PUT, PATCH, DELETE"
	}
	return "GET"
}
